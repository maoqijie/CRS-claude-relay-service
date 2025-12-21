package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 并发控制配置常量
const (
	// DefaultConcurrencyLeaseSeconds 默认租约时间（秒）
	DefaultConcurrencyLeaseSeconds = 300 // 5分钟
	// DefaultConcurrencyCleanupGraceSeconds 清理宽限期（秒）
	DefaultConcurrencyCleanupGraceSeconds = 60 // 1分钟
	// MinConcurrencyLeaseSeconds 最小租约时间（秒）
	MinConcurrencyLeaseSeconds = 30
)

// ConcurrencyConfig 并发控制配置
type ConcurrencyConfig struct {
	LeaseSeconds        int // 租约时间（秒）
	CleanupGraceSeconds int // 清理宽限期（秒）
}

// ConcurrencyStatus 并发状态
type ConcurrencyStatus struct {
	APIKeyID       string            `json:"apiKeyId"`
	Key            string            `json:"key"`
	ActiveCount    int64             `json:"activeCount"`
	ExpiredCount   int64             `json:"expiredCount"`
	ActiveRequests []ActiveRequest   `json:"activeRequests"`
	ExpiredRequests []ActiveRequest  `json:"expiredRequests,omitempty"`
	Exists         bool              `json:"exists"`
}

// ActiveRequest 活跃请求信息
type ActiveRequest struct {
	RequestID        string `json:"requestId"`
	ExpireAt         string `json:"expireAt"`
	RemainingSeconds int64  `json:"remainingSeconds"`
}

// Lua 脚本（嵌入式）
const (
	// 并发控制脚本
	luaConcurrencyIncr = `
local key = KEYS[1]
local member = ARGV[1]
local expireAt = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
redis.call('ZADD', key, expireAt, member)

if ttl > 0 then
    redis.call('PEXPIRE', key, ttl)
end

local count = redis.call('ZCARD', key)
return count
`

	// 释放并发租约脚本
	luaConcurrencyDecr = `
local key = KEYS[1]
local member = ARGV[1]
local now = tonumber(ARGV[2])

if member and member ~= '' then
    redis.call('ZREM', key, member)
end

redis.call('ZREMRANGEBYSCORE', key, '-inf', now)

local count = redis.call('ZCARD', key)
if count <= 0 then
    redis.call('DEL', key)
    return 0
end

return count
`

	// 刷新并发租约脚本
	luaConcurrencyRefresh = `
local key = KEYS[1]
local member = ARGV[1]
local expireAt = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now)

local exists = redis.call('ZSCORE', key, member)

if exists then
    redis.call('ZADD', key, expireAt, member)
    if ttl > 0 then
        redis.call('PEXPIRE', key, ttl)
    end
    return 1
end

return 0
`
)

// getConcurrencyConfig 获取并发控制配置
func (c *Client) getConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		LeaseSeconds:        DefaultConcurrencyLeaseSeconds,
		CleanupGraceSeconds: DefaultConcurrencyCleanupGraceSeconds,
	}
}

// IncrConcurrency 增加并发计数（基于租约的有序集合）
func (c *Client) IncrConcurrency(ctx context.Context, apiKeyID, requestID string, leaseSeconds int) (int64, error) {
	if requestID == "" {
		return 0, fmt.Errorf("request ID is required for concurrency tracking")
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	config := c.getConcurrencyConfig()
	if leaseSeconds <= 0 {
		leaseSeconds = config.LeaseSeconds
	}
	if leaseSeconds < MinConcurrencyLeaseSeconds {
		leaseSeconds = MinConcurrencyLeaseSeconds
	}

	key := PrefixConcurrency + apiKeyID
	now := time.Now().UnixMilli()
	expireAt := now + int64(leaseSeconds)*1000
	ttl := int64((leaseSeconds + config.CleanupGraceSeconds) * 1000)
	if ttl < 60000 {
		ttl = 60000 // 最小 60 秒
	}

	result, err := client.Eval(ctx, luaConcurrencyIncr, []string{key},
		requestID, expireAt, now, ttl).Result()
	if err != nil {
		logger.Error("Failed to increment concurrency", zap.Error(err))
		return 0, err
	}

	count, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected result type from concurrency incr: %T", result)
	}
	logger.Debug("Incremented concurrency",
		zap.String("apiKeyId", apiKeyID),
		zap.String("requestId", requestID),
		zap.Int64("count", count))

	return count, nil
}

