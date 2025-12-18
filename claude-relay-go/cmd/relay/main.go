package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/middleware"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/catstream/claude-relay-go/pkg/types"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	version = "0.1.0"

	// Redis 操作超时常量
	healthCheckTimeout  = 3 * time.Second  // 健康检查超时（快速响应）
	redisQueryTimeout   = 5 * time.Second  // 简单查询超时
	redisScanTimeout    = 10 * time.Second // SCAN 操作超时（可能遍历大量数据）
	shutdownTimeout     = 30 * time.Second // 优雅关闭超时
	readTimeout         = 30 * time.Second // HTTP 读取超时
	writeTimeout        = 600 * time.Second // HTTP 写入超时（流式响应需要较长时间）
	idleTimeout         = 120 * time.Second // HTTP 空闲超时
	redisScanBatchSize  = 1000              // Redis SCAN 批次大小
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. 初始化日志
	if err := logger.Init(cfg.Server.Env, cfg.Server.LogDir); err != nil {
		fmt.Printf("❌ Failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Claude Relay Service (Go)",
		zap.String("version", version),
		zap.String("env", cfg.Server.Env),
		zap.Int("port", cfg.Server.Port))

	// 3. 连接 Redis
	redisClient := redis.GetInstance()
	if err := redisClient.Connect(&cfg.Redis); err != nil {
		logger.Fatal("❌ Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Disconnect()

	// 4. 设置 Gin 模式
	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 5. 创建路由
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginLogger())

	// 健康检查
	router.GET("/health", healthHandler(redisClient))

	// 版本信息
	router.GET("/version", versionHandler())

	// Redis 数据读取测试（仅开发环境）
	testRoutes := router.Group("/test")
	testRoutes.Use(middleware.DevelopmentOnly(cfg.Server.Env))
	{
		testRoutes.GET("/redis/apikeys/count", testAPIKeyCountHandler(redisClient))
		testRoutes.GET("/redis/accounts/count", testAccountsCountHandler(redisClient))
		testRoutes.GET("/redis/info", testRedisInfoHandler(redisClient))
	}

	// 6. 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// 启动协程运行服务器
	go func() {
		logger.Info("🌐 Server listening",
			zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("❌ Server failed", zap.Error(err))
		}
	}()

	// 7. 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("🛑 Shutting down server...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("❌ Server forced to shutdown", zap.Error(err))
	}

	logger.Info("👋 Server exited")
}

// ginLogger Gin 日志中间件
func ginLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		logger.Info("HTTP Request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.String("ip", c.ClientIP()))
	}
}

// healthHandler 健康检查处理器
func healthHandler(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置健康检查超时（应快速响应）
		ctx, cancel := context.WithTimeout(c.Request.Context(), healthCheckTimeout)
		defer cancel()

		// 检查 Redis
		redisOK := redisClient.Health(ctx) == nil

		status := "healthy"
		httpStatus := http.StatusOK

		if !redisOK {
			status = "unhealthy"
			httpStatus = http.StatusServiceUnavailable
		}

		response := &types.HealthResponse{
			Status:    status,
			Service:   "claude-relay-go",
			Version:   version,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Components: map[string]bool{
				"redis": redisOK,
			},
		}

		c.JSON(httpStatus, response)
	}
}

// versionHandler 版本信息
func versionHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		response := &types.VersionResponse{
			Service: "claude-relay-go",
			Version: version,
			Go:      "1.24",
		}
		c.JSON(http.StatusOK, response)
	}
}

// testAPIKeyCountHandler 测试读取 API Key 数量
func testAPIKeyCountHandler(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置 SCAN 操作超时
		ctx, cancel := context.WithTimeout(c.Request.Context(), redisScanTimeout)
		defer cancel()

		// 使用 SCAN 统计 apikey:* 的数量
		keys, err := redisClient.ScanKeys(ctx, "apikey:*", redisScanBatchSize)
		if err != nil {
			response := &types.ErrorResponse{
				Error:     "Failed to scan Redis keys",
				Message:   "Internal server error",
				Timestamp: time.Now(),
			}
			c.JSON(http.StatusInternalServerError, response)
			logger.Error("Failed to scan API keys", zap.Error(err))
			return
		}

		// 排除 hash_map
		count := 0
		for _, key := range keys {
			if key != "apikey:hash_map" {
				count++
			}
		}

		response := &types.CountResponse{
			Count:   count,
			Message: "Successfully read from Redis (shared with Node.js)",
		}
		c.JSON(http.StatusOK, response)
	}
}

// testAccountsCountHandler 测试读取账户数量
func testAccountsCountHandler(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置 SCAN 操作超时
		ctx, cancel := context.WithTimeout(c.Request.Context(), redisScanTimeout)
		defer cancel()

		counts := make(map[string]int)

		// 统计各类账户
		accountTypes := map[string]string{
			"claude":           "claude:account:*",
			"claude_console":   "claude_console:account:*",
			"gemini":           "gemini:account:*",
			"gemini_api":       "gemini_api:account:*",
			"openai":           "openai:account:*",
			"openai_responses": "openai_responses:account:*",
			"bedrock":          "bedrock:account:*",
			"azure_openai":     "azure_openai:account:*",
			"droid":            "droid:account:*",
			"ccr":              "ccr:account:*",
		}

		total := 0
		for name, pattern := range accountTypes {
			keys, err := redisClient.ScanKeys(ctx, pattern, redisScanBatchSize)
			if err != nil {
				counts[name] = -1
				logger.Warn("Failed to scan account type", zap.String("type", name), zap.Error(err))
				continue
			}
			counts[name] = len(keys)
			total += len(keys)
		}

		response := &types.AccountsCountResponse{
			Accounts: counts,
			Total:    total,
			Message:  "Successfully read accounts from Redis",
		}
		c.JSON(http.StatusOK, response)
	}
}

// testRedisInfoHandler 获取 Redis 信息
func testRedisInfoHandler(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置简单查询超时
		ctx, cancel := context.WithTimeout(c.Request.Context(), redisQueryTimeout)
		defer cancel()

		dbSize, err := redisClient.DBSize(ctx)
		if err != nil {
			response := &types.ErrorResponse{
				Error:     "Failed to get Redis info",
				Message:   "Internal server error",
				Timestamp: time.Now(),
			}
			c.JSON(http.StatusInternalServerError, response)
			logger.Error("Failed to get Redis DBSize", zap.Error(err))
			return
		}

		response := &types.RedisInfoResponse{
			DBSize:  dbSize,
			Message: "Redis connection OK",
		}
		c.JSON(http.StatusOK, response)
	}
}
