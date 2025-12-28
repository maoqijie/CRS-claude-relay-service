# 第三步：核心服务层实现

**状态**: ✅ 已完成

---

## 1. 目标

完成核心服务层的实现，包括：
- API Key 服务（验证、限流、权限、客户端限制）
- 认证中间件
- 统一调度器（Claude、Gemini、OpenAI）
- 账户服务（多账户类型管理）
- 定价服务

**预计工期**: 3-4 周
**验收标准**: Go 服务能完整处理 API Key 验证、账户调度，与 Node.js 行为一致

---

## 2. 实施概览

### 2.1 模块划分

```
internal/
├── services/
│   ├── apikey/
│   │   ├── service.go           # 🎯 API Key 服务核心
│   │   ├── validator.go         # API Key 验证逻辑
│   │   ├── ratelimit.go         # 速率限制
│   │   └── permissions.go       # 权限控制
│   ├── scheduler/
│   │   ├── unified_claude.go    # 🎯 Claude 统一调度器
│   │   ├── unified_gemini.go    # Gemini 统一调度器
│   │   ├── unified_openai.go    # OpenAI 统一调度器
│   │   ├── droid.go             # Droid 调度器
│   │   └── common.go            # 调度通用逻辑
│   ├── account/
│   │   ├── claude.go            # 🎯 Claude 账户服务
│   │   ├── claude_console.go    # Claude Console 账户
│   │   ├── gemini.go            # Gemini 账户服务
│   │   ├── openai.go            # OpenAI 账户服务
│   │   ├── bedrock.go           # AWS Bedrock 账户
│   │   ├── azure.go             # Azure OpenAI 账户
│   │   ├── droid.go             # Droid 账户服务
│   │   ├── ccr.go               # CCR 账户服务
│   │   └── group.go             # 账户组管理
│   ├── pricing/
│   │   ├── service.go           # 定价服务
│   │   └── models.go            # 模型价格定义
│   └── user/
│       └── service.go           # 用户服务
├── middleware/
│   ├── auth.go                  # 🎯 认证中间件
│   ├── client_validator.go      # 客户端验证
│   ├── rate_limit.go            # 速率限制中间件
│   └── env_guard.go             # ✅ 已完成 - 环境保护
└── validators/
    ├── client.go                # 客户端验证器
    └── claude_code.go           # Claude Code 验证
```

### 2.2 实施优先级

| 优先级 | 模块 | 说明 | Node.js 对应 |
|--------|------|------|--------------|
| P0 | apikey/service.go | API Key 核心服务 | apiKeyService.js |
| P0 | middleware/auth.go | 认证中间件 | auth.js |
| P0 | scheduler/unified_claude.go | Claude 调度器 | unifiedClaudeScheduler.js |
| P1 | account/claude.go | Claude 账户服务 | claudeAccountService.js |
| P1 | pricing/service.go | 定价服务 | pricingService.js |
| P1 | scheduler/unified_gemini.go | Gemini 调度器 | unifiedGeminiScheduler.js |
| P2 | 其他账户服务 | Gemini/OpenAI/Bedrock 等 | 各 accountService.js |
| P2 | validators/ | 客户端验证 | validators/ |

---

## 3. 详细实施

### 3.1 API Key 服务 (services/apikey/service.go)

#### 3.1.1 核心结构

