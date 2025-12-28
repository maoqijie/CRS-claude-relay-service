package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// CostStats 成本统计
type CostStats struct {
	TotalCost    float64 `json:"totalCost"`
	InputCost    float64 `json:"inputCost"`
	OutputCost   float64 `json:"outputCost"`
	CacheCost    float64 `json:"cacheCost"`
	RequestCount int64   `json:"requestCount"`
}

// DailyCostRecord 每日成本记录
type DailyCostRecord struct {
	Date         string  `json:"date"`
	TotalCost    float64 `json:"totalCost"`
	InputCost    float64 `json:"inputCost"`
	OutputCost   float64 `json:"outputCost"`
	CacheCost    float64 `json:"cacheCost"`
	RequestCount int64   `json:"requestCount"`
}

// IncrementDailyCost 增加每日成本
func (c *Client) IncrementDailyCost(ctx context.Context, keyID string, amount float64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	dateStr := getDateStringInTimezone(now)
	monthStr := getMonthStringInTimezone(now)

	pipe := client.Pipeline()

	// 每日成本
	dailyCostKey := fmt.Sprintf("usage:cost:daily:%s:%s", keyID, dateStr)
	pipe.IncrByFloat(ctx, dailyCostKey, amount)
	pipe.Expire(ctx, dailyCostKey, TTLUsageDaily)

	// 每月成本
	monthlyCostKey := fmt.Sprintf("usage:cost:monthly:%s:%s", keyID, monthStr)
	pipe.IncrByFloat(ctx, monthlyCostKey, amount)
	pipe.Expire(ctx, monthlyCostKey, TTLUsageMonthly)

	// 总成本
	totalCostKey := fmt.Sprintf("usage:cost:total:%s", keyID)
	pipe.IncrByFloat(ctx, totalCostKey, amount)

	_, err = pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to increment daily cost", zap.Error(err))
		return err
	}

	return nil
}

// IncrementDetailedCost 增加详细成本（分输入/输出/缓存）
func (c *Client) IncrementDetailedCost(ctx context.Context, keyID string, inputCost, outputCost, cacheCost float64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	dateStr := getDateStringInTimezone(now)
	monthStr := getMonthStringInTimezone(now)

	totalCost := inputCost + outputCost + cacheCost

	pipe := client.Pipeline()

	// 每日详细成本
	dailyCostKey := fmt.Sprintf("usage:cost:daily:%s:%s", keyID, dateStr)
	pipe.HIncrByFloat(ctx, dailyCostKey, "totalCost", totalCost)
	pipe.HIncrByFloat(ctx, dailyCostKey, "inputCost", inputCost)
	pipe.HIncrByFloat(ctx, dailyCostKey, "outputCost", outputCost)
	pipe.HIncrByFloat(ctx, dailyCostKey, "cacheCost", cacheCost)
	pipe.HIncrBy(ctx, dailyCostKey, "requestCount", 1)
	pipe.Expire(ctx, dailyCostKey, TTLUsageDaily)

	// 每月详细成本
	monthlyCostKey := fmt.Sprintf("usage:cost:monthly:%s:%s", keyID, monthStr)
	pipe.HIncrByFloat(ctx, monthlyCostKey, "totalCost", totalCost)
	pipe.HIncrByFloat(ctx, monthlyCostKey, "inputCost", inputCost)
	pipe.HIncrByFloat(ctx, monthlyCostKey, "outputCost", outputCost)
	pipe.HIncrByFloat(ctx, monthlyCostKey, "cacheCost", cacheCost)
	pipe.HIncrBy(ctx, monthlyCostKey, "requestCount", 1)
	pipe.Expire(ctx, monthlyCostKey, TTLUsageMonthly)

	// 总成本
	totalCostKey := fmt.Sprintf("usage:cost:total:%s", keyID)
	pipe.HIncrByFloat(ctx, totalCostKey, "totalCost", totalCost)
	pipe.HIncrByFloat(ctx, totalCostKey, "inputCost", inputCost)
	pipe.HIncrByFloat(ctx, totalCostKey, "outputCost", outputCost)
	pipe.HIncrByFloat(ctx, totalCostKey, "cacheCost", cacheCost)
	pipe.HIncrBy(ctx, totalCostKey, "requestCount", 1)

	_, err = pipe.Exec(ctx)
	return err
}

// GetDailyCost 获取每日成本
func (c *Client) GetDailyCost(ctx context.Context, keyID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	dateStr := getDateStringInTimezone(time.Now())
	costKey := fmt.Sprintf("usage:cost:daily:%s:%s", keyID, dateStr)

	// 尝试从 Hash 获取
	result, err := client.HGet(ctx, costKey, "totalCost").Result()
	if err == nil {
		return parseFloat64(result), nil
	}

	// 兼容旧格式（直接存储）
	result, err = client.Get(ctx, costKey).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil // 未找到返回 0
		}
		return 0, err
	}

	return parseFloat64(result), nil
}

