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

// GeminiService Gemini 账户服务
type GeminiService struct {
	*BaseService
	tokenRefreshBuffer time.Duration
}

// NewGeminiService 创建 Gemini 账户服务
func NewGeminiService(redisClient *redis.Client, encryptionKey string) *GeminiService {
	return &GeminiService{
		BaseService:        NewBaseService(redisClient, encryptionKey, redis.AccountTypeGemini),
		tokenRefreshBuffer: 10 * time.Second,
	}
}

// WithTokenRefreshBuffer 设置 token 刷新缓冲时间
func (s *GeminiService) WithTokenRefreshBuffer(buffer time.Duration) *GeminiService {
	s.tokenRefreshBuffer = buffer
	return s
}

// GeminiAccountInput 创建 Gemini 账户的输入
type GeminiAccountInput struct {
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	AccessToken  string       `json:"accessToken,omitempty"`
	RefreshToken string       `json:"refreshToken,omitempty"`
	TokenExpiry  *time.Time   `json:"tokenExpiry,omitempty"`
	ClientID     string       `json:"clientId,omitempty"`
	ClientSecret string       `json:"clientSecret,omitempty"`
	APIKey       string       `json:"apiKey,omitempty"` // API Key 模式
	ProjectID    string       `json:"projectId,omitempty"`
	Region       string       `json:"region,omitempty"`
	ProxyConfig  *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 Gemini 账户
func (s *GeminiService) CreateAccount(ctx context.Context, input GeminiAccountInput) (*redis.GeminiAccount, error) {
	accountID := fmt.Sprintf("gemini_%s", uuid.New().String())

	account := &redis.GeminiAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeGemini),
			CreatedAt:   time.Now(),
		},
		TokenExpiry: input.TokenExpiry,
		ClientID:    input.ClientID,
		ProjectID:   input.ProjectID,
		Region:      input.Region,
	}

	// 加密敏感数据
	if input.AccessToken != "" {
		encrypted, err := s.Encrypt(input.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt access token: %w", err)
		}
		account.AccessToken = encrypted
	}

	if input.RefreshToken != "" {
		encrypted, err := s.Encrypt(input.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt refresh token: %w", err)
		}
		account.RefreshToken = encrypted
	}

	if input.ClientSecret != "" {
		encrypted, err := s.Encrypt(input.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt client secret: %w", err)
		}
		account.ClientSecret = encrypted
	}

	if input.APIKey != "" {
		encrypted, err := s.Encrypt(input.APIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt API key: %w", err)
		}
		account.APIKey = encrypted
	}

	// 设置代理配置
	if input.ProxyConfig != nil && input.ProxyConfig.Enabled {
		account.ProxyEnabled = true
		account.ProxyHost = input.ProxyConfig.Host
		account.ProxyPort = input.ProxyConfig.Port
		account.ProxyProtocol = input.ProxyConfig.Protocol
		account.ProxyUsername = input.ProxyConfig.Username
		if input.ProxyConfig.Password != "" {
			encrypted, err := s.Encrypt(input.ProxyConfig.Password)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt proxy password: %w", err)
			}
			account.ProxyPassword = encrypted
		}
	}

	if err := s.redis.SetGeminiAccount(ctx, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Gemini account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 Gemini 账户
func (s *GeminiService) GetAccount(ctx context.Context, accountID string) (*redis.GeminiAccount, error) {
	return s.redis.GetGeminiAccount(ctx, accountID)
}

// GetAllAccounts 获取所有 Gemini 账户
func (s *GeminiService) GetAllAccounts(ctx context.Context) ([]*redis.GeminiAccount, error) {
	return s.redis.GetAllGeminiAccounts(ctx)
}

// GetDecryptedAccessToken 获取解密后的 access token
func (s *GeminiService) GetDecryptedAccessToken(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	if account.AccessToken == "" {
		return "", nil
	}

	return s.Decrypt(account.AccessToken)
}

// GetDecryptedAPIKey 获取解密后的 API Key
func (s *GeminiService) GetDecryptedAPIKey(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	if account.APIKey == "" {
		return "", nil
	}

	return s.Decrypt(account.APIKey)
}

// IsOAuthAccount 检查是否是 OAuth 账户
func (s *GeminiService) IsOAuthAccount(ctx context.Context, accountID string) (bool, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, fmt.Errorf("account not found")
	}

	return account.RefreshToken != "", nil
}

// IsTokenExpiring 检查 token 是否即将过期
func (s *GeminiService) IsTokenExpiring(ctx context.Context, accountID string) (bool, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, fmt.Errorf("account not found")
	}

	// API Key 模式不会过期
	if account.APIKey != "" && account.RefreshToken == "" {
		return false, nil
	}

	if account.TokenExpiry == nil {
		return false, nil
	}

	expiryWithBuffer := account.TokenExpiry.Add(-s.tokenRefreshBuffer)
	return time.Now().After(expiryWithBuffer), nil
}

// UpdateTokens 更新账户的 tokens
func (s *GeminiService) UpdateTokens(ctx context.Context, accountID, accessToken string, expiry *time.Time) error {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("account not found")
	}

	encrypted, err := s.Encrypt(accessToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt access token: %w", err)
	}

	account.AccessToken = encrypted
	account.TokenExpiry = expiry
	account.UpdatedAt = time.Now()
	account.LastError = ""
	account.ErrorCount = 0
	account.LastErrorAt = nil

	if err := s.redis.SetGeminiAccount(ctx, account); err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}

	logger.Info("Gemini account tokens updated",
		zap.String("accountId", accountID))

	return nil
}

// GetAccountsNeedingRefresh 获取需要刷新 token 的账户
func (s *GeminiService) GetAccountsNeedingRefresh(ctx context.Context) ([]*redis.GeminiAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var needRefresh []*redis.GeminiAccount
	now := time.Now()

	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		// 只检查 OAuth 账户
		if account.RefreshToken == "" {
			continue
		}

		if account.TokenExpiry != nil {
			expiryWithBuffer := account.TokenExpiry.Add(-s.tokenRefreshBuffer)
			if now.After(expiryWithBuffer) {
				needRefresh = append(needRefresh, account)
			}
		}
	}

	return needRefresh, nil
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *GeminiService) GetSchedulableAccounts(ctx context.Context, model string) ([]*redis.GeminiAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*redis.GeminiAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		// OAuth 账户检查 token 是否有效
		if account.RefreshToken != "" {
			if account.TokenExpiry != nil && time.Now().After(*account.TokenExpiry) {
				continue
			}
		}

		// API Key 账户检查是否有 API Key
		if account.RefreshToken == "" && account.APIKey == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *GeminiService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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

	if account.ProxyPassword != "" {
		decrypted, err := s.Decrypt(account.ProxyPassword)
		if err != nil {
			logger.Warn("Failed to decrypt proxy password", zap.Error(err))
		} else {
			config.Password = decrypted
		}
	}

	return config, nil
}

// UpdateAccount 更新账户信息
func (s *GeminiService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("account not found")
	}

	if name, ok := updates["name"].(string); ok {
		account.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		account.Description = description
	}
	if status, ok := updates["status"].(string); ok {
		account.Status = status
	}
	if projectID, ok := updates["projectId"].(string); ok {
		account.ProjectID = projectID
	}
	if region, ok := updates["region"].(string); ok {
		account.Region = region
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetGeminiAccount(ctx, account)
}
