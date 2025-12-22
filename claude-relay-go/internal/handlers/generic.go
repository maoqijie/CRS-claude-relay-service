package handlers

import (
	"net/http"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GenericHandler 通用 Redis 操作处理器
type GenericHandler struct {
	redis *redis.Client
}

// NewGenericHandler 创建通用处理器
func NewGenericHandler(redisClient *redis.Client) *GenericHandler {
	return &GenericHandler{redis: redisClient}
}

// Get 获取键值
func (h *GenericHandler) Get(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	ctx := c.Request.Context()
	value, err := h.redis.Get(ctx, key)
	if err != nil {
		if err.Error() == "redis: nil" {
			c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
			return
		}
		logger.Error("Failed to get key", zap.String("key", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"value": value})
}

// Set 设置键值
func (h *GenericHandler) Set(c *gin.Context) {
	var req struct {
		Key        string      `json:"key"`
		Value      interface{} `json:"value"`
		Expiration int64       `json:"expiration"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	expiration := time.Duration(req.Expiration) * time.Second

	ctx := c.Request.Context()
	if err := h.redis.Set(ctx, req.Key, req.Value, expiration); err != nil {
		logger.Error("Failed to set key", zap.String("key", req.Key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Del 删除键
func (h *GenericHandler) Del(c *gin.Context) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Keys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keys is required"})
		return
	}

	ctx := c.Request.Context()
	deleted, err := h.redis.Del(ctx, req.Keys...)
	if err != nil {
		logger.Error("Failed to delete keys", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

// ScanKeys 扫描键
func (h *GenericHandler) ScanKeys(c *gin.Context) {
	pattern := c.Query("pattern")
	if pattern == "" {
		pattern = "*"
	}

	count := int64(1000)
	if c.Query("count") != "" {
		if v, err := time.ParseDuration(c.Query("count")); err == nil {
			count = int64(v.Seconds())
		}
	}

	ctx := c.Request.Context()
	keys, err := h.redis.ScanKeys(ctx, pattern, count)
	if err != nil {
		logger.Error("Failed to scan keys", zap.String("pattern", pattern), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"keys": keys, "total": len(keys)})
}

// HGetAll 获取 Hash 所有字段
func (h *GenericHandler) HGetAll(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	ctx := c.Request.Context()
	values, err := h.redis.HGetAll(ctx, key)
	if err != nil {
		logger.Error("Failed to hgetall", zap.String("key", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"values": values})
}

// HSet 设置 Hash 字段
func (h *GenericHandler) HSet(c *gin.Context) {
	var req struct {
		Key    string                 `json:"key"`
		Values map[string]interface{} `json:"values"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Key == "" || len(req.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key and values are required"})
		return
	}

	// 转换为扁平参数
	args := make([]interface{}, 0, len(req.Values)*2)
	for k, v := range req.Values {
		args = append(args, k, v)
	}

	ctx := c.Request.Context()
	if err := h.redis.HSet(ctx, req.Key, args...); err != nil {
		logger.Error("Failed to hset", zap.String("key", req.Key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DBSize 获取数据库大小
func (h *GenericHandler) DBSize(c *gin.Context) {
	ctx := c.Request.Context()
	size, err := h.redis.DBSize(ctx)
	if err != nil {
		logger.Error("Failed to get db size", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"size": size})
}

// Info 获取 Redis 信息
func (h *GenericHandler) Info(c *gin.Context) {
	ctx := c.Request.Context()
	info, err := h.redis.Info(ctx)
	if err != nil {
		logger.Error("Failed to get redis info", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"info": info})
}

// GetAllUsedModels 获取所有使用过的模型
func (h *GenericHandler) GetAllUsedModels(c *gin.Context) {
	ctx := c.Request.Context()
	models, err := h.redis.GetAllUsedModels(ctx)
	if err != nil {
		logger.Error("Failed to get all used models", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"models": models, "total": len(models)})
}
