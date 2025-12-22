package handlers

import (
	"net/http"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AccountHandler 账户处理器
type AccountHandler struct {
	redis *redis.Client
}

// NewAccountHandler 创建账户处理器
func NewAccountHandler(redisClient *redis.Client) *AccountHandler {
	return &AccountHandler{redis: redisClient}
}

// GetAccount 获取账户
func (h *AccountHandler) GetAccount(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	ctx := c.Request.Context()
	account, err := h.redis.GetAccount(ctx, redis.AccountType(accountType), accountID)
	if err != nil {
		logger.Error("Failed to get account", zap.String("type", accountType), zap.String("id", accountID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	c.JSON(http.StatusOK, account)
}

// GetAccountRaw 获取账户原始数据
func (h *AccountHandler) GetAccountRaw(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	ctx := c.Request.Context()
	data, err := h.redis.GetAccountRaw(ctx, redis.AccountType(accountType), accountID)
	if err != nil {
		logger.Error("Failed to get account raw", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	c.Data(http.StatusOK, "application/json", data)
}

// GetAllAccounts 获取所有账户
func (h *AccountHandler) GetAllAccounts(c *gin.Context) {
	accountType := c.Param("type")
	if accountType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}

	ctx := c.Request.Context()
	accounts, err := h.redis.GetAllAccounts(ctx, redis.AccountType(accountType))
	if err != nil {
		logger.Error("Failed to get all accounts", zap.String("type", accountType), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"accounts": accounts, "total": len(accounts)})
}

// GetActiveAccounts 获取活跃账户
func (h *AccountHandler) GetActiveAccounts(c *gin.Context) {
	accountType := c.Param("type")
	if accountType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}

	ctx := c.Request.Context()
	accounts, err := h.redis.GetActiveAccounts(ctx, redis.AccountType(accountType))
	if err != nil {
		logger.Error("Failed to get active accounts", zap.String("type", accountType), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"accounts": accounts, "total": len(accounts)})
}

// SetAccount 设置账户
func (h *AccountHandler) SetAccount(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.SetAccount(ctx, redis.AccountType(accountType), accountID, data); err != nil {
		logger.Error("Failed to set account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteAccount 删除账户
func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.DeleteAccount(ctx, redis.AccountType(accountType), accountID); err != nil {
		logger.Error("Failed to delete account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdateAccountStatus 更新账户状态
func (h *AccountHandler) UpdateAccountStatus(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.UpdateAccountStatus(ctx, redis.AccountType(accountType), accountID, req.Status); err != nil {
		logger.Error("Failed to update account status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetAccountError 设置账户错误
func (h *AccountHandler) SetAccountError(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	var req struct {
		ErrorMsg string `json:"errorMsg"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.SetAccountError(ctx, redis.AccountType(accountType), accountID, req.ErrorMsg); err != nil {
		logger.Error("Failed to set account error", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ClearAccountError 清除账户错误
func (h *AccountHandler) ClearAccountError(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.ClearAccountError(ctx, redis.AccountType(accountType), accountID); err != nil {
		logger.Error("Failed to clear account error", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetAccountOverloaded 设置账户过载
func (h *AccountHandler) SetAccountOverloaded(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	var req struct {
		Duration int64 `json:"duration"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	duration := time.Duration(req.Duration) * time.Second
	if duration == 0 {
		duration = 5 * time.Minute // 默认 5 分钟
	}

	ctx := c.Request.Context()
	if err := h.redis.SetAccountOverloaded(ctx, redis.AccountType(accountType), accountID, duration); err != nil {
		logger.Error("Failed to set account overloaded", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ClearAccountOverloaded 清除账户过载
func (h *AccountHandler) ClearAccountOverloaded(c *gin.Context) {
	accountType := c.Param("type")
	accountID := c.Param("id")

	if accountType == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and id are required"})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.ClearAccountOverloaded(ctx, redis.AccountType(accountType), accountID); err != nil {
		logger.Error("Failed to clear account overloaded", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetAccountCost 获取账户成本
func (h *AccountHandler) GetAccountCost(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx := c.Request.Context()
	cost, err := h.redis.GetAccountCost(ctx, accountID)
	if err != nil {
		logger.Error("Failed to get account cost", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cost": cost})
}

// GetAccountDailyCost 获取账户每日成本
func (h *AccountHandler) GetAccountDailyCost(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	dateStr := c.Query("date")
	var date time.Time
	if dateStr != "" {
		var err error
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
			return
		}
	} else {
		date = time.Now()
	}

	ctx := c.Request.Context()
	cost, err := h.redis.GetAccountDailyCost(ctx, accountID, date)
	if err != nil {
		logger.Error("Failed to get account daily cost", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cost": cost, "date": date.Format("2006-01-02")})
}

// IncrementAccountCost 增加账户成本
func (h *AccountHandler) IncrementAccountCost(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
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
	if err := h.redis.IncrementAccountCost(ctx, accountID, req.Amount); err != nil {
		logger.Error("Failed to increment account cost", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// IncrementAccountUsage 增加账户使用量
func (h *AccountHandler) IncrementAccountUsage(c *gin.Context) {
	var params redis.TokenUsageParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.redis.IncrementAccountUsage(ctx, params); err != nil {
		logger.Error("Failed to increment account usage", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetSessionWindowUsage 获取会话窗口使用量
func (h *AccountHandler) GetSessionWindowUsage(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	windowHours := 1
	if h := c.Query("windowHours"); h != "" {
		if v, err := time.ParseDuration(h + "h"); err == nil {
			windowHours = int(v.Hours())
		}
	}

	ctx := c.Request.Context()
	usage, err := h.redis.GetSessionWindowUsage(ctx, accountID, windowHours)
	if err != nil {
		logger.Error("Failed to get session window usage", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, usage)
}

// SetAccountLock 设置账户锁
func (h *AccountHandler) SetAccountLock(c *gin.Context) {
	var req struct {
		LockKey   string `json:"lockKey"`
		LockValue string `json:"lockValue"`
		TTL       int64  `json:"ttl"` // 秒
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	if ttl == 0 {
		ttl = 30 * time.Second
	}

	ctx := c.Request.Context()
	acquired, err := h.redis.SetAccountLock(ctx, req.LockKey, req.LockValue, ttl)
	if err != nil {
		logger.Error("Failed to set account lock", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"acquired": acquired})
}

// ReleaseAccountLock 释放账户锁
func (h *AccountHandler) ReleaseAccountLock(c *gin.Context) {
	var req struct {
		LockKey   string `json:"lockKey"`
		LockValue string `json:"lockValue"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	released, err := h.redis.ReleaseAccountLock(ctx, req.LockKey, req.LockValue)
	if err != nil {
		logger.Error("Failed to release account lock", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"released": released})
}
