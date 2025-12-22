package handlers

import (
	"net/http"
	"strconv"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// APIKeyHandler API Key 处理器
type APIKeyHandler struct {
	redis *redis.Client
}

// NewAPIKeyHandler 创建 API Key 处理器
func NewAPIKeyHandler(redisClient *redis.Client) *APIKeyHandler {
	return &APIKeyHandler{redis: redisClient}
}

// GetAPIKey 获取单个 API Key
func (h *APIKeyHandler) GetAPIKey(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	ctx := c.Request.Context()
	apiKey, err := h.redis.GetAPIKey(ctx, keyID)
	if err != nil {
		logger.Error("Failed to get API key", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if apiKey == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	c.JSON(http.StatusOK, apiKey)
}

// GetAPIKeyByHash 通过哈希获取 API Key
func (h *APIKeyHandler) GetAPIKeyByHash(c *gin.Context) {
	hash := c.Param("hash")
	if hash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hash is required"})
		return
	}

	ctx := c.Request.Context()
	apiKey, err := h.redis.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		logger.Error("Failed to get API key by hash", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if apiKey == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	c.JSON(http.StatusOK, apiKey)
}

// GetAllAPIKeys 获取所有 API Key
func (h *APIKeyHandler) GetAllAPIKeys(c *gin.Context) {
	includeDeleted := c.Query("includeDeleted") == "true"

	ctx := c.Request.Context()
	keys, err := h.redis.GetAllAPIKeys(ctx, includeDeleted)
	if err != nil {
		logger.Error("Failed to get all API keys", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"keys": keys, "total": len(keys)})
}

// GetAPIKeysPaginated 分页获取 API Key
func (h *APIKeyHandler) GetAPIKeysPaginated(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	sortBy := c.DefaultQuery("sortBy", "createdAt")
	order := c.DefaultQuery("order", "desc")
	search := c.Query("search")
	status := c.Query("status")
	excludeDeleted := c.Query("excludeDeleted") != "false"

	// 处理激活状态过滤
	var isActive *bool
	if status == "active" {
		t := true
		isActive = &t
	} else if status == "inactive" {
		f := false
		isActive = &f
	}

	opts := redis.APIKeyQueryOptions{
		Page:           page,
		PageSize:       pageSize,
		SortBy:         sortBy,
		SortOrder:      order,
		Search:         search,
		IsActive:       isActive,
		IncludeDeleted: !excludeDeleted,
	}

	ctx := c.Request.Context()
	result, err := h.redis.GetAPIKeysPaginated(ctx, opts)
	if err != nil {
		logger.Error("Failed to get paginated API keys", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// SetAPIKey 创建或更新 API Key
func (h *APIKeyHandler) SetAPIKey(c *gin.Context) {
	var apiKey redis.APIKey
	if err := c.ShouldBindJSON(&apiKey); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if apiKey.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.SetAPIKey(ctx, &apiKey); err != nil {
		logger.Error("Failed to set API key", zap.String("keyID", apiKey.ID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": apiKey.ID})
}

// UpdateAPIKeyFields 更新 API Key 字段
func (h *APIKeyHandler) UpdateAPIKeyFields(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.UpdateAPIKeyFields(ctx, keyID, updates); err != nil {
		logger.Error("Failed to update API key fields", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteAPIKey 删除 API Key (软删除)
func (h *APIKeyHandler) DeleteAPIKey(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.DeleteAPIKey(ctx, keyID); err != nil {
		logger.Error("Failed to delete API key", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// HardDeleteAPIKey 硬删除 API Key
func (h *APIKeyHandler) HardDeleteAPIKey(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.HardDeleteAPIKey(ctx, keyID); err != nil {
		logger.Error("Failed to hard delete API key", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetAPIKeyStats 获取 API Key 统计
func (h *APIKeyHandler) GetAPIKeyStats(c *gin.Context) {
	ctx := c.Request.Context()
	stats, err := h.redis.GetAPIKeyStats(ctx)
	if err != nil {
		logger.Error("Failed to get API key stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// IncrementDailyCost 增加每日成本
func (h *APIKeyHandler) IncrementDailyCost(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.IncrementDailyCost(ctx, keyID, req.Amount); err != nil {
		logger.Error("Failed to increment daily cost", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetDailyCost 获取每日成本
func (h *APIKeyHandler) GetDailyCost(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	ctx := c.Request.Context()
	cost, err := h.redis.GetDailyCost(ctx, keyID)
	if err != nil {
		logger.Error("Failed to get daily cost", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cost": cost})
}

// GetCostStats 获取成本统计
func (h *APIKeyHandler) GetCostStats(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))

	ctx := c.Request.Context()
	stats, err := h.redis.GetCostStats(ctx, keyID, days)
	if err != nil {
		logger.Error("Failed to get cost stats", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// IncrementTokenUsage 增加 Token 使用量
func (h *APIKeyHandler) IncrementTokenUsage(c *gin.Context) {
	var params redis.TokenUsageParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.IncrementTokenUsage(ctx, params); err != nil {
		logger.Error("Failed to increment token usage", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetUsageStats 获取使用统计
func (h *APIKeyHandler) GetUsageStats(c *gin.Context) {
	keyID := c.Param("id")
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keyID is required"})
		return
	}

	ctx := c.Request.Context()
	stats, err := h.redis.GetUsageStats(ctx, keyID)
	if err != nil {
		logger.Error("Failed to get usage stats", zap.String("keyID", keyID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}
