package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/clients"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/services/apikey"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ContextKey 上下文键
type ContextKey string

const (
	// ContextKeyAPIKey API Key 上下文键
	ContextKeyAPIKey ContextKey = "apiKey"
	// ContextKeyAPIKeyID API Key ID 上下文键
	ContextKeyAPIKeyID ContextKey = "apiKeyId"
	// ContextKeyClientType 客户端类型上下文键
	ContextKeyClientType ContextKey = "clientType"
	// ContextKeyRequestModel 请求模型上下文键
	ContextKeyRequestModel ContextKey = "requestModel"
	// ContextKeyRequestID 请求ID上下文键
	ContextKeyRequestID ContextKey = "requestId"
	// ContextKeyAuthDuration 认证耗时上下文键
	ContextKeyAuthDuration ContextKey = "authDuration"
)

// AuthMiddleware 认证中间件配置
type AuthMiddleware struct {
	apiKeyService *apikey.Service
	redis         *redis.Client
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(apiKeyService *apikey.Service, redisClient *redis.Client) *AuthMiddleware {
	return &AuthMiddleware{
		apiKeyService: apiKeyService,
		redis:         redisClient,
	}
}

// Authenticate 认证中间件
func (m *AuthMiddleware) Authenticate(requiredPermission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 生成请求 ID
		requestID := uuid.New().String()
		c.Set(string(ContextKeyRequestID), requestID)

		// 1. 提取 API Key
		rawKey := m.extractAPIKey(c)
		if rawKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":     "Missing API key",
				"code":      "missing_api_key",
				"requestId": requestID,
			})
			return
		}

		// 2. 解析客户端类型
		clientType := m.parseClientType(c.GetHeader("User-Agent"))
		c.Set(string(ContextKeyClientType), clientType)

		// 3. 检查全局 Claude Code Only 限制
		if config.Cfg != nil && config.Cfg.Security.ClaudeCodeOnly {
			if clientType != clients.TypeClaudeCode {
				logger.Warn("Request rejected: Claude Code Only mode enabled",
					zap.String("clientType", clientType),
					zap.String("userAgent", c.GetHeader("User-Agent")))
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":     "This API only accepts requests from Claude Code",
					"code":      "claude_code_only",
					"requestId": requestID,
				})
				return
			}
		}

		// 4. 解析请求模型
		model := m.parseRequestModel(c)
		c.Set(string(ContextKeyRequestModel), model)

		// 5. 验证 API Key
		result := m.apiKeyService.ValidateAPIKey(c.Request.Context(), rawKey, apikey.ValidationOptions{
			RequiredPermission: requiredPermission,
			ClientType:         clientType,
			Model:              model,
		})

		if !result.Valid {
			logger.Warn("API key validation failed",
				zap.String("error", result.Error),
				zap.String("code", result.ErrorCode),
				zap.String("clientType", clientType))

			c.AbortWithStatusJSON(result.StatusCode, gin.H{
				"error":     result.Error,
				"code":      result.ErrorCode,
				"requestId": requestID,
			})
			return
		}

		apiKey := result.APIKey

		// 6. 检查速率限制
		rateLimitResult, err := m.apiKeyService.CheckRateLimit(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Rate limit check failed", zap.Error(err))
			// 出错时允许通过，避免阻塞请求
		} else if !rateLimitResult.Allowed {
			c.Header("X-RateLimit-Limit", strconv.FormatInt(rateLimitResult.Limit, 10))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", rateLimitResult.ResetAt.Format(time.RFC3339))
			c.Header("Retry-After", strconv.Itoa(int(rateLimitResult.RetryAfter.Seconds())))

			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":      "Rate limit exceeded",
				"code":       "rate_limit_exceeded",
				"window":     rateLimitResult.Window,
				"retryAfter": int(rateLimitResult.RetryAfter.Seconds()),
				"requestId":  requestID,
			})
			return
		}

		// 7. 检查并发限制（领取并发槽位，请求结束释放）
		slotAcquired := false
		if apiKey.ConcurrentLimit > 0 {
			acquired, currentCount, err := m.apiKeyService.TryAcquireConcurrencySlot(c.Request.Context(), apiKey, requestID, 0)
			if err != nil {
				logger.Error("Concurrency acquire failed", zap.Error(err))
				// 出错时允许通过，避免阻塞请求
			} else if acquired {
				slotAcquired = true
			} else {
				// 并发已满：检查是否启用了并发排队
				if apiKey.ConcurrentRequestQueueEnabled {
					// 检查队列健康状态（P90 等待时间）
					isHealthy, p90WaitTime, healthErr := m.apiKeyService.CheckQueueHealth(c.Request.Context(), apiKey)
					if healthErr != nil {
						logger.Warn("Queue health check failed", zap.Error(healthErr))
					}

					if !isHealthy {
						// 队列过载，快速失败
						m.redis.IncrQueueStats(c.Request.Context(), apiKey.ID, "rejected_overload", 1)
						c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
							"error":       "Queue overloaded",
							"code":        "queue_overloaded",
							"p90WaitTime": p90WaitTime,
							"requestId":   requestID,
						})
						return
					}

					// 记录进入队列
					m.redis.IncrQueueStats(c.Request.Context(), apiKey.ID, "entered", 1)

					// 进入排队逻辑（成功后即持有并发槽位）
					queueResult := m.apiKeyService.WaitInQueue(c.Request.Context(), apiKey, requestID)
					if !queueResult.Success {
						c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
							"error":         "Concurrency limit exceeded and queue timeout",
							"code":          "queue_" + queueResult.TimeoutReason,
							"waitDuration":  queueResult.WaitDuration.Milliseconds(),
							"timeoutReason": queueResult.TimeoutReason,
							"requestId":     requestID,
						})
						return
					}

					slotAcquired = true
					logger.Debug("Request passed queue",
						zap.String("apiKeyId", apiKey.ID),
						zap.Duration("waitDuration", queueResult.WaitDuration))
				} else {
					currentConcurrency := currentCount
					if currentConcurrency > 0 {
						currentConcurrency--
					}
					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error":              "Concurrency limit exceeded",
						"code":               "concurrency_limit_exceeded",
						"currentConcurrency": currentConcurrency,
						"limit":              apiKey.ConcurrentLimit,
						"requestId":          requestID,
					})
					return
				}
			}
		}

		if slotAcquired {
			defer func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := m.apiKeyService.ReleaseConcurrencySlot(releaseCtx, apiKey.ID, requestID); err != nil {
					logger.Warn("Failed to release concurrency slot",
						zap.String("apiKeyId", apiKey.ID),
						zap.String("requestId", requestID),
						zap.Error(err))
				}
			}()
		}

		// 8. 检查每日成本限制（带加油包支持）
		costResult, err := m.apiKeyService.CheckDailyCostLimitWithFuel(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Daily cost check failed", zap.Error(err))
		}

		if costResult != nil && !costResult.Allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Daily cost limit exceeded",
				"code":        "daily_cost_limit_exceeded",
				"currentCost": costResult.CurrentCost,
				"limit":       costResult.DailyLimit,
				"requestId":   requestID,
			})
			return
		}

		// 9. 检查总成本限制
		totalCostResult, err := m.apiKeyService.CheckTotalCostLimit(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Total cost check failed", zap.Error(err))
		}

		if totalCostResult != nil && !totalCostResult.Allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Total cost limit exceeded",
				"code":        "total_cost_limit_exceeded",
				"currentCost": totalCostResult.CurrentCost,
				"limit":       totalCostResult.TotalLimit,
				"requestId":   requestID,
			})
			return
		}

		// 10. 检查 Opus 周成本限制
		weeklyOpusResult, err := m.apiKeyService.CheckWeeklyOpusCostLimit(c.Request.Context(), apiKey, model)
		if err != nil {
			logger.Error("Weekly Opus cost check failed", zap.Error(err))
		}

		if weeklyOpusResult != nil && !weeklyOpusResult.Allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Weekly Opus cost limit exceeded",
				"code":        "weekly_opus_cost_limit_exceeded",
				"currentCost": weeklyOpusResult.CurrentCost,
				"limit":       weeklyOpusResult.WeeklyLimit,
				"resetAt":     weeklyOpusResult.ResetAt.Format(time.RFC3339),
				"requestId":   requestID,
			})
			return
		}

		// 11. 检查速率限制窗口费用
		rateLimitCostResult, err := m.apiKeyService.CheckRateLimitCost(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Rate limit cost check failed", zap.Error(err))
		}

		if rateLimitCostResult != nil && !rateLimitCostResult.Allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":         "Rate limit cost exceeded",
				"code":          "rate_limit_cost_exceeded",
				"currentCost":   rateLimitCostResult.CurrentCost,
				"limit":         rateLimitCostResult.CostLimit,
				"windowMinutes": rateLimitCostResult.WindowMinutes,
				"resetAt":       rateLimitCostResult.ResetAt.Format(time.RFC3339),
				"requestId":     requestID,
			})
			return
		}

		// 12. 设置上下文
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyAPIKeyID), apiKey.ID)
		c.Set(string(ContextKeyAuthDuration), time.Since(startTime))

		// 13. 更新最后使用时间（异步）
		go m.updateLastUsedAt(context.Background(), apiKey.ID)

		// 14. 添加响应头
		if rateLimitResult != nil && rateLimitResult.Allowed {
			c.Header("X-RateLimit-Remaining", strconv.FormatInt(rateLimitResult.Remaining, 10))
		}

		c.Next()
	}
}

