package redis

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// UsageStats 使用统计
type UsageStats struct {
	TotalTokens         int64   `json:"totalTokens"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheCreateTokens   int64   `json:"cacheCreateTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	AllTokens           int64   `json:"allTokens"`
	RequestCount        int64   `json:"requests"`
	Ephemeral5mTokens   int64   `json:"ephemeral5mTokens,omitempty"`
	Ephemeral1hTokens   int64   `json:"ephemeral1hTokens,omitempty"`
	LongContextRequests int64   `json:"longContextRequests,omitempty"`
	TotalCost           float64 `json:"totalCost,omitempty"`
}

// UsageRecord 使用记录
type UsageRecord struct {
	Timestamp         time.Time `json:"timestamp"`
	Model             string    `json:"model"`
	InputTokens       int64     `json:"inputTokens"`
	OutputTokens      int64     `json:"outputTokens"`
	CacheCreateTokens int64     `json:"cacheCreateTokens"`
	CacheReadTokens   int64     `json:"cacheReadTokens"`
	Cost              float64   `json:"cost"`
}

// TokenUsageParams Token 使用参数
type TokenUsageParams struct {
	KeyID                string
	AccountID            string
	Model                string
	InputTokens          int64
	OutputTokens         int64
	CacheCreateTokens    int64
	CacheReadTokens      int64
	Ephemeral5mTokens    int64
	Ephemeral1hTokens    int64
	IsLongContextRequest bool
}

// usageContext 使用量统计上下文（内部辅助结构）
type usageContext struct {
	params          TokenUsageParams
	normalizedModel string
	coreTokens      int64
	totalTokens     int64
	dateStr         string
	monthStr        string
	hourStr         string
}

// newUsageContext 创建使用量统计上下文
func newUsageContext(params TokenUsageParams, now time.Time) *usageContext {
	normalizedModel := normalizeModelName(params.Model)
	coreTokens := params.InputTokens + params.OutputTokens
	totalTokens := coreTokens + params.CacheCreateTokens + params.CacheReadTokens

	return &usageContext{
		params:          params,
		normalizedModel: normalizedModel,
		coreTokens:      coreTokens,
		totalTokens:     totalTokens,
		dateStr:         getDateStringInTimezone(now),
		monthStr:        getMonthStringInTimezone(now),
		hourStr:         getHourStringInTimezone(now),
	}
}

// incrAPIKeyTotalUsage 增加 API Key 总体统计
func (uc *usageContext) incrAPIKeyTotalUsage(ctx context.Context, pipe goredis.Pipeliner) {
	usageKey := fmt.Sprintf("%s%s", PrefixUsage, uc.params.KeyID)

	pipe.HIncrBy(ctx, usageKey, "totalTokens", uc.coreTokens)
	pipe.HIncrBy(ctx, usageKey, "totalInputTokens", uc.params.InputTokens)
	pipe.HIncrBy(ctx, usageKey, "totalOutputTokens", uc.params.OutputTokens)
	pipe.HIncrBy(ctx, usageKey, "totalCacheCreateTokens", uc.params.CacheCreateTokens)
	pipe.HIncrBy(ctx, usageKey, "totalCacheReadTokens", uc.params.CacheReadTokens)
	pipe.HIncrBy(ctx, usageKey, "totalAllTokens", uc.totalTokens)
	pipe.HIncrBy(ctx, usageKey, "totalEphemeral5mTokens", uc.params.Ephemeral5mTokens)
	pipe.HIncrBy(ctx, usageKey, "totalEphemeral1hTokens", uc.params.Ephemeral1hTokens)
	pipe.HIncrBy(ctx, usageKey, "totalRequests", 1)

	if uc.params.IsLongContextRequest {
		pipe.HIncrBy(ctx, usageKey, "totalLongContextInputTokens", uc.params.InputTokens)
		pipe.HIncrBy(ctx, usageKey, "totalLongContextOutputTokens", uc.params.OutputTokens)
		pipe.HIncrBy(ctx, usageKey, "totalLongContextRequests", 1)
	}
}

