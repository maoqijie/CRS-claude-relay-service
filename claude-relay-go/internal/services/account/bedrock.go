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

// BedrockService AWS Bedrock 账户服务
type BedrockService struct {
	*BaseService
}

// NewBedrockService 创建 Bedrock 账户服务
func NewBedrockService(redisClient *redis.Client, encryptionKey string) *BedrockService {
	return &BedrockService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeBedrock),
	}
}

// BedrockAccountInput 创建 Bedrock 账户的输入
type BedrockAccountInput struct {
	Name             string       `json:"name"`
	Description      string       `json:"description,omitempty"`
	AccessKeyID      string       `json:"accessKeyId,omitempty"`
	SecretAccessKey  string       `json:"secretAccessKey,omitempty"`
	SessionToken     string       `json:"sessionToken,omitempty"`
	Region           string       `json:"region,omitempty"`
	RoleARN          string       `json:"roleArn,omitempty"`
	ExternalID       string       `json:"externalId,omitempty"`
	ProfileName      string       `json:"profileName,omitempty"`
	UseInstanceRole  bool         `json:"useInstanceRole,omitempty"`
	AssumeRoleTTL    int          `json:"assumeRoleTtl,omitempty"`
	DefaultModel     string       `json:"defaultModel,omitempty"`
	ProxyConfig      *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 Bedrock 账户
func (s *BedrockService) CreateAccount(ctx context.Context, input BedrockAccountInput) (*redis.BedrockAccount, error) {
	accountID := fmt.Sprintf("bedrock_%s", uuid.New().String())

	account := &redis.BedrockAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeBedrock),
			CreatedAt:   time.Now(),
		},
		Region:          input.Region,
		RoleARN:         input.RoleARN,
		ExternalID:      input.ExternalID,
		ProfileName:     input.ProfileName,
		UseInstanceRole: input.UseInstanceRole,
		AssumeRoleTTL:   input.AssumeRoleTTL,
		DefaultModel:    input.DefaultModel,
	}

	// 加密敏感数据
	if input.AccessKeyID != "" {
		encrypted, err := s.Encrypt(input.AccessKeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt access key ID: %w", err)
		}
		account.AccessKeyID = encrypted
	}

	if input.SecretAccessKey != "" {
		encrypted, err := s.Encrypt(input.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt secret access key: %w", err)
		}
		account.SecretAccessKey = encrypted
	}

	if input.SessionToken != "" {
		encrypted, err := s.Encrypt(input.SessionToken)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt session token: %w", err)
		}
		account.SessionToken = encrypted
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

	if err := s.redis.SetBedrockAccount(ctx, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Bedrock account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name),
		zap.String("region", input.Region))

	return account, nil
}

// GetAccount 获取 Bedrock 账户
func (s *BedrockService) GetAccount(ctx context.Context, accountID string) (*redis.BedrockAccount, error) {
	return s.redis.GetBedrockAccount(ctx, accountID)
}

// GetAllAccounts 获取所有 Bedrock 账户
func (s *BedrockService) GetAllAccounts(ctx context.Context) ([]*redis.BedrockAccount, error) {
	return s.redis.GetAllBedrockAccounts(ctx)
}

// GetDecryptedCredentials 获取解密后的凭据
func (s *BedrockService) GetDecryptedCredentials(ctx context.Context, accountID string) (accessKeyID, secretAccessKey, sessionToken string, err error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", "", "", err
	}
	if account == nil {
		return "", "", "", fmt.Errorf("account not found")
	}

	if account.AccessKeyID != "" {
		accessKeyID, err = s.Decrypt(account.AccessKeyID)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to decrypt access key ID: %w", err)
		}
	}

	if account.SecretAccessKey != "" {
		secretAccessKey, err = s.Decrypt(account.SecretAccessKey)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to decrypt secret access key: %w", err)
		}
	}

	if account.SessionToken != "" {
		sessionToken, err = s.Decrypt(account.SessionToken)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to decrypt session token: %w", err)
		}
	}

	return accessKeyID, secretAccessKey, sessionToken, nil
}

// IsUsingInstanceRole 检查是否使用实例角色
func (s *BedrockService) IsUsingInstanceRole(ctx context.Context, accountID string) (bool, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, fmt.Errorf("account not found")
	}

	return account.UseInstanceRole, nil
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *BedrockService) GetSchedulableAccounts(ctx context.Context, model string) ([]*redis.BedrockAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*redis.BedrockAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		// 检查是否有有效的凭据或使用实例角色
		if !account.UseInstanceRole && account.AccessKeyID == "" && account.RoleARN == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *BedrockService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *BedrockService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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
	if region, ok := updates["region"].(string); ok {
		account.Region = region
	}
	if defaultModel, ok := updates["defaultModel"].(string); ok {
		account.DefaultModel = defaultModel
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetBedrockAccount(ctx, account)
}
