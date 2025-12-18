# 第二步：Redis 数据访问层实现

**状态**: 🚧 进行中

---

## 1. 目标

完成 Redis 数据访问层的核心功能实现，包括：
- API Key 完整的 CRUD 操作
- 使用统计（Token 计数、成本统计）
- 并发控制和分布式锁
- 账户数据管理
- 会话管理

**预计工期**: 2-3 周
**验收标准**: Go 服务能完整读写 Redis 数据，与 Node.js 100% 兼容

---

## 2. 实施概览

### 2.1 模块划分

```
internal/storage/redis/
├── client.go          # ✅ 已完成 - Redis 客户端基础
├── keys.go            # ✅ 已完成 - Key 常量定义
├── apikey.go          # 🎯 本阶段 - API Key 操作
├── usage.go           # 🎯 本阶段 - 使用统计
├── cost.go            # 🎯 本阶段 - 成本统计
├── concurrency.go     # 🎯 本阶段 - 并发控制
├── lock.go            # 🎯 本阶段 - 分布式锁
├── account.go         # 🎯 本阶段 - 账户管理
├── session.go         # 🎯 本阶段 - 会话管理
├── queue.go           # 🎯 本阶段 - 请求排队
└── scripts/
    ├── concurrency.lua    # Lua 脚本 - 并发控制
    ├── lock.lua           # Lua 脚本 - 分布式锁
    └── queue.lua          # Lua 脚本 - 排队控制
```

### 2.2 实施优先级

| 优先级 | 模块 | 说明 | Node.js 对应 |
|--------|------|------|--------------|
| P0 | apikey.go | API Key CRUD、哈希映射 | redis.js: 237-896行 |
| P0 | usage.go | Token 使用统计 | redis.js: 1058-1608行 |
| P0 | cost.go | 成本计算和统计 | redis.js: 1635-1793行 |
| P1 | concurrency.go | 并发控制 | redis.js: 3040-3150行 |
| P1 | lock.go | 分布式锁 | redis.js: 2889-3039行 |
| P1 | queue.go | 请求排队 | redis.js: 3551-3840行 |
| P2 | account.go | 账户数据管理 | redis.js: 2120-2395行 |
| P2 | session.go | 会话管理 | redis.js: 2396-2810行 |

---

## 3. 详细实施

### 3.1 API Key 操作 (apikey.go)

#### 3.1.1 数据结构

```go
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// APIKey API Key 数据结构（与 Node.js 保持一致）
type APIKey struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	HashedKey             string    `json:"hashedKey"`
	Limit                 int64     `json:"limit"`
	UsedToday             int64     `json:"usedToday"`
	IsActive              bool      `json:"isActive"`
	CreatedAt             time.Time `json:"createdAt"`
	ExpiresAt             *time.Time `json:"expiresAt,omitempty"`

	// 新增字段（与 Node.js 完全对应）
	Permissions           []string  `json:"permissions,omitempty"`           // 权限列表 (all, claude, gemini, openai)
	AllowedClients        []string  `json:"allowedClients,omitempty"`        // 允许的客户端
	ModelBlacklist        []string  `json:"modelBlacklist,omitempty"`        // 模型黑名单
	ConcurrentLimit       int       `json:"concurrentLimit,omitempty"`       // 并发限制

	// 并发排队配置
	ConcurrentRequestQueueEnabled              bool    `json:"concurrentRequestQueueEnabled"`
	ConcurrentRequestQueueMaxSize              int     `json:"concurrentRequestQueueMaxSize"`
	ConcurrentRequestQueueMaxSizeMultiplier    float64 `json:"concurrentRequestQueueMaxSizeMultiplier"`
	ConcurrentRequestQueueTimeoutMs            int     `json:"concurrentRequestQueueTimeoutMs"`

	// 用户管理
	UserID                string    `json:"userId,omitempty"`
	Tags                  []string  `json:"tags,omitempty"`
	Description           string    `json:"description,omitempty"`

	// 元数据
	LastUsedAt            *time.Time `json:"lastUsedAt,omitempty"`
	IsDeleted             bool       `json:"isDeleted"`
}

// APIKeyPaginated 分页结果
type APIKeyPaginated struct {
	Keys       []APIKey `json:"keys"`
	Total      int      `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"pageSize"`
	TotalPages int      `json:"totalPages"`
}

// APIKeyStats API Key 统计信息
type APIKeyStats struct {
	TotalKeys       int `json:"totalKeys"`
	ActiveKeys      int `json:"activeKeys"`
	ExpiredKeys     int `json:"expiredKeys"`
	DeletedKeys     int `json:"deletedKeys"`
	KeysWithUsers   int `json:"keysWithUsers"`
}
```

