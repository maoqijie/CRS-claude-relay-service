package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 批量操作常量
const (
	// APIKeyBatchSize API Key 批量获取大小
	APIKeyBatchSize = 500
	// APIKeyMaxPageSize 分页最大页面大小
	APIKeyMaxPageSize = 100
	// APIKeyDefaultPageSize 默认页面大小
	APIKeyDefaultPageSize = 20
	// APIKeyScanLimit 扫描限制
	APIKeyScanLimit = 20000
)

// APIKey API Key 数据结构（与 Node.js 保持一致）
type APIKey struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	HashedKey   string     `json:"hashedKey,omitempty"`   // 存储哈希后的 Key
	APIKey      string     `json:"apiKey,omitempty"`      // 兼容 Node.js 字段名（存储哈希值）
	Limit       int64      `json:"limit"`                 // 每日限额
	UsedToday   int64      `json:"usedToday,omitempty"`   // 今日使用量
	IsActive    bool       `json:"isActive"`              // 是否激活
	CreatedAt   time.Time  `json:"createdAt"`             // 创建时间
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`   // 过期时间
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`  // 最后使用时间
	IsDeleted   bool       `json:"isDeleted,omitempty"`   // 是否已删除
	Description string     `json:"description,omitempty"` // 描述

	// 权限和限制
	Permissions      []string `json:"permissions,omitempty"`      // 权限列表 (all, claude, gemini, openai)
	AllowedClients   []string `json:"allowedClients,omitempty"`   // 允许的客户端
	ModelBlacklist   []string `json:"modelBlacklist,omitempty"`   // 模型黑名单
	ConcurrentLimit  int      `json:"concurrentLimit,omitempty"`  // 并发限制
	RateLimitPerMin  int      `json:"rateLimitPerMin,omitempty"`  // 每分钟请求限制
	RateLimitPerHour int      `json:"rateLimitPerHour,omitempty"` // 每小时请求限制

	// 并发排队配置
	ConcurrentRequestQueueEnabled           bool    `json:"concurrentRequestQueueEnabled,omitempty"`
	ConcurrentRequestQueueMaxSize           int     `json:"concurrentRequestQueueMaxSize,omitempty"`
	ConcurrentRequestQueueMaxSizeMultiplier float64 `json:"concurrentRequestQueueMaxSizeMultiplier,omitempty"`
	ConcurrentRequestQueueTimeoutMs         int     `json:"concurrentRequestQueueTimeoutMs,omitempty"`

	// 成本限制
	DailyCostLimit      float64 `json:"dailyCostLimit,omitempty"`      // 每日成本限制（美元）
	TotalCostLimit      float64 `json:"totalCostLimit,omitempty"`      // 总成本限制（美元）
	WeeklyOpusCostLimit float64 `json:"weeklyOpusCostLimit,omitempty"` // Opus 周成本限制（美元）

	// 速率限制（窗口费用）
	RateLimitWindow int     `json:"rateLimitWindow,omitempty"` // 速率限制窗口（分钟）
	RateLimitCost   float64 `json:"rateLimitCost,omitempty"`   // 窗口内费用限制（美元）

	// 激活模式
	ExpirationMode string     `json:"expirationMode,omitempty"` // 过期模式：fixed / activation
	ActivationDays int        `json:"activationDays,omitempty"` // 激活后有效天数
	ActivationUnit string     `json:"activationUnit,omitempty"` // 激活时间单位：days / hours
	IsActivated    bool       `json:"isActivated,omitempty"`    // 是否已激活
	ActivatedAt    *time.Time `json:"activatedAt,omitempty"`    // 激活时间

	// FuelPack 加油包
	FuelBalance        float64 `json:"fuelBalance,omitempty"`        // 加油包余额（美元）
	FuelEntries        int     `json:"fuelEntries,omitempty"`        // 加油包条目数
	FuelNextExpiresAtMs int64   `json:"fuelNextExpiresAtMs,omitempty"` // 最近过期时间（毫秒时间戳）

	// 用户管理
	UserID string   `json:"userId,omitempty"` // 关联用户 ID
	Tags   []string `json:"tags,omitempty"`   // 标签
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
	TotalKeys     int `json:"totalKeys"`
	ActiveKeys    int `json:"activeKeys"`
	ExpiredKeys   int `json:"expiredKeys"`
	DeletedKeys   int `json:"deletedKeys"`
	KeysWithUsers int `json:"keysWithUsers"`
}