// incrTimeBasedUsage 增加时间维度统计（每日/每月/每小时）
func (uc *usageContext) incrTimeBasedUsage(ctx context.Context, pipe goredis.Pipeliner) {
	dailyKey := fmt.Sprintf("%s%s:%s", PrefixUsageDaily, uc.params.KeyID, uc.dateStr)
	monthlyKey := fmt.Sprintf("%s%s:%s", PrefixUsageMonthly, uc.params.KeyID, uc.monthStr)
	hourlyKey := fmt.Sprintf("%s%s:%s", PrefixUsageHourly, uc.params.KeyID, uc.hourStr)

	// 每日统计
	uc.incrUsageHashWithExpire(ctx, pipe, dailyKey, TTLUsageDaily, true)
	if uc.params.IsLongContextRequest {
		pipe.HIncrBy(ctx, dailyKey, "longContextInputTokens", uc.params.InputTokens)
		pipe.HIncrBy(ctx, dailyKey, "longContextOutputTokens", uc.params.OutputTokens)
		pipe.HIncrBy(ctx, dailyKey, "longContextRequests", 1)
	}

	// 每月统计
	uc.incrUsageHashWithExpire(ctx, pipe, monthlyKey, TTLUsageMonthly, true)

	// 每小时统计
	uc.incrUsageHashWithExpire(ctx, pipe, hourlyKey, TTLUsageHourly, false)
}

// incrUsageHashWithExpire 增加使用量 Hash 并设置过期时间
func (uc *usageContext) incrUsageHashWithExpire(ctx context.Context, pipe goredis.Pipeliner, key string, ttl time.Duration, includeEphemeral bool) {
	pipe.HIncrBy(ctx, key, "tokens", uc.coreTokens)
	pipe.HIncrBy(ctx, key, "inputTokens", uc.params.InputTokens)
	pipe.HIncrBy(ctx, key, "outputTokens", uc.params.OutputTokens)
	pipe.HIncrBy(ctx, key, "cacheCreateTokens", uc.params.CacheCreateTokens)
	pipe.HIncrBy(ctx, key, "cacheReadTokens", uc.params.CacheReadTokens)
	pipe.HIncrBy(ctx, key, "allTokens", uc.totalTokens)
	if includeEphemeral {
		pipe.HIncrBy(ctx, key, "ephemeral5mTokens", uc.params.Ephemeral5mTokens)
		pipe.HIncrBy(ctx, key, "ephemeral1hTokens", uc.params.Ephemeral1hTokens)
	}
	pipe.HIncrBy(ctx, key, "requests", 1)
	pipe.Expire(ctx, key, ttl)
}

// incrModelBasicUsage 增加基本模型统计
func (uc *usageContext) incrModelBasicUsage(ctx context.Context, pipe goredis.Pipeliner, key string, ttl time.Duration) {
	pipe.HIncrBy(ctx, key, "inputTokens", uc.params.InputTokens)
	pipe.HIncrBy(ctx, key, "outputTokens", uc.params.OutputTokens)
	pipe.HIncrBy(ctx, key, "cacheCreateTokens", uc.params.CacheCreateTokens)
	pipe.HIncrBy(ctx, key, "cacheReadTokens", uc.params.CacheReadTokens)
	pipe.HIncrBy(ctx, key, "allTokens", uc.totalTokens)
	pipe.HIncrBy(ctx, key, "requests", 1)
	pipe.Expire(ctx, key, ttl)
}

// incrModelUsage 增加按模型统计
func (uc *usageContext) incrModelUsage(ctx context.Context, pipe goredis.Pipeliner) {
	modelDailyKey := fmt.Sprintf("usage:model:daily:%s:%s", uc.normalizedModel, uc.dateStr)
	modelMonthlyKey := fmt.Sprintf("usage:model:monthly:%s:%s", uc.normalizedModel, uc.monthStr)
	modelHourlyKey := fmt.Sprintf("usage:model:hourly:%s:%s", uc.normalizedModel, uc.hourStr)

	uc.incrModelBasicUsage(ctx, pipe, modelDailyKey, TTLUsageDaily)
	uc.incrModelBasicUsage(ctx, pipe, modelMonthlyKey, TTLUsageMonthly)
	uc.incrModelBasicUsage(ctx, pipe, modelHourlyKey, TTLUsageHourly)
}

