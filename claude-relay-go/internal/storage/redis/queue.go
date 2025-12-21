package redis

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 排队相关常量
const (
	// QueueStatsTTL 统计计数保留时间
	QueueStatsTTL = 7 * 24 * time.Hour // 7 天
	// WaitTimeTTL 等待时间样本保留时间
	WaitTimeTTL = 24 * time.Hour // 1 天
	// QueueTTLBuffer 排队计数器 TTL 缓冲时间
	QueueTTLBuffer = 30 * time.Second
)

// Lua 脚本
const (
	luaQueueIncr = `
local key = KEYS[1]
local ttl = tonumber(ARGV[1])

local count = redis.call('INCR', key)
redis.call('EXPIRE', key, ttl)

return count
`

	luaQueueDecr = `
local key = KEYS[1]

local count = redis.call('DECR', key)
if count <= 0 then
    redis.call('DEL', key)
    return 0
end

return count
`
)

// QueueStats 排队统计
type QueueStats struct {
	APIKeyID         string  `json:"apiKeyId"`
	QueueCount       int64   `json:"queueCount"`
	Entered          int64   `json:"entered"`
	Success          int64   `json:"success"`
	Timeout          int64   `json:"timeout"`
	Cancelled        int64   `json:"cancelled"`
	SocketChanged    int64   `json:"socketChanged"`
	RejectedOverload int64   `json:"rejectedOverload"`
	AvgWaitMs        float64 `json:"avgWaitMs"`
	P50WaitMs        float64 `json:"p50WaitMs"`
	P90WaitMs        float64 `json:"p90WaitMs"`
	P99WaitMs        float64 `json:"p99WaitMs"`
}

// GlobalQueueStats 全局排队统计
type GlobalQueueStats struct {
	TotalQueueCount int64        `json:"totalQueueCount"`
	TotalEntered    int64        `json:"totalEntered"`
	TotalSuccess    int64        `json:"totalSuccess"`
	TotalTimeout    int64        `json:"totalTimeout"`
	TotalCancelled  int64        `json:"totalCancelled"`
	GlobalAvgWaitMs float64      `json:"globalAvgWaitMs"`
	GlobalP50WaitMs float64      `json:"globalP50WaitMs"`
	GlobalP90WaitMs float64      `json:"globalP90WaitMs"`
	GlobalP99WaitMs float64      `json:"globalP99WaitMs"`
	PerKeyStats     []QueueStats `json:"perKeyStats,omitempty"`
}

// IncrConcurrencyQueue 增加排队计数
func (c *Client) IncrConcurrencyQueue(ctx context.Context, apiKeyID string, timeoutMs int64) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrencyQueue + apiKeyID

	// TTL = 超时时间 + 缓冲时间
	ttlSeconds := int64(timeoutMs/1000) + int64(QueueTTLBuffer.Seconds())

	result, err := client.Eval(ctx, luaQueueIncr, []string{key}, ttlSeconds).Result()
	if err != nil {
		logger.Error("Failed to increment concurrency queue", zap.Error(err))
		return 0, err
	}

	count, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected result type from queue incr: %T", result)
	}
	logger.Debug("Incremented queue count",
		zap.String("apiKeyId", apiKeyID),
		zap.Int64("count", count),
		zap.Int64("ttlSeconds", ttlSeconds))

	return count, nil
}

// DecrConcurrencyQueue 减少排队计数
func (c *Client) DecrConcurrencyQueue(ctx context.Context, apiKeyID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrencyQueue + apiKeyID

	result, err := client.Eval(ctx, luaQueueDecr, []string{key}).Result()
	if err != nil {
		logger.Error("Failed to decrement concurrency queue", zap.Error(err))
		return 0, err
	}

	count, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected result type from queue decr: %T", result)
	}
	if count == 0 {
		logger.Debug("Queue count is 0, removed key", zap.String("apiKeyId", apiKeyID))
	} else {
		logger.Debug("Decremented queue count",
			zap.String("apiKeyId", apiKeyID),
			zap.Int64("count", count))
	}

	return count, nil
}

// GetConcurrencyQueueCount 获取排队计数
func (c *Client) GetConcurrencyQueueCount(ctx context.Context, apiKeyID string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	key := PrefixConcurrencyQueue + apiKeyID
	result, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == goredis.Nil {
			return 0, nil // 未找到返回 0
		}
		return 0, err
	}

	count, err := strconv.ParseInt(result, 10, 64)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ClearConcurrencyQueue 清空排队计数
func (c *Client) ClearConcurrencyQueue(ctx context.Context, apiKeyID string) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixConcurrencyQueue + apiKeyID
	_, err = client.Del(ctx, key).Result()
	if err != nil {
		return err
	}

	logger.Debug("Cleared queue count", zap.String("apiKeyId", apiKeyID))
	return nil
}