// GetDailyCostDetailed 获取每日详细成本
func (c *Client) GetDailyCostDetailed(ctx context.Context, keyID string, date time.Time) (*CostStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	dateStr := getDateStringInTimezone(date)
	costKey := fmt.Sprintf("usage:cost:daily:%s:%s", keyID, dateStr)

	data, err := client.HGetAll(ctx, costKey).Result()
	if err != nil {
		return nil, err
	}

	return &CostStats{
		TotalCost:    parseFloat64(data["totalCost"]),
		InputCost:    parseFloat64(data["inputCost"]),
		OutputCost:   parseFloat64(data["outputCost"]),
		CacheCost:    parseFloat64(data["cacheCost"]),
		RequestCount: parseInt64(data["requestCount"]),
	}, nil
}

// GetMonthlyCost 获取每月成本
func (c *Client) GetMonthlyCost(ctx context.Context, keyID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	monthStr := getMonthStringInTimezone(time.Now())
	costKey := fmt.Sprintf("usage:cost:monthly:%s:%s", keyID, monthStr)

	// 尝试从 Hash 获取
	result, err := client.HGet(ctx, costKey, "totalCost").Result()
	if err == nil {
		return parseFloat64(result), nil
	}

	// 兼容旧格式
	result, err = client.Get(ctx, costKey).Result()
	if err != nil {
		return 0, nil
	}

	return parseFloat64(result), nil
}

// GetMonthlyCostDetailed 获取每月详细成本
func (c *Client) GetMonthlyCostDetailed(ctx context.Context, keyID string, date time.Time) (*CostStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	monthStr := getMonthStringInTimezone(date)
	costKey := fmt.Sprintf("usage:cost:monthly:%s:%s", keyID, monthStr)

	data, err := client.HGetAll(ctx, costKey).Result()
	if err != nil {
		return nil, err
	}

	return &CostStats{
		TotalCost:    parseFloat64(data["totalCost"]),
		InputCost:    parseFloat64(data["inputCost"]),
		OutputCost:   parseFloat64(data["outputCost"]),
		CacheCost:    parseFloat64(data["cacheCost"]),
		RequestCount: parseInt64(data["requestCount"]),
	}, nil
}

// GetTotalCost 获取总成本
func (c *Client) GetTotalCost(ctx context.Context, keyID string) (*CostStats, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	totalCostKey := fmt.Sprintf("usage:cost:total:%s", keyID)

	data, err := client.HGetAll(ctx, totalCostKey).Result()
	if err != nil {
		return nil, err
	}

	return &CostStats{
		TotalCost:    parseFloat64(data["totalCost"]),
		InputCost:    parseFloat64(data["inputCost"]),
		OutputCost:   parseFloat64(data["outputCost"]),
		CacheCost:    parseFloat64(data["cacheCost"]),
		RequestCount: parseInt64(data["requestCount"]),
	}, nil
}

// GetCostHistory 获取成本历史（最近 N 天）
func (c *Client) GetCostHistory(ctx context.Context, keyID string, days int) ([]DailyCostRecord, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	records := make([]DailyCostRecord, 0, days)

	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dateStr := getDateStringInTimezone(date)
		costKey := fmt.Sprintf("usage:cost:daily:%s:%s", keyID, dateStr)

		data, err := client.HGetAll(ctx, costKey).Result()
		if err != nil || len(data) == 0 {
			// 尝试旧格式
			result, err := client.Get(ctx, costKey).Result()
			if err != nil {
				continue
			}
			records = append(records, DailyCostRecord{
				Date:      dateStr,
				TotalCost: parseFloat64(result),
			})
			continue
		}

		records = append(records, DailyCostRecord{
			Date:         dateStr,
			TotalCost:    parseFloat64(data["totalCost"]),
			InputCost:    parseFloat64(data["inputCost"]),
			OutputCost:   parseFloat64(data["outputCost"]),
			CacheCost:    parseFloat64(data["cacheCost"]),
			RequestCount: parseInt64(data["requestCount"]),
		})
	}

	return records, nil
}

// GetCostStats 获取成本统计（汇总）
func (c *Client) GetCostStats(ctx context.Context, keyID string, days int) (*CostStats, error) {
	records, err := c.GetCostHistory(ctx, keyID, days)
	if err != nil {
		return nil, err
	}

	stats := &CostStats{}
	for _, record := range records {
		stats.TotalCost += record.TotalCost
		stats.InputCost += record.InputCost
		stats.OutputCost += record.OutputCost
		stats.CacheCost += record.CacheCost
		stats.RequestCount += record.RequestCount
	}

	return stats, nil
}