// DecrConcurrency 减少并发计数
func (c *Client) DecrConcurrency(ctx context.Context, apiKeyID, requestID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrency + apiKeyID
	now := time.Now().UnixMilli()

	result, err := client.Eval(ctx, luaConcurrencyDecr, []string{key},
		requestID, now).Result()
	if err != nil {
		logger.Error("Failed to decrement concurrency", zap.Error(err))
		return 0, err
	}

	count, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected result type from concurrency decr: %T", result)
	}
	logger.Debug("Decremented concurrency",
		zap.String("apiKeyId", apiKeyID),
		zap.String("requestId", requestID),
		zap.Int64("count", count))

	return count, nil
}

// RefreshConcurrencyLease 刷新并发租约，防止长连接提前过期
func (c *Client) RefreshConcurrencyLease(ctx context.Context, apiKeyID, requestID string, leaseSeconds int) (bool, error) {
	if requestID == "" {
		return false, nil
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	config := c.getConcurrencyConfig()
	if leaseSeconds <= 0 {
		leaseSeconds = config.LeaseSeconds
	}

	key := PrefixConcurrency + apiKeyID
	now := time.Now().UnixMilli()
	expireAt := now + int64(leaseSeconds)*1000
	ttl := int64((leaseSeconds + config.CleanupGraceSeconds) * 1000)
	if ttl < 60000 {
		ttl = 60000
	}

	result, err := client.Eval(ctx, luaConcurrencyRefresh, []string{key},
		requestID, expireAt, now, ttl).Result()
	if err != nil {
		logger.Error("Failed to refresh concurrency lease", zap.Error(err))
		return false, err
	}

	resultInt, ok := result.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected result type from concurrency refresh: %T", result)
	}
	refreshed := resultInt == 1
	if refreshed {
		logger.Debug("Refreshed concurrency lease",
			zap.String("apiKeyId", apiKeyID),
			zap.String("requestId", requestID))
	}

	return refreshed, nil
}