// AuthenticateOptional 可选认证中间件（不强制要求 API Key）
func (m *AuthMiddleware) AuthenticateOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.New().String()
		c.Set(string(ContextKeyRequestID), requestID)

		// 尝试提取 API Key
		rawKey := m.extractAPIKey(c)
		if rawKey == "" {
			c.Next()
			return
		}

		// 验证 API Key（如果提供）
		result := m.apiKeyService.ValidateAPIKey(c.Request.Context(), rawKey, apikey.ValidationOptions{})
		if result.Valid {
			c.Set(string(ContextKeyAPIKey), result.APIKey)
			c.Set(string(ContextKeyAPIKeyID), result.APIKey.ID)
		}

		c.Next()
	}
}

// extractAPIKey 从请求中提取 API Key
func (m *AuthMiddleware) extractAPIKey(c *gin.Context) string {
	// 1. 从 Authorization header 提取
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}

	// 2. 从 X-API-Key header 提取
	if key := c.GetHeader("X-API-Key"); key != "" {
		return key
	}

	// 3. 从 x-api-key header 提取（小写）
	if key := c.GetHeader("x-api-key"); key != "" {
		return key
	}

	// 4. 从 query parameter 提取
	// 警告：API Key 在 URL 中可能被记录到访问日志、浏览器历史等，存在安全风险
	if key := c.Query("api_key"); key != "" {
		logger.Warn("API key extracted from query parameter (security risk: may be logged in access logs)",
			zap.String("param", "api_key"),
			zap.String("clientIP", c.ClientIP()),
			zap.String("path", c.Request.URL.Path))
		return key
	}

	if key := c.Query("apiKey"); key != "" {
		logger.Warn("API key extracted from query parameter (security risk: may be logged in access logs)",
			zap.String("param", "apiKey"),
			zap.String("clientIP", c.ClientIP()),
			zap.String("path", c.Request.URL.Path))
		return key
	}

	return ""
}

