package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// 分布式锁配置常量
const (
	// DefaultLockTTL 默认锁 TTL
	DefaultLockTTL = 30 * time.Second
	// DefaultLockRetryDelay 默认重试延迟
	DefaultLockRetryDelay = 100 * time.Millisecond
	// DefaultLockMaxRetries 默认最大重试次数
	DefaultLockMaxRetries = 50
)

// Lua 脚本常量
const (
	// luaLockRelease 释放锁脚本
	luaLockRelease = `
local key = KEYS[1]
local token = ARGV[1]

if redis.call('GET', key) == token then
    return redis.call('DEL', key)
else
    return 0
end
`

	// luaLockExtend 延长锁 TTL 脚本
	luaLockExtend = `
local key = KEYS[1]
local token = ARGV[1]
local ttl = tonumber(ARGV[2])

if redis.call('GET', key) == token then
    return redis.call('PEXPIRE', key, ttl)
else
    return 0
end
`

	// luaUserMessageLockAcquire 获取用户消息队列锁脚本
	luaUserMessageLockAcquire = `
local lockKey = KEYS[1]
local lastTimeKey = KEYS[2]
local requestId = ARGV[1]
local lockTtl = tonumber(ARGV[2])
local delayMs = tonumber(ARGV[3])
local nowMs = tonumber(ARGV[4])

-- 检查锁是否空闲
local currentLock = redis.call('GET', lockKey)
if currentLock == false then
    -- 检查是否需要延迟
    local lastTime = redis.call('GET', lastTimeKey)

    if lastTime then
        local elapsed = nowMs - tonumber(lastTime)
        if elapsed < delayMs then
            -- 需要等待的毫秒数
            return {0, delayMs - elapsed}
        end
    end

    -- 获取锁
    redis.call('SET', lockKey, requestId, 'PX', lockTtl)
    return {1, 0}
end

-- 锁被占用，返回等待
return {0, -1}
`

	// luaUserMessageLockRelease 释放用户消息队列锁脚本
	luaUserMessageLockRelease = `
local lockKey = KEYS[1]
local lastTimeKey = KEYS[2]
local requestId = ARGV[1]
local nowMs = ARGV[2]

-- 验证锁持有者
local currentLock = redis.call('GET', lockKey)
if currentLock == requestId then
    -- 记录完成时间
    redis.call('SET', lastTimeKey, nowMs, 'EX', 60)  -- 60秒后过期

    -- 删除锁
    redis.call('DEL', lockKey)
    return 1
end
return 0
`
)

// LockResult 锁结果
type LockResult struct {
	Token   string // 锁令牌（用于释放）
	Success bool   // 是否成功获取
}

// AcquireLock 获取分布式锁
func (c *Client) AcquireLock(ctx context.Context, lockKey string, ttl time.Duration) (*LockResult, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	if ttl <= 0 {
		ttl = DefaultLockTTL
	}

	token := uuid.New().String()

	// SET NX EX 原子操作
	success, err := client.SetNX(ctx, lockKey, token, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if success {
		logger.Debug("Acquired lock", zap.String("key", lockKey))
	}

	return &LockResult{
		Token:   token,
		Success: success,
	}, nil
}

// ReleaseLock 释放分布式锁
func (c *Client) ReleaseLock(ctx context.Context, lockKey, token string) (bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	result, err := client.Eval(ctx, luaLockRelease, []string{lockKey}, token).Result()
	if err != nil {
		return false, fmt.Errorf("failed to release lock: %w", err)
	}

	resultInt, ok := result.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected result type from lock release: %T", result)
	}
	released := resultInt == 1
	if released {
		logger.Debug("Released lock", zap.String("key", lockKey))
	} else {
		logger.Warn("Failed to release lock: token mismatch", zap.String("key", lockKey))
	}

	return released, nil
}

