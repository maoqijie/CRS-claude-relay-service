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
	JWTSecret     string
	APIKeyPrefix  string
	EncryptionKey string
}

type SystemConfig struct {
	TimezoneOffset int
	MetricsWindow  int
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
