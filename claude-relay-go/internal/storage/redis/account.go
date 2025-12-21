package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// AccountType 账户类型
type AccountType string

const (
	AccountTypeClaude          AccountType = "claude"
	AccountTypeClaudeConsole   AccountType = "claude-console"
	AccountTypeDroid           AccountType = "droid"
	AccountTypeOpenAI          AccountType = "openai"
	AccountTypeOpenAIResponses AccountType = "openai-responses"
	AccountTypeGemini          AccountType = "gemini"
	AccountTypeGeminiAPI       AccountType = "gemini-api"
	AccountTypeBedrock         AccountType = "bedrock"
	AccountTypeAzureOpenAI     AccountType = "azure-openai"
	AccountTypeCCR             AccountType = "ccr"
)

// getAccountPrefix 获取账户类型对应的 Redis 前缀
func getAccountPrefix(accountType AccountType) string {
	switch accountType {
	case AccountTypeClaude:
		return PrefixClaudeAccount
	case AccountTypeClaudeConsole:
		return PrefixClaudeConsoleAccount
	case AccountTypeDroid:
		return PrefixDroidAccount
	case AccountTypeOpenAI:
		return PrefixOpenAIAccount
	case AccountTypeOpenAIResponses:
		return PrefixOpenAIResponsesAccount
	case AccountTypeGemini:
		return PrefixGeminiAccount
	case AccountTypeGeminiAPI:
		return PrefixGeminiAPIAccount
	case AccountTypeBedrock:
		return PrefixBedrockAccount
	case AccountTypeAzureOpenAI:
		return PrefixAzureOpenAIAccount
	case AccountTypeCCR:
		return PrefixCCRAccount
	default:
		return "account:"
	}
}

// BaseAccount 基础账户结构（所有账户类型共用）
type BaseAccount struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"` // active, inactive, error
	AccountType string    `json:"accountType"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`

	// 代理配置
	ProxyEnabled  bool   `json:"proxyEnabled,omitempty"`
	ProxyHost     string `json:"proxyHost,omitempty"`
	ProxyPort     int    `json:"proxyPort,omitempty"`
	ProxyProtocol string `json:"proxyProtocol,omitempty"` // socks5, http
	ProxyUsername string `json:"proxyUsername,omitempty"`
	ProxyPassword string `json:"proxyPassword,omitempty"` // 加密存储

	// 错误信息
	LastError   string     `json:"lastError,omitempty"`
	ErrorCount  int        `json:"errorCount,omitempty"`
	LastErrorAt *time.Time `json:"lastErrorAt,omitempty"`

	// 过载状态
	IsOverloaded    bool       `json:"isOverloaded,omitempty"`
	OverloadedAt    *time.Time `json:"overloadedAt,omitempty"`
	OverloadedUntil *time.Time `json:"overloadedUntil,omitempty"`
}

// ClaudeAccount Claude 账户（官方 OAuth）
type ClaudeAccount struct {
	BaseAccount

	// OAuth 信息
	AccessToken  string     `json:"accessToken,omitempty"`  // 加密存储
	RefreshToken string     `json:"refreshToken,omitempty"` // 加密存储
	TokenExpiry  *time.Time `json:"tokenExpiry,omitempty"`
	Scopes       []string   `json:"scopes,omitempty"`

	// 会话信息
	SessionKey string `json:"sessionKey,omitempty"` // 加密存储
	OrgID      string `json:"orgId,omitempty"`

	// 限制
	ConcurrentLimit int `json:"concurrentLimit,omitempty"`
}

// GeminiAccount Gemini 账户
type GeminiAccount struct {
	BaseAccount

	// OAuth 信息（Google OAuth）
	AccessToken  string     `json:"accessToken,omitempty"`
	RefreshToken string     `json:"refreshToken,omitempty"`
	TokenExpiry  *time.Time `json:"tokenExpiry,omitempty"`
	ClientID     string     `json:"clientId,omitempty"`
	ClientSecret string     `json:"clientSecret,omitempty"` // 加密存储

	// API Key 模式
	APIKey string `json:"apiKey,omitempty"` // 加密存储

	// 项目信息
	ProjectID string `json:"projectId,omitempty"`
	Region    string `json:"region,omitempty"`
}

