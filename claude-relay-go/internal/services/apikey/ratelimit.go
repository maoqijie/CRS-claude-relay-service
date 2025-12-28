package apikey

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// RateLimitResult 速率限制检查结果
type RateLimitResult struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	ResetAt    time.Time
	RetryAfter time.Duration
	Window     string // "minute" or "hour"
}

// ConcurrencyResult 并发限制检查结果
type ConcurrencyResult struct {
	Allowed            bool
	CurrentConcurrency int64
	Limit              int
	RequestID          string
	QueueEnabled       bool
}

// CostLimitResult 成本限制检查结果
type CostLimitResult struct {
	Allowed     bool
	CurrentCost float64
	DailyLimit  float64
	LimitType   string // "daily", "total", "weekly_opus", "rate_limit_cost"
}

// TotalCostLimitResult 总成本限制检查结果
type TotalCostLimitResult struct {
	Allowed     bool
	CurrentCost float64
	TotalLimit  float64
}

// WeeklyOpusCostResult Opus 周成本限制检查结果
type WeeklyOpusCostResult struct {
	Allowed     bool
	CurrentCost float64
	WeeklyLimit float64
	ResetAt     time.Time
}

// RateLimitCostResult 速率限制窗口费用检查结果
type RateLimitCostResult struct {
	Allowed       bool
	CurrentCost   float64
	CostLimit     float64
	WindowMinutes int
	ResetAt       time.Time
	HasActiveFuel bool
}

// QueueWaitResult 排队等待结果
type QueueWaitResult struct {
	Success       bool
	WaitDuration  time.Duration
	TimeoutReason string
}

// CheckRateLimit 检查速率限制
func (s *Service) CheckRateLimit(ctx context.Context, apiKey *redis.APIKey) (*RateLimitResult, error) {
	// 检查每分钟限制
	if apiKey.RateLimitPerMin > 0 {
		result, err := s.checkRateLimitWindow(ctx, apiKey.ID, "minute", apiKey.RateLimitPerMin, time.Minute)
		if err != nil {
			return nil, err
		}
		if !result.Allowed {
			return result, nil
		}
	}

	// 检查每小时限制
	if apiKey.RateLimitPerHour > 0 {
		result, err := s.checkRateLimitWindow(ctx, apiKey.ID, "hour", apiKey.RateLimitPerHour, time.Hour)
		if err != nil {
			return nil, err
		}
		if !result.Allowed {
			return result, nil
		}
	}

	// 没有配置限制或全部通过
	return &RateLimitResult{Allowed: true}, nil
}

// checkRateLimitWindow 检查单个时间窗口的速率限制
func (s *Service) checkRateLimitWindow(ctx context.Context, keyID, window string, limit int, duration time.Duration) (*RateLimitResult, error) {
	windowSeconds := int64(duration.Seconds())
	windowKey := fmt.Sprintf("rate_limit:%s:%s:%d", keyID, window, time.Now().Unix()/windowSeconds)

	// 原子递增并获取计数
	count, err := s.redis.IncrWithExpiry(ctx, windowKey, duration)
	if err != nil {
		return nil, fmt.Errorf("failed to check rate limit: %w", err)
	}

	remaining := int64(limit) - count
	if remaining < 0 {
		remaining = 0
	}

	resetAt := time.Now().Truncate(duration).Add(duration)

	if count > int64(limit) {
		return &RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			Limit:      int64(limit),
			ResetAt:    resetAt,
			RetryAfter: time.Until(resetAt),
			Window:     window,
		}, nil
	}

	return &RateLimitResult{
		Allowed:   true,
		Remaining: remaining,
		Limit:     int64(limit),
		ResetAt:   resetAt,
		Window:    window,
	}, nil
}

