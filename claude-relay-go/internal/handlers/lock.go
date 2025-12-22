package handlers

import (
	"net/http"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// LockHandler 锁处理器
type LockHandler struct {
	redis *redis.Client
}

// NewLockHandler 创建锁处理器
func NewLockHandler(redisClient *redis.Client) *LockHandler {
	return &LockHandler{redis: redisClient}
}

// AcquireLock 获取锁
func (h *LockHandler) AcquireLock(c *gin.Context) {
	var req struct {
		LockKey string `json:"lockKey"`
		TTL     int64  `json:"ttl"` // 毫秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LockKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lockKey is required"})
		return
	}

	ttl := time.Duration(req.TTL) * time.Millisecond
	if ttl == 0 {
		ttl = 30 * time.Second
	}

	ctx := c.Request.Context()
	result, err := h.redis.AcquireLock(ctx, req.LockKey, ttl)
	if err != nil {
		logger.Error("Failed to acquire lock", zap.String("lockKey", req.LockKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"acquired": result.Success,
		"token":    result.Token,
	})
}

// ReleaseLock 释放锁
func (h *LockHandler) ReleaseLock(c *gin.Context) {
	var req struct {
		LockKey string `json:"lockKey"`
		Token   string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LockKey == "" || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lockKey and token are required"})
		return
	}

	ctx := c.Request.Context()
	released, err := h.redis.ReleaseLock(ctx, req.LockKey, req.Token)
	if err != nil {
		logger.Error("Failed to release lock", zap.String("lockKey", req.LockKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"released": released})
}

// ExtendLock 延长锁
func (h *LockHandler) ExtendLock(c *gin.Context) {
	var req struct {
		LockKey string `json:"lockKey"`
		Token   string `json:"token"`
		TTL     int64  `json:"ttl"` // 毫秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LockKey == "" || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lockKey and token are required"})
		return
	}

	ttl := time.Duration(req.TTL) * time.Millisecond
	if ttl == 0 {
		ttl = 30 * time.Second
	}

	ctx := c.Request.Context()
	extended, err := h.redis.ExtendLock(ctx, req.LockKey, req.Token, ttl)
	if err != nil {
		logger.Error("Failed to extend lock", zap.String("lockKey", req.LockKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"extended": extended})
}

// AcquireUserMessageLock 获取用户消息锁
func (h *LockHandler) AcquireUserMessageLock(c *gin.Context) {
	var req struct {
		AccountID string `json:"accountId"`
		RequestID string `json:"requestId"`
		LockTTLMs int64  `json:"lockTTLMs"`
		DelayMs   int64  `json:"delayMs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AccountID == "" || req.RequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "accountId and requestId are required"})
		return
	}

	if req.LockTTLMs == 0 {
		req.LockTTLMs = 5000 // 默认 5 秒
	}
	if req.DelayMs == 0 {
		req.DelayMs = 200 // 默认 200ms
	}

	ctx := c.Request.Context()
	result, err := h.redis.AcquireUserMessageLock(ctx, req.AccountID, req.RequestID, req.LockTTLMs, req.DelayMs)
	if err != nil {
		logger.Error("Failed to acquire user message lock", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"acquired": result.Acquired,
		"waitMs":   result.WaitMs,
	})
}

// ReleaseUserMessageLock 释放用户消息锁
func (h *LockHandler) ReleaseUserMessageLock(c *gin.Context) {
	var req struct {
		AccountID string `json:"accountId"`
		RequestID string `json:"requestId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AccountID == "" || req.RequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "accountId and requestId are required"})
		return
	}

	ctx := c.Request.Context()
	released, err := h.redis.ReleaseUserMessageLock(ctx, req.AccountID, req.RequestID)
	if err != nil {
		logger.Error("Failed to release user message lock", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"released": released})
}

// ForceReleaseUserMessageLock 强制释放用户消息锁
func (h *LockHandler) ForceReleaseUserMessageLock(c *gin.Context) {
	accountID := c.Param("accountId")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "accountId is required"})
		return
	}

	ctx := c.Request.Context()
	released, err := h.redis.ForceReleaseUserMessageLock(ctx, accountID)
	if err != nil {
		logger.Error("Failed to force release user message lock", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"released": released})
}

// GetUserMessageQueueStats 获取用户消息队列统计
func (h *LockHandler) GetUserMessageQueueStats(c *gin.Context) {
	accountID := c.Param("accountId")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "accountId is required"})
		return
	}

	ctx := c.Request.Context()
	stats, err := h.redis.GetUserMessageQueueStats(ctx, accountID)
	if err != nil {
		logger.Error("Failed to get user message queue stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}
