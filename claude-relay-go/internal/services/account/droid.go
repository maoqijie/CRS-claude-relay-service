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

// DroidAccount Droid (Factory.ai) 账户结构
type DroidAccount struct {
	redis.BaseAccount

	// API Key
	APIKey string `json:"apiKey,omitempty"` // 加密存储

	// 端点配置
	BaseURL string `json:"baseUrl,omitempty"`
}

// DroidService Droid 账户服务
type DroidService struct {
	*BaseService
}

// NewDroidService 创建 Droid 账户服务
func NewDroidService(redisClient *redis.Client, encryptionKey string) *DroidService {
	return &DroidService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeDroid),
	}
}

// DroidAccountInput 创建 Droid 账户的输入
type DroidAccountInput struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	APIKey      string       `json:"apiKey"`
	BaseURL     string       `json:"baseUrl,omitempty"`
	ProxyConfig *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 Droid 账户
func (s *DroidService) CreateAccount(ctx context.Context, input DroidAccountInput) (*DroidAccount, error) {
	accountID := fmt.Sprintf("droid_%s", uuid.New().String())

	encryptedAPIKey, err := s.Encrypt(input.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	// 默认端点
	baseURL := input.BaseURL
	if baseURL == "" {
		baseURL = "https://api.factory.ai"
	}

	account := &DroidAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeDroid),
			CreatedAt:   time.Now(),
		},
		APIKey:  encryptedAPIKey,
		BaseURL: baseURL,
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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeDroid, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Droid account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 Droid 账户
func (s *DroidService) GetAccount(ctx context.Context, accountID string) (*DroidAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeDroid, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account DroidAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 Droid 账户
func (s *DroidService) GetAllAccounts(ctx context.Context) ([]*DroidAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeDroid)
	if err != nil {
		return nil, err
	}

	accounts := make([]*DroidAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account DroidAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Droid account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedAPIKey 获取解密后的 API Key
func (s *DroidService) GetDecryptedAPIKey(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	return s.Decrypt(account.APIKey)
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *DroidService) GetSchedulableAccounts(ctx context.Context, model string) ([]*DroidAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*DroidAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		if account.APIKey == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *DroidService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *DroidService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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

	return s.redis.SetAccount(ctx, redis.AccountTypeDroid, accountID, account)
}