```go
package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AccountTypeConfig 账户类型配置
var AccountTypeConfig = map[string]struct {
	Prefix string
}{
	"claude":           {Prefix: "claude:account:"},
	"claude-console":   {Prefix: "claude_console_account:"},
	"openai":           {Prefix: "openai:account:"},
	"openai-responses": {Prefix: "openai_responses_account:"},
	"azure-openai":     {Prefix: "azure_openai:account:"},
	"gemini":           {Prefix: "gemini_account:"},
	"gemini-api":       {Prefix: "gemini_api_account:"},
	"droid":            {Prefix: "droid:account:"},
}

// AccountTypePriority 账户类型优先级
var AccountTypePriority = []string{
	"openai",
	"openai-responses",
	"azure-openai",
	"claude",
	"claude-console",
	"gemini",
	"gemini-api",
	"droid",
}

// AccountCategoryMap 账户类型到类别的映射
var AccountCategoryMap = map[string]string{
	"claude":           "claude",
	"claude-console":   "claude",
	"openai":           "openai",
	"openai-responses": "openai",
	"azure-openai":     "openai",
	"gemini":           "gemini",
	"gemini-api":       "gemini",
	"droid":            "droid",
}

// Service API Key 服务
type Service struct {
	redis  *redis.Client
	prefix string
}

// NewService 创建 API Key 服务
func NewService(redisClient *redis.Client) *Service {
	return &Service{
		redis:  redisClient,
		prefix: config.Cfg.Security.APIKeyPrefix,
	}
}

// GenerateAPIKey 生成新的 API Key
func (s *Service) GenerateAPIKey(ctx context.Context, opts GenerateOptions) (*redis.APIKey, string, error) {
	// 生成原始 Key（带前缀）
	rawKey := s.prefix + generateRandomString(32)

	// 计算哈希
	hashedKey := s.hashAPIKey(rawKey)

	// 创建 API Key 对象
	now := time.Now()
	keyID := uuid.New().String()

	apiKey := &redis.APIKey{
		ID:                    keyID,
		Name:                  opts.Name,
		Description:           opts.Description,
		HashedKey:             hashedKey,
		Limit:                 opts.TokenLimit,
		IsActive:              opts.IsActive,
		CreatedAt:             now,
		Permissions:           opts.Permissions,
		AllowedClients:        opts.AllowedClients,
		ModelBlacklist:        opts.ModelBlacklist,
		ConcurrentLimit:       opts.ConcurrencyLimit,
		UserID:                opts.UserID,
		Tags:                  opts.Tags,

		// 并发排队配置
		ConcurrentRequestQueueEnabled:           opts.ConcurrentRequestQueueEnabled,
		ConcurrentRequestQueueMaxSize:           opts.ConcurrentRequestQueueMaxSize,
		ConcurrentRequestQueueMaxSizeMultiplier: opts.ConcurrentRequestQueueMaxSizeMultiplier,
		ConcurrentRequestQueueTimeoutMs:         opts.ConcurrentRequestQueueTimeoutMs,
	}

	// 处理过期时间
	if opts.ExpiresAt != nil {
		apiKey.ExpiresAt = opts.ExpiresAt
	} else if opts.ActivationDays > 0 {
		// 激活后有效天数
		expiresAt := now.AddDate(0, 0, opts.ActivationDays)
		apiKey.ExpiresAt = &expiresAt
	}

	// 保存到 Redis
	if err := s.redis.SetAPIKey(ctx, apiKey); err != nil {
		return nil, "", fmt.Errorf("failed to save API key: %w", err)
	}

	logger.Info("Generated new API Key",
		zap.String("id", keyID),
		zap.String("name", opts.Name))

	// 返回原始 Key（仅此一次展示）
	return apiKey, rawKey, nil
}

// GenerateOptions API Key 生成选项
type GenerateOptions struct {
	Name                                    string
	Description                             string
	TokenLimit                              int64
	ExpiresAt                               *time.Time
	IsActive                                bool
	Permissions                             []string
	AllowedClients                          []string
	ModelBlacklist                          []string
	ConcurrencyLimit                        int
	UserID                                  string
	Tags                                    []string
	ActivationDays                          int
	ConcurrentRequestQueueEnabled           bool
	ConcurrentRequestQueueMaxSize           int
	ConcurrentRequestQueueMaxSizeMultiplier float64
	ConcurrentRequestQueueTimeoutMs         int
}

// hashAPIKey 计算 API Key 的 SHA256 哈希
func (s *Service) hashAPIKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[uuid.New()[0]%byte(len(charset))]
	}
	return string(result)
}
```

#### 3.1.2 验证逻辑 (validator.go)

