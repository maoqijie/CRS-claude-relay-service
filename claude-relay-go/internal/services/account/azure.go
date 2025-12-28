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

// AzureOpenAIService Azure OpenAI 账户服务
type AzureOpenAIService struct {
	*BaseService
}

// NewAzureOpenAIService 创建 Azure OpenAI 账户服务
func NewAzureOpenAIService(redisClient *redis.Client, encryptionKey string) *AzureOpenAIService {
	return &AzureOpenAIService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeAzureOpenAI),
	}
}

// AzureOpenAIAccountInput 创建 Azure OpenAI 账户的输入
type AzureOpenAIAccountInput struct {
	Name            string       `json:"name"`
	Description     string       `json:"description,omitempty"`
	APIKey          string       `json:"apiKey"`
	Endpoint        string       `json:"endpoint"`
	DeploymentID    string       `json:"deploymentId,omitempty"`
	APIVersion      string       `json:"apiVersion,omitempty"`
	ResourceName    string       `json:"resourceName,omitempty"`
	SubscriptionID  string       `json:"subscriptionId,omitempty"`
	ResourceGroup   string       `json:"resourceGroup,omitempty"`
	ProxyConfig     *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 Azure OpenAI 账户
func (s *AzureOpenAIService) CreateAccount(ctx context.Context, input AzureOpenAIAccountInput) (*redis.AzureOpenAIAccount, error) {
	accountID := fmt.Sprintf("azure_openai_%s", uuid.New().String())

	encryptedAPIKey, err := s.Encrypt(input.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	// 默认 API 版本
	apiVersion := input.APIVersion
	if apiVersion == "" {
		apiVersion = "2024-02-01"
	}

	account := &redis.AzureOpenAIAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeAzureOpenAI),
			CreatedAt:   time.Now(),
		},
		APIKey:         encryptedAPIKey,
		Endpoint:       input.Endpoint,
		DeploymentID:   input.DeploymentID,
		APIVersion:     apiVersion,
		ResourceName:   input.ResourceName,
		SubscriptionID: input.SubscriptionID,
		ResourceGroup:  input.ResourceGroup,
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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeAzureOpenAI, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Azure OpenAI account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name),
		zap.String("endpoint", input.Endpoint))

	return account, nil
}

// GetAccount 获取 Azure OpenAI 账户
func (s *AzureOpenAIService) GetAccount(ctx context.Context, accountID string) (*redis.AzureOpenAIAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeAzureOpenAI, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account redis.AzureOpenAIAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 Azure OpenAI 账户
func (s *AzureOpenAIService) GetAllAccounts(ctx context.Context) ([]*redis.AzureOpenAIAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeAzureOpenAI)
	if err != nil {
		return nil, err
	}

	accounts := make([]*redis.AzureOpenAIAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account redis.AzureOpenAIAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Azure OpenAI account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedAPIKey 获取解密后的 API Key
func (s *AzureOpenAIService) GetDecryptedAPIKey(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	return s.Decrypt(account.APIKey)
}

// GetEndpointURL 获取完整的端点 URL
func (s *AzureOpenAIService) GetEndpointURL(ctx context.Context, accountID, deploymentName string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	deployment := deploymentName
	if deployment == "" {
		deployment = account.DeploymentID
	}

	// 构建完整的端点 URL
	// 格式: https://{resource-name}.openai.azure.com/openai/deployments/{deployment-id}/chat/completions?api-version={api-version}
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		account.Endpoint, deployment, account.APIVersion), nil
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *AzureOpenAIService) GetSchedulableAccounts(ctx context.Context, model string) ([]*redis.AzureOpenAIAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*redis.AzureOpenAIAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		if account.APIKey == "" || account.Endpoint == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *AzureOpenAIService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *AzureOpenAIService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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
	if endpoint, ok := updates["endpoint"].(string); ok {
		account.Endpoint = endpoint
	}
	if deploymentID, ok := updates["deploymentId"].(string); ok {
		account.DeploymentID = deploymentID
	}
	if apiVersion, ok := updates["apiVersion"].(string); ok {
		account.APIVersion = apiVersion
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetAccount(ctx, redis.AccountTypeAzureOpenAI, accountID, account)
}
