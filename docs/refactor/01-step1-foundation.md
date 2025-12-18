# 第一步：Go 项目基础设施搭建

**状态**: ✅ 已完成 - 基础设施已搭建，Go 服务可运行并连接 Redis

---

## 1. 目标

完成 Go 项目的基础设施搭建，使其能够：
- 读取与 Node.js 相同的配置
- 连接到共享的 Redis 实例
- 提供健康检查接口
- 与 Node.js 服务并行运行

**预计工期**: 1 周
**验收标准**: Go 服务独立运行，能读取 Redis 中的数据

---

## 2. 目录结构

```
claude-relay-go/
├── cmd/
│   └── relay/
│       └── main.go              # 程序入口
├── internal/
│   ├── config/
│   │   └── config.go            # 配置加载
│   ├── storage/
│   │   └── redis/
│   │       ├── client.go        # Redis 客户端
│   │       ├── keys.go          # Key 常量定义
│   │       └── scripts/         # Lua 脚本 (后续添加)
│   └── pkg/
│       └── logger/
│           └── logger.go        # 日志系统
├── go.mod
├── go.sum
├── Makefile
└── .env                         # 复用现有 .env
```

---

## 3. 实施步骤

### 3.1 初始化 Go 模块

```bash
cd /home/catstream/claude-relay-service/claude-relay-go

# 初始化模块
go mod init github.com/catstream/claude-relay-go

# 安装核心依赖
go get github.com/gin-gonic/gin@latest
go get github.com/redis/go-redis/v9@latest
go get github.com/spf13/viper@latest
go get go.uber.org/zap@latest
go get github.com/joho/godotenv@latest
```

### 3.2 配置文件：internal/config/config.go

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config 全局配置结构
type Config struct {
	Server   ServerConfig
	Redis    RedisConfig
	Postgres PostgresConfig
	Security SecurityConfig
	System   SystemConfig
}

type ServerConfig struct {
	Port       int
	Host       string
	Env        string
	TrustProxy bool
}

type RedisConfig struct {
	Host               string
	Port               int
	Password           string
	DB                 int
	ConnectTimeout     time.Duration
	CommandTimeout     time.Duration
	MaxRetries         int
	EnableTLS          bool
}

type PostgresConfig struct {
	Enabled  bool
	URL      string
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSL      bool
	MaxPool  int
}

type SecurityConfig struct {
	JWTSecret     string
	APIKeyPrefix  string
	EncryptionKey string
}

type SystemConfig struct {
	TimezoneOffset int
	MetricsWindow  int
}

var Cfg *Config

// Load 加载配置
func Load() (*Config, error) {
	// 尝试从父目录加载 .env (与 Node.js 共用)
	envPaths := []string{
		".env",
		"../.env",
		"../../.env",
	}

	for _, p := range envPaths {
		if _, err := os.Stat(p); err == nil {
			godotenv.Load(p)
			break
		}
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:       getEnvInt("GO_PORT", 8080), // Go 服务使用不同端口
			Host:       getEnv("HOST", "0.0.0.0"),
			Env:        getEnv("NODE_ENV", "development"),
			TrustProxy: getEnvBool("TRUST_PROXY", false),
		},
		Redis: RedisConfig{
			Host:           getEnv("REDIS_HOST", "127.0.0.1"),
			Port:           getEnvInt("REDIS_PORT", 6379),
			Password:       getEnv("REDIS_PASSWORD", ""),
			DB:             getEnvInt("REDIS_DB", 0),
			ConnectTimeout: time.Duration(getEnvInt("REDIS_CONNECT_TIMEOUT", 10000)) * time.Millisecond,
			CommandTimeout: time.Duration(getEnvInt("REDIS_COMMAND_TIMEOUT", 5000)) * time.Millisecond,
			MaxRetries:     getEnvInt("REDIS_MAX_RETRIES", 3),
			EnableTLS:      getEnvBool("REDIS_ENABLE_TLS", false),
		},
		Postgres: PostgresConfig{
			Enabled:  getEnvBool("POSTGRES_ENABLED", false) || getEnv("POSTGRES_URL", "") != "",
			URL:      getEnv("POSTGRES_URL", ""),
			Host:     getEnv("POSTGRES_HOST", "127.0.0.1"),
			Port:     getEnvInt("POSTGRES_PORT", 5432),
			User:     getEnv("POSTGRES_USER", "postgres"),
			Password: getEnv("POSTGRES_PASSWORD", ""),
			Database: getEnv("POSTGRES_DATABASE", "postgres"),
			SSL:      getEnvBool("POSTGRES_SSL", false),
			MaxPool:  getEnvInt("POSTGRES_MAX_POOL_SIZE", 10),
		},
		Security: SecurityConfig{
			JWTSecret:     getEnv("JWT_SECRET", ""),
			APIKeyPrefix:  getEnv("API_KEY_PREFIX", "cr_"),
			EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		},
		System: SystemConfig{
			TimezoneOffset: getEnvInt("TIMEZONE_OFFSET", 8),
			MetricsWindow:  getEnvInt("METRICS_WINDOW", 5),
		},
	}

	// 验证必要配置
	if cfg.Security.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if cfg.Security.EncryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required")
	}

	Cfg = cfg
	return cfg, nil
}

