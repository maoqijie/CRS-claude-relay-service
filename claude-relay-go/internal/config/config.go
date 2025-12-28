package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config 全局配置结构
type Config struct {
	Server         ServerConfig
	Redis          RedisConfig
	Postgres       PostgresConfig
	Security       SecurityConfig
	System         SystemConfig
	Pricing        PricingConfig
	UserManagement UserManagementConfig
	Web            WebConfig
}

type ServerConfig struct {
	Port       int
	Host       string
	Env        string
	TrustProxy bool
	LogDir     string
}

type RedisConfig struct {
	Host           string
	Port           int
	Password       string
	DB             int
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
	MaxRetries     int
	EnableTLS      bool
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
	JWTSecret      string
	APIKeyPrefix   string
	EncryptionKey  string
	ClaudeCodeOnly bool // 全局 Claude Code Only 限制
}

type SystemConfig struct {
	TimezoneOffset int
	MetricsWindow  int
}

type UserManagementConfig struct {
	Enabled bool
}

type WebConfig struct {
	EnableCors bool
}

type PricingConfig struct {
	// 远程价格源配置
	MirrorRepo     string        // GitHub 仓库，如 "Wei-Shaw/claude-relay-service"
	MirrorBranch   string        // 分支名，如 "price-mirror"
	MirrorBaseURL  string        // 自定义基础 URL（可选）
	JSONFileName   string        // JSON 文件名
	HashFileName   string        // 哈希文件名
	JSONUrl        string        // 完整的 JSON URL（可选，覆盖自动生成）
	HashUrl        string        // 完整的哈希 URL（可选，覆盖自动生成）
	UpdateInterval time.Duration // 定时更新间隔（默认 24 小时）
	HashCheckInterval time.Duration // 哈希校验间隔（默认 10 分钟）
	DataDir        string        // 数据目录
	FallbackFile   string        // 回退文件路径
}

// Cfg 全局配置实例
var Cfg *Config

// Load 加载配置
func Load() (*Config, error) {
	// 尝试从多个路径加载 .env (与 Node.js 共用)
	envPaths := []string{
		".env",
		"../.env",
		"../../.env",
	}

	envLoaded := false
	for _, p := range envPaths {
		if _, err := os.Stat(p); err == nil {
			if err := godotenv.Load(p); err != nil {
				fmt.Printf("⚠️  Failed to load .env from %s: %v\n", p, err)
			} else {
				fmt.Printf("✅ Loaded .env from %s\n", p)
				envLoaded = true
				break
			}
		}
	}

	if !envLoaded {
		fmt.Println("⚠️  No .env file found, using environment variables")
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:       getEnvInt("GO_PORT", 8080), // Go 服务使用不同端口
			Host:       getEnv("HOST", "0.0.0.0"),
			Env:        getEnv("NODE_ENV", "development"),
			TrustProxy: getEnvBool("TRUST_PROXY", false),
			LogDir:     getEnv("LOG_DIR", "../logs"), // 与 Node.js 共用日志目录
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
			JWTSecret:      getEnv("JWT_SECRET", ""),
			APIKeyPrefix:   getEnv("API_KEY_PREFIX", "cr_"),
			EncryptionKey:  getEnv("ENCRYPTION_KEY", ""),
			ClaudeCodeOnly: getEnvBool("CLAUDE_CODE_ONLY", false),
		},
		System: SystemConfig{
			TimezoneOffset: getEnvInt("TIMEZONE_OFFSET", 8),
			MetricsWindow:  getEnvInt("METRICS_WINDOW", 5),
		},
		Pricing: buildPricingConfig(),
		UserManagement: UserManagementConfig{
			Enabled: getEnvBool("USER_MANAGEMENT_ENABLED", false),
		},
		Web: WebConfig{
			EnableCors: getEnvBool("ENABLE_CORS", false),
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

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		// 尝试解析为毫秒数
		if ms, err := strconv.Atoi(val); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultVal
}

// buildPricingConfig 构建定价配置
func buildPricingConfig() PricingConfig {
	repo := getEnv("PRICE_MIRROR_REPO", getEnv("GITHUB_REPOSITORY", "Wei-Shaw/claude-relay-service"))
	branch := getEnv("PRICE_MIRROR_BRANCH", "price-mirror")
	jsonFileName := getEnv("PRICE_MIRROR_FILENAME", "model_prices_and_context_window.json")
	hashFileName := getEnv("PRICE_MIRROR_HASH_FILENAME", "model_prices_and_context_window.sha256")

	// 构建基础 URL
	baseURL := getEnv("PRICE_MIRROR_BASE_URL", "")
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s", repo, branch)
	}

	// 构建完整 URL
	jsonURL := getEnv("PRICE_MIRROR_JSON_URL", fmt.Sprintf("%s/%s", baseURL, jsonFileName))
	hashURL := getEnv("PRICE_MIRROR_HASH_URL", fmt.Sprintf("%s/%s", baseURL, hashFileName))

	return PricingConfig{
		MirrorRepo:        repo,
		MirrorBranch:      branch,
		MirrorBaseURL:     baseURL,
		JSONFileName:      jsonFileName,
		HashFileName:      hashFileName,
		JSONUrl:           jsonURL,
		HashUrl:           hashURL,
		UpdateInterval:    getEnvDuration("PRICE_UPDATE_INTERVAL", 24*time.Hour),
		HashCheckInterval: getEnvDuration("PRICE_HASH_CHECK_INTERVAL", 10*time.Minute),
		DataDir:           getEnv("PRICE_DATA_DIR", "../data"),
		FallbackFile:      getEnv("PRICE_FALLBACK_FILE", "../resources/model-pricing/model_prices_and_context_window.json"),
	}
}
