package account

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ClaudeService Claude 账户服务
type ClaudeService struct {
	*BaseService
	tokenRefreshBuffer time.Duration // token 刷新缓冲时间
}

// NewClaudeService 创建 Claude 账户服务
func NewClaudeService(redisClient *redis.Client, encryptionKey string) *ClaudeService {
	return &ClaudeService{
		BaseService:        NewBaseService(redisClient, encryptionKey, redis.AccountTypeClaude),
		tokenRefreshBuffer: 10 * time.Second, // 提前 10 秒刷新
	}
}

// WithTokenRefreshBuffer 设置 token 刷新缓冲时间
func (s *ClaudeService) WithTokenRefreshBuffer(buffer time.Duration) *ClaudeService {
	s.tokenRefreshBuffer = buffer
	return s
}

// ClaudeAccountInput 创建 Claude 账户的输入
type ClaudeAccountInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	AccessToken string   `json:"accessToken"`
	RefreshToken string  `json:"refreshToken"`
	TokenExpiry *time.Time `json:"tokenExpiry,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	SessionKey  string   `json:"sessionKey,omitempty"`
	OrgID       string   `json:"orgId,omitempty"`
	ProxyConfig *ProxyConfig `json:"proxyConfig,omitempty"`
	ConcurrentLimit int  `json:"concurrentLimit,omitempty"`
}

// CreateAccount 创建 Claude 账户
func (s *ClaudeService) CreateAccount(ctx context.Context, input ClaudeAccountInput) (*redis.ClaudeAccount, error) {
	// 生成账户 ID（使用 UUID 避免高并发冲突）
	accountID := fmt.Sprintf("claude_%s", uuid.New().String())

	// 加密敏感数据
	encryptedAccessToken, err := s.Encrypt(input.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt access token: %w", err)
	}

	encryptedRefreshToken, err := s.Encrypt(input.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt refresh token: %w", err)
	}

	var encryptedSessionKey string
	if input.SessionKey != "" {
		encryptedSessionKey, err = s.Encrypt(input.SessionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt session key: %w", err)
		}
	}

	account := &redis.ClaudeAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeClaude),
			CreatedAt:   time.Now(),
		},
		AccessToken:     encryptedAccessToken,
		RefreshToken:    encryptedRefreshToken,
		TokenExpiry:     input.TokenExpiry,
		Scopes:          input.Scopes,
		SessionKey:      encryptedSessionKey,
		OrgID:           input.OrgID,
		ConcurrentLimit: input.ConcurrentLimit,
	}

	// 设置代理配置
	if input.ProxyConfig != nil && input.ProxyConfig.Enabled {
		account.ProxyEnabled = true
		account.ProxyHost = input.ProxyConfig.Host
		account.ProxyPort = input.ProxyConfig.Port
		account.ProxyProtocol = input.ProxyConfig.Protocol
		account.ProxyUsername = input.ProxyConfig.Username
		if input.ProxyConfig.Password != "" {
			encryptedPassword, err := s.Encrypt(input.ProxyConfig.Password)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt proxy password: %w", err)
			}
			account.ProxyPassword = encryptedPassword
		}
	}

	// 保存账户
	if err := s.redis.SetClaudeAccount(ctx, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Claude account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 Claude 账户
func (s *ClaudeService) GetAccount(ctx context.Context, accountID string) (*redis.ClaudeAccount, error) {
	return s.redis.GetClaudeAccount(ctx, accountID)
}

// GetAllAccounts 获取所有 Claude 账户
func (s *ClaudeService) GetAllAccounts(ctx context.Context) ([]*redis.ClaudeAccount, error) {
	return s.redis.GetAllClaudeAccounts(ctx)
}

// GetDecryptedAccessToken 获取解密后的 access token
func (s *ClaudeService) GetDecryptedAccessToken(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	return s.Decrypt(account.AccessToken)
}

// GetDecryptedRefreshToken 获取解密后的 refresh token
func (s *ClaudeService) GetDecryptedRefreshToken(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	return s.Decrypt(account.RefreshToken)
}

// IsTokenExpiring 检查 token 是否即将过期
func (s *ClaudeService) IsTokenExpiring(ctx context.Context, accountID string) (bool, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, fmt.Errorf("account not found")
	}

	if account.TokenExpiry == nil {
		return false, nil // 无过期时间则认为不会过期
	}

	// 检查是否在缓冲时间内
	expiryWithBuffer := account.TokenExpiry.Add(-s.tokenRefreshBuffer)
	return time.Now().After(expiryWithBuffer), nil
}

// UpdateTokens 更新账户的 tokens
func (s *ClaudeService) UpdateTokens(ctx context.Context, accountID, accessToken, refreshToken string, expiry *time.Time) error {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("account not found")
	}

	// 加密新 tokens
	encryptedAccessToken, err := s.Encrypt(accessToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt access token: %w", err)
	}

	account.AccessToken = encryptedAccessToken
	account.TokenExpiry = expiry
	account.UpdatedAt = time.Now()

	// 如果提供了新的 refresh token，也更新它
	if refreshToken != "" {
		encryptedRefreshToken, err := s.Encrypt(refreshToken)
		if err != nil {
			return fmt.Errorf("failed to encrypt refresh token: %w", err)
		}
		account.RefreshToken = encryptedRefreshToken
	}

	// 清除错误状态
	account.LastError = ""
	account.ErrorCount = 0
	account.LastErrorAt = nil

	if err := s.redis.SetClaudeAccount(ctx, account); err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}

	logger.Info("Claude account tokens updated",
		zap.String("accountId", accountID))

	return nil
}

// GetAccountsNeedingRefresh 获取需要刷新 token 的账户
func (s *ClaudeService) GetAccountsNeedingRefresh(ctx context.Context) ([]*redis.ClaudeAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var needRefresh []*redis.ClaudeAccount
	now := time.Now()

	for _, account := range accounts {
		// 跳过非活跃账户
		if account.Status != StatusActive {
			continue
		}

		// 跳过无 refresh token 的账户
		if account.RefreshToken == "" {
			continue
		}

		// 检查是否需要刷新
		if account.TokenExpiry != nil {
			expiryWithBuffer := account.TokenExpiry.Add(-s.tokenRefreshBuffer)
			if now.After(expiryWithBuffer) {
				needRefresh = append(needRefresh, account)
			}
		}
	}

	return needRefresh, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *ClaudeService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("account not found")
	}

	config := &ProxyConfig{
		Enabled:  account.ProxyEnabled,
		Host:     account.ProxyHost,
		Port:     account.ProxyPort,
		Protocol: account.ProxyProtocol,
		Username: account.ProxyUsername,
	}

	// 解密代理密码
	if account.ProxyPassword != "" {
		decryptedPassword, err := s.Decrypt(account.ProxyPassword)
		if err != nil {
			logger.Warn("Failed to decrypt proxy password", zap.Error(err))
		} else {
			config.Password = decryptedPassword
		}
	}

	return config, nil
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *ClaudeService) GetSchedulableAccounts(ctx context.Context, model string) ([]*redis.ClaudeAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*redis.ClaudeAccount
	for _, account := range accounts {
		// 检查状态
		if account.Status != StatusActive {
			continue
		}

		// 检查过载状态
		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		// 检查 token 是否有效
		if account.TokenExpiry != nil && time.Now().After(*account.TokenExpiry) {
			continue
		}

		// 检查模型支持（如果指定了模型）
		if model != "" && !s.isModelSupported(account, model) {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// isModelSupported 检查账户是否支持指定模型
func (s *ClaudeService) isModelSupported(account *redis.ClaudeAccount, model string) bool {
	// Claude 账户默认支持所有 Claude 模型
	// 具体的订阅级别检查可以在这里实现
	return true
}

// UpdateAccount 更新账户信息
func (s *ClaudeService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("account not found")
	}

	// 应用更新
	if name, ok := updates["name"].(string); ok {
		account.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		account.Description = description
	}
	if status, ok := updates["status"].(string); ok {
		account.Status = status
	}
	if concurrentLimit, ok := updates["concurrentLimit"].(float64); ok {
		account.ConcurrentLimit = int(concurrentLimit)
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetClaudeAccount(ctx, account)
}