// CheckConcurrencyLimit 检查并发限制
func (s *Service) CheckConcurrencyLimit(ctx context.Context, apiKey *redis.APIKey, requestID string) (*ConcurrencyResult, error) {
	if apiKey.ConcurrentLimit <= 0 {
		return &ConcurrencyResult{
			Allowed:      true,
			RequestID:    requestID,
			QueueEnabled: apiKey.ConcurrentRequestQueueEnabled,
		}, nil
	}

	current, err := s.redis.GetConcurrency(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get concurrency", zap.Error(err))
		// 出错时允许通过，避免阻塞请求
		return &ConcurrencyResult{
			Allowed:      true,
			RequestID:    requestID,
			QueueEnabled: apiKey.ConcurrentRequestQueueEnabled,
		}, nil
	}

	allowed := current < int64(apiKey.ConcurrentLimit)

	return &ConcurrencyResult{
		Allowed:            allowed,
		CurrentConcurrency: current,
		Limit:              apiKey.ConcurrentLimit,
		RequestID:          requestID,
		QueueEnabled:       apiKey.ConcurrentRequestQueueEnabled,
	}, nil
}

// AcquireConcurrencySlot 获取并发槽位
func (s *Service) AcquireConcurrencySlot(ctx context.Context, apiKey *redis.APIKey, requestID string, leaseSeconds int) (int64, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = 300 // 默认 5 分钟
	}

	count, err := s.redis.IncrConcurrency(ctx, apiKey.ID, requestID, leaseSeconds)
	if err != nil {
		return 0, fmt.Errorf("failed to acquire concurrency slot: %w", err)
	}

	logger.Debug("Acquired concurrency slot",
		zap.String("apiKeyId", apiKey.ID),
		zap.String("requestId", requestID),
		zap.Int64("currentCount", count))

	return count, nil
}

// TryAcquireConcurrencySlot 尝试获取并发槽位（超过上限则立即释放）
func (s *Service) TryAcquireConcurrencySlot(ctx context.Context, apiKey *redis.APIKey, requestID string, leaseSeconds int) (bool, int64, error) {
	count, err := s.AcquireConcurrencySlot(ctx, apiKey, requestID, leaseSeconds)
	if err != nil {
		return false, 0, err
	}

	// 并发上限检查（Acquire 是自增/续约，必须在这里做原子化判断）
	if apiKey.ConcurrentLimit > 0 && count > int64(apiKey.ConcurrentLimit) {
		if releaseErr := s.ReleaseConcurrencySlot(ctx, apiKey.ID, requestID); releaseErr != nil {
			logger.Warn("Failed to release concurrency slot after limit exceeded",
				zap.String("apiKeyId", apiKey.ID),
				zap.String("requestId", requestID),
				zap.Error(releaseErr))
		}
		return false, count, nil
	}

	return true, count, nil
}

// ReleaseConcurrencySlot 释放并发槽位
func (s *Service) ReleaseConcurrencySlot(ctx context.Context, apiKeyID, requestID string) error {
	_, err := s.redis.DecrConcurrency(ctx, apiKeyID, requestID)
	if err != nil {
		return fmt.Errorf("failed to release concurrency slot: %w", err)
	}

	logger.Debug("Released concurrency slot",
		zap.String("apiKeyId", apiKeyID),
		zap.String("requestId", requestID))

	return nil
}

// RefreshConcurrencyLease 刷新并发租约
func (s *Service) RefreshConcurrencyLease(ctx context.Context, apiKeyID, requestID string, leaseSeconds int) (bool, error) {
	return s.redis.RefreshConcurrencyLease(ctx, apiKeyID, requestID, leaseSeconds)
}

// CheckDailyCostLimit 检查每日成本限制
func (s *Service) CheckDailyCostLimit(ctx context.Context, apiKey *redis.APIKey) (*CostLimitResult, error) {
	// 从 API Key 获取每日成本限制
	dailyLimit := apiKey.DailyCostLimit

	if dailyLimit <= 0 {
		// 未设置限制时允许通过
		return &CostLimitResult{Allowed: true}, nil
	}

	dailyCost, err := s.redis.GetDailyCost(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get daily cost", zap.Error(err))
		// 出错时允许通过，避免阻塞请求
		return &CostLimitResult{Allowed: true}, nil
	}

	if dailyCost >= dailyLimit {
		return &CostLimitResult{
			Allowed:     false,
			CurrentCost: dailyCost,
			DailyLimit:  dailyLimit,
		}, nil
	}

	return &CostLimitResult{
		Allowed:     true,
		CurrentCost: dailyCost,
		DailyLimit:  dailyLimit,
	}, nil
}