// APIKeyQueryOptions 查询选项
type APIKeyQueryOptions struct {
	Page           int      // 页码 (从 1 开始)
	PageSize       int      // 每页数量
	IncludeDeleted bool     // 是否包含已删除的
	UserID         string   // 按用户 ID 过滤
	Tags           []string // 按标签过滤
	IsActive       *bool    // 按激活状态过滤
	Search         string   // 搜索关键词 (名称或 ID)
	SortBy         string   // 排序字段 (createdAt, name, usedToday)
	SortOrder      string   // 排序顺序 (asc, desc)
}

// getHashedKeyValue 获取哈希键值（HashedKey 为主，APIKey 为兼容别名）
func (key *APIKey) getHashedKeyValue() string {
	if key.HashedKey != "" {
		return key.HashedKey
	}
	return key.APIKey
}

// syncHashedKeyFields 同步 HashedKey 和 APIKey 字段（Node.js 兼容）
func (key *APIKey) syncHashedKeyFields() {
	hashValue := key.getHashedKeyValue()
	if hashValue != "" {
		key.HashedKey = hashValue
		key.APIKey = hashValue
	}
}

// SetAPIKey 创建或更新 API Key
func (c *Client) SetAPIKey(ctx context.Context, key *APIKey) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	// 同步 HashedKey 和 APIKey 字段（Node.js 兼容）
	key.syncHashedKeyFields()

	// 转换为 map 以支持 HSET
	data := apiKeyToMap(key)

	// 保存到 Redis（使用 HSET，与 Node.js 兼容）
	redisKey := PrefixAPIKey + key.ID
	if err := client.HSet(ctx, redisKey, data).Err(); err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}

	// 设置过期时间
	if err := client.Expire(ctx, redisKey, TTLAPIKey).Err(); err != nil {
		logger.Warn("Failed to set API key TTL", zap.Error(err))
	}

	// 更新哈希映射（快速查找）
	if hashKey := key.getHashedKeyValue(); hashKey != "" {
		if err := client.HSet(ctx, PrefixAPIKeyHashMap, hashKey, key.ID).Err(); err != nil {
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

	// 尝试新前缀
	redisKey := PrefixAPIKey + keyID
	data, err := client.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	// 如果新前缀没有数据，尝试旧前缀
	if len(data) == 0 {
		legacyKey := PrefixAPIKeyLegacy + keyID
		data, err = client.HGetAll(ctx, legacyKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get API key (legacy): %w", err)
		}
	}

	if len(data) == 0 {
		return nil, nil // 未找到返回 nil
	}

	key := mapToAPIKey(data)
	key.ID = keyID
	return key, nil
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
			return nil, nil // 未找到
		}
		return nil, fmt.Errorf("failed to get API key ID: %w", err)
	}

	// 获取完整数据
	return c.GetAPIKey(ctx, keyID)
}

// GetAllAPIKeys 获取所有 API Key
func (c *Client) GetAllAPIKeys(ctx context.Context, includeDeleted bool) ([]APIKey, error) {
	// 先从哈希映射获取所有 ID
	keyIDs, err := c.scanAPIKeyIDs(ctx)
	if err != nil {
		return nil, err
	}

	// 批量获取
	return c.batchGetAPIKeys(ctx, keyIDs, includeDeleted)
}