// 辅助函数
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultVal
}

// GetProjectRoot 获取项目根目录
func GetProjectRoot() string {
	// 获取可执行文件目录
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}
```

### 3.3 日志系统：internal/pkg/logger/logger.go

```go
package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Log      *zap.Logger
	Sugar    *zap.SugaredLogger
)

// Init 初始化日志系统
func Init(env string, logDir string) error {
	var config zap.Config

	if env == "production" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// 确保日志目录存在
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}
		logFile := filepath.Join(logDir, "go-relay.log")
		config.OutputPaths = append(config.OutputPaths, logFile)
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	Log, err = config.Build()
	if err != nil {
		return err
	}

	Sugar = Log.Sugar()
	return nil
}

// Sync 刷新日志缓冲
func Sync() {
	if Log != nil {
		Log.Sync()
	}
}

// 便捷方法
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}

// Database 数据库操作日志 (对应 Node.js 的 logger.database)
func Database(msg string, fields ...zap.Field) {
	Log.Debug(msg, append(fields, zap.String("type", "database"))...)
}
```

### 3.4 Redis Key 常量：internal/storage/redis/keys.go

```go
package redis

import "time"

// Key 前缀常量 - 与 Node.js 保持完全一致
const (
	// API Key 相关
	PrefixAPIKey        = "apikey:"
	PrefixAPIKeyHashMap = "apikey:hash_map"
	PrefixAPIKeyLegacy  = "api_key:" // 历史兼容

	// 使用统计
	PrefixUsage        = "usage:"
	PrefixUsageDaily   = "usage:daily:"
	PrefixUsageMonthly = "usage:monthly:"
	PrefixUsageHourly  = "usage:hourly:"
	PrefixUsageModel   = "usage:model:"

	// 账户使用统计
	PrefixAccountUsage = "account_usage:"

	// 账户数据
	PrefixClaudeAccount         = "claude:account:"
	PrefixClaudeConsoleAccount  = "claude_console:account:"
	PrefixDroidAccount          = "droid:account:"
	PrefixOpenAIAccount         = "openai:account:"
	PrefixOpenAIResponsesAccount = "openai_responses:account:"
	PrefixGeminiAccount         = "gemini:account:"
	PrefixGeminiAPIAccount      = "gemini_api:account:"
	PrefixBedrockAccount        = "bedrock:account:"
	PrefixAzureOpenAIAccount    = "azure_openai:account:"
	PrefixCCRAccount            = "ccr:account:"

	// 并发控制
	PrefixConcurrency = "concurrency:"

	// 并发请求排队
	PrefixConcurrencyQueue     = "concurrency:queue:"
	PrefixConcurrencyQueueStats = "concurrency:queue:stats:"
	PrefixConcurrencyQueueWait  = "concurrency:queue:wait_times:"

	// 用户消息队列锁
	PrefixUserMsgLock = "user_msg_queue_lock:"
	PrefixUserMsgLast = "user_msg_queue_last:"

	// 会话
	PrefixSession       = "session:"
	PrefixStickySession = "sticky_session:"
	PrefixOAuthSession  = "oauth_session:"

	// 系统
	PrefixSystemMetrics = "system:metrics:minute:"
)

// TTL 常量
const (
	TTLAPIKey          = 365 * 24 * time.Hour // 1年
	TTLUsageDaily      = 32 * 24 * time.Hour  // 32天
	TTLUsageMonthly    = 365 * 24 * time.Hour // 1年
	TTLUsageHourly     = 7 * 24 * time.Hour   // 7天
	TTLQueueStats      = 7 * 24 * time.Hour   // 7天
	TTLWaitTimeSamples = 24 * time.Hour       // 1天
	TTLQueueBuffer     = 30 * time.Second     // 排队缓冲

	TTLSessionDefault  = 24 * time.Hour      // 默认会话 TTL
	TTLOAuthSession    = 10 * time.Minute    // OAuth 会话
)