// IncrQueueStats 增加排队统计
func (c *Client) IncrQueueStats(ctx context.Context, apiKeyID, field string, delta int64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	key := PrefixConcurrencyQueueStats + apiKeyID

	pipe := client.Pipeline()
	pipe.HIncrBy(ctx, key, field, delta)
	pipe.Expire(ctx, key, TTLQueueStats)

	_, err = pipe.Exec(ctx)
	return err
}

// RecordWaitTime 记录等待时间
func (c *Client) RecordWaitTime(ctx context.Context, apiKeyID string, waitMs int64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	// 每 API Key 的等待时间
	keyWaitKey := PrefixConcurrencyQueueWait + apiKeyID
	// 全局等待时间
	globalWaitKey := PrefixConcurrencyQueueWait + "global"

	pipe := client.Pipeline()

	// 添加到列表（从左边添加）
	pipe.LPush(ctx, keyWaitKey, waitMs)
	pipe.LTrim(ctx, keyWaitKey, 0, WaitTimeSamplesPerKey-1) // 保留最新的 N 个
	pipe.Expire(ctx, keyWaitKey, TTLWaitTimeSamples)

	pipe.LPush(ctx, globalWaitKey, waitMs)
	pipe.LTrim(ctx, globalWaitKey, 0, WaitTimeSamplesGlobal-1)
	pipe.Expire(ctx, globalWaitKey, TTLWaitTimeSamples)

	_, err = pipe.Exec(ctx)
	return err
}

// GetQueueStats 获取排队统计
func (c *Client) GetQueueStats(ctx context.Context, apiKeyID string) (*QueueStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	statsKey := PrefixConcurrencyQueueStats + apiKeyID
	queueKey := PrefixConcurrencyQueue + apiKeyID
	waitKey := PrefixConcurrencyQueueWait + apiKeyID

	pipe := client.Pipeline()
	statsCmd := pipe.HGetAll(ctx, statsKey)
	queueCmd := pipe.Get(ctx, queueKey)
	waitCmd := pipe.LRange(ctx, waitKey, 0, WaitTimeSamplesPerKey-1)

	pipe.Exec(ctx)

	stats := &QueueStats{
		APIKeyID: apiKeyID,
	}

	// 解析统计数据
	if data, err := statsCmd.Result(); err == nil {
		stats.Entered = parseInt64(data["entered"])
		stats.Success = parseInt64(data["success"])
		stats.Timeout = parseInt64(data["timeout"])
		stats.Cancelled = parseInt64(data["cancelled"])
		stats.SocketChanged = parseInt64(data["socket_changed"])
		stats.RejectedOverload = parseInt64(data["rejected_overload"])
	}

	// 获取当前排队数
	if result, err := queueCmd.Result(); err == nil {
		stats.QueueCount, _ = strconv.ParseInt(result, 10, 64)
	}

	// 计算等待时间统计
	if waitTimes, err := waitCmd.Result(); err == nil && len(waitTimes) > 0 {
		times := make([]float64, 0, len(waitTimes))
		for _, t := range waitTimes {
			if v, err := strconv.ParseFloat(t, 64); err == nil {
				times = append(times, v)
			}
		}

		if len(times) > 0 {
			stats.AvgWaitMs = calculateAvg(times)
			stats.P50WaitMs = calculatePercentile(times, 50)
			stats.P90WaitMs = calculatePercentile(times, 90)
			stats.P99WaitMs = calculatePercentile(times, 99)
		}
	}

	return stats, nil
}