// scanAPIKeyIDs 获取所有 API Key ID
func (c *Client) scanAPIKeyIDs(ctx context.Context) ([]string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	keyIDSet := make(map[string]struct{})

	// 1. 从哈希映射获取（高效）
	mappedIDs, err := client.HVals(ctx, PrefixAPIKeyHashMap).Result()
	if err == nil && len(mappedIDs) > 0 {
		for _, id := range mappedIDs {
			if id != "" {
				keyIDSet[id] = struct{}{}
			}
		}
	}

	// 2. SCAN 新前缀
	keys, err := c.ScanKeys(ctx, PrefixAPIKey+"*", APIKeyScanLimit)
	if err == nil {
		for _, key := range keys {
			if key == PrefixAPIKeyHashMap {
				continue
			}
			keyID := strings.TrimPrefix(key, PrefixAPIKey)
			if keyID != "" {
				keyIDSet[keyID] = struct{}{}
			}
		}
	}

	// 3. SCAN 旧前缀（兼容）
	legacyKeys, err := c.ScanKeys(ctx, PrefixAPIKeyLegacy+"*", APIKeyScanLimit)
	if err == nil {
		for _, key := range legacyKeys {
			keyID := strings.TrimPrefix(key, PrefixAPIKeyLegacy)
			if keyID != "" {
				keyIDSet[keyID] = struct{}{}
			}
		}
	}

	// 转换为 slice
	keyIDs := make([]string, 0, len(keyIDSet))
	for id := range keyIDSet {
		keyIDs = append(keyIDs, id)
	}

	return keyIDs, nil
}

// batchGetAPIKeys 批量获取 API Keys（使用 Pipeline）
func (c *Client) batchGetAPIKeys(ctx context.Context, keyIDs []string, includeDeleted bool) ([]APIKey, error) {
	if len(keyIDs) == 0 {
		return []APIKey{}, nil
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	var apiKeys []APIKey

	for offset := 0; offset < len(keyIDs); offset += APIKeyBatchSize {
		end := offset + APIKeyBatchSize
		if end > len(keyIDs) {
			end = len(keyIDs)
		}
		chunkIDs := keyIDs[offset:end]

		// Pipeline 批量获取
		pipe := client.Pipeline()
		cmds := make(map[string]*redis.MapStringStringCmd)

		for _, keyID := range chunkIDs {
			redisKey := PrefixAPIKey + keyID
			cmds[keyID] = pipe.HGetAll(ctx, redisKey)
		}

		_, err := pipe.Exec(ctx)
		if err != nil && err != redis.Nil {
			logger.Warn("Failed to batch get API keys", zap.Error(err))
		}

		for keyID, cmd := range cmds {
			data, err := cmd.Result()
			if err != nil || len(data) == 0 {
				continue
			}

			key := mapToAPIKey(data)
			key.ID = keyID

			// 过滤已删除的
			if !includeDeleted && key.IsDeleted {
				continue
			}

			apiKeys = append(apiKeys, *key)
		}
	}

	return apiKeys, nil
}

// DeleteAPIKey 删除 API Key（软删除）
func (c *Client) DeleteAPIKey(ctx context.Context, keyID string) error {
	key, err := c.GetAPIKey(ctx, keyID)
	if err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("API key not found: %s", keyID)
	}

	// 标记为已删除
	key.IsDeleted = true
	return c.SetAPIKey(ctx, key)
}

// HardDeleteAPIKey 硬删除 API Key
func (c *Client) HardDeleteAPIKey(ctx context.Context, keyID string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	// 获取 Key 以获取哈希值
	key, _ := c.GetAPIKey(ctx, keyID)

	// 删除主键
	redisKey := PrefixAPIKey + keyID
	legacyKey := PrefixAPIKeyLegacy + keyID

	deleted, _ := client.Del(ctx, redisKey, legacyKey).Result()

	// 删除哈希映射
	if key != nil {
		if hashKey := key.getHashedKeyValue(); hashKey != "" {
			client.HDel(ctx, PrefixAPIKeyHashMap, hashKey)
		}
	}

	logger.Info("API Key hard deleted", zap.String("id", keyID), zap.Int64("deleted", deleted))
	return nil
}

