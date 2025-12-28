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

// UnifiedClaudeScheduler Claude 统一调度器
type UnifiedClaudeScheduler struct {
	*BaseScheduler
	stickySessionTTL time.Duration
}

// ClaudeAccountTypes Claude 支持的账户类型
var ClaudeAccountTypes = []AccountType{
	AccountTypeClaude,
	AccountTypeClaudeOfficial,
	AccountTypeClaudeConsole,
	AccountTypeBedrock,
	AccountTypeCCR,
}

// NewUnifiedClaudeScheduler 创建 Claude 统一调度器
func NewUnifiedClaudeScheduler(redisClient *redis.Client) *UnifiedClaudeScheduler {
	return &UnifiedClaudeScheduler{
		BaseScheduler:    NewBaseScheduler(redisClient, CategoryClaude, ClaudeAccountTypes),
		stickySessionTTL: time.Hour,
	}
}

// WithStickySessionTTL 设置粘性会话 TTL
func (s *UnifiedClaudeScheduler) WithStickySessionTTL(ttl time.Duration) *UnifiedClaudeScheduler {
	s.stickySessionTTL = ttl
	return s
}

// SelectAccount 选择最优账户
func (s *UnifiedClaudeScheduler) SelectAccount(ctx context.Context, opts SelectOptions) *SelectResult {
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
			Error: fmt.Errorf("no available Claude accounts for model: %s", opts.Model),
		}
	}

	// 3. 按优先级和负载选择最优账户
	selected := s.SelectBestAccount(candidates)
	if selected == nil {
		return &SelectResult{
			Error: fmt.Errorf("failed to select Claude account"),
		}
	}

	// 4. 建立会话绑定
	if opts.SessionHash != "" {
		if err := s.BindSessionAccount(ctx, opts.SessionHash, selected.AccountType, selected.AccountID, s.stickySessionTTL); err != nil {
			logger.Warn("Failed to bind session", zap.Error(err))
		}
	}

	logger.Info("Selected Claude account",
		zap.String("accountType", string(selected.AccountType)),
		zap.String("accountId", selected.AccountID),
		zap.String("model", opts.Model))

	return selected
}

// SelectAccountForModel 为特定模型选择账户（简化方法）
func (s *UnifiedClaudeScheduler) SelectAccountForModel(ctx context.Context, model, sessionHash string) *SelectResult {
	return s.SelectAccount(ctx, SelectOptions{
		Model:       model,
		SessionHash: sessionHash,
	})
}

// SelectOfficialAccount 选择 Claude 官方账户
func (s *UnifiedClaudeScheduler) SelectOfficialAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeClaude, AccountTypeClaudeOfficial}
	return s.SelectAccount(ctx, opts)
}

// SelectConsoleAccount 选择 Claude Console 账户
func (s *UnifiedClaudeScheduler) SelectConsoleAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeClaudeConsole}
	return s.SelectAccount(ctx, opts)
}

// SelectBedrockAccount 选择 Bedrock 账户
func (s *UnifiedClaudeScheduler) SelectBedrockAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeBedrock}
	return s.SelectAccount(ctx, opts)
}

// SelectAccountByType 按指定账户类型选择
func (s *UnifiedClaudeScheduler) SelectAccountByType(ctx context.Context, accountType AccountType, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{accountType}
	return s.SelectAccount(ctx, opts)
}

// GetAvailableAccountsCount 获取可用账户数量
func (s *UnifiedClaudeScheduler) GetAvailableAccountsCount(ctx context.Context, model string) map[AccountType]int {
	counts := make(map[AccountType]int)

	for _, accountType := range ClaudeAccountTypes {
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
func (s *UnifiedClaudeScheduler) MarkAccountOverloaded(ctx context.Context, accountType AccountType, accountID string, duration time.Duration) error {
	return s.redis.SetAccountOverloaded(ctx, redis.AccountType(accountType), accountID, duration)
}

// ClearAccountOverloaded 清除账户过载标记
func (s *UnifiedClaudeScheduler) ClearAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) error {
	return s.redis.ClearAccountOverloaded(ctx, redis.AccountType(accountType), accountID)
}

// IsOpusModelSupported 检查 Opus 模型是否支持
func (s *UnifiedClaudeScheduler) IsOpusModelSupported(ctx context.Context, accountType AccountType, accountID, model string) bool {
	if !strings.Contains(strings.ToLower(model), "opus") {
		return true // 非 Opus 模型不需要检查
	}

	account, err := s.redis.GetAccount(ctx, redis.AccountType(accountType), accountID)
	if err != nil || account == nil {
		return false
	}

	return s.checkOpusAccess(account, strings.ToLower(model))
}

// GetAccountsBySubscriptionLevel 按订阅等级获取账户
func (s *UnifiedClaudeScheduler) GetAccountsBySubscriptionLevel(ctx context.Context, level string) []AccountCandidate {
	var result []AccountCandidate

	for _, accountType := range []AccountType{AccountTypeClaude, AccountTypeClaudeOfficial} {
		accounts, err := s.redis.GetActiveAccounts(ctx, redis.AccountType(accountType))
		if err != nil {
			continue
		}

		for _, account := range accounts {
			accountLevel := ""
			if l, ok := account["subscriptionLevel"].(string); ok {
				accountLevel = strings.ToLower(l)
			}

			if strings.ToLower(level) == accountLevel {
				result = append(result, AccountCandidate{
					Account:     account,
					AccountType: accountType,
					AccountID:   s.getAccountID(account),
					Priority:    s.getAccountPriority(accountType, account),
				})
			}
		}
	}

	return result
}

// RefreshSessionBinding 刷新会话绑定
func (s *UnifiedClaudeScheduler) RefreshSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.RenewStickySession(ctx, sessionHash, s.stickySessionTTL)
}

// ClearSessionBinding 清除会话绑定
func (s *UnifiedClaudeScheduler) ClearSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.DeleteStickySession(ctx, sessionHash)
}

// GetSessionBinding 获取会话绑定信息
func (s *UnifiedClaudeScheduler) GetSessionBinding(ctx context.Context, sessionHash string) (*redis.StickySession, error) {
	return s.redis.GetStickySession(ctx, sessionHash)
}