// WaitInQueue 在队列中等待
func (s *Service) WaitInQueue(ctx context.Context, apiKey *redis.APIKey, requestID string) *QueueWaitResult {
	if !apiKey.ConcurrentRequestQueueEnabled {
		return &QueueWaitResult{
			Success:       false,
			TimeoutReason: "queue_disabled",
		}
	}

	// 计算最大排队数
	maxQueueSize := s.calculateMaxQueueSize(apiKey)

	// 检查当前排队数
	queueCount, err := s.redis.GetConcurrencyQueueCount(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get queue count", zap.Error(err))
	}

	if queueCount >= int64(maxQueueSize) {
		return &QueueWaitResult{
			Success:       false,
			TimeoutReason: "queue_full",
		}
	}

	// 获取超时时间
	timeoutMs := apiKey.ConcurrentRequestQueueTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 10000 // 默认 10 秒
	}

	// 增加排队计数
	_, err = s.redis.IncrConcurrencyQueue(ctx, apiKey.ID, int64(timeoutMs))
	if err != nil {
		logger.Warn("Failed to increment queue count", zap.Error(err))
	}

	// 记录开始时间
	startTime := time.Now()
	deadline := startTime.Add(time.Duration(timeoutMs) * time.Millisecond)

	// 指数退避参数
	pollInterval := 200 * time.Millisecond
	maxPollInterval := 2 * time.Second
	backoffFactor := 1.5
	jitterFactor := 0.2

	defer func() {
		// 减少排队计数
		s.redis.DecrConcurrencyQueue(ctx, apiKey.ID)

		// 记录等待时间
		waitMs := time.Since(startTime).Milliseconds()
		s.redis.RecordWaitTime(ctx, apiKey.ID, waitMs)
	}()

	for time.Now().Before(deadline) {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			// 记录取消统计
			s.redis.IncrQueueStats(ctx, apiKey.ID, "cancelled", 1)
			return &QueueWaitResult{
				Success:       false,
				WaitDuration:  time.Since(startTime),
				TimeoutReason: "context_cancelled",
			}
		default:
		}

		// 尝试获取并发槽位（成功即持有）
		result, err := s.CheckConcurrencyLimit(ctx, apiKey, requestID)
		if err != nil {
			logger.Warn("Queue check failed", zap.Error(err))
		}

		if result.Allowed {
			acquired, _, acquireErr := s.TryAcquireConcurrencySlot(ctx, apiKey, requestID, 0)
			if acquireErr != nil {
				logger.Warn("Queue acquire failed", zap.Error(acquireErr))
				// 出错时允许通过，避免阻塞请求
				s.redis.IncrQueueStats(ctx, apiKey.ID, "success", 1)
				return &QueueWaitResult{
					Success:      true,
					WaitDuration: time.Since(startTime),
				}
			}

			if acquired {
				// 记录成功统计
				s.redis.IncrQueueStats(ctx, apiKey.ID, "success", 1)
				return &QueueWaitResult{
					Success:      true,
					WaitDuration: time.Since(startTime),
				}
			}
		}

		// 计算下次轮询间隔（带抖动）
		jitter := 1.0 + (rand.Float64()-0.5)*2*jitterFactor
		actualInterval := time.Duration(float64(pollInterval) * jitter)

		// 等待
		select {
		case <-ctx.Done():
			s.redis.IncrQueueStats(ctx, apiKey.ID, "cancelled", 1)
			return &QueueWaitResult{
				Success:       false,
				WaitDuration:  time.Since(startTime),
				TimeoutReason: "context_cancelled",
			}
		case <-time.After(actualInterval):
			// 指数退避
			pollInterval = time.Duration(float64(pollInterval) * backoffFactor)
			if pollInterval > maxPollInterval {
				pollInterval = maxPollInterval
			}
		}
	}

	// 超时
	s.redis.IncrQueueStats(ctx, apiKey.ID, "timeout", 1)
	return &QueueWaitResult{
		Success:       false,
		WaitDuration:  time.Since(startTime),
		TimeoutReason: "timeout",
	}
}