// 采样数配置
const (
	WaitTimeSamplesPerKey = 500  // 每 API Key 等待时间样本数
	WaitTimeSamplesGlobal = 2000 // 全局等待时间样本数
)
```

### 3.5 Redis 客户端：internal/storage/redis/client.go

```go
package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	ErrNotConnected = errors.New("redis client is not connected")
)

// Client Redis 客户端封装
type Client struct {
	client      *redis.Client
	isConnected bool
	mu          sync.RWMutex
	cfg         *config.RedisConfig
}

var (
	instance *Client
	once     sync.Once
)

// GetInstance 获取 Redis 客户端单例
func GetInstance() *Client {
	once.Do(func() {
		instance = &Client{}
	})
	return instance
}

// Connect 连接 Redis
func (c *Client) Connect(cfg *config.RedisConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cfg = cfg

	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.ConnectTimeout,
		ReadTimeout:  cfg.CommandTimeout,
		WriteTimeout: cfg.CommandTimeout,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     100,
		MinIdleConns: 10,
	}

	if cfg.EnableTLS {
		opts.TLSConfig = &tls.Config{}
	}

	c.client = redis.NewClient(opts)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	if _, err := c.client.Ping(ctx).Result(); err != nil {
		logger.Error("❌ Failed to connect to Redis", zap.Error(err))
		return err
	}

	c.isConnected = true
	logger.Info("🔗 Redis connected successfully",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.Int("db", cfg.DB))

	return nil
}

// Disconnect 断开连接
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		if err := c.client.Close(); err != nil {
			return err
		}
		c.isConnected = false
		logger.Info("👋 Redis disconnected")
	}
	return nil
}

// GetClient 获取原始客户端 (允许返回 nil)
func (c *Client) GetClient() *redis.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected {
		logger.Warn("⚠️ Redis client is not connected")
		return nil
	}
	return c.client
}

// GetClientSafe 安全获取客户端 (错误时返回 error)
func (c *Client) GetClientSafe() (*redis.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected || c.client == nil {
		return nil, ErrNotConnected
	}
	return c.client, nil
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}

// ========== 通用操作 ==========

// Get 获取字符串值
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return "", err
	}
	return client.Get(ctx, key).Result()
}

// Set 设置字符串值
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}
	return client.Set(ctx, key, value, expiration).Err()
}

// Del 删除键
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}
	return client.Del(ctx, keys...).Result()
}

// HGetAll 获取 Hash 所有字段
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}
	return client.HGetAll(ctx, key).Result()
}

// HSet 设置 Hash 字段
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}
	return client.HSet(ctx, key, values...).Err()
}

// ScanKeys 使用 SCAN 获取匹配的所有 key (避免阻塞)
func (c *Client) ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}

	var keys []string
	var cursor uint64

	for {
		var batch []string
		var err error
		batch, cursor, err = client.Scan(ctx, cursor, pattern, count).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

// Eval 执行 Lua 脚本
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	client, err := c.GetClientSafe()
	if err != nil {
		cmd := redis.NewCmd(ctx)
		cmd.SetErr(err)
		return cmd
	}
	return client.Eval(ctx, script, keys, args...)
}

// Pipeline 获取 Pipeline
func (c *Client) Pipeline() (redis.Pipeliner, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}
	return client.Pipeline(), nil
}

// ========== 健康检查 ==========

// Health 健康检查
func (c *Client) Health(ctx context.Context) error {
	client, err := c.GetClientSafe()
	if err != nil {
		return err
	}
	return client.Ping(ctx).Err()
}