// GetGlobalQueueStats 获取全局排队统计
func (c *Client) GetGlobalQueueStats(ctx context.Context, includePerKey bool) (*GlobalQueueStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	globalStats := &GlobalQueueStats{}

	// 扫描所有排队计数器（单次扫描，同时收集 keyIDs）
	queueKeys, _ := c.ScanKeys(ctx, PrefixConcurrencyQueue+"*", 1000)
	keyIDs := make([]string, 0, len(queueKeys))

	for _, key := range queueKeys {
		// 排除统计和等待时间相关的键
		if strings.Contains(key, ":stats:") || strings.Contains(key, ":wait_times:") {
			continue
		}

		keyID := strings.TrimPrefix(key, PrefixConcurrencyQueue)
		keyIDs = append(keyIDs, keyID)

		count, _ := client.Get(ctx, key).Result()
		if v, err := strconv.ParseInt(count, 10, 64); err == nil {
			globalStats.TotalQueueCount += v
		}
	}

	// 扫描所有统计
	statsKeys, _ := c.ScanKeys(ctx, PrefixConcurrencyQueueStats+"*", 1000)
	for _, key := range statsKeys {
		data, _ := client.HGetAll(ctx, key).Result()
		globalStats.TotalEntered += parseInt64(data["entered"])
		globalStats.TotalSuccess += parseInt64(data["success"])
		globalStats.TotalTimeout += parseInt64(data["timeout"])
		globalStats.TotalCancelled += parseInt64(data["cancelled"])
	}

	// 获取全局等待时间统计
	globalWaitKey := PrefixConcurrencyQueueWait + "global"
	waitTimes, _ := client.LRange(ctx, globalWaitKey, 0, WaitTimeSamplesGlobal-1).Result()
	if len(waitTimes) > 0 {
		times := make([]float64, 0, len(waitTimes))
		for _, t := range waitTimes {
			if v, err := strconv.ParseFloat(t, 64); err == nil {
				times = append(times, v)
			}
		}

		if len(times) > 0 {
			globalStats.GlobalAvgWaitMs = calculateAvg(times)
			globalStats.GlobalP50WaitMs = calculatePercentile(times, 50)
			globalStats.GlobalP90WaitMs = calculatePercentile(times, 90)
			globalStats.GlobalP99WaitMs = calculatePercentile(times, 99)
		}
	}

	// 如果需要每个 Key 的统计，复用已收集的 keyIDs
	if includePerKey {
		for _, keyID := range keyIDs {
			stats, err := c.GetQueueStats(ctx, keyID)
			if err == nil && stats != nil {
				globalStats.PerKeyStats = append(globalStats.PerKeyStats, *stats)
			}
		}
	}

	return globalStats, nil
}

// ScanConcurrencyQueueKeys 扫描所有排队计数器的 API Key ID
func (c *Client) ScanConcurrencyQueueKeys(ctx context.Context) ([]string, error) {
	keys, err := c.ScanKeys(ctx, PrefixConcurrencyQueue+"*", 100)
	if err != nil {
		return nil, err
	}

	apiKeyIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		// 排除统计和等待时间相关的键
		if strings.Contains(key, ":stats:") || strings.Contains(key, ":wait_times:") {
			continue
		}
		keyID := strings.TrimPrefix(key, PrefixConcurrencyQueue)
		apiKeyIDs = append(apiKeyIDs, keyID)
	}

	return apiKeyIDs, nil
}

// ClearAllConcurrencyQueues 清理所有排队计数器
func (c *Client) ClearAllConcurrencyQueues(ctx context.Context) (int, error) {
	keyIDs, err := c.ScanConcurrencyQueueKeys(ctx)
	if err != nil {
		return 0, err
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	for _, keyID := range keyIDs {
		key := PrefixConcurrencyQueue + keyID
		client.Del(ctx, key)
	}

	logger.Info("Cleared all concurrency queues", zap.Int("count", len(keyIDs)))
	return len(keyIDs), nil
}

// ========== 辅助函数 ==========

// calculateAvg 计算平均值
func calculateAvg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// calculatePercentile 计算百分位数
func calculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// 复制并排序
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// 计算索引
	index := (percentile / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	// 线性插值
	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// DefaultHealthCheckTimeout 健康检查默认超时时间
const DefaultHealthCheckTimeout = 5 * time.Second

// GetP90WaitTime 获取全局 P90 等待时间（用于健康检查）
func (c *Client) GetP90WaitTime(ctx context.Context) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	// 确保有超时控制
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthCheckTimeout)
		defer cancel()
	}

	globalWaitKey := PrefixConcurrencyQueueWait + "global"
	waitTimes, err := client.LRange(ctx, globalWaitKey, 0, WaitTimeSamplesGlobal-1).Result()
	if err != nil || len(waitTimes) == 0 {
		return 0, nil
	}

	times := make([]float64, 0, len(waitTimes))
	for _, t := range waitTimes {
		if v, err := strconv.ParseFloat(t, 64); err == nil {
			times = append(times, v)
		}
	}

	if len(times) == 0 {
		return 0, nil
	}

	return calculatePercentile(times, 90), nil
}

// CheckQueueHealth 检查队列健康状态
func (c *Client) CheckQueueHealth(ctx context.Context, threshold float64, timeoutMs int64) (bool, float64, error) {
	// 确保有超时控制
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthCheckTimeout)
		defer cancel()
	}

	p90, err := c.GetP90WaitTime(ctx)
	if err != nil {
		return true, 0, err // 出错时默认健康
	}

	// 如果 P90 超过阈值比例的超时时间，认为不健康
	maxAllowedWait := float64(timeoutMs) * threshold
	isHealthy := p90 < maxAllowedWait

	return isHealthy, p90, nil
}
