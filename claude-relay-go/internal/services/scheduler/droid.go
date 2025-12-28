package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// DroidScheduler Droid 调度器
type DroidScheduler struct {
	*BaseScheduler
	stickySessionTTL time.Duration
}

// DroidAccountTypes Droid 支持的账户类型
var DroidAccountTypes = []AccountType{
	AccountTypeDroid,
}

// NewDroidScheduler 创建 Droid 调度器
func NewDroidScheduler(redisClient *redis.Client) *DroidScheduler {
	return &DroidScheduler{
		BaseScheduler:    NewBaseScheduler(redisClient, CategoryDroid, DroidAccountTypes),
		stickySessionTTL: time.Hour,
	}
}

// WithStickySessionTTL 设置粘性会话 TTL
func (s *DroidScheduler) WithStickySessionTTL(ttl time.Duration) *DroidScheduler {
	s.stickySessionTTL = ttl
	return s
}

// SelectAccount 选择最优账户
func (s *DroidScheduler) SelectAccount(ctx context.Context, opts SelectOptions) *SelectResult {
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
			Error: fmt.Errorf("no available Droid accounts for model: %s", opts.Model),
		}
	}

	// 3. 按优先级和负载选择最优账户
	selected := s.SelectBestAccount(candidates)
	if selected == nil {
		return &SelectResult{
			Error: fmt.Errorf("failed to select Droid account"),
		}
	}

	// 4. 建立会话绑定
	if opts.SessionHash != "" {
		if err := s.BindSessionAccount(ctx, opts.SessionHash, selected.AccountType, selected.AccountID, s.stickySessionTTL); err != nil {
			logger.Warn("Failed to bind session", zap.Error(err))
		}
	}

	logger.Info("Selected Droid account",
		zap.String("accountType", string(selected.AccountType)),
		zap.String("accountId", selected.AccountID),
		zap.String("model", opts.Model))

	return selected
}

// SelectAccountForModel 为特定模型选择账户（简化方法）
func (s *DroidScheduler) SelectAccountForModel(ctx context.Context, model, sessionHash string) *SelectResult {
	return s.SelectAccount(ctx, SelectOptions{
		Model:       model,
		SessionHash: sessionHash,
	})
}

// GetAvailableAccountsCount 获取可用账户数量
func (s *DroidScheduler) GetAvailableAccountsCount(ctx context.Context, model string) int {
	accounts, err := s.redis.GetActiveAccounts(ctx, redis.AccountType(AccountTypeDroid))
	if err != nil {
		return 0
	}

	count := 0
	for _, account := range accounts {
		if model == "" || s.isModelSupported(account, AccountTypeDroid, model) {
			count++
		}
	}

	return count
}

// MarkAccountOverloaded 标记账户过载
func (s *DroidScheduler) MarkAccountOverloaded(ctx context.Context, accountID string, duration time.Duration) error {
	return s.redis.SetAccountOverloaded(ctx, redis.AccountType(AccountTypeDroid), accountID, duration)
}

// ClearAccountOverloaded 清除账户过载标记
func (s *DroidScheduler) ClearAccountOverloaded(ctx context.Context, accountID string) error {
	return s.redis.ClearAccountOverloaded(ctx, redis.AccountType(AccountTypeDroid), accountID)
}

// RefreshSessionBinding 刷新会话绑定
func (s *DroidScheduler) RefreshSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.RenewStickySession(ctx, sessionHash, s.stickySessionTTL)
}

// ClearSessionBinding 清除会话绑定
func (s *DroidScheduler) ClearSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.DeleteStickySession(ctx, sessionHash)
}

// GetSessionBinding 获取会话绑定信息
func (s *DroidScheduler) GetSessionBinding(ctx context.Context, sessionHash string) (*redis.StickySession, error) {
	return s.redis.GetStickySession(ctx, sessionHash)
}