// IncrementAccountCost 增加账户级别成本
func (c *Client) IncrementAccountCost(ctx context.Context, accountID string, amount float64) error {
	if accountID == "" {
		return nil
	}

	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	dateStr := getDateStringInTimezone(now)
	monthStr := getMonthStringInTimezone(now)

	pipe := client.Pipeline()

	// 账户总成本
	accountCostKey := fmt.Sprintf("account_usage:%s", accountID)
	pipe.HIncrByFloat(ctx, accountCostKey, "totalCost", amount)

	// 账户每日成本
	accountDailyCostKey := fmt.Sprintf("account_usage:daily:%s:%s", accountID, dateStr)
	pipe.HIncrByFloat(ctx, accountDailyCostKey, "cost", amount)
	pipe.Expire(ctx, accountDailyCostKey, TTLUsageDaily)

	// 账户每月成本
	accountMonthlyCostKey := fmt.Sprintf("account_usage:monthly:%s:%s", accountID, monthStr)
	pipe.HIncrByFloat(ctx, accountMonthlyCostKey, "cost", amount)
	pipe.Expire(ctx, accountMonthlyCostKey, TTLUsageMonthly)

	_, err = pipe.Exec(ctx)
	return err
}

// GetAccountCost 获取账户总成本
func (c *Client) GetAccountCost(ctx context.Context, accountID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	accountCostKey := fmt.Sprintf("account_usage:%s", accountID)
	result, err := client.HGet(ctx, accountCostKey, "totalCost").Result()
	if err != nil {
		return 0, nil
	}

	return parseFloat64(result), nil
}

// GetAccountDailyCost 获取账户每日成本
func (c *Client) GetAccountDailyCost(ctx context.Context, accountID string, date time.Time) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	dateStr := getDateStringInTimezone(date)
	accountDailyCostKey := fmt.Sprintf("account_usage:daily:%s:%s", accountID, dateStr)

	result, err := client.HGet(ctx, accountDailyCostKey, "cost").Result()
	if err != nil {
		return 0, nil
	}

	return parseFloat64(result), nil
}

// getWeekStartDate 获取本周一的日期字符串
func getWeekStartDate(t time.Time) string {
	// 获取本周一
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // 周日
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return getDateStringInTimezone(monday)
}

// IncrementWeeklyOpusCost 增加 Opus 周成本
func (c *Client) IncrementWeeklyOpusCost(ctx context.Context, keyID string, amount float64) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	now := time.Now()
	weekStartDate := getWeekStartDate(now)
	weeklyOpusCostKey := fmt.Sprintf("usage:cost:weekly_opus:%s:%s", keyID, weekStartDate)

	pipe := client.Pipeline()
	pipe.IncrByFloat(ctx, weeklyOpusCostKey, amount)
	// 设置 8 天过期，确保跨周时仍可读取
	pipe.Expire(ctx, weeklyOpusCostKey, 8*24*time.Hour)

	_, err = pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to increment weekly opus cost", zap.Error(err))
		return err
	}

	return nil
}

// GetWeeklyOpusCost 获取 Opus 周成本
func (c *Client) GetWeeklyOpusCost(ctx context.Context, keyID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	now := time.Now()
	weekStartDate := getWeekStartDate(now)
	weeklyOpusCostKey := fmt.Sprintf("usage:cost:weekly_opus:%s:%s", keyID, weekStartDate)

	result, err := client.Get(ctx, weeklyOpusCostKey).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}

	return parseFloat64(result), nil
}

// GetRateLimitWindowCost 获取速率限制窗口内的费用
func (c *Client) GetRateLimitWindowCost(ctx context.Context, keyID string) (float64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}

	costCountKey := fmt.Sprintf("rate_limit:cost:%s", keyID)
	result, err := client.Get(ctx, costCountKey).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}

	return parseFloat64(result), nil
}

// IncrementRateLimitWindowCost 增加速率限制窗口内的费用
func (c *Client) IncrementRateLimitWindowCost(ctx context.Context, keyID string, amount float64, windowMinutes int) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}

	costCountKey := fmt.Sprintf("rate_limit:cost:%s", keyID)

	pipe := client.Pipeline()
	pipe.IncrByFloat(ctx, costCountKey, amount)
	pipe.Expire(ctx, costCountKey, time.Duration(windowMinutes)*time.Minute)

	_, err = pipe.Exec(ctx)
	return err
}