// incrKeyModelUsage 增加 API Key 级别的模型统计
func (uc *usageContext) incrKeyModelUsage(ctx context.Context, pipe goredis.Pipeliner) {
	keyModelDailyKey := fmt.Sprintf("usage:%s:model:daily:%s:%s", uc.params.KeyID, uc.normalizedModel, uc.dateStr)
	keyModelMonthlyKey := fmt.Sprintf("usage:%s:model:monthly:%s:%s", uc.params.KeyID, uc.normalizedModel, uc.monthStr)
	keyModelHourlyKey := fmt.Sprintf("usage:%s:model:hourly:%s:%s", uc.params.KeyID, uc.normalizedModel, uc.hourStr)

	// 每日（含 ephemeral）
	uc.incrModelBasicUsage(ctx, pipe, keyModelDailyKey, TTLUsageDaily)
	pipe.HIncrBy(ctx, keyModelDailyKey, "ephemeral5mTokens", uc.params.Ephemeral5mTokens)
	pipe.HIncrBy(ctx, keyModelDailyKey, "ephemeral1hTokens", uc.params.Ephemeral1hTokens)

	// 每月（含 ephemeral）
	uc.incrModelBasicUsage(ctx, pipe, keyModelMonthlyKey, TTLUsageMonthly)
	pipe.HIncrBy(ctx, keyModelMonthlyKey, "ephemeral5mTokens", uc.params.Ephemeral5mTokens)
	pipe.HIncrBy(ctx, keyModelMonthlyKey, "ephemeral1hTokens", uc.params.Ephemeral1hTokens)

	// 每小时
	uc.incrModelBasicUsage(ctx, pipe, keyModelHourlyKey, TTLUsageHourly)
}

// incrSystemMetrics 增加系统级分钟统计
func (uc *usageContext) incrSystemMetrics(ctx context.Context, pipe goredis.Pipeliner, now time.Time) {
	minuteTimestamp := getMinuteTimestamp(now)
	systemMinuteKey := fmt.Sprintf("%s%d", PrefixSystemMetrics, minuteTimestamp)

	pipe.HIncrBy(ctx, systemMinuteKey, "requests", 1)
	pipe.HIncrBy(ctx, systemMinuteKey, "totalTokens", uc.totalTokens)
	pipe.HIncrBy(ctx, systemMinuteKey, "inputTokens", uc.params.InputTokens)
	pipe.HIncrBy(ctx, systemMinuteKey, "outputTokens", uc.params.OutputTokens)
	pipe.HIncrBy(ctx, systemMinuteKey, "cacheCreateTokens", uc.params.CacheCreateTokens)
	pipe.HIncrBy(ctx, systemMinuteKey, "cacheReadTokens", uc.params.CacheReadTokens)

	metricsWindow := 5
	if config.Cfg != nil {
		metricsWindow = config.Cfg.System.MetricsWindow
	}
	pipe.Expire(ctx, systemMinuteKey, time.Duration(metricsWindow*60*2)*time.Second)
}

// IncrementTokenUsage 增加 Token 使用量（与 Node.js 完全兼容）
func (c *Client) IncrementTokenUsage(ctx context.Context, params TokenUsageParams) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	uc := newUsageContext(params, now)
	pipe := client.Pipeline()

	// 分模块增加统计
	uc.incrAPIKeyTotalUsage(ctx, pipe)
	uc.incrTimeBasedUsage(ctx, pipe)
	uc.incrModelUsage(ctx, pipe)
	uc.incrKeyModelUsage(ctx, pipe)
	uc.incrSystemMetrics(ctx, pipe, now)

	// 执行管道
	_, err = pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to increment token usage", zap.Error(err))
		return err
	}

	return nil
}