```go
package apikey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/storage/redis"
)

// ValidationResult 验证结果
type ValidationResult struct {
	Valid       bool
	APIKey      *redis.APIKey
	Error       string
	ErrorCode   string
	StatusCode  int
}

// ValidateAPIKey 验证 API Key
func (s *Service) ValidateAPIKey(ctx context.Context, rawKey string, opts ValidationOptions) *ValidationResult {
	// 1. 格式检查
	if !strings.HasPrefix(rawKey, s.prefix) {
		return &ValidationResult{
			Valid:      false,
			Error:      "Invalid API key format",
			ErrorCode:  "invalid_format",
			StatusCode: 401,
		}
	}

	// 2. 查找 API Key
	hashedKey := s.hashAPIKey(rawKey)
	apiKey, err := s.redis.GetAPIKeyByHash(ctx, hashedKey)
	if err != nil {
		return &ValidationResult{
			Valid:      false,
			Error:      "API key not found",
			ErrorCode:  "not_found",
			StatusCode: 401,
		}
	}

	// 3. 检查是否激活
	if !apiKey.IsActive {
		return &ValidationResult{
			Valid:      false,
			Error:      "API key is inactive",
			ErrorCode:  "inactive",
			StatusCode: 403,
		}
	}

	// 4. 检查是否过期
	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return &ValidationResult{
			Valid:      false,
			Error:      "API key has expired",
			ErrorCode:  "expired",
			StatusCode: 403,
		}
	}

	// 5. 检查是否被删除
	if apiKey.IsDeleted {
		return &ValidationResult{
			Valid:      false,
			Error:      "API key has been deleted",
			ErrorCode:  "deleted",
			StatusCode: 403,
		}
	}

	// 6. 检查权限
	if !s.checkPermission(apiKey, opts.RequiredPermission) {
		return &ValidationResult{
			Valid:      false,
			Error:      fmt.Sprintf("API key does not have '%s' permission", opts.RequiredPermission),
			ErrorCode:  "permission_denied",
			StatusCode: 403,
		}
	}

	// 7. 检查客户端限制
	if len(apiKey.AllowedClients) > 0 && opts.ClientType != "" {
		if !s.isClientAllowed(apiKey.AllowedClients, opts.ClientType) {
			return &ValidationResult{
				Valid:      false,
				Error:      fmt.Sprintf("Client '%s' is not allowed", opts.ClientType),
				ErrorCode:  "client_not_allowed",
				StatusCode: 403,
			}
		}
	}

	// 8. 检查模型黑名单
	if len(apiKey.ModelBlacklist) > 0 && opts.Model != "" {
		if s.isModelBlacklisted(apiKey.ModelBlacklist, opts.Model) {
			return &ValidationResult{
				Valid:      false,
				Error:      fmt.Sprintf("Model '%s' is blacklisted", opts.Model),
				ErrorCode:  "model_blacklisted",
				StatusCode: 403,
			}
		}
	}

	// 验证通过
	return &ValidationResult{
		Valid:  true,
		APIKey: apiKey,
	}
}

// ValidationOptions 验证选项
type ValidationOptions struct {
	RequiredPermission string // claude, gemini, openai, droid, all
	ClientType         string // 客户端类型（从 User-Agent 解析）
	Model              string // 请求的模型
}

// checkPermission 检查权限
func (s *Service) checkPermission(apiKey *redis.APIKey, required string) bool {
	if len(apiKey.Permissions) == 0 {
		return true // 未设置权限时默认允许
	}

	for _, perm := range apiKey.Permissions {
		if perm == "all" || perm == required {
			return true
		}
	}

	return false
}

// isClientAllowed 检查客户端是否允许
func (s *Service) isClientAllowed(allowedClients []string, clientType string) bool {
	for _, allowed := range allowedClients {
		if strings.EqualFold(allowed, clientType) {
			return true
		}
	}
	return false
}

// isModelBlacklisted 检查模型是否在黑名单中
func (s *Service) isModelBlacklisted(blacklist []string, model string) bool {
	modelLower := strings.ToLower(model)
	for _, blocked := range blacklist {
		if strings.Contains(modelLower, strings.ToLower(blocked)) {
			return true
		}
	}
	return false
}
```

#### 3.1.3 速率限制 (ratelimit.go)

```go
package apikey

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/storage/redis"
)

// RateLimitResult 速率限制检查结果
type RateLimitResult struct {
	Allowed     bool
	Remaining   int64
	ResetAt     time.Time
	RetryAfter  time.Duration
}

// CheckRateLimit 检查速率限制
func (s *Service) CheckRateLimit(ctx context.Context, apiKey *redis.APIKey) (*RateLimitResult, error) {
	// 如果未配置速率限制，直接通过
	if apiKey.RateLimitWindow == 0 || apiKey.RateLimitRequests == 0 {
		return &RateLimitResult{Allowed: true}, nil
	}

	window := time.Duration(apiKey.RateLimitWindow) * time.Second
	windowKey := fmt.Sprintf("rate_limit:%s:%d", apiKey.ID, time.Now().Unix()/int64(window.Seconds()))

	// 原子递增并获取计数
	count, err := s.redis.IncrWithExpiry(ctx, windowKey, window)
	if err != nil {
		return nil, err
	}

	remaining := int64(apiKey.RateLimitRequests) - count
	if remaining < 0 {
		remaining = 0
	}

	resetAt := time.Now().Add(window)

	if count > int64(apiKey.RateLimitRequests) {
		return &RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			ResetAt:    resetAt,
			RetryAfter: window,
		}, nil
	}

	return &RateLimitResult{
		Allowed:   true,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}

// CheckConcurrencyLimit 检查并发限制
func (s *Service) CheckConcurrencyLimit(ctx context.Context, apiKey *redis.APIKey) (bool, int64, error) {
	if apiKey.ConcurrentLimit == 0 {
		return true, 0, nil // 未配置并发限制
	}

	current, err := s.redis.GetConcurrency(ctx, apiKey.ID)
	if err != nil {
		return false, 0, err
	}

	if current >= int64(apiKey.ConcurrentLimit) {
		return false, current, nil
	}

	return true, current, nil
}

// CheckDailyCostLimit 检查每日成本限制
func (s *Service) CheckDailyCostLimit(ctx context.Context, apiKey *redis.APIKey) (bool, float64, error) {
	if apiKey.DailyCostLimit == 0 {
		return true, 0, nil
	}

	dailyCost, err := s.redis.GetDailyCost(ctx, apiKey.ID)
	if err != nil {
		return false, 0, err
	}

	if dailyCost >= apiKey.DailyCostLimit {
		return false, dailyCost, nil
	}

	return true, dailyCost, nil
}
```