// UpdateAPIKeyFields 更新指定字段
func (c *Client) UpdateAPIKeyFields(ctx context.Context, keyID string, updates map[string]interface{}) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	redisKey := PrefixAPIKey + keyID

	// 检查 Key 是否存在
	exists, err := client.Exists(ctx, redisKey).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		// 尝试旧前缀
		legacyKey := PrefixAPIKeyLegacy + keyID
		exists, _ = client.Exists(ctx, legacyKey).Result()
		if exists == 0 {
			return fmt.Errorf("API key not found: %s", keyID)
		}
		redisKey = legacyKey
	}

	stringUpdates, newHashValue, hashValueUpdated := normalizeAPIKeyFieldUpdates(updates)
	var oldHashValue string
	if hashValueUpdated {
		oldHashValue, err = getHashedKeyValueFromRedis(ctx, client, redisKey)
		if err != nil {
			return err
		}
	}

	// 更新字段 + 维护哈希映射（保证 GetAPIKeyByHash 可用，旧哈希不再生效）
	_, err = client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, redisKey, stringUpdates)

		if hashValueUpdated {
			if oldHashValue != "" && oldHashValue != newHashValue {
				pipe.HDel(ctx, PrefixAPIKeyHashMap, oldHashValue)
			}
			if newHashValue != "" {
				pipe.HSet(ctx, PrefixAPIKeyHashMap, newHashValue, keyID)
			}
		}

		pipe.Expire(ctx, redisKey, TTLAPIKey)
		return nil
	})
	return err
}

// GetAPIKeysPaginated 分页获取 API Key
func (c *Client) GetAPIKeysPaginated(ctx context.Context, opts APIKeyQueryOptions) (*APIKeyPaginated, error) {
	// 设置默认值
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = APIKeyDefaultPageSize
	}
	if opts.PageSize > APIKeyMaxPageSize {
		opts.PageSize = APIKeyMaxPageSize
	}

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

// GetAPIKeyStats 获取 API Key 统计
func (c *Client) GetAPIKeyStats(ctx context.Context) (*APIKeyStats, error) {
	allKeys, err := c.GetAllAPIKeys(ctx, true)
	if err != nil {
		return nil, err
	}

	stats := &APIKeyStats{
		TotalKeys: len(allKeys),
	}

	now := time.Now()
	for _, key := range allKeys {
		if key.IsDeleted {
			stats.DeletedKeys++
			continue
		}

		if key.IsActive {
			stats.ActiveKeys++
		}

		if key.ExpiresAt != nil && key.ExpiresAt.Before(now) {
			stats.ExpiredKeys++
		}

		if key.UserID != "" {
			stats.KeysWithUsers++
		}
	}

	return stats, nil
}

// filterAPIKeys 过滤 API Keys
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
			search := strings.ToLower(opts.Search)
			if !strings.Contains(strings.ToLower(key.Name), search) &&
				!strings.Contains(strings.ToLower(key.ID), search) {
				continue
			}
		}

		filtered = append(filtered, key)
	}
	return filtered
}

// sortAPIKeys 排序 API Keys
func (c *Client) sortAPIKeys(keys []APIKey, sortBy, order string) {
	if sortBy == "" {
		sortBy = "createdAt"
	}
	if order == "" {
		order = "desc"
	}

	sort.Slice(keys, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "name":
			less = keys[i].Name < keys[j].Name
		case "usedToday":
			less = keys[i].UsedToday < keys[j].UsedToday
		case "limit":
			less = keys[i].Limit < keys[j].Limit
		default: // createdAt
			less = keys[i].CreatedAt.Before(keys[j].CreatedAt)
		}

		if order == "desc" {
			return !less
		}
		return less
	})
}

// ========== 辅助函数 ==========