#### 3.1.2 核心方法实现

```go
// SetAPIKey 创建或更新 API Key
func (c *Client) SetAPIKey(ctx context.Context, key *APIKey) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	// 序列化为 JSON
	data, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("failed to marshal API key: %w", err)
	}

	// 保存到 Redis
	redisKey := PrefixAPIKey + key.ID
	if err := client.Set(ctx, redisKey, data, TTLAPIKey).Err(); err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}

	// 更新哈希映射（快速查找）
	if key.HashedKey != "" {
		if err := client.HSet(ctx, PrefixAPIKeyHashMap, key.HashedKey, key.ID).Err(); err != nil {
			logger.Error("Failed to update hash map", zap.Error(err))
		}
	}

	logger.Info("API Key saved", zap.String("id", key.ID), zap.String("name", key.Name))
	return nil
}

// GetAPIKey 获取 API Key
func (c *Client) GetAPIKey(ctx context.Context, keyID string) (*APIKey, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	redisKey := PrefixAPIKey + keyID
	data, err := client.Get(ctx, redisKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("API key not found: %s", keyID)
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	var key APIKey
	if err := json.Unmarshal([]byte(data), &key); err != nil {
		return nil, fmt.Errorf("failed to unmarshal API key: %w", err)
	}

	return &key, nil
}

// GetAPIKeyByHash 通过哈希值获取 API Key
func (c *Client) GetAPIKeyByHash(ctx context.Context, hashedKey string) (*APIKey, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	// 从哈希映射获取 ID
	keyID, err := client.HGet(ctx, PrefixAPIKeyHashMap, hashedKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("API key not found for hash")
		}
		return nil, fmt.Errorf("failed to get API key ID: %w", err)
	}

	// 获取完整数据
	return c.GetAPIKey(ctx, keyID)
}

// GetAllAPIKeys 获取所有 API Key
func (c *Client) GetAllAPIKeys(ctx context.Context, includeDeleted bool) ([]APIKey, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	// 使用 SCAN 获取所有 apikey:* 的 key
	keys, err := c.ScanKeys(ctx, PrefixAPIKey+"*", 1000)
	if err != nil {
		return nil, err
	}

	var apiKeys []APIKey
	for _, key := range keys {
		// 跳过 hash_map
		if key == PrefixAPIKeyHashMap {
			continue
		}

		data, err := client.Get(ctx, key).Result()
		if err != nil {
			logger.Warn("Failed to get API key", zap.String("key", key), zap.Error(err))
			continue
		}

		var apiKey APIKey
		if err := json.Unmarshal([]byte(data), &apiKey); err != nil {
			logger.Warn("Failed to unmarshal API key", zap.String("key", key), zap.Error(err))
			continue
		}

		// 过滤已删除的 Key
		if !includeDeleted && apiKey.IsDeleted {
			continue
		}

		apiKeys = append(apiKeys, apiKey)
	}

	return apiKeys, nil
}

// DeleteAPIKey 删除 API Key（软删除）
func (c *Client) DeleteAPIKey(ctx context.Context, keyID string) error {
	key, err := c.GetAPIKey(ctx, keyID)
	if err != nil {
		return err
	}

	// 标记为已删除
	key.IsDeleted = true
	return c.SetAPIKey(ctx, key)
}

// UpdateAPIKeyFields 更新指定字段
func (c *Client) UpdateAPIKeyFields(ctx context.Context, keyID string, updates map[string]interface{}) error {
	key, err := c.GetAPIKey(ctx, keyID)
	if err != nil {
		return err
	}

	// 转换为 JSON 更新（简化版，实际应该使用反射）
	data, _ := json.Marshal(key)
	var keyMap map[string]interface{}
	json.Unmarshal(data, &keyMap)

	// 应用更新
	for k, v := range updates {
		keyMap[k] = v
	}

	// 重新序列化
	newData, _ := json.Marshal(keyMap)
	json.Unmarshal(newData, key)

	return c.SetAPIKey(ctx, key)
}
```

