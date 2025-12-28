package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RateLimitConfig 速率限制配置
type RateLimitConfig struct {
	// 每分钟请求数限制
	RequestsPerMinute int
	// 每小时请求数限制
	RequestsPerHour int
	// 限制的键前缀（用于区分不同的限制规则）
	KeyPrefix string
	// 是否跳过对已认证用户的限制
	SkipAuthenticated bool
	// 获取限制键的函数
	KeyFunc func(*gin.Context) string
}

// RateLimiter 速率限制器
type RateLimiter struct {
	redis  *redis.Client
	config RateLimitConfig
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(redisClient *redis.Client, config RateLimitConfig) *RateLimiter {
	if config.KeyPrefix == "" {
		config.KeyPrefix = "rate_limit:global"
	}
	if config.KeyFunc == nil {
		config.KeyFunc = defaultKeyFunc
	}

	return &RateLimiter{
		redis:  redisClient,
		config: config,
	}
}

// defaultKeyFunc 默认的限制键函数（基于客户端 IP）
func defaultKeyFunc(c *gin.Context) string {
	return c.ClientIP()
}

// Limit 返回速率限制中间件
func (rl *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过已认证用户
		if rl.config.SkipAuthenticated {
			if apiKey := GetAPIKeyFromContext(c); apiKey != nil {
				c.Next()
				return
			}
		}

		// 获取限制键
		key := rl.config.KeyFunc(c)

		// 检查每分钟限制
		if rl.config.RequestsPerMinute > 0 {
			allowed, remaining, resetAt := rl.checkLimit(c, key, "minute", rl.config.RequestsPerMinute, time.Minute)
			if !allowed {
				rl.sendRateLimitResponse(c, remaining, resetAt, "minute")
				return
			}
			c.Header("X-RateLimit-Limit-Minute", strconv.Itoa(rl.config.RequestsPerMinute))
			c.Header("X-RateLimit-Remaining-Minute", strconv.FormatInt(remaining, 10))
		}

		// 检查每小时限制
		if rl.config.RequestsPerHour > 0 {
			allowed, remaining, resetAt := rl.checkLimit(c, key, "hour", rl.config.RequestsPerHour, time.Hour)
			if !allowed {
				rl.sendRateLimitResponse(c, remaining, resetAt, "hour")
				return
			}
			c.Header("X-RateLimit-Limit-Hour", strconv.Itoa(rl.config.RequestsPerHour))
			c.Header("X-RateLimit-Remaining-Hour", strconv.FormatInt(remaining, 10))
		}

		c.Next()
	}
}

// checkLimit 检查限制
func (rl *RateLimiter) checkLimit(c *gin.Context, key, window string, limit int, duration time.Duration) (bool, int64, time.Time) {
	ctx := c.Request.Context()
	windowSeconds := int64(duration.Seconds())
	redisKey := rl.config.KeyPrefix + ":" + key + ":" + window + ":" + strconv.FormatInt(time.Now().Unix()/windowSeconds, 10)

	count, err := rl.redis.IncrWithExpiry(ctx, redisKey, duration)
	if err != nil {
		logger.Warn("Failed to check rate limit", zap.Error(err))
		// 出错时允许通过
		return true, int64(limit), time.Now().Add(duration)
	}

	remaining := int64(limit) - count
	if remaining < 0 {
		remaining = 0
	}

	resetAt := time.Now().Truncate(duration).Add(duration)

	if count > int64(limit) {
		return false, 0, resetAt
	}

	return true, remaining, resetAt
}

// sendRateLimitResponse 发送速率限制响应
func (rl *RateLimiter) sendRateLimitResponse(c *gin.Context, remaining int64, resetAt time.Time, window string) {
	retryAfter := int(time.Until(resetAt).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}

	c.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
	c.Header("X-RateLimit-Reset", resetAt.Format(time.RFC3339))
	c.Header("Retry-After", strconv.Itoa(retryAfter))

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":      "Rate limit exceeded",
		"code":       "rate_limit_exceeded",
		"window":     window,
		"retryAfter": retryAfter,
	})
}

// IPRateLimiter 基于 IP 的速率限制
func IPRateLimiter(redisClient *redis.Client, requestsPerMinute, requestsPerHour int) gin.HandlerFunc {
	limiter := NewRateLimiter(redisClient, RateLimitConfig{
		RequestsPerMinute: requestsPerMinute,
		RequestsPerHour:   requestsPerHour,
		KeyPrefix:         "rate_limit:ip",
		KeyFunc:           defaultKeyFunc,
	})
	return limiter.Limit()
}

// PathRateLimiter 基于路径的速率限制
func PathRateLimiter(redisClient *redis.Client, requestsPerMinute int) gin.HandlerFunc {
	limiter := NewRateLimiter(redisClient, RateLimitConfig{
		RequestsPerMinute: requestsPerMinute,
		KeyPrefix:         "rate_limit:path",
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP() + ":" + c.Request.URL.Path
		},
	})
	return limiter.Limit()
}

