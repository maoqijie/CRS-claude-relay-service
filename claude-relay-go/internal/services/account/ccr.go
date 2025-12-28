package account

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// CCRAccount CCR 账户结构
type CCRAccount struct {
	redis.BaseAccount

	// 凭据
	APIKey      string `json:"apiKey,omitempty"`      // 加密存储
	AccessToken string `json:"accessToken,omitempty"` // 加密存储

	// 端点配置
	BaseURL string `json:"baseUrl,omitempty"`
}

// CCRService CCR 账户服务
type CCRService struct {
	*BaseService
}

// NewCCRService 创建 CCR 账户服务
func NewCCRService(redisClient *redis.Client, encryptionKey string) *CCRService {
	return &CCRService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeCCR),
	}
}

// CCRAccountInput 创建 CCR 账户的输入
type CCRAccountInput struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	APIKey      string       `json:"apiKey,omitempty"`
	AccessToken string       `json:"accessToken,omitempty"`
	BaseURL     string       `json:"baseUrl,omitempty"`
	ProxyConfig *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 CCR 账户
func (s *CCRService) CreateAccount(ctx context.Context, input CCRAccountInput) (*CCRAccount, error) {
	accountID := fmt.Sprintf("ccr_%s", uuid.New().String())

	account := &CCRAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeCCR),
			CreatedAt:   time.Now(),
		},
		BaseURL: input.BaseURL,
	}

	// 加密敏感数据
	if input.APIKey != "" {
		encrypted, err := s.Encrypt(input.APIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt API key: %w", err)
		}
		account.APIKey = encrypted
	}

	if input.AccessToken != "" {
		encrypted, err := s.Encrypt(input.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt access token: %w", err)
		}
		account.AccessToken = encrypted
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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeCCR, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("CCR account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 CCR 账户
func (s *CCRService) GetAccount(ctx context.Context, accountID string) (*CCRAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeCCR, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account CCRAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 CCR 账户
func (s *CCRService) GetAllAccounts(ctx context.Context) ([]*CCRAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeCCR)
	if err != nil {
		return nil, err
	}

	accounts := make([]*CCRAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account CCRAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal CCR account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedCredentials 获取解密后的凭据
func (s *CCRService) GetDecryptedCredentials(ctx context.Context, accountID string) (apiKey, accessToken string, err error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", "", err
	}
	if account == nil {
		return "", "", fmt.Errorf("account not found")
	}

	if account.APIKey != "" {
		apiKey, err = s.Decrypt(account.APIKey)
		if err != nil {
			return "", "", fmt.Errorf("failed to decrypt API key: %w", err)
		}
	}

	if account.AccessToken != "" {
		accessToken, err = s.Decrypt(account.AccessToken)
		if err != nil {
			return "", "", fmt.Errorf("failed to decrypt access token: %w", err)
		}
	}

	return apiKey, accessToken, nil
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *CCRService) GetSchedulableAccounts(ctx context.Context, model string) ([]*CCRAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*CCRAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		// 需要至少有一种凭据
		if account.APIKey == "" && account.AccessToken == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *CCRService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *CCRService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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
	if baseURL, ok := updates["baseUrl"].(string); ok {
		account.BaseURL = baseURL
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetAccount(ctx, redis.AccountTypeCCR, accountID, account)
}