// parseClientType 解析客户端类型
func (m *AuthMiddleware) parseClientType(userAgent string) string {
	return clients.ParseClientType(userAgent)
}

// parseRequestModel 解析请求中的模型
func (m *AuthMiddleware) parseRequestModel(c *gin.Context) string {
	// 1. 从 URL 路径参数获取（Gemini 格式: /models/:model:generateContent）
	if model := c.Param("model"); model != "" {
		// 去除可能的方法后缀
		if idx := strings.Index(model, ":"); idx > 0 {
			return model[:idx]
		}
		return model
	}

	// 2. 从请求体获取
	if c.Request.Method == "POST" && c.Request.Body != nil {
		// 读取 body
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return ""
		}

		// 恢复 body 供后续处理
		c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

		// 尝试解析 JSON
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			return ""
		}

		// 获取 model 字段
		if model, ok := req["model"].(string); ok {
			return model
		}
	}

	return ""
}

// updateLastUsedAt 更新最后使用时间
func (m *AuthMiddleware) updateLastUsedAt(ctx context.Context, keyID string) {
	if err := m.apiKeyService.UpdateLastUsedAt(ctx, keyID); err != nil {
		logger.Warn("Failed to update lastUsedAt", zap.Error(err))
	}
}

// GetAPIKeyFromContext 从上下文获取 API Key
func GetAPIKeyFromContext(c *gin.Context) *redis.APIKey {
	if apiKey, exists := c.Get(string(ContextKeyAPIKey)); exists {
		if key, ok := apiKey.(*redis.APIKey); ok {
			return key
		}
	}
	return nil
}