// Info 获取 Redis 信息
func (c *Client) Info(ctx context.Context) (string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return "", err
	}
	return client.Info(ctx).Result()
}
```

### 3.6 主程序：cmd/relay/main.go

```go
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
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. 初始化日志
	logDir := "../logs" // 与 Node.js 共用日志目录
	if err := logger.Init(cfg.Server.Env, logDir); err != nil {
		fmt.Printf("❌ Failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("🚀 Starting Claude Relay Service (Go)",
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

	// 简单的 Redis 数据读取测试
	router.GET("/test/redis/apikeys/count", testAPIKeyCountHandler(redisClient))

	// 6. 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second, // 流式响应需要较长超时
		IdleTimeout:  120 * time.Second,
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		ctx := c.Request.Context()

		// 检查 Redis
		redisOK := redisClient.Health(ctx) == nil

		status := "healthy"
		httpStatus := http.StatusOK

		if !redisOK {
			status = "unhealthy"
			httpStatus = http.StatusServiceUnavailable
		}

		c.JSON(httpStatus, gin.H{
			"status":    status,
			"service":   "claude-relay-go",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"components": gin.H{
				"redis": redisOK,
			},
		})
	}
}

// versionHandler 版本信息
func versionHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "claude-relay-go",
			"version": "0.1.0",
			"go":      "1.22",
		})
	}
}

// testAPIKeyCountHandler 测试读取 API Key 数量
func testAPIKeyCountHandler(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 使用 SCAN 统计 apikey:* 的数量
		keys, err := redisClient.ScanKeys(ctx, "apikey:*", 1000)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		// 排除 hash_map
		count := 0
		for _, key := range keys {
			if key != "apikey:hash_map" {
				count++
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"apiKeyCount": count,
			"message":     "Successfully read from Redis (shared with Node.js)",
		})
	}
}
```

### 3.7 Makefile

```makefile
.PHONY: build run test clean dev

# 变量
BINARY_NAME=claude-relay-go
BUILD_DIR=./bin
MAIN_PATH=./cmd/relay

# 构建
build:
	@echo "🔨 Building..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 开发模式运行
dev:
	@echo "🚀 Running in development mode..."
	@GO_PORT=8080 go run $(MAIN_PATH)/main.go

# 生产运行
run: build
	@echo "🚀 Running..."
	@$(BUILD_DIR)/$(BINARY_NAME)

# 测试
test:
	@echo "🧪 Running tests..."
	@go test -v ./...

# 清理
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

# 依赖整理
tidy:
	@echo "📦 Tidying dependencies..."
	@go mod tidy

# 格式化
fmt:
	@echo "🎨 Formatting code..."
	@go fmt ./...

# 代码检查
lint:
	@echo "🔍 Linting..."
	@golangci-lint run

# 帮助
help:
	@echo "Available commands:"
	@echo "  make build  - Build the binary"
	@echo "  make dev    - Run in development mode"
	@echo "  make run    - Build and run"
	@echo "  make test   - Run tests"
	@echo "  make clean  - Clean build artifacts"
	@echo "  make tidy   - Tidy go modules"
	@echo "  make fmt    - Format code"
	@echo "  make lint   - Run linter"
```

---

## 4. 验证步骤

### 4.1 构建并运行

```bash
cd /home/catstream/claude-relay-service/claude-relay-go

# 安装依赖
go mod tidy

# 构建
make build

# 运行 (使用 8080 端口，避免与 Node.js 3000 冲突)
make dev
```

### 4.2 测试接口

```bash
# 健康检查
curl http://localhost:8080/health

# 预期响应:
# {"status":"healthy","service":"claude-relay-go","timestamp":"...","components":{"redis":true}}

# 版本信息
curl http://localhost:8080/version

# 测试读取 Redis 数据
curl http://localhost:8080/test/redis/apikeys/count

# 预期响应:
# {"apiKeyCount":10,"message":"Successfully read from Redis (shared with Node.js)"}
```

### 4.3 验证 Redis 数据兼容性

```bash
# 在 Node.js 中创建一个测试 API Key
curl -X POST http://localhost:3000/admin/api-keys \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"name":"test-go-compatibility"}'

# 在 Go 服务中读取
curl http://localhost:8080/test/redis/apikeys/count

# 数量应该增加 1
```

---

## 5. 检查清单

- [ ] Go 模块初始化 (`go mod init`)
- [ ] 配置加载正常 (读取 .env)
- [ ] 日志系统工作 (控制台 + 文件)
- [ ] Redis 连接成功
- [ ] 健康检查接口正常
- [ ] 能读取 Node.js 写入的 Redis 数据
- [ ] 两个服务可同时运行

---

## 6. 下一步

完成第一步后，进入 [02-step2-redis-operations.md](./02-step2-redis-operations.md)：
- 实现完整的 API Key CRUD 操作
- 实现并发控制 (Lua 脚本)
- 实现分布式锁
- 实现使用统计

---

**文档版本**: v1.0
**创建日期**: 2024-12-18