// apiKeyToMap 将 APIKey 转换为 map
func apiKeyToMap(key *APIKey) map[string]interface{} {
	m := map[string]interface{}{
		"id":        key.ID,
		"name":      key.Name,
		"limit":     fmt.Sprintf("%d", key.Limit),
		"isActive":  fmt.Sprintf("%t", key.IsActive),
		"createdAt": key.CreatedAt.Format(time.RFC3339),
		"isDeleted": fmt.Sprintf("%t", key.IsDeleted),
	}

	if key.HashedKey != "" {
		m["hashedKey"] = key.HashedKey
	}
	if key.APIKey != "" {
		m["apiKey"] = key.APIKey
	}
	if key.Description != "" {
		m["description"] = key.Description
	}
	if key.ExpiresAt != nil {
		m["expiresAt"] = key.ExpiresAt.Format(time.RFC3339)
	}
	if key.LastUsedAt != nil {
		m["lastUsedAt"] = key.LastUsedAt.Format(time.RFC3339)
	}
	if key.UserID != "" {
		m["userId"] = key.UserID
	}
	if key.ConcurrentLimit > 0 {
		m["concurrentLimit"] = fmt.Sprintf("%d", key.ConcurrentLimit)
	}
	if key.RateLimitPerMin > 0 {
		m["rateLimitPerMin"] = fmt.Sprintf("%d", key.RateLimitPerMin)
	}
	if key.RateLimitPerHour > 0 {
		m["rateLimitPerHour"] = fmt.Sprintf("%d", key.RateLimitPerHour)
	}

	// 成本限制
	if key.DailyCostLimit > 0 {
		m["dailyCostLimit"] = fmt.Sprintf("%f", key.DailyCostLimit)
	}
	if key.TotalCostLimit > 0 {
		m["totalCostLimit"] = fmt.Sprintf("%f", key.TotalCostLimit)
	}
	if key.WeeklyOpusCostLimit > 0 {
		m["weeklyOpusCostLimit"] = fmt.Sprintf("%f", key.WeeklyOpusCostLimit)
	}

	// 速率限制（窗口费用）
	if key.RateLimitWindow > 0 {
		m["rateLimitWindow"] = fmt.Sprintf("%d", key.RateLimitWindow)
	}
	if key.RateLimitCost > 0 {
		m["rateLimitCost"] = fmt.Sprintf("%f", key.RateLimitCost)
	}

	// 激活模式
	if key.ExpirationMode != "" {
		m["expirationMode"] = key.ExpirationMode
	}
	if key.ActivationDays > 0 {
		m["activationDays"] = fmt.Sprintf("%d", key.ActivationDays)
	}
	if key.ActivationUnit != "" {
		m["activationUnit"] = key.ActivationUnit
	}
	if key.IsActivated {
		m["isActivated"] = "true"
	}
	if key.ActivatedAt != nil {
		m["activatedAt"] = key.ActivatedAt.Format(time.RFC3339)
	}

	// FuelPack 加油包
	if key.FuelBalance > 0 {
		m["fuelBalance"] = fmt.Sprintf("%f", key.FuelBalance)
	}
	if key.FuelEntries > 0 {
		m["fuelEntries"] = fmt.Sprintf("%d", key.FuelEntries)
	}
	if key.FuelNextExpiresAtMs > 0 {
		m["fuelNextExpiresAtMs"] = fmt.Sprintf("%d", key.FuelNextExpiresAtMs)
	}

	// JSON 数组字段
	if len(key.Permissions) > 0 {
		data, _ := json.Marshal(key.Permissions)
		m["permissions"] = string(data)
	}
	if len(key.AllowedClients) > 0 {
		data, _ := json.Marshal(key.AllowedClients)
		m["allowedClients"] = string(data)
	}
	if len(key.ModelBlacklist) > 0 {
		data, _ := json.Marshal(key.ModelBlacklist)
		m["modelBlacklist"] = string(data)
	}
	if len(key.Tags) > 0 {
		data, _ := json.Marshal(key.Tags)
		m["tags"] = string(data)
	}

	// 并发排队配置
	if key.ConcurrentRequestQueueEnabled {
		m["concurrentRequestQueueEnabled"] = "true"
	}
	if key.ConcurrentRequestQueueMaxSize > 0 {
		m["concurrentRequestQueueMaxSize"] = fmt.Sprintf("%d", key.ConcurrentRequestQueueMaxSize)
	}
	if key.ConcurrentRequestQueueMaxSizeMultiplier > 0 {
		m["concurrentRequestQueueMaxSizeMultiplier"] = fmt.Sprintf("%f", key.ConcurrentRequestQueueMaxSizeMultiplier)
	}
	if key.ConcurrentRequestQueueTimeoutMs > 0 {
		m["concurrentRequestQueueTimeoutMs"] = fmt.Sprintf("%d", key.ConcurrentRequestQueueTimeoutMs)
	}

	return m
}