// GetAPIKeyIDFromContext 从上下文获取 API Key ID
func GetAPIKeyIDFromContext(c *gin.Context) string {
	if id, exists := c.Get(string(ContextKeyAPIKeyID)); exists {
		if keyID, ok := id.(string); ok {
			return keyID
		}
	}
	return ""
}

// GetClientTypeFromContext 从上下文获取客户端类型
func GetClientTypeFromContext(c *gin.Context) string {
	if clientType, exists := c.Get(string(ContextKeyClientType)); exists {
		if ct, ok := clientType.(string); ok {
			return ct
		}
	}
	return apikey.ClientUnknown
}

// GetRequestIDFromContext 从上下文获取请求 ID
func GetRequestIDFromContext(c *gin.Context) string {
	if requestID, exists := c.Get(string(ContextKeyRequestID)); exists {
		if rid, ok := requestID.(string); ok {
			return rid
		}
	}
	return ""
}

// GetRequestModelFromContext 从上下文获取请求模型
func GetRequestModelFromContext(c *gin.Context) string {
	if model, exists := c.Get(string(ContextKeyRequestModel)); exists {
		if m, ok := model.(string); ok {
			return m
		}
	}
	return ""
}

// RequirePermission 创建需要特定权限的中间件
func (m *AuthMiddleware) RequirePermission(permission string) gin.HandlerFunc {
	return m.Authenticate(permission)
}

// RequireClaude 创建需要 Claude 权限的中间件
func (m *AuthMiddleware) RequireClaude() gin.HandlerFunc {
	return m.Authenticate(apikey.PermissionClaude)
}

// RequireGemini 创建需要 Gemini 权限的中间件
func (m *AuthMiddleware) RequireGemini() gin.HandlerFunc {
	return m.Authenticate(apikey.PermissionGemini)
}

// RequireOpenAI 创建需要 OpenAI 权限的中间件
func (m *AuthMiddleware) RequireOpenAI() gin.HandlerFunc {
	return m.Authenticate(apikey.PermissionOpenAI)
}

// RequireDroid 创建需要 Droid 权限的中间件
func (m *AuthMiddleware) RequireDroid() gin.HandlerFunc {
	return m.Authenticate(apikey.PermissionDroid)
}

// ExtractAPIKeyHeader 提取 API Key 的辅助函数（用于外部调用）
func ExtractAPIKeyHeader(r *http.Request) string {
	// 从 Authorization header 提取
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}

	// 从 X-API-Key header 提取
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	// 从 x-api-key header 提取
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}

	// 从 query parameter 提取
	// 警告：API Key 在 URL 中可能被记录到访问日志、浏览器历史等，存在安全风险
	if key := r.URL.Query().Get("api_key"); key != "" {
		logger.Warn("API key extracted from query parameter (security risk: may be logged in access logs)",
			zap.String("param", "api_key"),
			zap.String("path", r.URL.Path))
		return key
	}

	return ""
}

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"requestId,omitempty"`
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(err, code, requestID string) *ErrorResponse {
	return &ErrorResponse{
		Error:     err,
		Code:      code,
		RequestID: requestID,
	}
}

// JSON 返回 JSON 格式
func (e *ErrorResponse) JSON() gin.H {
	return gin.H{
		"error":     e.Error,
		"code":      e.Code,
		"requestId": e.RequestID,
	}
}