// GlobalRateLimiter 全局速率限制（所有请求共享）
func GlobalRateLimiter(redisClient *redis.Client, requestsPerMinute int) gin.HandlerFunc {
	limiter := NewRateLimiter(redisClient, RateLimitConfig{
		RequestsPerMinute: requestsPerMinute,
		KeyPrefix:         "rate_limit:global",
		KeyFunc: func(c *gin.Context) string {
			return "all"
		},
	})
	return limiter.Limit()
}

// ConcurrencyLimiter 并发限制中间件
type ConcurrencyLimiter struct {
	redis    *redis.Client
	maxConns int
	keyFunc  func(*gin.Context) string
}

// NewConcurrencyLimiter 创建并发限制器
func NewConcurrencyLimiter(redisClient *redis.Client, maxConns int) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		redis:    redisClient,
		maxConns: maxConns,
		keyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	}
}

// WithKeyFunc 设置键函数
func (cl *ConcurrencyLimiter) WithKeyFunc(fn func(*gin.Context) string) *ConcurrencyLimiter {
	cl.keyFunc = fn
	return cl
}

// Limit 返回并发限制中间件
func (cl *ConcurrencyLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		key := cl.keyFunc(c)
		requestID := GetRequestIDFromContext(c)
		if requestID == "" {
			requestID = "req-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}

		// 尝试获取槽位
		count, err := cl.redis.IncrConcurrency(ctx, "concurrency:global:"+key, requestID, 300)
		if err != nil {
			logger.Warn("Failed to check concurrency", zap.Error(err))
			c.Next()
			return
		}

		if count > int64(cl.maxConns) {
			// 释放刚刚获取的槽位
			cl.redis.DecrConcurrency(ctx, "concurrency:global:"+key, requestID)

			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":              "Too many concurrent connections",
				"code":               "concurrency_limit_exceeded",
				"currentConcurrency": count,
				"limit":              cl.maxConns,
			})
			return
		}

		// 请求完成后释放槽位
		defer func() {
			cl.redis.DecrConcurrency(ctx, "concurrency:global:"+key, requestID)
		}()

		c.Next()
	}
}

// BurstLimiter 突发流量限制器（令牌桶算法）
type BurstLimiter struct {
	redis       *redis.Client
	rate        int           // 每秒补充的令牌数
	burst       int           // 最大令牌数
	keyPrefix   string
	keyFunc     func(*gin.Context) string
}

// NewBurstLimiter 创建突发流量限制器
func NewBurstLimiter(redisClient *redis.Client, rate, burst int) *BurstLimiter {
	return &BurstLimiter{
		redis:     redisClient,
		rate:      rate,
		burst:     burst,
		keyPrefix: "burst_limit",
		keyFunc:   defaultKeyFunc,
	}
}

// Limit 返回突发流量限制中间件
func (bl *BurstLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		key := bl.keyPrefix + ":" + bl.keyFunc(c)

		// 使用 Lua 脚本实现令牌桶
		allowed, remaining, err := bl.checkBurstLimit(ctx, key)
		if err != nil {
			logger.Warn("Burst limit check failed", zap.Error(err))
			c.Next()
			return
		}

		c.Header("X-RateLimit-Burst-Remaining", strconv.FormatInt(remaining, 10))
		c.Header("X-RateLimit-Burst-Limit", strconv.Itoa(bl.burst))

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":     "Burst limit exceeded",
				"code":      "burst_limit_exceeded",
				"remaining": remaining,
				"limit":     bl.burst,
			})
			return
		}

		c.Next()
	}
}

// checkBurstLimit 检查突发流量限制
func (bl *BurstLimiter) checkBurstLimit(ctx context.Context, key string) (bool, int64, error) {
	client, err := bl.redis.GetClientSafe()
	if err != nil {
		return true, int64(bl.burst), err
	}

	// Lua 脚本实现令牌桶
	script := `
		local key = KEYS[1]
		local rate = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local requested = 1

		local tokens_key = key .. ":tokens"
		local timestamp_key = key .. ":ts"

		local last_tokens = tonumber(redis.call("get", tokens_key) or burst)
		local last_timestamp = tonumber(redis.call("get", timestamp_key) or now)

		local elapsed = now - last_timestamp
		local new_tokens = math.min(burst, last_tokens + (elapsed * rate))

		local allowed = new_tokens >= requested
		local remaining = new_tokens

		if allowed then
			remaining = new_tokens - requested
		end

		redis.call("setex", tokens_key, 60, remaining)
		redis.call("setex", timestamp_key, 60, now)

		return {allowed and 1 or 0, remaining}
	`

	result, err := client.Eval(ctx, script, []string{key},
		bl.rate, bl.burst, time.Now().Unix()).Result()
	if err != nil {
		return true, int64(bl.burst), err
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) < 2 {
		return true, int64(bl.burst), nil
	}

	// 安全类型断言
	allowedVal, ok := arr[0].(int64)
	if !ok {
		return true, int64(bl.burst), nil
	}

	remaining, ok := arr[1].(int64)
	if !ok {
		return true, int64(bl.burst), nil
	}

	return allowedVal == 1, remaining, nil
}
