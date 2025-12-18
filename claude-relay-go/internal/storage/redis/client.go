package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 连接池配置常量
const (
	// DefaultPoolSize Redis 默认连接池大小
	DefaultPoolSize = 100
	// DefaultMinIdleConns Redis 默认最小空闲连接数
	DefaultMinIdleConns = 10
)

var (
	// ErrNotConnected Redis 未连接错误
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
		PoolSize:     DefaultPoolSize,     // 100 个连接，适用于高并发场景
		MinIdleConns: DefaultMinIdleConns, // 10 个最小空闲连接，保持连接可用性
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

// HVals 获取 Hash 所有值
func (c *Client) HVals(ctx context.Context, key string) ([]string, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return nil, err
	}
	return client.HVals(ctx, key).Result()
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

// DBSize 获取数据库 key 数量
func (c *Client) DBSize(ctx context.Context) (int64, error) {
	client, err := c.GetClientSafe()
	if err != nil {
		return 0, err
	}
	return client.DBSize(ctx).Result()
}