#### 3.1.3 分页查询

```go
// GetAPIKeysPaginated 分页获取 API Key
func (c *Client) GetAPIKeysPaginated(ctx context.Context, opts APIKeyQueryOptions) (*APIKeyPaginated, error) {
	// 获取所有 Key
	allKeys, err := c.GetAllAPIKeys(ctx, opts.IncludeDeleted)
	if err != nil {
		return nil, err
	}

	// 过滤
	filtered := c.filterAPIKeys(allKeys, opts)

	// 排序
	c.sortAPIKeys(filtered, opts.SortBy, opts.SortOrder)

	// 分页
	total := len(filtered)
	start := (opts.Page - 1) * opts.PageSize
	end := start + opts.PageSize
	if end > total {
		end = total
	}
	if start > total {
		start = total
	}

	return &APIKeyPaginated{
		Keys:       filtered[start:end],
		Total:      total,
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: (total + opts.PageSize - 1) / opts.PageSize,
	}, nil
}

// APIKeyQueryOptions 查询选项
type APIKeyQueryOptions struct {
	Page          int
	PageSize      int
	IncludeDeleted bool
	UserID        string
	Tags          []string
	IsActive      *bool
	Search        string
	SortBy        string // createdAt, name, usedToday
	SortOrder     string // asc, desc
}

func (c *Client) filterAPIKeys(keys []APIKey, opts APIKeyQueryOptions) []APIKey {
	var filtered []APIKey
	for _, key := range keys {
		// UserID 过滤
		if opts.UserID != "" && key.UserID != opts.UserID {
			continue
		}

		// IsActive 过滤
		if opts.IsActive != nil && key.IsActive != *opts.IsActive {
			continue
		}

		// Tags 过滤
		if len(opts.Tags) > 0 && !hasAnyTag(key.Tags, opts.Tags) {
			continue
		}

		// 搜索过滤（名称或ID）
		if opts.Search != "" {
			if !contains(key.Name, opts.Search) && !contains(key.ID, opts.Search) {
				continue
			}
		}

		filtered = append(filtered, key)
	}
	return filtered
}

func (c *Client) sortAPIKeys(keys []APIKey, sortBy, order string) {
	// 实现排序逻辑（使用 sort.Slice）
	// ...
}
```

---

### 3.2 使用统计 (usage.go)

#### 3.2.1 数据结构

```go
package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// UsageStats 使用统计
type UsageStats struct {
	TotalTokens       int64   `json:"totalTokens"`
	InputTokens       int64   `json:"inputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	CacheReadTokens   int64   `json:"cacheReadTokens"`
	RequestCount      int64   `json:"requestCount"`
	TotalCost         float64 `json:"totalCost"`
}