// BedrockAccount AWS Bedrock 账户
type BedrockAccount struct {
	BaseAccount

	// AWS 凭据
	AccessKeyID     string `json:"accessKeyId,omitempty"`     // 加密存储
	SecretAccessKey string `json:"secretAccessKey,omitempty"` // 加密存储
	SessionToken    string `json:"sessionToken,omitempty"`    // 加密存储

	// AWS 配置
	Region           string `json:"region,omitempty"`
	RoleARN          string `json:"roleArn,omitempty"`
	ExternalID       string `json:"externalId,omitempty"`
	ProfileName      string `json:"profileName,omitempty"`
	UseInstanceRole  bool   `json:"useInstanceRole,omitempty"`
	AssumeRoleTTL    int    `json:"assumeRoleTtl,omitempty"` // 秒

	// 模型配置
	DefaultModel string `json:"defaultModel,omitempty"`
}

// AzureOpenAIAccount Azure OpenAI 账户
type AzureOpenAIAccount struct {
	BaseAccount

	// Azure 凭据
	APIKey       string `json:"apiKey,omitempty"` // 加密存储
	Endpoint     string `json:"endpoint,omitempty"`
	DeploymentID string `json:"deploymentId,omitempty"`
	APIVersion   string `json:"apiVersion,omitempty"`

	// 资源信息
	ResourceName    string `json:"resourceName,omitempty"`
	SubscriptionID  string `json:"subscriptionId,omitempty"`
	ResourceGroup   string `json:"resourceGroup,omitempty"`
}

// ========== 通用账户操作 ==========

// SetAccount 保存账户（通用方法）
func (c *Client) SetAccount(ctx context.Context, accountType AccountType, accountID string, data interface{}) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	prefix := getAccountPrefix(accountType)
	key := prefix + accountID

	// 序列化为 JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	// 保存到 Redis
	if err := client.Set(ctx, key, jsonData, 0).Err(); err != nil {
		return fmt.Errorf("failed to save account: %w", err)
	}

	logger.Info("Account saved",
		zap.String("type", string(accountType)),
		zap.String("id", accountID))

	return nil
}

// GetAccountRaw 获取账户原始 JSON 数据（避免双重序列化）
func (c *Client) GetAccountRaw(ctx context.Context, accountType AccountType, accountID string) ([]byte, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	prefix := getAccountPrefix(accountType)
	key := prefix + accountID

	data, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil // 未找到
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	return []byte(data), nil
}

// GetAccount 获取账户（通用方法）
func (c *Client) GetAccount(ctx context.Context, accountType AccountType, accountID string) (map[string]interface{}, error) {
	data, err := c.GetAccountRaw(ctx, accountType, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}

	return result, nil
}

// DeleteAccount 删除账户
func (c *Client) DeleteAccount(ctx context.Context, accountType AccountType, accountID string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	prefix := getAccountPrefix(accountType)
	key := prefix + accountID

	_, err = client.Del(ctx, key).Result()
	return err
}

// AccountBatchSize 账户批量获取大小
const AccountBatchSize = 100

// AccountRawData 账户原始数据
type AccountRawData struct {
	ID   string
	Data []byte
}

// GetAllAccountsRaw 获取所有指定类型的账户原始 JSON 数据（避免双重序列化）
func (c *Client) GetAllAccountsRaw(ctx context.Context, accountType AccountType) ([]AccountRawData, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	prefix := getAccountPrefix(accountType)
	keys, err := c.ScanKeys(ctx, prefix+"*", 1000)
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []AccountRawData{}, nil
	}

	var results []AccountRawData

	// 使用 Pipeline 批量获取，分批处理避免一次性加载过多
	for offset := 0; offset < len(keys); offset += AccountBatchSize {
		end := offset + AccountBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		batchKeys := keys[offset:end]

		// 使用 Pipeline 批量获取
		pipe := client.Pipeline()
		cmds := make(map[string]*goredis.StringCmd)

		for _, key := range batchKeys {
			cmds[key] = pipe.Get(ctx, key)
		}

		_, err := pipe.Exec(ctx)
		if err != nil && err != goredis.Nil {
			logger.Warn("Failed to batch get accounts", zap.Error(err))
		}

		// 处理结果
		for key, cmd := range cmds {
			data, err := cmd.Result()
			if err != nil {
				continue
			}

			accountID := strings.TrimPrefix(key, prefix)
			results = append(results, AccountRawData{
				ID:   accountID,
				Data: []byte(data),
			})
		}
	}

	return results, nil
}

