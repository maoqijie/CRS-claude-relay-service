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

// OpenAIAccount OpenAI 账户结构
type OpenAIAccount struct {
	redis.BaseAccount

	// API Key
	APIKey string `json:"apiKey,omitempty"` // 加密存储

	// 组织配置
	OrganizationID string `json:"organizationId,omitempty"`

	// 基础 URL（用于自定义端点）
	BaseURL string `json:"baseUrl,omitempty"`
}

// OpenAIService OpenAI 账户服务
type OpenAIService struct {
	*BaseService
}

// NewOpenAIService 创建 OpenAI 账户服务
func NewOpenAIService(redisClient *redis.Client, encryptionKey string) *OpenAIService {
	return &OpenAIService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeOpenAI),
	}
}

// OpenAIAccountInput 创建 OpenAI 账户的输入
type OpenAIAccountInput struct {
	Name           string       `json:"name"`
	Description    string       `json:"description,omitempty"`
	APIKey         string       `json:"apiKey"`
	OrganizationID string       `json:"organizationId,omitempty"`
	BaseURL        string       `json:"baseUrl,omitempty"`
	ProxyConfig    *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 OpenAI 账户
func (s *OpenAIService) CreateAccount(ctx context.Context, input OpenAIAccountInput) (*OpenAIAccount, error) {
	accountID := fmt.Sprintf("openai_%s", uuid.New().String())

	encryptedAPIKey, err := s.Encrypt(input.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	account := &OpenAIAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeOpenAI),
			CreatedAt:   time.Now(),
		},
		APIKey:         encryptedAPIKey,
		OrganizationID: input.OrganizationID,
		BaseURL:        input.BaseURL,
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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeOpenAI, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("OpenAI account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 OpenAI 账户
func (s *OpenAIService) GetAccount(ctx context.Context, accountID string) (*OpenAIAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeOpenAI, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account OpenAIAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 OpenAI 账户
func (s *OpenAIService) GetAllAccounts(ctx context.Context) ([]*OpenAIAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeOpenAI)
	if err != nil {
		return nil, err
	}

	accounts := make([]*OpenAIAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account OpenAIAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal OpenAI account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedAPIKey 获取解密后的 API Key
func (s *OpenAIService) GetDecryptedAPIKey(ctx context.Context, accountID string) (string, error) {
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
func (s *OpenAIService) GetSchedulableAccounts(ctx context.Context, model string) ([]*OpenAIAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*OpenAIAccount
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
func (s *OpenAIService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *OpenAIService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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
	if orgID, ok := updates["organizationId"].(string); ok {
		account.OrganizationID = orgID
	}
	if baseURL, ok := updates["baseUrl"].(string); ok {
		account.BaseURL = baseURL
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetAccount(ctx, redis.AccountTypeOpenAI, accountID, account)
}

// ========== OpenAI Responses 账户服务 ==========

// OpenAIResponsesAccount OpenAI Responses 账户结构
type OpenAIResponsesAccount struct {
	redis.BaseAccount

	// API Key
	APIKey string `json:"apiKey,omitempty"`

	// 组织配置
	OrganizationID string `json:"organizationId,omitempty"`
}

// OpenAIResponsesService OpenAI Responses 账户服务
type OpenAIResponsesService struct {
	*BaseService
}

// NewOpenAIResponsesService 创建 OpenAI Responses 账户服务
func NewOpenAIResponsesService(redisClient *redis.Client, encryptionKey string) *OpenAIResponsesService {
	return &OpenAIResponsesService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeOpenAIResponses),
	}
}

// OpenAIResponsesAccountInput 创建 OpenAI Responses 账户的输入
type OpenAIResponsesAccountInput struct {
	Name           string       `json:"name"`
	Description    string       `json:"description,omitempty"`
	APIKey         string       `json:"apiKey"`
	OrganizationID string       `json:"organizationId,omitempty"`
	ProxyConfig    *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 OpenAI Responses 账户
func (s *OpenAIResponsesService) CreateAccount(ctx context.Context, input OpenAIResponsesAccountInput) (*OpenAIResponsesAccount, error) {
	accountID := fmt.Sprintf("openai_responses_%s", uuid.New().String())

	encryptedAPIKey, err := s.Encrypt(input.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	account := &OpenAIResponsesAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeOpenAIResponses),
			CreatedAt:   time.Now(),
		},
		APIKey:         encryptedAPIKey,
		OrganizationID: input.OrganizationID,
	}

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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeOpenAIResponses, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("OpenAI Responses account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 OpenAI Responses 账户
func (s *OpenAIResponsesService) GetAccount(ctx context.Context, accountID string) (*OpenAIResponsesAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeOpenAIResponses, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account OpenAIResponsesAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 OpenAI Responses 账户
func (s *OpenAIResponsesService) GetAllAccounts(ctx context.Context) ([]*OpenAIResponsesAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeOpenAIResponses)
	if err != nil {
		return nil, err
	}

	accounts := make([]*OpenAIResponsesAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account OpenAIResponsesAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal OpenAI Responses account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedAPIKey 获取解密后的 API Key
func (s *OpenAIResponsesService) GetDecryptedAPIKey(ctx context.Context, accountID string) (string, error) {
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
func (s *OpenAIResponsesService) GetSchedulableAccounts(ctx context.Context, model string) ([]*OpenAIResponsesAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*OpenAIResponsesAccount
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