// UsageRecord 使用记录
type UsageRecord struct {
	Timestamp         time.Time `json:"timestamp"`
	Model             string    `json:"model"`
	InputTokens       int64     `json:"inputTokens"`
	OutputTokens      int64     `json:"outputTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	CacheReadTokens   int64     `json:"cacheReadTokens"`
	Cost              float64   `json:"cost"`
}
```

#### 3.2.2 核心方法

```go
// IncrementTokenUsage 增加 Token 使用量
func (c *Client) IncrementTokenUsage(ctx context.Context, params TokenUsageParams) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	pipe := client.Pipeline()

	// 获取当前时区日期
	dateStr := getDateStringInTimezone(time.Now())

	// 1. 按 API Key + 日期 + 模型统计
	dailyKey := fmt.Sprintf("%s%s:%s:%s", PrefixUsageDaily, dateStr, params.KeyID, params.Model)
	pipe.HIncrBy(ctx, dailyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, dailyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, dailyKey, "cacheCreationTokens", params.CacheCreationTokens)
	pipe.HIncrBy(ctx, dailyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, dailyKey, "requestCount", 1)
	pipe.Expire(ctx, dailyKey, TTLUsageDaily)

	// 2. 按小时统计（用于实时监控）
	hour := getHourInTimezone(time.Now())
	hourlyKey := fmt.Sprintf("%s%s:%02d:%s:%s", PrefixUsageHourly, dateStr, hour, params.KeyID, params.Model)
	pipe.HIncrBy(ctx, hourlyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, hourlyKey, "outputTokens", params.OutputTokens)
	pipe.Expire(ctx, hourlyKey, TTLUsageHourly)

	// 3. 按账户统计（如果提供了 accountId）
	if params.AccountID != "" {
		accountKey := fmt.Sprintf("%s%s:%s", PrefixAccountUsage, params.AccountID, dateStr)
		pipe.HIncrBy(ctx, accountKey, "inputTokens", params.InputTokens)
		pipe.HIncrBy(ctx, accountKey, "outputTokens", params.OutputTokens)
		pipe.HIncrBy(ctx, accountKey, "requestCount", 1)
		pipe.Expire(ctx, accountKey, TTLUsageDaily)
	}

	// 4. 全局统计
	globalKey := fmt.Sprintf("%sglobal:%s", PrefixUsageDaily, dateStr)
	pipe.HIncrBy(ctx, globalKey, "totalTokens", params.InputTokens+params.OutputTokens)
	pipe.HIncrBy(ctx, globalKey, "requestCount", 1)
	pipe.Expire(ctx, globalKey, TTLUsageDaily)

	// 执行管道
	_, err = pipe.Exec(ctx)
	return err
}

// TokenUsageParams Token 使用参数
type TokenUsageParams struct {
	KeyID               string
	AccountID           string
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

// GetUsageStats 获取使用统计
func (c *Client) GetUsageStats(ctx context.Context, keyID string, days int) (*UsageStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	stats := &UsageStats{}
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dateStr := getDateStringInTimezone(date)

		// 获取该日期下所有模型的使用统计
		pattern := fmt.Sprintf("%s%s:%s:*", PrefixUsageDaily, dateStr, keyID)
		keys, err := c.ScanKeys(ctx, pattern, 100)
		if err != nil {
			logger.Warn("Failed to scan usage keys", zap.Error(err))
			continue
		}

		for _, key := range keys {
			data, err := client.HGetAll(ctx, key).Result()
			if err != nil {
				continue
			}

			stats.InputTokens += parseInt64(data["inputTokens"])
			stats.OutputTokens += parseInt64(data["outputTokens"])
			stats.CacheCreationTokens += parseInt64(data["cacheCreationTokens"])
			stats.CacheReadTokens += parseInt64(data["cacheReadTokens"])
			stats.RequestCount += parseInt64(data["requestCount"])
		}
	}

	stats.TotalTokens = stats.InputTokens + stats.OutputTokens +
	                   stats.CacheCreationTokens + stats.CacheReadTokens

	return stats, nil
}

// 辅助函数
func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func parseFloat64(s string) float64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
```

---

### 3.3 成本统计 (cost.go)

```go
package redis

import (
	"context"
	"fmt"
	"time"
)

// CostStats 成本统计
type CostStats struct {
	TotalCost       float64 `json:"totalCost"`
	InputCost       float64 `json:"inputCost"`
	OutputCost      float64 `json:"outputCost"`
	CacheCost       float64 `json:"cacheCost"`
	RequestCount    int64   `json:"requestCount"`
}

// IncrementDailyCost 增加每日成本
func (c *Client) IncrementDailyCost(ctx context.Context, keyID string, amount float64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	dateStr := getDateStringInTimezone(time.Now())
	costKey := fmt.Sprintf("cost:daily:%s:%s", dateStr, keyID)

	return client.HIncrByFloat(ctx, costKey, "totalCost", amount).Err()
}

// GetDailyCost 获取每日成本
func (c *Client) GetDailyCost(ctx context.Context, keyID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	dateStr := getDateStringInTimezone(time.Now())
	costKey := fmt.Sprintf("cost:daily:%s:%s", dateStr, keyID)

	result, err := client.HGet(ctx, costKey, "totalCost").Result()
	if err != nil {
		return 0, nil // 未找到返回 0
	}

	return parseFloat64(result), nil
}

// GetCostStats 获取成本统计
func (c *Client) GetCostStats(ctx context.Context, keyID string, days int) (*CostStats, error) {
	stats := &CostStats{}
	now := time.Now()

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dateStr := getDateStringInTimezone(date)
		cost, _ := c.GetDailyCost(ctx, keyID)
		stats.TotalCost += cost
	}

	return stats, nil
}
```

---

### 3.4 并发控制 (concurrency.go)

#### 3.4.1 Lua 脚本

```lua
-- scripts/concurrency.lua
-- 并发控制脚本 (acquire lease)

local key = KEYS[1]           -- concurrency:{accountId}
local requestId = ARGV[1]     -- 请求唯一ID
local maxConcurrency = tonumber(ARGV[2])  -- 最大并发数
local currentTime = tonumber(ARGV[3])     -- 当前时间戳（毫秒）
local ttl = tonumber(ARGV[4])            -- 租约TTL（毫秒）

-- 1. 清理过期的并发计数
redis.call('ZREMRANGEBYSCORE', key, '-inf', currentTime)

-- 2. 获取当前并发数
local currentCount = redis.call('ZCARD', key)

-- 3. 检查是否超过限制
if currentCount >= maxConcurrency then
    return {0, currentCount}  -- 失败，返回当前并发数
end

-- 4. 添加新的租约
local expiryTime = currentTime + ttl
redis.call('ZADD', key, expiryTime, requestId)

-- 5. 设置 Key 的过期时间（防止永久存在）
redis.call('EXPIRE', key, math.ceil(ttl / 1000) + 60)

return {1, currentCount + 1}  -- 成功，返回新的并发数
```

```lua
-- scripts/concurrency_release.lua
-- 释放并发租约

local key = KEYS[1]
local requestId = ARGV[1]

-- 移除指定的租约
local removed = redis.call('ZREM', key, requestId)

-- 返回是否成功释放
return removed
```

#### 3.4.2 Go 实现

```go
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultConcurrencyTTL = 300000 // 5分钟（毫秒）
)

// AcquireConcurrencyLease 获取并发租约
func (c *Client) AcquireConcurrencyLease(ctx context.Context, accountID string, maxConcurrency int, ttl time.Duration) (string, bool, error) {
	script := c.loadLuaScript("concurrency.lua")

	requestID := uuid.New().String()
	currentTime := time.Now().UnixMilli()

	key := PrefixConcurrency + accountID

	result, err := script.Run(ctx, c.client, []string{key},
		requestID, maxConcurrency, currentTime, ttl.Milliseconds()).Result()
	if err != nil {
		return "", false, err
	}

	// 解析结果
	arr := result.([]interface{})
	success := arr[0].(int64) == 1

	if success {
		logger.Debug("Acquired concurrency lease",
			zap.String("accountId", accountID),
			zap.String("requestId", requestID))
		return requestID, true, nil
	}

	return "", false, nil
}

// ReleaseConcurrencyLease 释放并发租约
func (c *Client) ReleaseConcurrencyLease(ctx context.Context, accountID, requestID string) error {
	script := c.loadLuaScript("concurrency_release.lua")

	key := PrefixConcurrency + accountID

	_, err := script.Run(ctx, c.client, []string{key}, requestID).Result()
	if err != nil {
		return err
	}

	logger.Debug("Released concurrency lease",
		zap.String("accountId", accountID),
		zap.String("requestId", requestID))

	return nil
}

// GetConcurrency 获取当前并发数
func (c *Client) GetConcurrency(ctx context.Context, accountID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrency + accountID

	// 先清理过期
	currentTime := time.Now().UnixMilli()
	client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", currentTime))

	// 获取计数
	count, err := client.ZCard(ctx, key).Result()
	if err != nil {
		return 0, err
	}

	return count, nil
}
```

---

### 3.5 分布式锁 (lock.go)

#### 3.5.1 Lua 脚本

```lua
-- scripts/lock.lua
-- 分布式锁释放脚本

local key = KEYS[1]
local token = ARGV[1]

-- 只有持有锁的客户端才能释放
if redis.call('GET', key) == token then
    return redis.call('DEL', key)
else
    return 0
end
```

#### 3.5.2 Go 实现

```go
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AcquireLock 获取分布式锁
func (c *Client) AcquireLock(ctx context.Context, lockKey string, ttl time.Duration) (string, bool, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return "", false, err
	}

	token := uuid.New().String()

	// SET NX EX 原子操作
	success, err := client.SetNX(ctx, lockKey, token, ttl).Result()
	if err != nil {
		return "", false, err
	}

	if success {
		logger.Debug("Acquired lock", zap.String("key", lockKey))
	}

	return token, success, nil
}

// ReleaseLock 释放分布式锁
func (c *Client) ReleaseLock(ctx context.Context, lockKey, token string) error {
	script := c.loadLuaScript("lock.lua")

	result, err := script.Run(ctx, c.client, []string{lockKey}, token).Result()
	if err != nil {
		return err
	}

	if result.(int64) == 1 {
		logger.Debug("Released lock", zap.String("key", lockKey))
	} else {
		logger.Warn("Failed to release lock: token mismatch", zap.String("key", lockKey))
	}

	return nil
}

// TryLockWithRetry 重试获取锁
func (c *Client) TryLockWithRetry(ctx context.Context, lockKey string, ttl time.Duration, maxRetries int, retryDelay time.Duration) (string, error) {
	for i := 0; i < maxRetries; i++ {
		token, success, err := c.AcquireLock(ctx, lockKey, ttl)
		if err != nil {
			return "", err
		}

		if success {
			return token, nil
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
```

---

### 3.6 Lua 脚本加载器

```go
package redis

import (
	"embed"
	"sync"

	"github.com/redis/go-redis/v9"
)

//go:embed scripts/*.lua
var luaScripts embed.FS

var (
	scriptCache = make(map[string]*redis.Script)
	scriptMu    sync.RWMutex
)

// loadLuaScript 加载并缓存 Lua 脚本
func (c *Client) loadLuaScript(filename string) *redis.Script {
	scriptMu.RLock()
	if script, ok := scriptCache[filename]; ok {
		scriptMu.RUnlock()
		return script
	}
	scriptMu.RUnlock()

	// 读取脚本文件
	scriptMu.Lock()
	defer scriptMu.Unlock()

	// Double check
	if script, ok := scriptCache[filename]; ok {
		return script
	}

	content, err := luaScripts.ReadFile("scripts/" + filename)
	if err != nil {
		logger.Fatal("Failed to load Lua script", zap.String("file", filename), zap.Error(err))
	}

	script := redis.NewScript(string(content))
	scriptCache[filename] = script

	logger.Info("Loaded Lua script", zap.String("file", filename))
	return script
}
```

---

## 4. 时区处理

**关键点**：必须与 Node.js 保持完全一致的时区处理逻辑

```go
package redis

import (
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
)

// getDateInTimezone 获取指定时区的日期（对应 Node.js 的 getDateInTimezone）
func getDateInTimezone(date time.Time) time.Time {
	offset := config.Cfg.System.TimezoneOffset
	offsetMs := time.Duration(offset) * time.Hour
	return date.Add(offsetMs)
}

// getDateStringInTimezone 获取指定时区的日期字符串 (YYYY-MM-DD)
func getDateStringInTimezone(date time.Time) string {
	tzDate := getDateInTimezone(date)
	return tzDate.UTC().Format("2006-01-02")
}

// getHourInTimezone 获取指定时区的小时 (0-23)
func getHourInTimezone(date time.Time) int {
	tzDate := getDateInTimezone(date)
	return tzDate.UTC().Hour()
}

// getWeekStringInTimezone 获取指定时区的 ISO 周 (YYYY-Wxx)
func getWeekStringInTimezone(date time.Time) string {
	tzDate := getDateInTimezone(date)
	year, week := tzDate.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}
```

---

## 5. 测试和验证

### 5.1 单元测试

```go
package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
)

func TestAPIKeyOperations(t *testing.T) {
	ctx := context.Background()
	client := redis.GetInstance()

	// 创建测试 API Key
	key := &redis.APIKey{
		ID:        "test_key_" + time.Now().Format("20060102150405"),
		Name:      "Test Key",
		HashedKey: "hash_" + time.Now().Format("20060102150405"),
		Limit:     1000,
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	// 测试保存
	err := client.SetAPIKey(ctx, key)
	assert.NoError(t, err)

	// 测试读取
	retrieved, err := client.GetAPIKey(ctx, key.ID)
	assert.NoError(t, err)
	assert.Equal(t, key.Name, retrieved.Name)

	// 测试通过哈希查询
	retrieved, err = client.GetAPIKeyByHash(ctx, key.HashedKey)
	assert.NoError(t, err)
	assert.Equal(t, key.ID, retrieved.ID)

	// 测试更新
	err = client.UpdateAPIKeyFields(ctx, key.ID, map[string]interface{}{
		"limit": 2000,
	})
	assert.NoError(t, err)

	// 测试删除
	err = client.DeleteAPIKey(ctx, key.ID)
	assert.NoError(t, err)
}

func TestConcurrencyControl(t *testing.T) {
	ctx := context.Background()
	client := redis.GetInstance()

	accountID := "test_account"
	maxConcurrency := 5

	// 测试获取租约
	requestID, success, err := client.AcquireConcurrencyLease(ctx, accountID, maxConcurrency, 5*time.Minute)
	assert.NoError(t, err)
	assert.True(t, success)

	// 测试获取并发数
	count, err := client.GetConcurrency(ctx, accountID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// 测试释放租约
	err = client.ReleaseConcurrencyLease(ctx, accountID, requestID)
	assert.NoError(t, err)

	// 验证释放后并发数为0
	count, err = client.GetConcurrency(ctx, accountID)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
}
```

### 5.2 集成测试

```bash
# 创建测试脚本
cat > scripts/test-redis-ops.sh << 'EOF'
#!/bin/bash

echo "🧪 Testing Redis Operations..."

# 1. 测试 API Key 操作
echo "Testing API Key CRUD..."
go run cmd/test/redis_test.go apikey

# 2. 测试并发控制
echo "Testing Concurrency Control..."
go run cmd/test/redis_test.go concurrency

# 3. 测试使用统计
echo "Testing Usage Stats..."
go run cmd/test/redis_test.go usage

# 4. 测试分布式锁
echo "Testing Distributed Lock..."
go run cmd/test/redis_test.go lock

echo "✅ All tests passed!"
EOF

chmod +x scripts/test-redis-ops.sh
./scripts/test-redis-ops.sh
```

### 5.3 与 Node.js 兼容性测试

```bash
# 1. Node.js 创建数据
curl -X POST http://localhost:3000/admin/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"go-compat-test","limit":5000}'

# 2. Go 读取数据
curl http://localhost:8080/test/redis/apikey/go-compat-test

# 3. Go 修改数据
curl -X PUT http://localhost:8080/test/redis/apikey/go-compat-test \
  -d '{"limit":10000}'

# 4. Node.js 验证修改
curl http://localhost:3000/admin/api-keys/go-compat-test

# 预期：双向读写完全兼容
```

---

## 6. 性能优化

### 6.1 连接池配置

```go
// client.go 中的连接池优化
opts := &redis.Options{
	// ... 现有配置
	PoolSize:     100,           // 连接池大小
	MinIdleConns: 10,            // 最小空闲连接
	MaxRetries:   3,             // 最大重试次数
	PoolTimeout:  4 * time.Second, // 连接池超时
}
```

### 6.2 Pipeline 批量操作

```go
// 批量获取 API Keys
func (c *Client) GetAPIKeysBatch(ctx context.Context, keyIDs []string) (map[string]*APIKey, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	pipe := client.Pipeline()
	cmds := make(map[string]*redis.StringCmd)

	for _, keyID := range keyIDs {
		redisKey := PrefixAPIKey + keyID
		cmds[keyID] = pipe.Get(ctx, redisKey)
	}

	_, err = pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	results := make(map[string]*APIKey)
	for keyID, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			continue
		}

		var key APIKey
		if err := json.Unmarshal([]byte(data), &key); err != nil {
			continue
		}

		results[keyID] = &key
	}

	return results, nil
}
```

### 6.3 缓存优化

```go
// 添加内存缓存层（LRU）
import "github.com/hashicorp/golang-lru/v2"

type CachedRedisClient struct {
	*Client
	apiKeyCache *lru.Cache[string, *APIKey]
}

func NewCachedRedisClient(client *Client) (*CachedRedisClient, error) {
	cache, err := lru.New[string, *APIKey](1000) // 缓存1000个Key
	if err != nil {
		return nil, err
	}

	return &CachedRedisClient{
		Client:      client,
		apiKeyCache: cache,
	}, nil
}

func (c *CachedRedisClient) GetAPIKey(ctx context.Context, keyID string) (*APIKey, error) {
	// 先查缓存
	if key, ok := c.apiKeyCache.Get(keyID); ok {
		return key, nil
	}

	// 缓存未命中，从 Redis 读取
	key, err := c.Client.GetAPIKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	// 写入缓存
	c.apiKeyCache.Add(keyID, key)
	return key, nil
}
```

---

## 7. 检查清单

### 7.1 核心功能

- [ ] API Key CRUD 操作
- [ ] API Key 哈希映射（快速查找）
- [ ] API Key 分页查询
- [ ] 使用统计记录（Token 计数）
- [ ] 成本统计记录
- [ ] 并发控制（Lua 脚本）
- [ ] 分布式锁（SET NX + Lua 释放）
- [ ] 账户数据管理
- [ ] 会话管理
- [ ] 请求排队控制

### 7.2 兼容性

- [ ] Redis Key 命名完全一致
- [ ] 数据结构 JSON 字段完全对应
- [ ] 时区处理逻辑一致
- [ ] Lua 脚本行为一致
- [ ] TTL 设置一致

### 7.3 性能

- [ ] 连接池配置优化
- [ ] Pipeline 批量操作
- [ ] LRU 缓存实现
- [ ] SCAN 替代 KEYS（避免阻塞）

### 7.4 测试

- [ ] 单元测试覆盖核心方法
- [ ] 集成测试验证完整流程
- [ ] 与 Node.js 双向兼容性测试
- [ ] 压力测试（并发读写）

---

## 8. 下一步

完成本阶段后，进入 **03-step3-core-services.md**：
- API Key 服务（验证、限流、权限）
- 认证中间件实现
- 统一调度器
- 账户服务

---

**文档版本**: v1.0
**创建日期**: 2024-12-18
**维护者**: Claude Relay Team
