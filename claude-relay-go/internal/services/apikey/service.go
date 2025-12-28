package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	"bedrock":          {Prefix: "bedrock:account:"},
	"ccr":              {Prefix: "ccr:account:"},
}

// AccountTypePriority 账户类型优先级
var AccountTypePriority = []string{
	"openai",
	"openai-responses",
	"azure-openai",
	"claude",
	"claude-console",
	"bedrock",
	"ccr",
	"gemini",
	"gemini-api",
	"droid",
}

// AccountCategoryMap 账户类型到类别的映射
var AccountCategoryMap = map[string]string{
	"claude":           "claude",
	"claude-console":   "claude",
	"bedrock":          "claude",
	"ccr":              "claude",
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
	prefix := "cr_"
	if config.Cfg != nil && config.Cfg.Security.APIKeyPrefix != "" {
		prefix = config.Cfg.Security.APIKeyPrefix
	}
	return &Service{
		redis:  redisClient,
		prefix: prefix,
	}
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
	RateLimitPerMin                         int
	RateLimitPerHour                        int
	DailyCostLimit                          float64
	UserID                                  string
	Tags                                    []string
	ActivationDays                          int
	ConcurrentRequestQueueEnabled           bool
	ConcurrentRequestQueueMaxSize           int
	ConcurrentRequestQueueMaxSizeMultiplier float64
	ConcurrentRequestQueueTimeoutMs         int
}