---

### 3.2 认证中间件 (middleware/auth.go)

```go
package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/services/apikey"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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

		// 1. 提取 API Key
		rawKey := m.extractAPIKey(c)
		if rawKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Missing API key",
				"code":  "missing_api_key",
			})
			return
		}

		// 2. 解析客户端类型
		clientType := m.parseClientType(c.GetHeader("User-Agent"))

		// 3. 解析请求模型
		model := m.parseRequestModel(c)

		// 4. 验证 API Key
		result := m.apiKeyService.ValidateAPIKey(c.Request.Context(), rawKey, apikey.ValidationOptions{
			RequiredPermission: requiredPermission,
			ClientType:         clientType,
			Model:              model,
		})

		if !result.Valid {
			c.AbortWithStatusJSON(result.StatusCode, gin.H{
				"error": result.Error,
				"code":  result.ErrorCode,
			})
			return
		}

		apiKey := result.APIKey

		// 5. 检查速率限制
		rateLimitResult, err := m.apiKeyService.CheckRateLimit(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Rate limit check failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal server error",
			})
			return
		}

		if !rateLimitResult.Allowed {
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", rateLimitResult.ResetAt.Format(time.RFC3339))
			c.Header("Retry-After", string(int(rateLimitResult.RetryAfter.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"code":  "rate_limit_exceeded",
			})
			return
		}

		// 6. 检查并发限制（可能需要排队）
		concurrencyAllowed, currentConcurrency, err := m.apiKeyService.CheckConcurrencyLimit(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Concurrency check failed", zap.Error(err))
		}

		if !concurrencyAllowed {
			// 检查是否启用了并发排队
			if apiKey.ConcurrentRequestQueueEnabled {
				// 进入排队逻辑
				if !m.waitInQueue(c, apiKey) {
					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error": "Concurrency limit exceeded and queue timeout",
						"code":  "queue_timeout",
					})
					return
				}
			} else {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":              "Concurrency limit exceeded",
					"code":               "concurrency_limit_exceeded",
					"currentConcurrency": currentConcurrency,
					"limit":              apiKey.ConcurrentLimit,
				})
				return
			}
		}

		// 7. 检查每日成本限制
		costAllowed, dailyCost, err := m.apiKeyService.CheckDailyCostLimit(c.Request.Context(), apiKey)
		if err != nil {
			logger.Error("Daily cost check failed", zap.Error(err))
		}

		if !costAllowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":     "Daily cost limit exceeded",
				"code":      "daily_cost_limit_exceeded",
				"dailyCost": dailyCost,
				"limit":     apiKey.DailyCostLimit,
			})
			return
		}

		// 8. 设置上下文
		c.Set("apiKey", apiKey)
		c.Set("apiKeyId", apiKey.ID)
		c.Set("clientType", clientType)
		c.Set("requestModel", model)
		c.Set("authDuration", time.Since(startTime))

		// 9. 更新最后使用时间（异步）
		go m.updateLastUsedAt(context.Background(), apiKey.ID)

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

	// 3. 从 query parameter 提取
	if key := c.Query("api_key"); key != "" {
		return key
	}

	return ""
}

// parseClientType 解析客户端类型
func (m *AuthMiddleware) parseClientType(userAgent string) string {
	ua := strings.ToLower(userAgent)

	// Claude Code 客户端
	if strings.Contains(ua, "claude-code") || strings.Contains(ua, "claudecode") {
		return "ClaudeCode"
	}

	// Gemini CLI
	if strings.Contains(ua, "gemini-cli") {
		return "Gemini-CLI"
	}

	// Codex
	if strings.Contains(ua, "codex") {
		return "Codex"
	}

	// Cherry Studio
	if strings.Contains(ua, "cherry-studio") || strings.Contains(ua, "cherrystudio") {
		return "CherryStudio"
	}

	// 其他客户端
	return "Unknown"
}

// parseRequestModel 解析请求中的模型
func (m *AuthMiddleware) parseRequestModel(c *gin.Context) string {
	// 1. 从 URL 路径参数获取（Gemini 格式）
	if model := c.Param("model"); model != "" {
		return model
	}

	// 2. 从请求体获取（需要读取并恢复 body）
	// 注意：这里简化处理，实际需要考虑 body 恢复
	return ""
}

// waitInQueue 等待队列
func (m *AuthMiddleware) waitInQueue(c *gin.Context, apiKey *redis.APIKey) bool {
	ctx := c.Request.Context()
	timeout := time.Duration(apiKey.ConcurrentRequestQueueTimeoutMs) * time.Millisecond

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond
	maxPollInterval := 2 * time.Second
	backoffFactor := 1.5

	for time.Now().Before(deadline) {
		// 检查是否可以获取并发槽
		allowed, _, err := m.apiKeyService.CheckConcurrencyLimit(ctx, apiKey)
		if err != nil {
			logger.Warn("Queue check failed", zap.Error(err))
		}

		if allowed {
			return true
		}

		// 等待
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
			// 指数退避
			pollInterval = time.Duration(float64(pollInterval) * backoffFactor)
			if pollInterval > maxPollInterval {
				pollInterval = maxPollInterval
			}
		}
	}

	return false
}

// updateLastUsedAt 更新最后使用时间
func (m *AuthMiddleware) updateLastUsedAt(ctx context.Context, keyID string) {
	if err := m.redis.UpdateAPIKeyFields(ctx, keyID, map[string]interface{}{
		"lastUsedAt": time.Now(),
	}); err != nil {
		logger.Warn("Failed to update lastUsedAt", zap.Error(err))
	}
}
```