// GetAllAccounts 获取所有指定类型的账户（复用 GetAllAccountsRaw 避免代码重复）
func (c *Client) GetAllAccounts(ctx context.Context, accountType AccountType) ([]map[string]interface{}, error) {
	rawData, err := c.GetAllAccountsRaw(ctx, accountType)
	if err != nil {
		return nil, err
	}

	accounts := make([]map[string]interface{}, 0, len(rawData))
	for _, raw := range rawData {
		var account map[string]interface{}
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account["id"] = raw.ID
		accounts = append(accounts, account)
	}

	return accounts, nil
}

// ========== Claude 账户专用方法 ==========

// SetClaudeAccount 保存 Claude 账户
func (c *Client) SetClaudeAccount(ctx context.Context, account *ClaudeAccount) error {
	return c.SetAccount(ctx, AccountTypeClaude, account.ID, account)
}

// GetClaudeAccount 获取 Claude 账户
func (c *Client) GetClaudeAccount(ctx context.Context, accountID string) (*ClaudeAccount, error) {
	data, err := c.GetAccountRaw(ctx, AccountTypeClaude, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account ClaudeAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllClaudeAccounts 获取所有 Claude 账户
func (c *Client) GetAllClaudeAccounts(ctx context.Context) ([]*ClaudeAccount, error) {
	rawData, err := c.GetAllAccountsRaw(ctx, AccountTypeClaude)
	if err != nil {
		return nil, err
	}

	accounts := make([]*ClaudeAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account ClaudeAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Claude account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// ========== Gemini 账户专用方法 ==========

// SetGeminiAccount 保存 Gemini 账户
func (c *Client) SetGeminiAccount(ctx context.Context, account *GeminiAccount) error {
	return c.SetAccount(ctx, AccountTypeGemini, account.ID, account)
}

// GetGeminiAccount 获取 Gemini 账户
func (c *Client) GetGeminiAccount(ctx context.Context, accountID string) (*GeminiAccount, error) {
	data, err := c.GetAccountRaw(ctx, AccountTypeGemini, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account GeminiAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllGeminiAccounts 获取所有 Gemini 账户
func (c *Client) GetAllGeminiAccounts(ctx context.Context) ([]*GeminiAccount, error) {
	rawData, err := c.GetAllAccountsRaw(ctx, AccountTypeGemini)
	if err != nil {
		return nil, err
	}

	accounts := make([]*GeminiAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account GeminiAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Gemini account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// ========== Bedrock 账户专用方法 ==========

// SetBedrockAccount 保存 Bedrock 账户
func (c *Client) SetBedrockAccount(ctx context.Context, account *BedrockAccount) error {
	return c.SetAccount(ctx, AccountTypeBedrock, account.ID, account)
}

// GetBedrockAccount 获取 Bedrock 账户
func (c *Client) GetBedrockAccount(ctx context.Context, accountID string) (*BedrockAccount, error) {
	data, err := c.GetAccountRaw(ctx, AccountTypeBedrock, accountID)
	if err != nil || data == nil {
		return nil, err
	}

	var account BedrockAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.ID = accountID

	return &account, nil
}

// GetAllBedrockAccounts 获取所有 Bedrock 账户
func (c *Client) GetAllBedrockAccounts(ctx context.Context) ([]*BedrockAccount, error) {
	rawData, err := c.GetAllAccountsRaw(ctx, AccountTypeBedrock)
	if err != nil {
		return nil, err
	}

	accounts := make([]*BedrockAccount, 0, len(rawData))
	for _, raw := range rawData {
		var account BedrockAccount
		if err := json.Unmarshal(raw.Data, &account); err != nil {
			logger.Warn("Failed to unmarshal Bedrock account", zap.String("id", raw.ID), zap.Error(err))
			continue
		}
		account.ID = raw.ID
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// ========== 账户状态管理 ==========

// UpdateAccountStatus 更新账户状态
func (c *Client) UpdateAccountStatus(ctx context.Context, accountType AccountType, accountID, status string) error {
	data, err := c.GetAccount(ctx, accountType, accountID)
	if err != nil || data == nil {
		return fmt.Errorf("account not found")
	}

	data["status"] = status
	data["updatedAt"] = time.Now().Format(time.RFC3339)

	return c.SetAccount(ctx, accountType, accountID, data)
}

// SetAccountError 设置账户错误状态
func (c *Client) SetAccountError(ctx context.Context, accountType AccountType, accountID, errorMsg string) error {
	data, err := c.GetAccount(ctx, accountType, accountID)
	if err != nil || data == nil {
		return fmt.Errorf("account not found")
	}

	data["lastError"] = errorMsg
	data["lastErrorAt"] = time.Now().Format(time.RFC3339)

	errorCount := 0
	if count, ok := data["errorCount"].(float64); ok {
		errorCount = int(count)
	}
	data["errorCount"] = errorCount + 1

	return c.SetAccount(ctx, accountType, accountID, data)
}

// ClearAccountError 清除账户错误状态
func (c *Client) ClearAccountError(ctx context.Context, accountType AccountType, accountID string) error {
	data, err := c.GetAccount(ctx, accountType, accountID)
	if err != nil || data == nil {
		return fmt.Errorf("account not found")
	}

	delete(data, "lastError")
	delete(data, "lastErrorAt")
	data["errorCount"] = 0
	data["updatedAt"] = time.Now().Format(time.RFC3339)

	return c.SetAccount(ctx, accountType, accountID, data)
}

// SetAccountOverloaded 设置账户过载状态
func (c *Client) SetAccountOverloaded(ctx context.Context, accountType AccountType, accountID string, duration time.Duration) error {
	data, err := c.GetAccount(ctx, accountType, accountID)
	if err != nil || data == nil {
		return fmt.Errorf("account not found")
	}

	now := time.Now()
	data["isOverloaded"] = true
	data["overloadedAt"] = now.Format(time.RFC3339)
	data["overloadedUntil"] = now.Add(duration).Format(time.RFC3339)
	data["updatedAt"] = now.Format(time.RFC3339)

	return c.SetAccount(ctx, accountType, accountID, data)
}

// ClearAccountOverloaded 清除账户过载状态
func (c *Client) ClearAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) error {
	data, err := c.GetAccount(ctx, accountType, accountID)
	if err != nil || data == nil {
		return fmt.Errorf("account not found")
	}

	data["isOverloaded"] = false
	delete(data, "overloadedAt")
	delete(data, "overloadedUntil")
	data["updatedAt"] = time.Now().Format(time.RFC3339)

	return c.SetAccount(ctx, accountType, accountID, data)
}

// GetActiveAccounts 获取所有活跃账户（指定类型）
func (c *Client) GetActiveAccounts(ctx context.Context, accountType AccountType) ([]map[string]interface{}, error) {
	accounts, err := c.GetAllAccounts(ctx, accountType)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var active []map[string]interface{}

	for _, account := range accounts {
		// 检查状态
		status, _ := account["status"].(string)
		if status != "active" {
			continue
		}

		// 检查过载状态
		if isOverloaded, ok := account["isOverloaded"].(bool); ok && isOverloaded {
			// 检查过载是否已过期
			if overloadedUntil, ok := account["overloadedUntil"].(string); ok {
				if t, err := time.Parse(time.RFC3339, overloadedUntil); err == nil {
					if now.Before(t) {
						continue // 仍在过载期内
					}
				}
			}
		}

		active = append(active, account)
	}

	return active, nil
}