// calculateMaxQueueSize 计算最大排队数
func (s *Service) calculateMaxQueueSize(apiKey *redis.APIKey) int {
	fixedSize := apiKey.ConcurrentRequestQueueMaxSize
	if fixedSize <= 0 {
		fixedSize = 3 // 默认值
	}

	multiplier := apiKey.ConcurrentRequestQueueMaxSizeMultiplier
	dynamicSize := 0
	if multiplier > 0 && apiKey.ConcurrentLimit > 0 {
		dynamicSize = int(math.Ceil(float64(apiKey.ConcurrentLimit) * multiplier))
	}

	if dynamicSize > fixedSize {
		return dynamicSize
	}
	return fixedSize
}

// CheckQueueHealth 检查队列健康状态
func (s *Service) CheckQueueHealth(ctx context.Context, apiKey *redis.APIKey) (bool, float64, error) {
	timeoutMs := apiKey.ConcurrentRequestQueueTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}

	// 默认阈值 0.8
	threshold := 0.8

	return s.redis.CheckQueueHealth(ctx, threshold, int64(timeoutMs))
}

// GetQueueStats 获取排队统计
func (s *Service) GetQueueStats(ctx context.Context, apiKeyID string) (*redis.QueueStats, error) {
	return s.redis.GetQueueStats(ctx, apiKeyID)
}

// CheckTotalCostLimit 检查总成本限制
func (s *Service) CheckTotalCostLimit(ctx context.Context, apiKey *redis.APIKey) (*TotalCostLimitResult, error) {
	totalLimit := apiKey.TotalCostLimit

	if totalLimit <= 0 {
		// 未设置限制时允许通过
		return &TotalCostLimitResult{Allowed: true}, nil
	}

	// 检查是否有活跃的加油包
	if s.hasActiveFuel(apiKey) {
		return &TotalCostLimitResult{Allowed: true}, nil
	}

	costStats, err := s.redis.GetTotalCost(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get total cost", zap.Error(err))
		// 出错时允许通过，避免阻塞请求
		return &TotalCostLimitResult{Allowed: true}, nil
	}

	totalCost := costStats.TotalCost
	if totalCost >= totalLimit {
		return &TotalCostLimitResult{
			Allowed:     false,
			CurrentCost: totalCost,
			TotalLimit:  totalLimit,
		}, nil
	}

	return &TotalCostLimitResult{
		Allowed:     true,
		CurrentCost: totalCost,
		TotalLimit:  totalLimit,
	}, nil
}

// CheckWeeklyOpusCostLimit 检查 Opus 周成本限制
func (s *Service) CheckWeeklyOpusCostLimit(ctx context.Context, apiKey *redis.APIKey, model string) (*WeeklyOpusCostResult, error) {
	weeklyLimit := apiKey.WeeklyOpusCostLimit

	if weeklyLimit <= 0 {
		// 未设置限制时允许通过
		return &WeeklyOpusCostResult{Allowed: true}, nil
	}

	// 检查是否为 Opus 模型
	if !isOpusModel(model) {
		return &WeeklyOpusCostResult{Allowed: true}, nil
	}

	weeklyCost, err := s.redis.GetWeeklyOpusCost(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get weekly opus cost", zap.Error(err))
		return &WeeklyOpusCostResult{Allowed: true}, nil
	}

	// 计算下周一的重置时间
	resetAt := getNextMondayMidnight()

	if weeklyCost >= weeklyLimit {
		return &WeeklyOpusCostResult{
			Allowed:     false,
			CurrentCost: weeklyCost,
			WeeklyLimit: weeklyLimit,
			ResetAt:     resetAt,
		}, nil
	}

	return &WeeklyOpusCostResult{
		Allowed:     true,
		CurrentCost: weeklyCost,
		WeeklyLimit: weeklyLimit,
		ResetAt:     resetAt,
	}, nil
}