// mapToAPIKey 将 map 转换为 APIKey
func mapToAPIKey(data map[string]string) *APIKey {
	key := &APIKey{
		ID:             data["id"],
		Name:           data["name"],
		HashedKey:      data["hashedKey"],
		APIKey:         data["apiKey"],
		Description:    data["description"],
		UserID:         data["userId"],
		ExpirationMode: data["expirationMode"],
		ActivationUnit: data["activationUnit"],
	}

	// 数值字段
	key.Limit = parseInt64(data["limit"])
	key.UsedToday = parseInt64(data["usedToday"])
	key.ConcurrentLimit = int(parseInt64(data["concurrentLimit"]))
	key.RateLimitPerMin = int(parseInt64(data["rateLimitPerMin"]))
	key.RateLimitPerHour = int(parseInt64(data["rateLimitPerHour"]))
	key.ConcurrentRequestQueueMaxSize = int(parseInt64(data["concurrentRequestQueueMaxSize"]))
	key.ConcurrentRequestQueueTimeoutMs = int(parseInt64(data["concurrentRequestQueueTimeoutMs"]))
	key.ConcurrentRequestQueueMaxSizeMultiplier = parseFloat64(data["concurrentRequestQueueMaxSizeMultiplier"])

	// 成本限制
	key.DailyCostLimit = parseFloat64(data["dailyCostLimit"])
	key.TotalCostLimit = parseFloat64(data["totalCostLimit"])
	key.WeeklyOpusCostLimit = parseFloat64(data["weeklyOpusCostLimit"])

	// 速率限制（窗口费用）
	key.RateLimitWindow = int(parseInt64(data["rateLimitWindow"]))
	key.RateLimitCost = parseFloat64(data["rateLimitCost"])

	// 激活模式
	key.ActivationDays = int(parseInt64(data["activationDays"]))

	// FuelPack 加油包
	key.FuelBalance = parseFloat64(data["fuelBalance"])
	key.FuelEntries = int(parseInt64(data["fuelEntries"]))
	key.FuelNextExpiresAtMs = parseInt64(data["fuelNextExpiresAtMs"])

	// 布尔字段
	key.IsActive = data["isActive"] == "true" || data["isActive"] == "1"
	key.IsDeleted = data["isDeleted"] == "true" || data["isDeleted"] == "1"
	key.ConcurrentRequestQueueEnabled = data["concurrentRequestQueueEnabled"] == "true" || data["concurrentRequestQueueEnabled"] == "1"
	key.IsActivated = data["isActivated"] == "true" || data["isActivated"] == "1"

	// 时间字段
	if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
		key.CreatedAt = t
	}
	if data["expiresAt"] != "" {
		if t, err := time.Parse(time.RFC3339, data["expiresAt"]); err == nil {
			key.ExpiresAt = &t
		}
	}
	if data["lastUsedAt"] != "" {
		if t, err := time.Parse(time.RFC3339, data["lastUsedAt"]); err == nil {
			key.LastUsedAt = &t
		}
	}
	if data["activatedAt"] != "" {
		if t, err := time.Parse(time.RFC3339, data["activatedAt"]); err == nil {
			key.ActivatedAt = &t
		}
	}

	// JSON 数组字段
	if data["permissions"] != "" {
		if err := json.Unmarshal([]byte(data["permissions"]), &key.Permissions); err != nil {
			logger.Warn("Failed to parse permissions JSON", zap.String("data", data["permissions"]), zap.Error(err))
		}
	}
	if data["allowedClients"] != "" {
		if err := json.Unmarshal([]byte(data["allowedClients"]), &key.AllowedClients); err != nil {
			logger.Warn("Failed to parse allowedClients JSON", zap.String("data", data["allowedClients"]), zap.Error(err))
		}
	}
	if data["modelBlacklist"] != "" {
		if err := json.Unmarshal([]byte(data["modelBlacklist"]), &key.ModelBlacklist); err != nil {
			logger.Warn("Failed to parse modelBlacklist JSON", zap.String("data", data["modelBlacklist"]), zap.Error(err))
		}
	}
	if data["tags"] != "" {
		if err := json.Unmarshal([]byte(data["tags"]), &key.Tags); err != nil {
			logger.Warn("Failed to parse tags JSON", zap.String("data", data["tags"]), zap.Error(err))
		}
	}

	return key
}