// TryLockWithRetry 重试获取锁
func (c *Client) TryLockWithRetry(ctx context.Context, lockKey string, ttl time.Duration, maxRetries int, retryDelay time.Duration) (string, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultLockMaxRetries
	}
	if retryDelay <= 0 {
		retryDelay = DefaultLockRetryDelay
	}

	for i := 0; i < maxRetries; i++ {
		result, err := c.AcquireLock(ctx, lockKey, ttl)
		if err != nil {
			return "", err
		}

		if result.Success {
			return result.Token, nil
		}

		// 等待后重试
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(retryDelay):
			// 继续重试
		}
	}

	return "", fmt.Errorf("failed to acquire lock after %d retries", maxRetries)
}

// ExtendLock 延长锁的 TTL
func (c *Client) ExtendLock(ctx context.Context, lockKey, token string, ttl time.Duration) (bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	result, err := client.Eval(ctx, luaLockExtend, []string{lockKey}, token, ttl.Milliseconds()).Result()
	if err != nil {
		return false, err
	}

	resultInt, ok := result.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected result type from lock extend: %T", result)
	}
	return resultInt == 1, nil
}

// WithLock 在持有锁的情况下执行函数
func (c *Client) WithLock(ctx context.Context, lockKey string, ttl time.Duration, fn func() error) error {
	result, err := c.AcquireLock(ctx, lockKey, ttl)
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("failed to acquire lock: %s", lockKey)
	}

	defer func() {
		c.ReleaseLock(ctx, lockKey, result.Token)
	}()

	return fn()
}

// WithLockRetry 在持有锁的情况下执行函数（带重试）
func (c *Client) WithLockRetry(ctx context.Context, lockKey string, ttl time.Duration, maxRetries int, fn func() error) error {
	token, err := c.TryLockWithRetry(ctx, lockKey, ttl, maxRetries, DefaultLockRetryDelay)
	if err != nil {
		return err
	}

	defer func() {
		c.ReleaseLock(ctx, lockKey, token)
	}()

	return fn()
}

// ========== 账户锁定（与 Node.js 兼容）==========

// SetAccountLock 设置账户锁（用于 Token 刷新等场景）
func (c *Client) SetAccountLock(ctx context.Context, lockKey, lockValue string, ttl time.Duration) (bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	// 使用 SET NX PX 实现原子性的锁获取
	success, err := client.SetNX(ctx, lockKey, lockValue, ttl).Result()
	if err != nil {
		logger.Error("Failed to acquire account lock", zap.String("key", lockKey), zap.Error(err))
		return false, err
	}

	return success, nil
}

// ReleaseAccountLock 释放账户锁
func (c *Client) ReleaseAccountLock(ctx context.Context, lockKey, lockValue string) (bool, error) {
	return c.ReleaseLock(ctx, lockKey, lockValue)
}

// ========== 用户消息队列锁（与 Node.js 兼容）==========

// UserMessageLockResult 用户消息锁结果
type UserMessageLockResult struct {
	Acquired   bool  // 是否成功获取
	WaitMs     int64 // 需要等待的毫秒数（-1表示被占用需等待，>=0表示需要延迟的毫秒数）
	RedisError bool  // 是否为 Redis 错误
}

// AcquireUserMessageLock 获取用户消息队列锁
func (c *Client) AcquireUserMessageLock(ctx context.Context, accountID, requestID string, lockTTLMs, delayMs int64) (*UserMessageLockResult, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return &UserMessageLockResult{
			Acquired:   false,
			WaitMs:     -1,
			RedisError: true,
		}, err
	}

	lockKey := PrefixUserMsgLock + accountID
	lastTimeKey := PrefixUserMsgLast + accountID
	nowMs := time.Now().UnixMilli() // 从 Go 传入时间，避免 Lua 使用 TIME 命令（Redis Cluster 兼容性）

	result, err := client.Eval(ctx, luaUserMessageLockAcquire, []string{lockKey, lastTimeKey},
		requestID, lockTTLMs, delayMs, nowMs).Result()
	if err != nil {
		return &UserMessageLockResult{
			Acquired:   false,
			WaitMs:     -1,
			RedisError: true,
		}, err
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) < 2 {
		return &UserMessageLockResult{
			Acquired:   false,
			WaitMs:     -1,
			RedisError: true,
		}, fmt.Errorf("unexpected result type from user message lock: %T", result)
	}

	acquired, ok := arr[0].(int64)
	if !ok {
		return &UserMessageLockResult{
			Acquired:   false,
			WaitMs:     -1,
			RedisError: true,
		}, fmt.Errorf("unexpected acquired type: %T", arr[0])
	}

	waitMs, ok := arr[1].(int64)
	if !ok {
		return &UserMessageLockResult{
			Acquired:   false,
			WaitMs:     -1,
			RedisError: true,
		}, fmt.Errorf("unexpected waitMs type: %T", arr[1])
	}

	return &UserMessageLockResult{
		Acquired: acquired == 1,
		WaitMs:   waitMs,
	}, nil
}