// CheckRateLimitCost 检查速率限制窗口费用
func (s *Service) CheckRateLimitCost(ctx context.Context, apiKey *redis.APIKey) (*RateLimitCostResult, error) {
	windowMinutes := apiKey.RateLimitWindow
	costLimit := apiKey.RateLimitCost

	if windowMinutes <= 0 || costLimit <= 0 {
		// 未设置限制时允许通过
		return &RateLimitCostResult{Allowed: true}, nil
	}

	// 检查是否有活跃的加油包
	hasActiveFuel := s.hasActiveFuel(apiKey)
	if hasActiveFuel {
		return &RateLimitCostResult{
			Allowed:       true,
			HasActiveFuel: true,
		}, nil
	}

	currentCost, err := s.redis.GetRateLimitWindowCost(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get rate limit window cost", zap.Error(err))
		return &RateLimitCostResult{Allowed: true}, nil
	}

	resetAt := time.Now().Add(time.Duration(windowMinutes) * time.Minute)

	if currentCost >= costLimit {
		return &RateLimitCostResult{
			Allowed:       false,
			CurrentCost:   currentCost,
			CostLimit:     costLimit,
			WindowMinutes: windowMinutes,
			ResetAt:       resetAt,
		}, nil
	}

	return &RateLimitCostResult{
		Allowed:       true,
		CurrentCost:   currentCost,
		CostLimit:     costLimit,
		WindowMinutes: windowMinutes,
		ResetAt:       resetAt,
	}, nil
}

// hasActiveFuel 检查是否有活跃的加油包
func (s *Service) hasActiveFuel(apiKey *redis.APIKey) bool {
	if apiKey.FuelBalance <= 0 {
		return false
	}
	if apiKey.FuelNextExpiresAtMs <= 0 {
		return false
	}
	now := time.Now().UnixMilli()
	return apiKey.FuelNextExpiresAtMs > now
}

// isOpusModel 检查是否为 Opus 模型
func isOpusModel(model string) bool {
	if model == "" {
		return false
	}
	// 转换为小写进行匹配
	modelLower := strings.ToLower(model)
	return strings.Contains(modelLower, "claude-opus") ||
		strings.Contains(modelLower, "opus")
}

// getNextMondayMidnight 获取下周一凌晨时间
func getNextMondayMidnight() time.Time {
	now := time.Now()
	dayOfWeek := int(now.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7 // 周日
	}
	daysUntilMonday := (8 - dayOfWeek) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7 // 已是周一，返回下周一
	}
	nextMonday := now.AddDate(0, 0, daysUntilMonday)
	return time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), 0, 0, 0, 0, now.Location())
}

// CheckDailyCostLimitWithFuel 检查每日成本限制（带加油包支持）
func (s *Service) CheckDailyCostLimitWithFuel(ctx context.Context, apiKey *redis.APIKey) (*CostLimitResult, error) {
	dailyLimit := apiKey.DailyCostLimit

	if dailyLimit <= 0 {
		return &CostLimitResult{Allowed: true, LimitType: "daily"}, nil
	}

	// 检查是否有活跃的加油包
	if s.hasActiveFuel(apiKey) {
		return &CostLimitResult{Allowed: true, LimitType: "daily"}, nil
	}

	dailyCost, err := s.redis.GetDailyCost(ctx, apiKey.ID)
	if err != nil {
		logger.Warn("Failed to get daily cost", zap.Error(err))
		return &CostLimitResult{Allowed: true, LimitType: "daily"}, nil
	}

	if dailyCost >= dailyLimit {
		return &CostLimitResult{
			Allowed:     false,
			CurrentCost: dailyCost,
			DailyLimit:  dailyLimit,
			LimitType:   "daily",
		}, nil
	}

	return &CostLimitResult{
		Allowed:     true,
		CurrentCost: dailyCost,
		DailyLimit:  dailyLimit,
		LimitType:   "daily",
	}, nil
}