// GenerateAPIKey 生成新的 API Key
func (s *Service) GenerateAPIKey(ctx context.Context, opts GenerateOptions) (*redis.APIKey, string, error) {
	// 生成原始 Key（带前缀）
	rawKey := s.prefix + generateRandomString(32)

	// 计算哈希
	hashedKey := s.HashAPIKey(rawKey)

	// 创建 API Key 对象
	now := time.Now()
	keyID := uuid.New().String()

	apiKey := &redis.APIKey{
		ID:               keyID,
		Name:             opts.Name,
		Description:      opts.Description,
		HashedKey:        hashedKey,
		APIKey:           hashedKey, // 兼容 Node.js
		Limit:            opts.TokenLimit,
		IsActive:         opts.IsActive,
		CreatedAt:        now,
		Permissions:      opts.Permissions,
		AllowedClients:   opts.AllowedClients,
		ModelBlacklist:   opts.ModelBlacklist,
		ConcurrentLimit:  opts.ConcurrencyLimit,
		RateLimitPerMin:  opts.RateLimitPerMin,
		RateLimitPerHour: opts.RateLimitPerHour,
		DailyCostLimit:   opts.DailyCostLimit,
		UserID:           opts.UserID,
		Tags:             opts.Tags,

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

// GetAPIKey 获取 API Key
func (s *Service) GetAPIKey(ctx context.Context, keyID string) (*redis.APIKey, error) {
	return s.redis.GetAPIKey(ctx, keyID)
}

// GetAPIKeyByRawKey 通过原始 Key 获取 API Key
func (s *Service) GetAPIKeyByRawKey(ctx context.Context, rawKey string) (*redis.APIKey, error) {
	hashedKey := s.HashAPIKey(rawKey)
	return s.redis.GetAPIKeyByHash(ctx, hashedKey)
}

// UpdateAPIKey 更新 API Key
func (s *Service) UpdateAPIKey(ctx context.Context, keyID string, updates map[string]interface{}) error {
	return s.redis.UpdateAPIKeyFields(ctx, keyID, updates)
}

// DeleteAPIKey 软删除 API Key
func (s *Service) DeleteAPIKey(ctx context.Context, keyID string) error {
	return s.redis.DeleteAPIKey(ctx, keyID)
}

// HardDeleteAPIKey 硬删除 API Key
func (s *Service) HardDeleteAPIKey(ctx context.Context, keyID string) error {
	return s.redis.HardDeleteAPIKey(ctx, keyID)
}

// GetAllAPIKeys 获取所有 API Key
func (s *Service) GetAllAPIKeys(ctx context.Context, includeDeleted bool) ([]redis.APIKey, error) {
	return s.redis.GetAllAPIKeys(ctx, includeDeleted)
}

// GetAPIKeysPaginated 分页获取 API Key
func (s *Service) GetAPIKeysPaginated(ctx context.Context, opts redis.APIKeyQueryOptions) (*redis.APIKeyPaginated, error) {
	return s.redis.GetAPIKeysPaginated(ctx, opts)
}

// GetAPIKeyStats 获取 API Key 统计
func (s *Service) GetAPIKeyStats(ctx context.Context) (*redis.APIKeyStats, error) {
	return s.redis.GetAPIKeyStats(ctx)
}

// HashAPIKey 计算 API Key 的 SHA256 哈希
func (s *Service) HashAPIKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// GetPrefix 获取 API Key 前缀
func (s *Service) GetPrefix() string {
	return s.prefix
}

// IncrementUsage 增加使用量
func (s *Service) IncrementUsage(ctx context.Context, params redis.TokenUsageParams) error {
	return s.redis.IncrementTokenUsage(ctx, params)
}

// GetUsageStats 获取使用统计
func (s *Service) GetUsageStats(ctx context.Context, keyID string) (*redis.UsageStatsResult, error) {
	return s.redis.GetUsageStats(ctx, keyID)
}

// IncrementDailyCost 增加每日成本
func (s *Service) IncrementDailyCost(ctx context.Context, keyID string, amount float64) error {
	return s.redis.IncrementDailyCost(ctx, keyID, amount)
}

// GetDailyCost 获取每日成本
func (s *Service) GetDailyCost(ctx context.Context, keyID string) (float64, error) {
	return s.redis.GetDailyCost(ctx, keyID)
}

// UpdateLastUsedAt 更新最后使用时间
func (s *Service) UpdateLastUsedAt(ctx context.Context, keyID string) error {
	return s.redis.UpdateAPIKeyFields(ctx, keyID, map[string]interface{}{
		"lastUsedAt": time.Now(),
	})
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		// 使用 uuid 生成随机字节
		u := uuid.New()
		result[i] = charset[u[0]%byte(len(charset))]
	}
	return string(result)
}

// RestoreAPIKey 恢复已删除的 API Key
func (s *Service) RestoreAPIKey(ctx context.Context, keyID string) error {
	apiKey, err := s.redis.GetAPIKey(ctx, keyID)
	if err != nil {
		return fmt.Errorf("failed to get API key: %w", err)
	}
	if apiKey == nil {
		return fmt.Errorf("API key not found: %s", keyID)
	}

	if !apiKey.IsDeleted {
		return fmt.Errorf("API key is not deleted: %s", keyID)
	}

	// 恢复 API Key
	updates := map[string]interface{}{
		"isDeleted": false,
	}

	if err := s.redis.UpdateAPIKeyFields(ctx, keyID, updates); err != nil {
		return fmt.Errorf("failed to restore API key: %w", err)
	}

	logger.Info("API Key restored",
		zap.String("id", keyID),
		zap.String("name", apiKey.Name))

	return nil
}

// activateAPIKey 激活 API Key（首次使用时调用）
func (s *Service) activateAPIKey(ctx context.Context, apiKey *redis.APIKey) error {
	now := time.Now()

	// 计算激活有效期
	activationDays := apiKey.ActivationDays
	if activationDays <= 0 {
		activationDays = 30 // 默认 30 天
	}

	var expiresAt time.Time
	if apiKey.ActivationUnit == "hours" {
		expiresAt = now.Add(time.Duration(activationDays) * time.Hour)
	} else {
		// 默认按天计算
		expiresAt = now.AddDate(0, 0, activationDays)
	}

	// 更新 API Key 状态
	updates := map[string]interface{}{
		"isActivated": true,
		"activatedAt": now,
		"expiresAt":   expiresAt,
	}

	if err := s.redis.UpdateAPIKeyFields(ctx, apiKey.ID, updates); err != nil {
		return fmt.Errorf("failed to activate API key: %w", err)
	}

	// 更新内存中的对象
	apiKey.IsActivated = true
	apiKey.ActivatedAt = &now
	apiKey.ExpiresAt = &expiresAt

	logger.Info("API Key activated",
		zap.String("id", apiKey.ID),
		zap.String("name", apiKey.Name),
		zap.Time("expiresAt", expiresAt))

	return nil
}