// ReleaseUserMessageLock 释放用户消息队列锁并记录完成时间
func (c *Client) ReleaseUserMessageLock(ctx context.Context, accountID, requestID string) (bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	lockKey := PrefixUserMsgLock + accountID
	lastTimeKey := PrefixUserMsgLast + accountID
	nowMs := time.Now().UnixMilli() // 从 Go 传入时间，避免 Lua 使用 TIME 命令

	result, err := client.Eval(ctx, luaUserMessageLockRelease, []string{lockKey, lastTimeKey}, requestID, nowMs).Result()
	if err != nil {
		return false, err
	}

	resultInt, ok := result.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected result type from user message lock release: %T", result)
	}
	return resultInt == 1, nil
}

// ForceReleaseUserMessageLock 强制释放用户消息队列锁
func (c *Client) ForceReleaseUserMessageLock(ctx context.Context, accountID string) (bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return false, err
	}

	lockKey := PrefixUserMsgLock + accountID
	_, err = client.Del(ctx, lockKey).Result()
	return err == nil, err
}

// UserMessageQueueStats 用户消息队列统计
type UserMessageQueueStats struct {
	AccountID       string  `json:"accountId"`
	IsLocked        bool    `json:"isLocked"`
	LockHolder      string  `json:"lockHolder,omitempty"`
	LockTTLMs       int64   `json:"lockTtlMs"`
	LastCompletedAt *string `json:"lastCompletedAt,omitempty"`
}

// GetUserMessageQueueStats 获取用户消息队列统计
func (c *Client) GetUserMessageQueueStats(ctx context.Context, accountID string) (*UserMessageQueueStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	lockKey := PrefixUserMsgLock + accountID
	lastTimeKey := PrefixUserMsgLast + accountID

	pipe := client.Pipeline()
	lockHolderCmd := pipe.Get(ctx, lockKey)
	lastTimeCmd := pipe.Get(ctx, lastTimeKey)
	lockTTLCmd := pipe.PTTL(ctx, lockKey)

	pipe.Exec(ctx)

	lockHolder, _ := lockHolderCmd.Result()
	lastTime, _ := lastTimeCmd.Result()
	lockTTL, _ := lockTTLCmd.Result()

	stats := &UserMessageQueueStats{
		AccountID: accountID,
		IsLocked:  lockHolder != "",
	}

	if lockHolder != "" {
		stats.LockHolder = lockHolder
	}

	if lockTTL > 0 {
		stats.LockTTLMs = int64(lockTTL.Milliseconds())
	}

	if lastTime != "" {
		ts := parseInt64(lastTime)
		t := time.UnixMilli(ts).Format(time.RFC3339)
		stats.LastCompletedAt = &t
	}

	return stats, nil
}

// ScanUserMessageQueueLocks 扫描所有用户消息队列锁
func (c *Client) ScanUserMessageQueueLocks(ctx context.Context) ([]string, error) {
	keys, err := c.ScanKeys(ctx, PrefixUserMsgLock+"*", 100)
	if err != nil {
		return nil, err
	}

	accountIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		accountID := key[len(PrefixUserMsgLock):]
		accountIDs = append(accountIDs, accountID)
	}

	return accountIDs, nil
}