// IncrementAccountUsage 增加账户级别使用统计
func (c *Client) IncrementAccountUsage(ctx context.Context, params TokenUsageParams) error {
	if params.AccountID == "" {
		return nil
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	dateStr := getDateStringInTimezone(now)
	monthStr := getMonthStringInTimezone(now)
	hourStr := getHourStringInTimezone(now)

	normalizedModel := normalizeModelName(params.Model)

	coreTokens := params.InputTokens + params.OutputTokens
	totalTokens := coreTokens + params.CacheCreateTokens + params.CacheReadTokens

	// 账户级别统计的键
	accountKey := fmt.Sprintf("%s%s", PrefixAccountUsage, params.AccountID)
	accountDailyKey := fmt.Sprintf("account_usage:daily:%s:%s", params.AccountID, dateStr)
	accountMonthlyKey := fmt.Sprintf("account_usage:monthly:%s:%s", params.AccountID, monthStr)
	accountHourlyKey := fmt.Sprintf("account_usage:hourly:%s:%s", params.AccountID, hourStr)

	// 账户按模型统计的键
	accountModelDailyKey := fmt.Sprintf("account_usage:model:daily:%s:%s:%s", params.AccountID, normalizedModel, dateStr)
	accountModelMonthlyKey := fmt.Sprintf("account_usage:model:monthly:%s:%s:%s", params.AccountID, normalizedModel, monthStr)
	accountModelHourlyKey := fmt.Sprintf("account_usage:model:hourly:%s:%s:%s", params.AccountID, normalizedModel, hourStr)

	pipe := client.Pipeline()

	// 账户总体统计
	pipe.HIncrBy(ctx, accountKey, "totalTokens", coreTokens)
	pipe.HIncrBy(ctx, accountKey, "totalInputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountKey, "totalOutputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountKey, "totalCacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountKey, "totalCacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountKey, "totalAllTokens", totalTokens)
	pipe.HIncrBy(ctx, accountKey, "totalRequests", 1)

	if params.IsLongContextRequest {
		pipe.HIncrBy(ctx, accountKey, "totalLongContextInputTokens", params.InputTokens)
		pipe.HIncrBy(ctx, accountKey, "totalLongContextOutputTokens", params.OutputTokens)
		pipe.HIncrBy(ctx, accountKey, "totalLongContextRequests", 1)
	}

	// 账户每日统计
	pipe.HIncrBy(ctx, accountDailyKey, "tokens", coreTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountDailyKey, "requests", 1)
	pipe.Expire(ctx, accountDailyKey, TTLUsageDaily)

	if params.IsLongContextRequest {
		pipe.HIncrBy(ctx, accountDailyKey, "longContextInputTokens", params.InputTokens)
		pipe.HIncrBy(ctx, accountDailyKey, "longContextOutputTokens", params.OutputTokens)
		pipe.HIncrBy(ctx, accountDailyKey, "longContextRequests", 1)
	}

	// 账户每月统计
	pipe.HIncrBy(ctx, accountMonthlyKey, "tokens", coreTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountMonthlyKey, "requests", 1)
	pipe.Expire(ctx, accountMonthlyKey, TTLUsageMonthly)

	// 账户每小时统计
	pipe.HIncrBy(ctx, accountHourlyKey, "tokens", coreTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, "requests", 1)
	pipe.Expire(ctx, accountHourlyKey, TTLUsageHourly)

	// 添加模型级别的数据到 hourly 键中（支持会话窗口统计）
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:inputTokens", normalizedModel), params.InputTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:outputTokens", normalizedModel), params.OutputTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:cacheCreateTokens", normalizedModel), params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:cacheReadTokens", normalizedModel), params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:allTokens", normalizedModel), totalTokens)
	pipe.HIncrBy(ctx, accountHourlyKey, fmt.Sprintf("model:%s:requests", normalizedModel), 1)

	// 账户按模型统计 - 每日
	pipe.HIncrBy(ctx, accountModelDailyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountModelDailyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountModelDailyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountModelDailyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountModelDailyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountModelDailyKey, "requests", 1)
	pipe.Expire(ctx, accountModelDailyKey, TTLUsageDaily)

	// 账户按模型统计 - 每月
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountModelMonthlyKey, "requests", 1)
	pipe.Expire(ctx, accountModelMonthlyKey, TTLUsageMonthly)

	// 账户按模型统计 - 每小时
	pipe.HIncrBy(ctx, accountModelHourlyKey, "inputTokens", params.InputTokens)
	pipe.HIncrBy(ctx, accountModelHourlyKey, "outputTokens", params.OutputTokens)
	pipe.HIncrBy(ctx, accountModelHourlyKey, "cacheCreateTokens", params.CacheCreateTokens)
	pipe.HIncrBy(ctx, accountModelHourlyKey, "cacheReadTokens", params.CacheReadTokens)
	pipe.HIncrBy(ctx, accountModelHourlyKey, "allTokens", totalTokens)
	pipe.HIncrBy(ctx, accountModelHourlyKey, "requests", 1)
	pipe.Expire(ctx, accountModelHourlyKey, TTLUsageHourly)

	_, err = pipe.Exec(ctx)
	return err
}