---

### 3.3 统一 Claude 调度器 (scheduler/unified_claude.go)

```go
package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// AccountType 账户类型
type AccountType string

const (
	AccountTypeClaude        AccountType = "claude-official"
	AccountTypeClaudeConsole AccountType = "claude-console"
	AccountTypeBedrock       AccountType = "bedrock"
	AccountTypeCCR           AccountType = "ccr"
)

// UnifiedClaudeScheduler Claude 统一调度器
type UnifiedClaudeScheduler struct {
	redis                *redis.Client
	sessionMappingPrefix string
}

// NewUnifiedClaudeScheduler 创建调度器
func NewUnifiedClaudeScheduler(redisClient *redis.Client) *UnifiedClaudeScheduler {
	return &UnifiedClaudeScheduler{
		redis:                redisClient,
		sessionMappingPrefix: "unified_claude_session_mapping:",
	}
}

// SelectAccountResult 账户选择结果
type SelectAccountResult struct {
	Account     interface{}
	AccountType AccountType
	AccountID   string
	FromSession bool
	Error       error
}

// SelectAccount 选择最优账户
func (s *UnifiedClaudeScheduler) SelectAccount(ctx context.Context, opts SelectOptions) *SelectAccountResult {
	// 1. 检查粘性会话
	if opts.SessionHash != "" {
		if result := s.getSessionAccount(ctx, opts.SessionHash, opts.Model); result != nil {
			return result
		}
	}

	// 2. 收集所有可用账户
	candidates := s.collectAvailableAccounts(ctx, opts)
	if len(candidates) == 0 {
		return &SelectAccountResult{
			Error: fmt.Errorf("no available accounts for model: %s", opts.Model),
		}
	}

	// 3. 按优先级和负载选择最优账户
	selected := s.selectBestAccount(ctx, candidates, opts)
	if selected == nil {
		return &SelectAccountResult{
			Error: fmt.Errorf("failed to select account"),
		}
	}

	// 4. 建立会话绑定
	if opts.SessionHash != "" {
		s.bindSessionAccount(ctx, opts.SessionHash, selected.AccountType, selected.AccountID)
	}

	return selected
}

// SelectOptions 账户选择选项
type SelectOptions struct {
	Model        string
	SessionHash  string
	APIKeyID     string
	Permissions  []string
	PreferredAccountTypes []AccountType
}

// AccountCandidate 候选账户
type AccountCandidate struct {
	Account     interface{}
	AccountType AccountType
	AccountID   string
	Priority    int
	Load        float64
}

// getSessionAccount 获取会话绑定的账户
func (s *UnifiedClaudeScheduler) getSessionAccount(ctx context.Context, sessionHash, model string) *SelectAccountResult {
	key := s.sessionMappingPrefix + sessionHash
	data, err := s.redis.HGetAll(ctx, key)
	if err != nil || len(data) == 0 {
		return nil
	}

	accountType := AccountType(data["accountType"])
	accountID := data["accountId"]

	// 验证账户是否仍然可用
	account, err := s.getAccount(ctx, accountType, accountID)
	if err != nil {
		logger.Warn("Session account not available, will select new one",
			zap.String("sessionHash", sessionHash),
			zap.String("accountId", accountID),
			zap.Error(err))
		return nil
	}

	// 验证账户是否支持请求的模型
	if !s.isModelSupported(account, accountType, model) {
		return nil
	}

	// 续期会话
	s.renewSession(ctx, sessionHash)

	logger.Info("Using session-bound account",
		zap.String("sessionHash", sessionHash[:8]+"..."),
		zap.String("accountType", string(accountType)),
		zap.String("accountId", accountID))

	return &SelectAccountResult{
		Account:     account,
		AccountType: accountType,
		AccountID:   accountID,
		FromSession: true,
	}
}

// collectAvailableAccounts 收集可用账户
func (s *UnifiedClaudeScheduler) collectAvailableAccounts(ctx context.Context, opts SelectOptions) []AccountCandidate {
	var candidates []AccountCandidate

	// 根据权限确定可用的账户类型
	accountTypes := s.getAvailableAccountTypes(opts.Permissions, opts.PreferredAccountTypes)

	for _, accountType := range accountTypes {
		accounts, err := s.getAccountsByType(ctx, accountType)
		if err != nil {
			logger.Warn("Failed to get accounts", zap.String("type", string(accountType)), zap.Error(err))
			continue
		}

		for _, account := range accounts {
			// 检查账户是否可调度
			if !s.isAccountSchedulable(account) {
				continue
			}

			// 检查账户是否支持模型
			if !s.isModelSupported(account, accountType, opts.Model) {
				continue
			}

			// 检查账户是否过载
			if s.isAccountOverloaded(ctx, accountType, s.getAccountID(account)) {
				continue
			}

			candidates = append(candidates, AccountCandidate{
				Account:     account,
				AccountType: accountType,
				AccountID:   s.getAccountID(account),
				Priority:    s.getAccountPriority(accountType),
				Load:        s.getAccountLoad(ctx, accountType, s.getAccountID(account)),
			})
		}
	}

	return candidates
}

// selectBestAccount 选择最优账户
func (s *UnifiedClaudeScheduler) selectBestAccount(ctx context.Context, candidates []AccountCandidate, opts SelectOptions) *SelectAccountResult {
	if len(candidates) == 0 {
		return nil
	}

	// 按优先级和负载排序
	// 优先级高 + 负载低的账户优先
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Priority > best.Priority || (c.Priority == best.Priority && c.Load < best.Load) {
			best = c
		}
	}

	return &SelectAccountResult{
		Account:     best.Account,
		AccountType: best.AccountType,
		AccountID:   best.AccountID,
		FromSession: false,
	}
}

// bindSessionAccount 绑定会话账户
func (s *UnifiedClaudeScheduler) bindSessionAccount(ctx context.Context, sessionHash string, accountType AccountType, accountID string) {
	key := s.sessionMappingPrefix + sessionHash
	s.redis.HSet(ctx, key, map[string]interface{}{
		"accountType": string(accountType),
		"accountId":   accountID,
		"createdAt":   time.Now().Unix(),
	})

	// 设置 TTL（默认 1 小时）
	s.redis.Expire(ctx, key, time.Hour)

	logger.Info("Bound session to account",
		zap.String("sessionHash", sessionHash[:8]+"..."),
		zap.String("accountType", string(accountType)),
		zap.String("accountId", accountID))
}

// renewSession 续期会话
func (s *UnifiedClaudeScheduler) renewSession(ctx context.Context, sessionHash string) {
	key := s.sessionMappingPrefix + sessionHash
	s.redis.Expire(ctx, key, time.Hour)
}

// isModelSupported 检查账户是否支持模型
func (s *UnifiedClaudeScheduler) isModelSupported(account interface{}, accountType AccountType, model string) bool {
	if model == "" {
		return true
	}

	modelLower := strings.ToLower(model)

	// Claude 官方账户类型的模型检查
	if accountType == AccountTypeClaude {
		// 只支持 Claude 模型
		if !strings.Contains(modelLower, "claude") &&
			!strings.Contains(modelLower, "sonnet") &&
			!strings.Contains(modelLower, "opus") &&
			!strings.Contains(modelLower, "haiku") {
			return false
		}

		// Opus 模型需要检查订阅等级
		if strings.Contains(modelLower, "opus") {
			return s.checkOpusModelAccess(account, model)
		}
	}

	return true
}

// checkOpusModelAccess 检查 Opus 模型访问权限
func (s *UnifiedClaudeScheduler) checkOpusModelAccess(account interface{}, model string) bool {
	// TODO: 实现订阅等级检查
	// - Free: 不支持任何 Opus 模型
	// - Pro: 只支持 Opus 4.5+
	// - Max: 支持所有 Opus 版本
	return true
}

// getAvailableAccountTypes 根据权限获取可用账户类型
func (s *UnifiedClaudeScheduler) getAvailableAccountTypes(permissions []string, preferred []AccountType) []AccountType {
	// 默认所有 Claude 相关类型
	allTypes := []AccountType{
		AccountTypeClaude,
		AccountTypeClaudeConsole,
		AccountTypeBedrock,
		AccountTypeCCR,
	}

	if len(preferred) > 0 {
		return preferred
	}

	return allTypes
}

// getAccountPriority 获取账户类型优先级
func (s *UnifiedClaudeScheduler) getAccountPriority(accountType AccountType) int {
	priorities := map[AccountType]int{
		AccountTypeClaude:        100,
		AccountTypeClaudeConsole: 90,
		AccountTypeBedrock:       80,
		AccountTypeCCR:           70,
	}
	return priorities[accountType]
}

// getAccountLoad 获取账户负载
func (s *UnifiedClaudeScheduler) getAccountLoad(ctx context.Context, accountType AccountType, accountID string) float64 {
	concurrency, _ := s.redis.GetConcurrency(ctx, accountID)
	return float64(concurrency)
}

// isAccountOverloaded 检查账户是否过载（529 错误状态）
func (s *UnifiedClaudeScheduler) isAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) bool {
	key := fmt.Sprintf("overload:%s:%s", accountType, accountID)
	exists, _ := s.redis.Exists(ctx, key)
	return exists
}

// 辅助方法（需要根据实际账户结构实现）
func (s *UnifiedClaudeScheduler) getAccount(ctx context.Context, accountType AccountType, accountID string) (interface{}, error) {
	// TODO: 根据账户类型获取账户
	return nil, nil
}

func (s *UnifiedClaudeScheduler) getAccountsByType(ctx context.Context, accountType AccountType) ([]interface{}, error) {
	// TODO: 获取指定类型的所有账户
	return nil, nil
}

func (s *UnifiedClaudeScheduler) isAccountSchedulable(account interface{}) bool {
	// TODO: 检查账户是否可调度
	return true
}

func (s *UnifiedClaudeScheduler) getAccountID(account interface{}) string {
	// TODO: 获取账户 ID
	return ""
}
```

