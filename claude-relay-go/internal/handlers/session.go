package handlers

import (
	"net/http"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SessionHandler 会话处理器
type SessionHandler struct {
	redis *redis.Client
}

// NewSessionHandler 创建会话处理器
func NewSessionHandler(redisClient *redis.Client) *SessionHandler {
	return &SessionHandler{redis: redisClient}
}

// SetSession 设置会话
func (h *SessionHandler) SetSession(c *gin.Context) {
	var req struct {
		Token   string         `json:"token"`
		Session *redis.Session `json:"session"`
		TTL     int64          `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = 24 * time.Hour // 默认 24 小时
	}

	ctx := c.Request.Context()
	if err := h.redis.SetSession(ctx, req.Token, req.Session, ttl); err != nil {
		logger.Error("Failed to set session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetSession 获取会话
func (h *SessionHandler) GetSession(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	ctx := c.Request.Context()
	session, err := h.redis.GetSession(ctx, token)
	if err != nil {
		logger.Error("Failed to get session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// DeleteSession 删除会话
func (h *SessionHandler) DeleteSession(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.DeleteSession(ctx, token); err != nil {
		logger.Error("Failed to delete session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RefreshSession 刷新会话
func (h *SessionHandler) RefreshSession(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
		TTL   int64  `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = 24 * time.Hour
	}

	ctx := c.Request.Context()
	if err := h.redis.RefreshSession(ctx, req.Token, ttl); err != nil {
		logger.Error("Failed to refresh session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetOAuthSession 设置 OAuth 会话
func (h *SessionHandler) SetOAuthSession(c *gin.Context) {
	var req struct {
		State   string              `json:"state"`
		Session *redis.OAuthSession `json:"session"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.State == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.SetOAuthSession(ctx, req.State, req.Session); err != nil {
		logger.Error("Failed to set OAuth session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetOAuthSession 获取 OAuth 会话
func (h *SessionHandler) GetOAuthSession(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}

	ctx := c.Request.Context()
	session, err := h.redis.GetOAuthSession(ctx, state)
	if err != nil {
		logger.Error("Failed to get OAuth session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth session not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// ConsumeOAuthSession 消费 OAuth 会话（获取后删除）
func (h *SessionHandler) ConsumeOAuthSession(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}

	ctx := c.Request.Context()
	session, err := h.redis.ConsumeOAuthSession(ctx, state)
	if err != nil {
		logger.Error("Failed to consume OAuth session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth session not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// DeleteOAuthSession 删除 OAuth 会话
func (h *SessionHandler) DeleteOAuthSession(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.DeleteOAuthSession(ctx, state); err != nil {
		logger.Error("Failed to delete OAuth session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetStickySession 设置粘性会话
func (h *SessionHandler) SetStickySession(c *gin.Context) {
	var req struct {
		SessionHash string `json:"sessionHash"`
		AccountID   string `json:"accountId"`
		AccountType string `json:"accountType"`
		TTL         int64  `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour // 默认 1 小时
	}

	ctx := c.Request.Context()
	if err := h.redis.SetStickySession(ctx, req.SessionHash, req.AccountID, req.AccountType, ttl); err != nil {
		logger.Error("Failed to set sticky session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetStickySession 获取粘性会话
func (h *SessionHandler) GetStickySession(c *gin.Context) {
	sessionHash := c.Param("sessionHash")
	if sessionHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionHash is required"})
		return
	}

	ctx := c.Request.Context()
	session, err := h.redis.GetStickySession(ctx, sessionHash)
	if err != nil {
		logger.Error("Failed to get sticky session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sticky session not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// GetOrCreateStickySession 获取或创建粘性会话
func (h *SessionHandler) GetOrCreateStickySession(c *gin.Context) {
	var req struct {
		SessionHash string `json:"sessionHash"`
		AccountID   string `json:"accountId"`
		AccountType string `json:"accountType"`
		TTL         int64  `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}

	ctx := c.Request.Context()
	session, created, err := h.redis.GetOrCreateStickySession(ctx, req.SessionHash, req.AccountID, req.AccountType, ttl)
	if err != nil {
		logger.Error("Failed to get or create sticky session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"session": session, "created": created})
}

// DeleteStickySession 删除粘性会话
func (h *SessionHandler) DeleteStickySession(c *gin.Context) {
	sessionHash := c.Param("sessionHash")
	if sessionHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionHash is required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.DeleteStickySession(ctx, sessionHash); err != nil {
		logger.Error("Failed to delete sticky session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RenewStickySession 续期粘性会话
func (h *SessionHandler) RenewStickySession(c *gin.Context) {
	var req struct {
		SessionHash string `json:"sessionHash"`
		TTL         int64  `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}

	ctx := c.Request.Context()
	if err := h.redis.RenewStickySession(ctx, req.SessionHash, ttl); err != nil {
		logger.Error("Failed to renew sticky session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetAllStickySessions 获取所有粘性会话
func (h *SessionHandler) GetAllStickySessions(c *gin.Context) {
	ctx := c.Request.Context()
	sessions, err := h.redis.GetAllStickySessions(ctx)
	if err != nil {
		logger.Error("Failed to get all sticky sessions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions, "total": len(sessions)})
}

// CleanupExpiredStickySessions 清理过期粘性会话
func (h *SessionHandler) CleanupExpiredStickySessions(c *gin.Context) {
	ctx := c.Request.Context()
	cleaned, err := h.redis.CleanupExpiredStickySessions(ctx)
	if err != nil {
		logger.Error("Failed to cleanup expired sticky sessions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cleaned": cleaned})
}