// GetUsageStats 获取使用统计
func (c *Client) GetUsageStats(ctx context.Context, keyID string) (*UsageStatsResult, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	dateStr := getDateStringInTimezone(now)
	monthStr := getMonthStringInTimezone(now)

	totalKey := fmt.Sprintf("%s%s", PrefixUsage, keyID)
	dailyKey := fmt.Sprintf("%s%s:%s", PrefixUsageDaily, keyID, dateStr)
	monthlyKey := fmt.Sprintf("%s%s:%s", PrefixUsageMonthly, keyID, monthStr)

	// 获取 API Key 创建时间计算平均值
	apiKeyKey := PrefixAPIKey + keyID
	keyData, _ := client.HGetAll(ctx, apiKeyKey).Result()

	createdAt := time.Now()
	if keyData["createdAt"] != "" {
		if t, err := time.Parse(time.RFC3339, keyData["createdAt"]); err == nil {
			createdAt = t
		}
	}

	daysSinceCreated := int64(1)
	if d := int64(time.Since(createdAt).Hours() / 24); d > 0 {
		daysSinceCreated = d
	}

	// 批量获取
	total, _ := client.HGetAll(ctx, totalKey).Result()
	daily, _ := client.HGetAll(ctx, dailyKey).Result()
	monthly, _ := client.HGetAll(ctx, monthlyKey).Result()

	totalStats := parseUsageData(total)
	dailyStats := parseUsageData(daily)
	monthlyStats := parseUsageData(monthly)

	totalTokens := totalStats.TotalTokens
	if totalTokens == 0 {
		totalTokens = totalStats.AllTokens
	}
	totalRequests := totalStats.RequestCount

	totalMinutes := daysSinceCreated * 24 * 60
	if totalMinutes < 1 {
		totalMinutes = 1
	}

	return &UsageStatsResult{
		Total:   totalStats,
		Daily:   dailyStats,
		Monthly: monthlyStats,
		Averages: UsageAverages{
			RPM:           float64(totalRequests) / float64(totalMinutes),
			TPM:           float64(totalTokens) / float64(totalMinutes),
			DailyRequests: float64(totalRequests) / float64(daysSinceCreated),
			DailyTokens:   float64(totalTokens) / float64(daysSinceCreated),
		},
	}, nil
}

// UsageStatsResult 使用统计结果
type UsageStatsResult struct {
	Total    *UsageStats   `json:"total"`
	Daily    *UsageStats   `json:"daily"`
	Monthly  *UsageStats   `json:"monthly"`
	Averages UsageAverages `json:"averages"`
}

// UsageAverages 平均值
type UsageAverages struct {
	RPM           float64 `json:"rpm"`
	TPM           float64 `json:"tpm"`
	DailyRequests float64 `json:"dailyRequests"`
	DailyTokens   float64 `json:"dailyTokens"`
}