---

### 3.4 定价服务 (pricing/service.go)

```go
package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// ModelPricing 模型价格
type ModelPricing struct {
	InputPricePerMillion       float64 `json:"inputPricePerMillion"`
	OutputPricePerMillion      float64 `json:"outputPricePerMillion"`
	CacheCreationPricePerMillion float64 `json:"cacheCreationPricePerMillion"`
	CacheReadPricePerMillion   float64 `json:"cacheReadPricePerMillion"`
}

// Service 定价服务
type Service struct {
	redis    *redis.Client
	cache    map[string]*ModelPricing
	cacheMu  sync.RWMutex
}

// 默认价格（Claude 模型）
var defaultPricing = map[string]*ModelPricing{
	"claude-sonnet-4-20250514": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-3-5-sonnet-20241022": {
		InputPricePerMillion:         3.0,
		OutputPricePerMillion:        15.0,
		CacheCreationPricePerMillion: 3.75,
		CacheReadPricePerMillion:     0.30,
	},
	"claude-opus-4-20250514": {
		InputPricePerMillion:         15.0,
		OutputPricePerMillion:        75.0,
		CacheCreationPricePerMillion: 18.75,
		CacheReadPricePerMillion:     1.50,
	},
	"claude-3-5-haiku-20241022": {
		InputPricePerMillion:         0.80,
		OutputPricePerMillion:        4.0,
		CacheCreationPricePerMillion: 1.0,
		CacheReadPricePerMillion:     0.08,
	},
}

// NewService 创建定价服务
func NewService(redisClient *redis.Client) *Service {
	s := &Service{
		redis: redisClient,
		cache: make(map[string]*ModelPricing),
	}

	// 初始化默认价格
	for model, pricing := range defaultPricing {
		s.cache[model] = pricing
	}

	return s
}

// GetPricing 获取模型价格
func (s *Service) GetPricing(model string) *ModelPricing {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	// 精确匹配
	if pricing, ok := s.cache[model]; ok {
		return pricing
	}

	// 模糊匹配（处理版本后缀）
	modelLower := strings.ToLower(model)
	for key, pricing := range s.cache {
		if strings.Contains(modelLower, strings.ToLower(key)) ||
			strings.Contains(strings.ToLower(key), modelLower) {
			return pricing
		}
	}

	// 返回默认值（Sonnet 价格）
	return defaultPricing["claude-sonnet-4-20250514"]
}

// CalculateCost 计算成本
func (s *Service) CalculateCost(model string, usage UsageData) float64 {
	pricing := s.GetPricing(model)
	if pricing == nil {
		return 0
	}

	cost := float64(usage.InputTokens) * pricing.InputPricePerMillion / 1_000_000
	cost += float64(usage.OutputTokens) * pricing.OutputPricePerMillion / 1_000_000
	cost += float64(usage.CacheCreationTokens) * pricing.CacheCreationPricePerMillion / 1_000_000
	cost += float64(usage.CacheReadTokens) * pricing.CacheReadPricePerMillion / 1_000_000

	return cost
}

// UsageData 使用数据
type UsageData struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

// LoadFromRedis 从 Redis 加载价格
func (s *Service) LoadFromRedis(ctx context.Context) error {
	data, err := s.redis.Get(ctx, "model_pricing")
	if err != nil {
		return err
	}

	var pricing map[string]*ModelPricing
	if err := json.Unmarshal([]byte(data), &pricing); err != nil {
		return err
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for model, p := range pricing {
		s.cache[model] = p
	}

	logger.Info("Loaded pricing from Redis", zap.Int("count", len(pricing)))
	return nil
}

// SaveToRedis 保存价格到 Redis
func (s *Service) SaveToRedis(ctx context.Context) error {
	s.cacheMu.RLock()
	data, err := json.Marshal(s.cache)
	s.cacheMu.RUnlock()

	if err != nil {
		return err
	}

	return s.redis.Set(ctx, "model_pricing", data, 0)
}
```

