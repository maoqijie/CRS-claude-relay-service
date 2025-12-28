package apikey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// ValidationResult 验证结果
type ValidationResult struct {
	Valid      bool
	APIKey     *redis.APIKey
	Error      string
	ErrorCode  string
	StatusCode int
}

// ValidationOptions 验证选项
type ValidationOptions struct {
	RequiredPermission string   // claude, gemini, openai, droid, all
	ClientType         string   // 客户端类型（从 User-Agent 解析）
	Model              string   // 请求的模型
	SkipRateLimit      bool     // 跳过速率限制检查
	SkipConcurrency    bool     // 跳过并发检查
	SkipCostLimit      bool     // 跳过成本限制检查
}

// ValidateAPIKey 验证 API Key
func (s *Service) ValidateAPIKey(ctx context.Context, rawKey string, opts ValidationOptions) *ValidationResult {
	// 1. 格式检查
	if !strings.HasPrefix(rawKey, s.prefix) {
		return &ValidationResult{
			Valid:      false,
			Error:      fmt.Sprintf("Invalid API key format, expected prefix '%s'", s.prefix),
			ErrorCode:  "invalid_format",
			StatusCode: 401,
		}
	}

	// 2. 查找 API Key
	hashedKey := s.HashAPIKey(rawKey)
	apiKey, err := s.redis.GetAPIKeyByHash(ctx, hashedKey)
	if err != nil {
		return &ValidationResult{
			Valid:      false,
			Error:      "Failed to lookup API key",
			ErrorCode:  "lookup_error",
			StatusCode: 500,
		}
	}

	if apiKey == nil {
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
			APIKey:    apiKey,
			Error:      "API key is inactive",
			ErrorCode:  "inactive",
			StatusCode: 403,
		}
	}

	// 4. 检查激活模式
	if apiKey.ExpirationMode == "activation" && !apiKey.IsActivated {
		// 首次使用，激活 API Key
		if err := s.activateAPIKey(ctx, apiKey); err != nil {
			logger.Warn("Failed to activate API key", zap.Error(err), zap.String("keyId", apiKey.ID))
		}
	}

	// 5. 检查是否过期
	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return &ValidationResult{
			Valid:      false,
			APIKey:    apiKey,
			Error:      "API key has expired",
			ErrorCode:  "expired",
			StatusCode: 403,
		}
	}

	// 5. 检查是否被删除
	if apiKey.IsDeleted {
		return &ValidationResult{
			Valid:      false,
			APIKey:    apiKey,
			Error:      "API key has been deleted",
			ErrorCode:  "deleted",
			StatusCode: 403,
		}
	}

	// 6. 检查权限
	if opts.RequiredPermission != "" && !s.CheckPermission(apiKey, opts.RequiredPermission) {
		return &ValidationResult{
			Valid:      false,
			APIKey:    apiKey,
			Error:      fmt.Sprintf("API key does not have '%s' permission", opts.RequiredPermission),
			ErrorCode:  "permission_denied",
			StatusCode: 403,
		}
	}

	// 7. 检查客户端限制
	if len(apiKey.AllowedClients) > 0 && opts.ClientType != "" {
		if !s.IsClientAllowed(apiKey.AllowedClients, opts.ClientType) {
			return &ValidationResult{
				Valid:      false,
				APIKey:    apiKey,
				Error:      fmt.Sprintf("Client '%s' is not allowed for this API key", opts.ClientType),
				ErrorCode:  "client_not_allowed",
				StatusCode: 403,
			}
		}
	}

	// 8. 检查模型黑名单
	if len(apiKey.ModelBlacklist) > 0 && opts.Model != "" {
		if s.IsModelBlacklisted(apiKey.ModelBlacklist, opts.Model) {
			return &ValidationResult{
				Valid:      false,
				APIKey:    apiKey,
				Error:      fmt.Sprintf("Model '%s' is blacklisted for this API key", opts.Model),
				ErrorCode:  "model_blacklisted",
				StatusCode: 403,
			}
		}
	}

	// 验证通过
	return &ValidationResult{
		Valid:      true,
		APIKey:    apiKey,
		StatusCode: 200,
	}
}

// CheckPermission 检查权限
func (s *Service) CheckPermission(apiKey *redis.APIKey, required string) bool {
	if len(apiKey.Permissions) == 0 {
		return true // 未设置权限时默认允许所有
	}

	for _, perm := range apiKey.Permissions {
		if perm == "all" || perm == required {
			return true
		}
	}

	return false
}

// IsClientAllowed 检查客户端是否允许
func (s *Service) IsClientAllowed(allowedClients []string, clientType string) bool {
	clientLower := strings.ToLower(clientType)
	for _, allowed := range allowedClients {
		allowedLower := strings.ToLower(allowed)
		// 支持通配符匹配
		if allowedLower == "*" || allowedLower == "all" {
			return true
		}
		if strings.EqualFold(allowed, clientType) {
			return true
		}
		// 前缀匹配
		if strings.HasSuffix(allowedLower, "*") {
			prefix := strings.TrimSuffix(allowedLower, "*")
			if strings.HasPrefix(clientLower, prefix) {
				return true
			}
		}
	}
	return false
}

// IsModelBlacklisted 检查模型是否在黑名单中
func (s *Service) IsModelBlacklisted(blacklist []string, model string) bool {
	modelLower := strings.ToLower(model)
	for _, blocked := range blacklist {
		blockedLower := strings.ToLower(blocked)
		// 精确匹配
		if blockedLower == modelLower {
			return true
		}
		// 包含匹配（支持模型家族）
		if strings.Contains(modelLower, blockedLower) {
			return true
		}
		// 通配符匹配
		if strings.HasSuffix(blockedLower, "*") {
			prefix := strings.TrimSuffix(blockedLower, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return true
			}
		}
	}
	return false
}

// ValidateAndGetAPIKey 验证并返回 API Key（简化方法）
func (s *Service) ValidateAndGetAPIKey(ctx context.Context, rawKey string) (*redis.APIKey, error) {
	result := s.ValidateAPIKey(ctx, rawKey, ValidationOptions{})
	if !result.Valid {
		return nil, fmt.Errorf("%s: %s", result.ErrorCode, result.Error)
	}
	return result.APIKey, nil
}

// QuickValidate 快速验证（仅检查存在性和激活状态）
func (s *Service) QuickValidate(ctx context.Context, rawKey string) (bool, *redis.APIKey) {
	if !strings.HasPrefix(rawKey, s.prefix) {
		return false, nil
	}

	hashedKey := s.HashAPIKey(rawKey)
	apiKey, err := s.redis.GetAPIKeyByHash(ctx, hashedKey)
	if err != nil || apiKey == nil {
		return false, nil
	}

	if !apiKey.IsActive || apiKey.IsDeleted {
		return false, apiKey
	}

	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return false, apiKey
	}

	return true, apiKey
}
