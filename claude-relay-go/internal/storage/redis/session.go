package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 会话相关常量
const (
	// DefaultStickySessionTTL 默认粘性会话 TTL
	DefaultStickySessionTTL = 1 * time.Hour
	// DefaultOAuthSessionTTL 默认 OAuth 会话 TTL
	DefaultOAuthSessionTTL = 10 * time.Minute
)

// Session 会话数据
type Session struct {
	Token     string                 `json:"token"`
	UserID    string                 `json:"userId,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
	ExpiresAt time.Time              `json:"expiresAt"`
}

// StickySession 粘性会话（会话级账户绑定）
type StickySession struct {
	SessionHash string    `json:"sessionHash"`
	AccountID   string    `json:"accountId"`
	AccountType string    `json:"accountType"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	RenewedAt   time.Time `json:"renewedAt,omitempty"`
}

// OAuthSession OAuth 会话数据
type OAuthSession struct {
	State        string    `json:"state"`
	CodeVerifier string    `json:"codeVerifier,omitempty"`
	RedirectURI  string    `json:"redirectUri,omitempty"`
	ProxyConfig  string    `json:"proxyConfig,omitempty"` // JSON 编码的代理配置
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// ========== 通用会话操作 ==========

// SetSession 保存会话
func (c *Client) SetSession(ctx context.Context, token string, session *Session, ttl time.Duration) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	if ttl <= 0 {
		ttl = TTLSessionDefault
	}

	session.Token = token
	session.CreatedAt = time.Now()
	session.ExpiresAt = time.Now().Add(ttl)

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	key := PrefixSession + token
	return client.Set(ctx, key, data, ttl).Err()
}

// GetSession 获取会话
func (c *Client) GetSession(ctx context.Context, token string) (*Session, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	key := PrefixSession + token
	data, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil // 未找到
		}
		return nil, err
	}

	var session Session
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// DeleteSession 删除会话
func (c *Client) DeleteSession(ctx context.Context, token string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixSession + token
	return client.Del(ctx, key).Err()
}

// RefreshSession 刷新会话 TTL
func (c *Client) RefreshSession(ctx context.Context, token string, ttl time.Duration) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixSession + token
	return client.Expire(ctx, key, ttl).Err()
}

// ========== 粘性会话操作 ==========

// SetStickySession 设置粘性会话
func (c *Client) SetStickySession(ctx context.Context, sessionHash, accountID, accountType string, ttl time.Duration) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	if ttl <= 0 {
		ttl = DefaultStickySessionTTL
	}

	session := &StickySession{
		SessionHash: sessionHash,
		AccountID:   accountID,
		AccountType: accountType,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(ttl),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal sticky session: %w", err)
	}

	key := PrefixStickySession + sessionHash
	if err := client.Set(ctx, key, data, ttl).Err(); err != nil {
		return err
	}

	logger.Debug("Sticky session set",
		zap.String("sessionHash", sessionHash),
		zap.String("accountId", accountID),
		zap.String("accountType", accountType))

	return nil
}

// GetStickySession 获取粘性会话
func (c *Client) GetStickySession(ctx context.Context, sessionHash string) (*StickySession, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	key := PrefixStickySession + sessionHash
	data, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil // 未找到
		}
		return nil, err
	}

	var session StickySession
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// DeleteStickySession 删除粘性会话
func (c *Client) DeleteStickySession(ctx context.Context, sessionHash string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixStickySession + sessionHash
	return client.Del(ctx, key).Err()
}

// RenewStickySession 续期粘性会话
func (c *Client) RenewStickySession(ctx context.Context, sessionHash string, ttl time.Duration) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	if ttl <= 0 {
		ttl = DefaultStickySessionTTL
	}

	key := PrefixStickySession + sessionHash

	// 获取现有会话
	session, err := c.GetStickySession(ctx, sessionHash)
	if err != nil || session == nil {
		return fmt.Errorf("sticky session not found")
	}

	// 更新续期时间
	session.RenewedAt = time.Now()
	session.ExpiresAt = time.Now().Add(ttl)

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	return client.Set(ctx, key, data, ttl).Err()
}

// GetOrCreateStickySession 获取或创建粘性会话
func (c *Client) GetOrCreateStickySession(ctx context.Context, sessionHash, accountID, accountType string, ttl time.Duration) (*StickySession, bool, error) {
	// 先尝试获取
	session, err := c.GetStickySession(ctx, sessionHash)
	if err != nil {
		return nil, false, err
	}

	if session != nil {
		// 已存在
		return session, false, nil
	}

	// 创建新会话
	if err := c.SetStickySession(ctx, sessionHash, accountID, accountType, ttl); err != nil {
		return nil, false, err
	}

	session = &StickySession{
		SessionHash: sessionHash,
		AccountID:   accountID,
		AccountType: accountType,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(ttl),
	}

	return session, true, nil
}

// GetAllStickySessions 获取所有粘性会话
func (c *Client) GetAllStickySessions(ctx context.Context) ([]*StickySession, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	keys, err := c.ScanKeys(ctx, PrefixStickySession+"*", 1000)
	if err != nil {
		return nil, err
	}

	var sessions []*StickySession
	for _, key := range keys {
		data, err := client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var session StickySession
		if err := json.Unmarshal([]byte(data), &session); err != nil {
			continue
		}

		sessions = append(sessions, &session)
	}

	return sessions, nil
}

