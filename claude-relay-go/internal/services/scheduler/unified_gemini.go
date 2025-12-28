package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// UnifiedGeminiScheduler Gemini 统一调度器
type UnifiedGeminiScheduler struct {
	*BaseScheduler
	stickySessionTTL time.Duration
}

// GeminiAccountTypes Gemini 支持的账户类型
var GeminiAccountTypes = []AccountType{
	AccountTypeGemini,
	AccountTypeGeminiAPI,
}

// NewUnifiedGeminiScheduler 创建 Gemini 统一调度器
func NewUnifiedGeminiScheduler(redisClient *redis.Client) *UnifiedGeminiScheduler {
	return &UnifiedGeminiScheduler{
		BaseScheduler:    NewBaseScheduler(redisClient, CategoryGemini, GeminiAccountTypes),
		stickySessionTTL: time.Hour,
	}
}

// WithStickySessionTTL 设置粘性会话 TTL
func (s *UnifiedGeminiScheduler) WithStickySessionTTL(ttl time.Duration) *UnifiedGeminiScheduler {
	s.stickySessionTTL = ttl
	return s
}

// SelectAccount 选择最优账户
func (s *UnifiedGeminiScheduler) SelectAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	// 1. 检查粘性会话
	if opts.SessionHash != "" {
		if result := s.GetSessionAccount(ctx, opts.SessionHash, opts.Model); result != nil {
			return result
		}
	}

	// 2. 收集所有可用账户
	candidates := s.CollectAvailableAccounts(ctx, opts)
	if len(candidates) == 0 {
		return &SelectResult{
			Error: fmt.Errorf("no available Gemini accounts for model: %s", opts.Model),
		}
	}

	// 3. 按优先级和负载选择最优账户
	selected := s.SelectBestAccount(candidates)
	if selected == nil {
		return &SelectResult{
			Error: fmt.Errorf("failed to select Gemini account"),
		}
	}

	// 4. 建立会话绑定
	if opts.SessionHash != "" {
		if err := s.BindSessionAccount(ctx, opts.SessionHash, selected.AccountType, selected.AccountID, s.stickySessionTTL); err != nil {
			logger.Warn("Failed to bind session", zap.Error(err))
		}
	}

	logger.Info("Selected Gemini account",
		zap.String("accountType", string(selected.AccountType)),
		zap.String("accountId", selected.AccountID),
		zap.String("model", opts.Model))

	return selected
}

// SelectAccountForModel 为特定模型选择账户（简化方法）
func (s *UnifiedGeminiScheduler) SelectAccountForModel(ctx context.Context, model, sessionHash string) *SelectResult {
	return s.SelectAccount(ctx, SelectOptions{
		Model:       model,
		SessionHash: sessionHash,
	})
}

// SelectOAuthAccount 选择 OAuth 账户（Gemini CLI 类型）
func (s *UnifiedGeminiScheduler) SelectOAuthAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeGemini}
	return s.SelectAccount(ctx, opts)
}

// SelectAPIKeyAccount 选择 API Key 账户
func (s *UnifiedGeminiScheduler) SelectAPIKeyAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeGeminiAPI}
	return s.SelectAccount(ctx, opts)
}

// GetAvailableAccountsCount 获取可用账户数量
func (s *UnifiedGeminiScheduler) GetAvailableAccountsCount(ctx context.Context, model string) map[AccountType]int {
	counts := make(map[AccountType]int)

	for _, accountType := range GeminiAccountTypes {
		accounts, err := s.redis.GetActiveAccounts(ctx, redis.AccountType(accountType))
		if err != nil {
			continue
		}

		count := 0
		for _, account := range accounts {
			if model == "" || s.isModelSupported(account, accountType, model) {
				count++
			}
		}
		counts[accountType] = count
	}

	return counts
}

// MarkAccountOverloaded 标记账户过载
func (s *UnifiedGeminiScheduler) MarkAccountOverloaded(ctx context.Context, accountType AccountType, accountID string, duration time.Duration) error {
	return s.redis.SetAccountOverloaded(ctx, redis.AccountType(accountType), accountID, duration)
}

// ClearAccountOverloaded 清除账户过载标记
func (s *UnifiedGeminiScheduler) ClearAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) error {
	return s.redis.ClearAccountOverloaded(ctx, redis.AccountType(accountType), accountID)
}

// RefreshSessionBinding 刷新会话绑定
func (s *UnifiedGeminiScheduler) RefreshSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.RenewStickySession(ctx, sessionHash, s.stickySessionTTL)
}

// ClearSessionBinding 清除会话绑定
func (s *UnifiedGeminiScheduler) ClearSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.DeleteStickySession(ctx, sessionHash)
}

// GetSessionBinding 获取会话绑定信息
func (s *UnifiedGeminiScheduler) GetSessionBinding(ctx context.Context, sessionHash string) (*redis.StickySession, error) {
	return s.redis.GetStickySession(ctx, sessionHash)
}

// NeedsTokenRefresh 检查账户是否需要刷新 Token
func (s *UnifiedGeminiScheduler) NeedsTokenRefresh(account map[string]interface{}) bool {
	// 检查是否是 OAuth 账户
	if account["accessToken"] == nil {
		return false
	}

	// 检查 Token 过期时间
	if expiresAt, ok := account["expiresAt"].(string); ok {
		expTime, err := time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return true // 无法解析，需要刷新
		}
		// 提前 10 秒刷新
		return time.Until(expTime) < 10*time.Second
	}

	return false
}
