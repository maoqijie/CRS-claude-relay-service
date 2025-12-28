package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// UnifiedOpenAIScheduler OpenAI 统一调度器
type UnifiedOpenAIScheduler struct {
	*BaseScheduler
	stickySessionTTL time.Duration
}

// OpenAIAccountTypes OpenAI 支持的账户类型
var OpenAIAccountTypes = []AccountType{
	AccountTypeOpenAI,
	AccountTypeOpenAIResponses,
	AccountTypeAzureOpenAI,
}

// NewUnifiedOpenAIScheduler 创建 OpenAI 统一调度器
func NewUnifiedOpenAIScheduler(redisClient *redis.Client) *UnifiedOpenAIScheduler {
	return &UnifiedOpenAIScheduler{
		BaseScheduler:    NewBaseScheduler(redisClient, CategoryOpenAI, OpenAIAccountTypes),
		stickySessionTTL: time.Hour,
	}
}

// WithStickySessionTTL 设置粘性会话 TTL
func (s *UnifiedOpenAIScheduler) WithStickySessionTTL(ttl time.Duration) *UnifiedOpenAIScheduler {
	s.stickySessionTTL = ttl
	return s
}

// SelectAccount 选择最优账户
func (s *UnifiedOpenAIScheduler) SelectAccount(ctx context.Context, opts SelectOptions) *SelectResult {
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
			Error: fmt.Errorf("no available OpenAI accounts for model: %s", opts.Model),
		}
	}

	// 3. 按优先级和负载选择最优账户
	selected := s.SelectBestAccount(candidates)
	if selected == nil {
		return &SelectResult{
			Error: fmt.Errorf("failed to select OpenAI account"),
		}
	}

	// 4. 建立会话绑定
	if opts.SessionHash != "" {
		if err := s.BindSessionAccount(ctx, opts.SessionHash, selected.AccountType, selected.AccountID, s.stickySessionTTL); err != nil {
			logger.Warn("Failed to bind session", zap.Error(err))
		}
	}

	logger.Info("Selected OpenAI account",
		zap.String("accountType", string(selected.AccountType)),
		zap.String("accountId", selected.AccountID),
		zap.String("model", opts.Model))

	return selected
}

// SelectAccountForModel 为特定模型选择账户（简化方法）
func (s *UnifiedOpenAIScheduler) SelectAccountForModel(ctx context.Context, model, sessionHash string) *SelectResult {
	return s.SelectAccount(ctx, SelectOptions{
		Model:       model,
		SessionHash: sessionHash,
	})
}

// SelectOpenAIAccount 选择 OpenAI 官方账户
func (s *UnifiedOpenAIScheduler) SelectOpenAIAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeOpenAI}
	return s.SelectAccount(ctx, opts)
}

// SelectResponsesAccount 选择 OpenAI Responses 账户（Codex）
func (s *UnifiedOpenAIScheduler) SelectResponsesAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeOpenAIResponses}
	return s.SelectAccount(ctx, opts)
}

// SelectAzureAccount 选择 Azure OpenAI 账户
func (s *UnifiedOpenAIScheduler) SelectAzureAccount(ctx context.Context, opts SelectOptions) *SelectResult {
	opts.PreferredAccountTypes = []AccountType{AccountTypeAzureOpenAI}
	return s.SelectAccount(ctx, opts)
}

// GetAvailableAccountsCount 获取可用账户数量
func (s *UnifiedOpenAIScheduler) GetAvailableAccountsCount(ctx context.Context, model string) map[AccountType]int {
	counts := make(map[AccountType]int)

	for _, accountType := range OpenAIAccountTypes {
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
func (s *UnifiedOpenAIScheduler) MarkAccountOverloaded(ctx context.Context, accountType AccountType, accountID string, duration time.Duration) error {
	return s.redis.SetAccountOverloaded(ctx, redis.AccountType(accountType), accountID, duration)
}

// ClearAccountOverloaded 清除账户过载标记
func (s *UnifiedOpenAIScheduler) ClearAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) error {
	return s.redis.ClearAccountOverloaded(ctx, redis.AccountType(accountType), accountID)
}

// RefreshSessionBinding 刷新会话绑定
func (s *UnifiedOpenAIScheduler) RefreshSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.RenewStickySession(ctx, sessionHash, s.stickySessionTTL)
}

// ClearSessionBinding 清除会话绑定
func (s *UnifiedOpenAIScheduler) ClearSessionBinding(ctx context.Context, sessionHash string) error {
	return s.redis.DeleteStickySession(ctx, sessionHash)
}

// GetSessionBinding 获取会话绑定信息
func (s *UnifiedOpenAIScheduler) GetSessionBinding(ctx context.Context, sessionHash string) (*redis.StickySession, error) {
	return s.redis.GetStickySession(ctx, sessionHash)
}

// GetAzureEndpoint 获取 Azure 端点
func (s *UnifiedOpenAIScheduler) GetAzureEndpoint(account map[string]interface{}) string {
	if endpoint, ok := account["endpoint"].(string); ok {
		return endpoint
	}
	return ""
}

// GetAzureDeploymentName 获取 Azure 部署名称
func (s *UnifiedOpenAIScheduler) GetAzureDeploymentName(account map[string]interface{}) string {
	if deployment, ok := account["deploymentName"].(string); ok {
		return deployment
	}
	return ""
}
