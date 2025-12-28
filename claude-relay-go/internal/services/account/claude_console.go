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

// ClaudeConsoleAccount Claude Console 账户结构
type ClaudeConsoleAccount struct {
	redis.BaseAccount

	// 会话凭据
	SessionKey string `json:"sessionKey,omitempty"` // 加密存储
	OrgID      string `json:"orgId,omitempty"`

	// Cookie（如需要）
	Cookie string `json:"cookie,omitempty"` // 加密存储

	// 限制
	ConcurrentLimit int `json:"concurrentLimit,omitempty"`
}

// ClaudeConsoleService Claude Console 账户服务
type ClaudeConsoleService struct {
	*BaseService
}

// NewClaudeConsoleService 创建 Claude Console 账户服务
func NewClaudeConsoleService(redisClient *redis.Client, encryptionKey string) *ClaudeConsoleService {
	return &ClaudeConsoleService{
		BaseService: NewBaseService(redisClient, encryptionKey, redis.AccountTypeClaudeConsole),
	}
}

// ClaudeConsoleAccountInput 创建 Claude Console 账户的输入
type ClaudeConsoleAccountInput struct {
	Name            string       `json:"name"`
	Description     string       `json:"description,omitempty"`
	SessionKey      string       `json:"sessionKey,omitempty"`
	OrgID           string       `json:"orgId,omitempty"`
	Cookie          string       `json:"cookie,omitempty"`
	ConcurrentLimit int          `json:"concurrentLimit,omitempty"`
	ProxyConfig     *ProxyConfig `json:"proxyConfig,omitempty"`
}

// CreateAccount 创建 Claude Console 账户
func (s *ClaudeConsoleService) CreateAccount(ctx context.Context, input ClaudeConsoleAccountInput) (*ClaudeConsoleAccount, error) {
	accountID := fmt.Sprintf("claude_console_%s", uuid.New().String())

	account := &ClaudeConsoleAccount{
		BaseAccount: redis.BaseAccount{
			ID:          accountID,
			Name:        input.Name,
			Description: input.Description,
			Status:      StatusActive,
			AccountType: string(redis.AccountTypeClaudeConsole),
			CreatedAt:   time.Now(),
		},
		OrgID:           input.OrgID,
		ConcurrentLimit: input.ConcurrentLimit,
	}

	// 加密敏感数据
	if input.SessionKey != "" {
		encrypted, err := s.Encrypt(input.SessionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt session key: %w", err)
		}
		account.SessionKey = encrypted
	}

	if input.Cookie != "" {
		encrypted, err := s.Encrypt(input.Cookie)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt cookie: %w", err)
		}
		account.Cookie = encrypted
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

	if err := s.redis.SetAccount(ctx, redis.AccountTypeClaudeConsole, accountID, account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Claude Console account created",
		zap.String("accountId", accountID),
		zap.String("name", input.Name))

	return account, nil
}

// GetAccount 获取 Claude Console 账户
func (s *ClaudeConsoleService) GetAccount(ctx context.Context, accountID string) (*ClaudeConsoleAccount, error) {
	data, err := s.redis.GetAccountRaw(ctx, redis.AccountTypeClaudeConsole, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account ClaudeConsoleAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllAccounts 获取所有 Claude Console 账户
func (s *ClaudeConsoleService) GetAllAccounts(ctx context.Context) ([]*ClaudeConsoleAccount, error) {
	rawData, err := s.redis.GetAllAccountsRaw(ctx, redis.AccountTypeClaudeConsole)
	if err != nil {
		return nil, err
	}

	accounts := make([]*ClaudeConsoleAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account ClaudeConsoleAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Claude Console account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// GetDecryptedSessionKey 获取解密后的会话密钥
func (s *ClaudeConsoleService) GetDecryptedSessionKey(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	if account.SessionKey == "" {
		return "", nil
	}

	return s.Decrypt(account.SessionKey)
}

// GetDecryptedCookie 获取解密后的 Cookie
func (s *ClaudeConsoleService) GetDecryptedCookie(ctx context.Context, accountID string) (string, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", fmt.Errorf("account not found")
	}

	if account.Cookie == "" {
		return "", nil
	}

	return s.Decrypt(account.Cookie)
}

// GetSchedulableAccounts 获取可调度的账户列表
func (s *ClaudeConsoleService) GetSchedulableAccounts(ctx context.Context, model string) ([]*ClaudeConsoleAccount, error) {
	accounts, err := s.GetAllAccounts(ctx)
	if err != nil {
		return nil, err
	}

	var schedulable []*ClaudeConsoleAccount
	for _, account := range accounts {
		if account.Status != StatusActive {
			continue
		}

		if account.IsOverloaded {
			if account.OverloadedUntil != nil && time.Now().Before(*account.OverloadedUntil) {
				continue
			}
		}

		// 需要会话凭据
		if account.SessionKey == "" && account.Cookie == "" {
			continue
		}

		schedulable = append(schedulable, account)
	}

	return schedulable, nil
}

// GetProxyConfig 获取账户的代理配置
func (s *ClaudeConsoleService) GetProxyConfig(ctx context.Context, accountID string) (*ProxyConfig, error) {
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
func (s *ClaudeConsoleService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) error {
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
	if orgID, ok := updates["orgId"].(string); ok {
		account.OrgID = orgID
	}
	if concurrentLimit, ok := updates["concurrentLimit"].(float64); ok {
		account.ConcurrentLimit = int(concurrentLimit)
	}

	account.UpdatedAt = time.Now()

	return s.redis.SetAccount(ctx, redis.AccountTypeClaudeConsole, accountID, account)
}