---

## 4. 测试和验证

### 4.1 单元测试

```go
package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/catstream/claude-relay-go/internal/services/apikey"
)

func TestAPIKeyValidation(t *testing.T) {
	ctx := context.Background()
	service := apikey.NewService(testRedisClient)

	// 创建测试 API Key
	key, rawKey, err := service.GenerateAPIKey(ctx, apikey.GenerateOptions{
		Name:       "Test Key",
		IsActive:   true,
		Permissions: []string{"claude"},
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, rawKey)

	// 测试验证成功
	result := service.ValidateAPIKey(ctx, rawKey, apikey.ValidationOptions{
		RequiredPermission: "claude",
	})
	assert.True(t, result.Valid)
	assert.Equal(t, key.ID, result.APIKey.ID)

	// 测试权限不足
	result = service.ValidateAPIKey(ctx, rawKey, apikey.ValidationOptions{
		RequiredPermission: "gemini",
	})
	assert.False(t, result.Valid)
	assert.Equal(t, "permission_denied", result.ErrorCode)

	// 测试无效 Key
	result = service.ValidateAPIKey(ctx, "invalid_key", apikey.ValidationOptions{})
	assert.False(t, result.Valid)
}

func TestRateLimit(t *testing.T) {
	ctx := context.Background()
	service := apikey.NewService(testRedisClient)

	// 创建带速率限制的 API Key
	key := &redis.APIKey{
		ID:                "test_rate_limit",
		RateLimitWindow:   60,  // 60秒
		RateLimitRequests: 5,   // 5次
	}

	// 前5次应该通过
	for i := 0; i < 5; i++ {
		result, err := service.CheckRateLimit(ctx, key)
		assert.NoError(t, err)
		assert.True(t, result.Allowed)
	}

	// 第6次应该被限制
	result, err := service.CheckRateLimit(ctx, key)
	assert.NoError(t, err)
	assert.False(t, result.Allowed)
}
```

