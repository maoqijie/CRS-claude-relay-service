package handlers

import (
	"net/http"
	"strconv"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ConcurrencyHandler 并发控制处理器
type ConcurrencyHandler struct {
	redis *redis.Client
}

// NewConcurrencyHandler 创建并发控制处理器
func NewConcurrencyHandler(redisClient *redis.Client) *ConcurrencyHandler {
	return &ConcurrencyHandler{redis: redisClient}
}

// IncrConcurrency 增加并发计数
func (h *ConcurrencyHandler) IncrConcurrency(c *gin.Context) {
	var req struct {
		APIKeyID     string `json:"apiKeyId"`
		RequestID    string `json:"requestId"`
		LeaseSeconds int    `json:"leaseSeconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LeaseSeconds == 0 {
		req.LeaseSeconds = 600 // 默认 10 分钟
	}

	ctx := c.Request.Context()
	count, err := h.redis.IncrConcurrency(ctx, req.APIKeyID, req.RequestID, req.LeaseSeconds)
	if err != nil {
		logger.Error("Failed to incr concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// DecrConcurrency 减少并发计数
func (h *ConcurrencyHandler) DecrConcurrency(c *gin.Context) {
	var req struct {
		APIKeyID  string `json:"apiKeyId"`
		RequestID string `json:"requestId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.DecrConcurrency(ctx, req.APIKeyID, req.RequestID)
	if err != nil {
		logger.Error("Failed to decr concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// GetConcurrency 获取并发计数
func (h *ConcurrencyHandler) GetConcurrency(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.GetConcurrency(ctx, apiKeyID)
	if err != nil {
		logger.Error("Failed to get concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// GetConcurrencyStatus 获取并发状态
func (h *ConcurrencyHandler) GetConcurrencyStatus(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	status, err := h.redis.GetConcurrencyStatus(ctx, apiKeyID)
	if err != nil {
		logger.Error("Failed to get concurrency status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetAllConcurrencyStatus 获取所有并发状态
func (h *ConcurrencyHandler) GetAllConcurrencyStatus(c *gin.Context) {
	ctx := c.Request.Context()
	statuses, err := h.redis.GetAllConcurrencyStatus(ctx)
	if err != nil {
		logger.Error("Failed to get all concurrency status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"statuses": statuses, "total": len(statuses)})
}

// RefreshConcurrencyLease 刷新并发租约
func (h *ConcurrencyHandler) RefreshConcurrencyLease(c *gin.Context) {
	var req struct {
		APIKeyID     string `json:"apiKeyId"`
		RequestID    string `json:"requestId"`
		LeaseSeconds int    `json:"leaseSeconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LeaseSeconds == 0 {
		req.LeaseSeconds = 600
	}

	ctx := c.Request.Context()
	refreshed, err := h.redis.RefreshConcurrencyLease(ctx, req.APIKeyID, req.RequestID, req.LeaseSeconds)
	if err != nil {
		logger.Error("Failed to refresh concurrency lease", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"refreshed": refreshed})
}

// CleanupExpiredConcurrency 清理过期并发
func (h *ConcurrencyHandler) CleanupExpiredConcurrency(c *gin.Context) {
	ctx := c.Request.Context()
	cleaned, removed, err := h.redis.CleanupExpiredConcurrency(ctx)
	if err != nil {
		logger.Error("Failed to cleanup expired concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cleaned": cleaned, "removed": removed})
}

// ForceClearConcurrency 强制清除并发
func (h *ConcurrencyHandler) ForceClearConcurrency(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	cleared, err := h.redis.ForceClearConcurrency(ctx, apiKeyID)
	if err != nil {
		logger.Error("Failed to force clear concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cleared": cleared})
}

// ForceClearAllConcurrency 强制清除所有并发
func (h *ConcurrencyHandler) ForceClearAllConcurrency(c *gin.Context) {
	ctx := c.Request.Context()
	cleaned, removed, err := h.redis.ForceClearAllConcurrency(ctx)
	if err != nil {
		logger.Error("Failed to force clear all concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cleaned": cleaned, "removed": removed})
}

// IncrConsoleAccountConcurrency Console 账户并发增加
func (h *ConcurrencyHandler) IncrConsoleAccountConcurrency(c *gin.Context) {
	var req struct {
		AccountID    string `json:"accountId"`
		RequestID    string `json:"requestId"`
		LeaseSeconds int    `json:"leaseSeconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LeaseSeconds == 0 {
		req.LeaseSeconds = 600
	}

	ctx := c.Request.Context()
	count, err := h.redis.IncrConsoleAccountConcurrency(ctx, req.AccountID, req.RequestID, req.LeaseSeconds)
	if err != nil {
		logger.Error("Failed to incr console account concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// DecrConsoleAccountConcurrency Console 账户并发减少
func (h *ConcurrencyHandler) DecrConsoleAccountConcurrency(c *gin.Context) {
	var req struct {
		AccountID string `json:"accountId"`
		RequestID string `json:"requestId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.DecrConsoleAccountConcurrency(ctx, req.AccountID, req.RequestID)
	if err != nil {
		logger.Error("Failed to decr console account concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// GetConsoleAccountConcurrency 获取 Console 账户并发
func (h *ConcurrencyHandler) GetConsoleAccountConcurrency(c *gin.Context) {
	accountID := c.Param("accountId")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "accountId is required"})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.GetConsoleAccountConcurrency(ctx, accountID)
	if err != nil {
		logger.Error("Failed to get console account concurrency", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// IncrConcurrencyQueue 增加并发队列计数
func (h *ConcurrencyHandler) IncrConcurrencyQueue(c *gin.Context) {
	var req struct {
		APIKeyID  string `json:"apiKeyId"`
		TimeoutMs int64  `json:"timeoutMs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TimeoutMs == 0 {
		req.TimeoutMs = 10000 // 默认 10 秒
	}

	ctx := c.Request.Context()
	count, err := h.redis.IncrConcurrencyQueue(ctx, req.APIKeyID, req.TimeoutMs)
	if err != nil {
		logger.Error("Failed to incr concurrency queue", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// DecrConcurrencyQueue 减少并发队列计数
func (h *ConcurrencyHandler) DecrConcurrencyQueue(c *gin.Context) {
	var req struct {
		APIKeyID string `json:"apiKeyId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.DecrConcurrencyQueue(ctx, req.APIKeyID)
	if err != nil {
		logger.Error("Failed to decr concurrency queue", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// GetConcurrencyQueueCount 获取并发队列计数
func (h *ConcurrencyHandler) GetConcurrencyQueueCount(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	count, err := h.redis.GetConcurrencyQueueCount(ctx, apiKeyID)
	if err != nil {
		logger.Error("Failed to get concurrency queue count", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// ClearConcurrencyQueue 清除并发队列
func (h *ConcurrencyHandler) ClearConcurrencyQueue(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.ClearConcurrencyQueue(ctx, apiKeyID); err != nil {
		logger.Error("Failed to clear concurrency queue", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ClearAllConcurrencyQueues 清除所有并发队列
func (h *ConcurrencyHandler) ClearAllConcurrencyQueues(c *gin.Context) {
	ctx := c.Request.Context()
	cleared, err := h.redis.ClearAllConcurrencyQueues(ctx)
	if err != nil {
		logger.Error("Failed to clear all concurrency queues", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cleared": cleared})
}

// GetQueueStats 获取队列统计
func (h *ConcurrencyHandler) GetQueueStats(c *gin.Context) {
	apiKeyID := c.Param("apiKeyId")
	if apiKeyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKeyId is required"})
		return
	}

	ctx := c.Request.Context()
	stats, err := h.redis.GetQueueStats(ctx, apiKeyID)
	if err != nil {
		logger.Error("Failed to get queue stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetGlobalQueueStats 获取全局队列统计
func (h *ConcurrencyHandler) GetGlobalQueueStats(c *gin.Context) {
	includePerKey := c.Query("includePerKey") == "true"

	ctx := c.Request.Context()
	stats, err := h.redis.GetGlobalQueueStats(ctx, includePerKey)
	if err != nil {
		logger.Error("Failed to get global queue stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// CheckQueueHealth 检查队列健康状态
func (h *ConcurrencyHandler) CheckQueueHealth(c *gin.Context) {
	threshold, _ := strconv.ParseFloat(c.DefaultQuery("threshold", "0.8"), 64)
	timeoutMs, _ := strconv.ParseInt(c.DefaultQuery("timeoutMs", "10000"), 10, 64)

	ctx := c.Request.Context()
	healthy, p90, err := h.redis.CheckQueueHealth(ctx, threshold, timeoutMs)
	if err != nil {
		logger.Error("Failed to check queue health", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"healthy": healthy, "p90WaitTime": p90})
}

// RecordWaitTime 记录等待时间
func (h *ConcurrencyHandler) RecordWaitTime(c *gin.Context) {
	var req struct {
		APIKeyID string `json:"apiKeyId"`
		WaitMs   int64  `json:"waitMs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.RecordWaitTime(ctx, req.APIKeyID, req.WaitMs); err != nil {
		logger.Error("Failed to record wait time", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