// GetConcurrency 获取当前并发数
func (c *Client) GetConcurrency(ctx context.Context, apiKeyID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrency + apiKeyID
	now := time.Now().UnixMilli()

	// 先清理过期
	client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", now))

	// 获取计数
	count, err := client.ZCard(ctx, key).Result()
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetConcurrencyStatus 获取特定 API Key 的并发状态详情
func (c *Client) GetConcurrencyStatus(ctx context.Context, apiKeyID string) (*ConcurrencyStatus, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	key := PrefixConcurrency + apiKeyID
	now := time.Now().UnixMilli()

	// 检查 key 是否存在
	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	if exists == 0 {
		return &ConcurrencyStatus{
			APIKeyID:       apiKeyID,
			Key:            key,
			ActiveCount:    0,
			ExpiredCount:   0,
			ActiveRequests: []ActiveRequest{},
			Exists:         false,
		}, nil
	}

	// 获取所有成员和分数
	members, err := client.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var activeRequests []ActiveRequest
	var expiredRequests []ActiveRequest

	for _, member := range members {
		requestID := member.Member.(string)
		expireAt := int64(member.Score)
		remainingSeconds := (expireAt - now) / 1000

		request := ActiveRequest{
			RequestID:        requestID,
			ExpireAt:         time.UnixMilli(expireAt).Format(time.RFC3339),
			RemainingSeconds: remainingSeconds,
		}

		if expireAt > now {
			activeRequests = append(activeRequests, request)
		} else {
			expiredRequests = append(expiredRequests, request)
		}
	}

	return &ConcurrencyStatus{
		APIKeyID:        apiKeyID,
		Key:             key,
		ActiveCount:     int64(len(activeRequests)),
		ExpiredCount:    int64(len(expiredRequests)),
		ActiveRequests:  activeRequests,
		ExpiredRequests: expiredRequests,
		Exists:          true,
	}, nil
}

// GetAllConcurrencyStatus 获取所有并发状态
func (c *Client) GetAllConcurrencyStatus(ctx context.Context) ([]ConcurrencyStatus, error) {
	keys, err := c.ScanKeys(ctx, PrefixConcurrency+"*", 1000)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	var results []ConcurrencyStatus

	for _, key := range keys {
		apiKeyID := key[len(PrefixConcurrency):]

		// 获取活跃成员
		members, err := client.ZRangeByScoreWithScores(ctx, key, &goredis.ZRangeBy{
			Min: fmt.Sprintf("%d", now),
			Max: "+inf",
		}).Result()
		if err != nil {
			continue
		}

		var activeRequests []ActiveRequest
		for _, member := range members {
			requestID := member.Member.(string)
			expireAt := int64(member.Score)
			remainingSeconds := (expireAt - now) / 1000

			activeRequests = append(activeRequests, ActiveRequest{
				RequestID:        requestID,
				ExpireAt:         time.UnixMilli(expireAt).Format(time.RFC3339),
				RemainingSeconds: remainingSeconds,
			})
		}

		// 获取过期成员数量
		expiredCount, _ := client.ZCount(ctx, key, "-inf", fmt.Sprintf("%d", now)).Result()

		results = append(results, ConcurrencyStatus{
			APIKeyID:       apiKeyID,
			Key:            key,
			ActiveCount:    int64(len(activeRequests)),
			ExpiredCount:   expiredCount,
			ActiveRequests: activeRequests,
		})
	}

	return results, nil
}

// ForceClearConcurrency 强制清理特定 API Key 的并发计数
func (c *Client) ForceClearConcurrency(ctx context.Context, apiKeyID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrency + apiKeyID

	// 获取清理前的计数
	beforeCount, _ := client.ZCard(ctx, key).Result()

	// 删除整个 key
	client.Del(ctx, key)

	logger.Warn("Force cleared concurrency",
		zap.String("apiKeyId", apiKeyID),
		zap.Int64("clearedCount", beforeCount))

	return beforeCount, nil
}

// ForceClearAllConcurrency 强制清理所有并发计数
func (c *Client) ForceClearAllConcurrency(ctx context.Context) (int, int64, error) {
	keys, err := c.ScanKeys(ctx, PrefixConcurrency+"*", 1000)
	if err != nil {
		return 0, 0, err
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return 0, 0, err
	}

	var totalCleared int64
	for _, key := range keys {
		count, _ := client.ZCard(ctx, key).Result()
		client.Del(ctx, key)
		totalCleared += count
	}

	logger.Warn("Force cleared all concurrency",
		zap.Int("keysCleared", len(keys)),
		zap.Int64("totalEntriesCleared", totalCleared))

	return len(keys), totalCleared, nil
}

// CleanupExpiredConcurrency 清理过期的并发条目（不影响活跃请求）
func (c *Client) CleanupExpiredConcurrency(ctx context.Context) (int, int64, error) {
	keys, err := c.ScanKeys(ctx, PrefixConcurrency+"*", 1000)
	if err != nil {
		return 0, 0, err
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return 0, 0, err
	}

	now := time.Now().UnixMilli()
	var totalCleaned int64
	var keysProcessed int

	for _, key := range keys {
		removed, err := client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", now)).Result()
		if err != nil {
			continue
		}

		if removed > 0 {
			keysProcessed++
			totalCleaned += removed

			// 如果清理后为空，删除 key
			count, _ := client.ZCard(ctx, key).Result()
			if count == 0 {
				client.Del(ctx, key)
			}
		}
	}

	if totalCleaned > 0 {
		logger.Info("Cleaned up expired concurrency",
			zap.Int("keysProcessed", keysProcessed),
			zap.Int64("entriesCleaned", totalCleaned))
	}

	return keysProcessed, totalCleaned, nil
}

// ========== Console 账户并发控制（复用现有机制）==========

// IncrConsoleAccountConcurrency 增加 Console 账户并发计数
func (c *Client) IncrConsoleAccountConcurrency(ctx context.Context, accountID, requestID string, leaseSeconds int) (int64, error) {
	compositeKey := "console_account:" + accountID
	return c.IncrConcurrency(ctx, compositeKey, requestID, leaseSeconds)
}

// DecrConsoleAccountConcurrency 减少 Console 账户并发计数
func (c *Client) DecrConsoleAccountConcurrency(ctx context.Context, accountID, requestID string) (int64, error) {
	compositeKey := "console_account:" + accountID
	return c.DecrConcurrency(ctx, compositeKey, requestID)
}

// RefreshConsoleAccountConcurrencyLease 刷新 Console 账户并发租约
func (c *Client) RefreshConsoleAccountConcurrencyLease(ctx context.Context, accountID, requestID string, leaseSeconds int) (bool, error) {
	compositeKey := "console_account:" + accountID
	return c.RefreshConcurrencyLease(ctx, compositeKey, requestID, leaseSeconds)
}

// GetConsoleAccountConcurrency 获取 Console 账户当前并发数
func (c *Client) GetConsoleAccountConcurrency(ctx context.Context, accountID string) (int64, error) {
	compositeKey := "console_account:" + accountID
	return c.GetConcurrency(ctx, compositeKey)
}