### 4.2 集成测试

```bash
# 测试认证中间件
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer cr_test_key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello"}]}'

# 测试速率限制
for i in {1..10}; do
  curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST http://localhost:8080/api/v1/messages \
    -H "Authorization: Bearer cr_test_key" \
    -d '{}'
done

# 测试并发限制
for i in {1..5}; do
  curl -X POST http://localhost:8080/api/v1/messages \
    -H "Authorization: Bearer cr_test_key" \
    -d '{}' &
done
wait
```

---

## 5. 检查清单

### 5.1 核心服务

- [x] API Key 服务
  - [x] 生成 API Key
  - [x] 验证 API Key
  - [x] 权限检查
  - [x] 客户端限制检查
  - [x] 模型黑名单检查
- [x] 速率限制
  - [x] 请求速率限制
  - [x] 并发限制
  - [x] 成本限制
- [x] 并发排队
  - [x] 排队逻辑
  - [x] 指数退避
  - [x] 健康检查

### 5.2 认证中间件

- [x] API Key 提取（Header/Query）
- [x] 客户端类型解析
- [x] 模型解析
- [x] 完整验证流程
- [x] 上下文设置

### 5.3 统一调度器

- [x] Claude 调度器
  - [x] 粘性会话支持
  - [x] 多账户类型支持
  - [x] 模型兼容性检查
  - [x] 负载均衡
  - [x] 过载检测
- [x] Gemini 调度器
- [x] OpenAI 调度器
- [x] Droid 调度器

### 5.4 账户服务

- [x] Claude 官方账户
- [x] Claude Console 账户
- [x] Gemini 账户
- [x] OpenAI 账户
- [x] Bedrock 账户
- [x] Azure OpenAI 账户
- [x] Droid 账户
- [x] CCR 账户
- [ ] 账户组管理（可选，后续实现）

### 5.5 定价服务

- [x] 模型价格加载
- [x] 成本计算
- [x] Redis 持久化

---

## 6. 下一步

完成本阶段后，进入 **04-step4-relay-services.md**：
- Claude 转发服务（流式响应）
- Gemini 转发服务
- OpenAI 转发服务
- 请求/响应转换
- Usage 捕获

---

**文档版本**: v1.0
**创建日期**: 2024-12-22
**维护者**: Claude Relay Team
