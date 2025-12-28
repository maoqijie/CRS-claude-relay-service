package account

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
	"golang.org/x/crypto/pbkdf2"
)

// AccountStatus 账户状态常量
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
	StatusError    = "error"
	StatusDisabled = "disabled"
)

// BaseService 基础账户服务
type BaseService struct {
	redis         *redis.Client
	encryptionKey []byte
	accountType   redis.AccountType
}

// 密钥派生常量
const (
	// pbkdf2Iterations PBKDF2 迭代次数
	pbkdf2Iterations = 100000
	// pbkdf2KeyLen 派生密钥长度（AES-256 需要 32 字节）
	pbkdf2KeyLen = 32
)

// 固定盐值（用于密钥派生，确保相同密码生成相同密钥）
// 注意：在生产环境中，这个盐值应该从配置或环境变量读取
var pbkdf2Salt = []byte("claude-relay-service-encryption-salt")

// NewBaseService 创建基础账户服务
func NewBaseService(redisClient *redis.Client, encryptionKey string, accountType redis.AccountType) *BaseService {
	var key []byte
	if encryptionKey != "" {
		// 使用 PBKDF2 派生固定长度的加密密钥
		// 这比简单的填充/截断更安全，能够抵抗弱密码攻击
		key = pbkdf2.Key([]byte(encryptionKey), pbkdf2Salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
	}

	return &BaseService{
		redis:         redisClient,
		encryptionKey: key,
		accountType:   accountType,
	}
}

// Encrypt 加密敏感数据
func (s *BaseService) Encrypt(plaintext string) (string, error) {
	if s.encryptionKey == nil || len(s.encryptionKey) == 0 {
		return plaintext, nil // 未配置加密密钥时直接返回
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密敏感数据
func (s *BaseService) Decrypt(ciphertext string) (string, error) {
	if s.encryptionKey == nil || len(s.encryptionKey) == 0 {
		return ciphertext, nil // 未配置加密密钥时直接返回
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// IsAccountActive 检查账户是否活跃
func (s *BaseService) IsAccountActive(ctx context.Context, accountID string) (bool, error) {
	account, err := s.redis.GetAccount(ctx, s.accountType, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, nil
	}

	status, _ := account["status"].(string)
	return status == StatusActive, nil
}

// IsAccountOverloaded 检查账户是否过载
func (s *BaseService) IsAccountOverloaded(ctx context.Context, accountID string) (bool, error) {
	account, err := s.redis.GetAccount(ctx, s.accountType, accountID)
	if err != nil {
		return false, err
	}
	if account == nil {
		return false, nil
	}

	isOverloaded, _ := account["isOverloaded"].(bool)
	if !isOverloaded {
		return false, nil
	}

	// 检查过载是否已过期
	if overloadedUntil, ok := account["overloadedUntil"].(string); ok {
		if t, err := time.Parse(time.RFC3339, overloadedUntil); err == nil {
			if time.Now().After(t) {
				// 过载已过期，自动清除
				go s.ClearOverloaded(context.Background(), accountID)
				return false, nil
			}
		}
	}

	return true, nil
}

// SetOverloaded 设置账户过载状态
func (s *BaseService) SetOverloaded(ctx context.Context, accountID string, duration time.Duration) error {
	err := s.redis.SetAccountOverloaded(ctx, s.accountType, accountID, duration)
	if err != nil {
		logger.Error("Failed to set account overloaded",
			zap.String("accountType", string(s.accountType)),
			zap.String("accountId", accountID),
			zap.Error(err))
		return err
	}

	logger.Warn("Account marked as overloaded",
		zap.String("accountType", string(s.accountType)),
		zap.String("accountId", accountID),
		zap.Duration("duration", duration))

	return nil
}

// ClearOverloaded 清除账户过载状态
func (s *BaseService) ClearOverloaded(ctx context.Context, accountID string) error {
	err := s.redis.ClearAccountOverloaded(ctx, s.accountType, accountID)
	if err != nil {
		logger.Error("Failed to clear account overloaded",
			zap.String("accountType", string(s.accountType)),
			zap.String("accountId", accountID),
			zap.Error(err))
		return err
	}

	logger.Info("Account overloaded status cleared",
		zap.String("accountType", string(s.accountType)),
		zap.String("accountId", accountID))

	return nil
}

// SetError 设置账户错误状态
func (s *BaseService) SetError(ctx context.Context, accountID, errorMsg string) error {
	return s.redis.SetAccountError(ctx, s.accountType, accountID, errorMsg)
}

// ClearError 清除账户错误状态
func (s *BaseService) ClearError(ctx context.Context, accountID string) error {
	return s.redis.ClearAccountError(ctx, s.accountType, accountID)
}

// UpdateStatus 更新账户状态
func (s *BaseService) UpdateStatus(ctx context.Context, accountID, status string) error {
	return s.redis.UpdateAccountStatus(ctx, s.accountType, accountID, status)
}

// GetActiveAccounts 获取所有活跃账户
func (s *BaseService) GetActiveAccounts(ctx context.Context) ([]map[string]interface{}, error) {
	return s.redis.GetActiveAccounts(ctx, s.accountType)
}

// GetAccountCount 获取账户总数
func (s *BaseService) GetAccountCount(ctx context.Context) (int, error) {
	accounts, err := s.redis.GetAllAccounts(ctx, s.accountType)
	if err != nil {
		return 0, err
	}
	return len(accounts), nil
}

// GetActiveAccountCount 获取活跃账户数
func (s *BaseService) GetActiveAccountCount(ctx context.Context) (int, error) {
	accounts, err := s.GetActiveAccounts(ctx)
	if err != nil {
		return 0, err
	}
	return len(accounts), nil
}

// DeleteAccount 删除账户
func (s *BaseService) DeleteAccount(ctx context.Context, accountID string) error {
	return s.redis.DeleteAccount(ctx, s.accountType, accountID)
}

// AccountInfo 账户基本信息（用于列表展示）
type AccountInfo struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	AccountType string     `json:"accountType"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	ErrorCount  int        `json:"errorCount,omitempty"`
	IsOverloaded bool      `json:"isOverloaded,omitempty"`
}

// GetAllAccountsInfo 获取所有账户信息（用于列表展示）
func (s *BaseService) GetAllAccountsInfo(ctx context.Context) ([]AccountInfo, error) {
	accounts, err := s.redis.GetAllAccounts(ctx, s.accountType)
	if err != nil {
		return nil, err
	}

	result := make([]AccountInfo, 0, len(accounts))
	for _, account := range accounts {
		info := AccountInfo{
			AccountType: string(s.accountType),
		}

		if id, ok := account["id"].(string); ok {
			info.ID = id
		}
		if name, ok := account["name"].(string); ok {
			info.Name = name
		}
		if status, ok := account["status"].(string); ok {
			info.Status = status
		}
		if createdAt, ok := account["createdAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
				info.CreatedAt = t
			}
		}
		if updatedAt, ok := account["updatedAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
				info.UpdatedAt = &t
			}
		}
		if lastError, ok := account["lastError"].(string); ok {
			info.LastError = lastError
		}
		if errorCount, ok := account["errorCount"].(float64); ok {
			info.ErrorCount = int(errorCount)
		}
		if isOverloaded, ok := account["isOverloaded"].(bool); ok {
			info.IsOverloaded = isOverloaded
		}

		result = append(result, info)
	}

	return result, nil
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // socks5, http
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// GetProxyURL 获取代理 URL
func (p *ProxyConfig) GetProxyURL() string {
	if !p.Enabled || p.Host == "" {
		return ""
	}

	protocol := p.Protocol
	if protocol == "" {
		protocol = "socks5"
	}

	if p.Username != "" && p.Password != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", protocol, p.Username, p.Password, p.Host, p.Port)
	}

	return fmt.Sprintf("%s://%s:%d", protocol, p.Host, p.Port)
}

// ExtractProxyConfig 从账户数据中提取代理配置
func ExtractProxyConfig(data map[string]interface{}) *ProxyConfig {
	config := &ProxyConfig{}

	if enabled, ok := data["proxyEnabled"].(bool); ok {
		config.Enabled = enabled
	}
	if host, ok := data["proxyHost"].(string); ok {
		config.Host = host
	}
	if port, ok := data["proxyPort"].(float64); ok {
		config.Port = int(port)
	}
	if protocol, ok := data["proxyProtocol"].(string); ok {
		config.Protocol = protocol
	}
	if username, ok := data["proxyUsername"].(string); ok {
		config.Username = username
	}
	if password, ok := data["proxyPassword"].(string); ok {
		config.Password = password
	}

	return config
}