// parseUsageData 解析使用数据
func parseUsageData(data map[string]string) *UsageStats {
	stats := &UsageStats{
		TotalTokens:         parseInt64(data["totalTokens"]),
		InputTokens:         parseInt64(data["totalInputTokens"]),
		OutputTokens:        parseInt64(data["totalOutputTokens"]),
		CacheCreateTokens:   parseInt64(data["totalCacheCreateTokens"]),
		CacheReadTokens:     parseInt64(data["totalCacheReadTokens"]),
		AllTokens:           parseInt64(data["totalAllTokens"]),
		RequestCount:        parseInt64(data["totalRequests"]),
		Ephemeral5mTokens:   parseInt64(data["totalEphemeral5mTokens"]),
		Ephemeral1hTokens:   parseInt64(data["totalEphemeral1hTokens"]),
		LongContextRequests: parseInt64(data["totalLongContextRequests"]),
	}

	// 兼容非 total 前缀的字段（每日/每月统计）
	if stats.TotalTokens == 0 {
		stats.TotalTokens = parseInt64(data["tokens"])
	}
	if stats.InputTokens == 0 {
		stats.InputTokens = parseInt64(data["inputTokens"])
	}
	if stats.OutputTokens == 0 {
		stats.OutputTokens = parseInt64(data["outputTokens"])
	}
	if stats.CacheCreateTokens == 0 {
		stats.CacheCreateTokens = parseInt64(data["cacheCreateTokens"])
	}
	if stats.CacheReadTokens == 0 {
		stats.CacheReadTokens = parseInt64(data["cacheReadTokens"])
	}
	if stats.AllTokens == 0 {
		stats.AllTokens = parseInt64(data["allTokens"])
	}
	if stats.RequestCount == 0 {
		stats.RequestCount = parseInt64(data["requests"])
	}
	if stats.Ephemeral5mTokens == 0 {
		stats.Ephemeral5mTokens = parseInt64(data["ephemeral5mTokens"])
	}
	if stats.Ephemeral1hTokens == 0 {
		stats.Ephemeral1hTokens = parseInt64(data["ephemeral1hTokens"])
	}
	if stats.LongContextRequests == 0 {
		stats.LongContextRequests = parseInt64(data["longContextRequests"])
	}

	return stats
}

// GetDailyUsageByModel 获取按模型分类的每日使用统计
func (c *Client) GetDailyUsageByModel(ctx context.Context, keyID string, date time.Time) (map[string]*UsageStats, error) {
	dateStr := getDateStringInTimezone(date)
	pattern := fmt.Sprintf("usage:%s:model:daily:*:%s", keyID, dateStr)

	keys, err := c.ScanKeys(ctx, pattern, 1000)
	if err != nil {
		return nil, err
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	result := make(map[string]*UsageStats)

	for _, key := range keys {
		// 从 key 中提取模型名
		// 格式: usage:{keyId}:model:daily:{model}:{date}
		parts := strings.Split(key, ":")
		if len(parts) < 5 {
			continue
		}
		model := parts[4]

		data, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}

		result[model] = parseUsageData(data)
	}

	return result, nil
}

// GetAllUsedModels 获取所有被使用过的模型列表
func (c *Client) GetAllUsedModels(ctx context.Context) ([]string, error) {
	pattern := "usage:*:model:daily:*"
	keys, err := c.ScanKeys(ctx, pattern, 1000)
	if err != nil {
		return nil, err
	}

	modelSet := make(map[string]struct{})

	// 从 key 中提取模型名
	// 格式: usage:{keyId}:model:daily:{model}:{date}
	re := regexp.MustCompile(`usage:[^:]+:model:daily:([^:]+):`)
	for _, key := range keys {
		matches := re.FindStringSubmatch(key)
		if len(matches) > 1 {
			modelSet[matches[1]] = struct{}{}
		}
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}

	// 排序
	sort.Strings(models)
	return models, nil
}

// ========== 辅助函数 ==========

// normalizeModelName 标准化模型名（与 Node.js 保持一致）
func normalizeModelName(model string) string {
	if model == "" {
		return "unknown"
	}

	normalized := model

	// 对于 Claude 模型，提取基础名称
	if strings.HasPrefix(model, "claude-") {
		// 去掉日期后缀（如 -20241022）
		re := regexp.MustCompile(`-\d{8}$`)
		normalized = re.ReplaceAllString(normalized, "")

		// 去掉版本后缀（如 -v1:0, -v2:1 等）
		re = regexp.MustCompile(`-v\d+:\d+$`)
		normalized = re.ReplaceAllString(normalized, "")

		return normalized
	}

	// 对于其他模型，去掉常见的版本后缀
	re := regexp.MustCompile(`-v\d+:\d+$|:latest$`)
	return re.ReplaceAllString(model, "")
}

// parseInt64 安全解析 int64
func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// parseFloat64 安全解析 float64
func parseFloat64(s string) float64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