// CleanupExpiredStickySessions 清理过期的粘性会话
func (c *Client) CleanupExpiredStickySessions(ctx context.Context) (int, error) {
	sessions, err := c.GetAllStickySessions(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	var cleaned int

	for _, session := range sessions {
		if session.ExpiresAt.Before(now) {
			if err := c.DeleteStickySession(ctx, session.SessionHash); err == nil {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		logger.Info("Cleaned up expired sticky sessions", zap.Int("count", cleaned))
	}

	return cleaned, nil
}

// ========== OAuth 会话操作 ==========

// SetOAuthSession 保存 OAuth 会话
func (c *Client) SetOAuthSession(ctx context.Context, state string, session *OAuthSession) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	session.State = state
	session.CreatedAt = time.Now()
	session.ExpiresAt = time.Now().Add(TTLOAuthSession)

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal OAuth session: %w", err)
	}

	key := PrefixOAuthSession + state
	return client.Set(ctx, key, data, TTLOAuthSession).Err()
}

// GetOAuthSession 获取 OAuth 会话
func (c *Client) GetOAuthSession(ctx context.Context, state string) (*OAuthSession, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	key := PrefixOAuthSession + state
	data, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var session OAuthSession
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// DeleteOAuthSession 删除 OAuth 会话
func (c *Client) DeleteOAuthSession(ctx context.Context, state string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixOAuthSession + state
	return client.Del(ctx, key).Err()
}

// ConsumeOAuthSession 消费 OAuth 会话（获取后删除）
func (c *Client) ConsumeOAuthSession(ctx context.Context, state string) (*OAuthSession, error) {
	session, err := c.GetOAuthSession(ctx, state)
	if err != nil {
		return nil, err
	}

	if session != nil {
		c.DeleteOAuthSession(ctx, state)
	}

	return session, nil
}

// ========== 会话窗口（账户使用统计的时间窗口）==========

// GetSessionWindowUsage 获取账户在会话窗口内的使用统计
func (c *Client) GetSessionWindowUsage(ctx context.Context, accountID string, windowHours int) (*UsageStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	stats := &UsageStats{}

	// 生成需要查询的小时级别 key
	var hourlyKeys []string
	currentHour := now.Add(time.Duration(-windowHours) * time.Hour)

	for currentHour.Before(now) || currentHour.Equal(now) {
		hourStr := getHourStringInTimezone(currentHour)
		key := fmt.Sprintf("account_usage:hourly:%s:%s", accountID, hourStr)
		hourlyKeys = append(hourlyKeys, key)
		currentHour = currentHour.Add(time.Hour)
	}

	// 批量获取
	pipe := client.Pipeline()
	cmds := make([]*goredis.MapStringStringCmd, len(hourlyKeys))

	for i, key := range hourlyKeys {
		cmds[i] = pipe.HGetAll(ctx, key)
	}

	pipe.Exec(ctx)

	// 聚合数据
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil || len(data) == 0 {
			continue
		}

		stats.InputTokens += parseInt64(data["inputTokens"])
		stats.OutputTokens += parseInt64(data["outputTokens"])
		stats.CacheCreateTokens += parseInt64(data["cacheCreateTokens"])
		stats.CacheReadTokens += parseInt64(data["cacheReadTokens"])
		stats.AllTokens += parseInt64(data["allTokens"])
		stats.RequestCount += parseInt64(data["requests"])
	}

	stats.TotalTokens = stats.InputTokens + stats.OutputTokens

	return stats, nil
}

// GetSessionWindowUsageByModel 获取账户在会话窗口内按模型分类的使用统计
func (c *Client) GetSessionWindowUsageByModel(ctx context.Context, accountID string, windowHours int) (map[string]*UsageStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	modelUsage := make(map[string]*UsageStats)

	// 生成需要查询的小时级别 key
	var hourlyKeys []string
	currentHour := now.Add(time.Duration(-windowHours) * time.Hour)

	for currentHour.Before(now) || currentHour.Equal(now) {
		hourStr := getHourStringInTimezone(currentHour)
		key := fmt.Sprintf("account_usage:hourly:%s:%s", accountID, hourStr)
		hourlyKeys = append(hourlyKeys, key)
		currentHour = currentHour.Add(time.Hour)
	}

	// 批量获取
	pipe := client.Pipeline()
	cmds := make([]*goredis.MapStringStringCmd, len(hourlyKeys))

	for i, key := range hourlyKeys {
		cmds[i] = pipe.HGetAll(ctx, key)
	}

	pipe.Exec(ctx)

	// 聚合数据
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil || len(data) == 0 {
			continue
		}

		// 处理每个模型的数据
		// 格式: model:{modelName}:{metric}
		for field, value := range data {
			if len(field) > 6 && field[:6] == "model:" {
				// 解析 model:{modelName}:{metric}
				remaining := field[6:]
				lastColon := -1
				for i := len(remaining) - 1; i >= 0; i-- {
					if remaining[i] == ':' {
						lastColon = i
						break
					}
				}

				if lastColon == -1 {
					continue
				}

				modelName := remaining[:lastColon]
				metric := remaining[lastColon+1:]

				if _, ok := modelUsage[modelName]; !ok {
					modelUsage[modelName] = &UsageStats{}
				}

				v := parseInt64(value)
				switch metric {
				case "inputTokens":
					modelUsage[modelName].InputTokens += v
				case "outputTokens":
					modelUsage[modelName].OutputTokens += v
				case "cacheCreateTokens":
					modelUsage[modelName].CacheCreateTokens += v
				case "cacheReadTokens":
					modelUsage[modelName].CacheReadTokens += v
				case "allTokens":
					modelUsage[modelName].AllTokens += v
				case "requests":
					modelUsage[modelName].RequestCount += v
				}
			}
		}
	}

	return modelUsage, nil
}