// hasAnyTag 检查是否包含任意标签
func hasAnyTag(keyTags, searchTags []string) bool {
	tagSet := make(map[string]struct{})
	for _, t := range keyTags {
		tagSet[t] = struct{}{}
	}
	for _, t := range searchTags {
		if _, ok := tagSet[t]; ok {
			return true
		}
	}
	return false
}

// interfaceToString 将 interface{} 转换为字符串
func interfaceToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int, int64, int32:
		return fmt.Sprintf("%d", val)
	case float64, float32:
		return fmt.Sprintf("%f", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case time.Time:
		return val.Format(time.RFC3339)
	case []string:
		data, _ := json.Marshal(val)
		return string(data)
	default:
		data, _ := json.Marshal(val)
		return string(data)
	}
}

func redisValueToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return ""
	}
}

func extractUpdatedHashedKeyValue(updates map[string]interface{}) (string, bool) {
	if updates == nil {
		return "", false
	}
	if v, ok := updates["hashedKey"]; ok {
		if v == nil {
			return "", true
		}
		return interfaceToString(v), true
	}
	if v, ok := updates["apiKey"]; ok {
		if v == nil {
			return "", true
		}
		return interfaceToString(v), true
	}
	return "", false
}

func getHashedKeyValueFromRedis(ctx context.Context, client *redis.Client, redisKey string) (string, error) {
	values, err := client.HMGet(ctx, redisKey, "hashedKey", "apiKey").Result()
	if err != nil {
		return "", err
	}
	if len(values) < 2 {
		if len(values) == 1 {
			return redisValueToString(values[0]), nil
		}
		return "", nil
	}

	hashedKey := redisValueToString(values[0])
	if hashedKey != "" {
		return hashedKey, nil
	}
	return redisValueToString(values[1]), nil
}

func normalizeAPIKeyFieldUpdates(updates map[string]interface{}) (map[string]interface{}, string, bool) {
	newHashValue, hashValueUpdated := extractUpdatedHashedKeyValue(updates)

	stringUpdates := make(map[string]interface{}, len(updates)+2)
	for k, v := range updates {
		if hashValueUpdated && (k == "hashedKey" || k == "apiKey") {
			continue
		}
		stringUpdates[k] = interfaceToString(v)
	}
	if hashValueUpdated {
		stringUpdates["hashedKey"] = newHashValue
		stringUpdates["apiKey"] = newHashValue
	}

	return stringUpdates, newHashValue, hashValueUpdated
}
