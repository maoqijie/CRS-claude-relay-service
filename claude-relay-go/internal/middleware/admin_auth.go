package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Admin 管理员信息
type Admin struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	LastLogin time.Time `json:"lastLogin,omitempty"`
}

// AdminClaims JWT claims for admin
type AdminClaims struct {
	AdminID  string `json:"adminId"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// AdminAuthMiddleware 管理员认证中间件
type AdminAuthMiddleware struct {
	redis     *redis.Client
	jwtSecret []byte
}

// NewAdminAuthMiddleware 创建管理员认证中间件
func NewAdminAuthMiddleware(redisClient *redis.Client) (*AdminAuthMiddleware, error) {
	secret, err := requiredJWTSecret()
	if err != nil {
		return nil, err
	}
	return &AdminAuthMiddleware{
		redis:     redisClient,
		jwtSecret: secret,
	}, nil
}

// Authenticate 管理员认证中间件
func (m *AdminAuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 提取 token
		token := m.extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Missing authentication token",
				"code":  "missing_token",
			})
			return
		}

		// 2. 验证 JWT token
		claims, err := m.validateToken(token)
		if err != nil {
			logger.Warn("Admin token validation failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
				"code":  "invalid_token",
			})
			return
		}

		// 3. 验证 session 是否有效
		if !m.validateSession(c.Request.Context(), claims.AdminID, token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Session expired or invalid",
				"code":  "session_invalid",
			})
			return
		}

		// 4. 获取管理员信息
		admin, err := m.getAdmin(c.Request.Context(), claims.AdminID)
		if err != nil || admin == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Admin not found",
				"code":  "admin_not_found",
			})
			return
		}

		// 5. 设置上下文
		c.Set("admin", admin)
		c.Set("adminId", admin.ID)
		c.Set("adminUsername", admin.Username)

		c.Next()
	}
}

// Login 管理员登录
func (m *AdminAuthMiddleware) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
			"code":  "invalid_request",
		})
		return
	}

	// 验证凭据
	admin, err := m.verifyCredentials(c.Request.Context(), req.Username, req.Password)
	if err != nil || admin == nil {
		logger.Warn("Admin login failed",
			zap.String("username", req.Username),
			zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid username or password",
			"code":  "invalid_credentials",
		})
		return
	}

	// 生成 JWT token
	token, expiresAt, err := m.generateToken(admin)
	if err != nil {
		logger.Error("Failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate token",
			"code":  "token_error",
		})
		return
	}

	// 保存 session
	if err := m.saveSession(c.Request.Context(), admin.ID, token, expiresAt); err != nil {
		logger.Error("Failed to save session", zap.Error(err))
	}

	// 更新最后登录时间
	go m.updateLastLogin(context.Background(), admin.ID)

	logger.Info("Admin logged in",
		zap.String("username", admin.Username),
		zap.String("adminId", admin.ID))

	c.JSON(http.StatusOK, gin.H{
		"token":     token,
		"expiresAt": expiresAt.Format(time.RFC3339),
		"admin": gin.H{
			"id":       admin.ID,
			"username": admin.Username,
		},
	})
}

// Logout 管理员登出
func (m *AdminAuthMiddleware) Logout(c *gin.Context) {
	token := m.extractToken(c)
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
		return
	}

	claims, err := m.validateToken(token)
	if err == nil && claims != nil {
		// 删除 session
		m.deleteSession(c.Request.Context(), claims.AdminID)
		logger.Info("Admin logged out", zap.String("adminId", claims.AdminID))
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// extractToken 从请求中提取 token
func (m *AdminAuthMiddleware) extractToken(c *gin.Context) string {
	// 从 Authorization header 提取
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}

	// 从 cookie 提取
	if token, err := c.Cookie("admin_token"); err == nil && token != "" {
		return token
	}

	// 从 query parameter 提取
	if token := c.Query("token"); token != "" {
		return token
	}

	return ""
}

// validateToken 验证 JWT token
func (m *AdminAuthMiddleware) validateToken(tokenString string) (*AdminClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AdminClaims{}, func(token *jwt.Token) (interface{}, error) {
		return m.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*AdminClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrTokenInvalidClaims
}

// generateToken 生成 JWT token
func (m *AdminAuthMiddleware) generateToken(admin *Admin) (string, time.Time, error) {
	expiresAt := time.Now().Add(24 * time.Hour) // 24 小时过期

	claims := &AdminClaims{
		AdminID:  admin.ID,
		Username: admin.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "claude-relay-go",
			Subject:   admin.ID,
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}

// verifyCredentials 验证管理员凭据
func (m *AdminAuthMiddleware) verifyCredentials(ctx context.Context, username, password string) (*Admin, error) {
	// 从 Redis 获取管理员凭据
	credentialsKey := "admin_credentials"
	data, err := m.redis.Get(ctx, credentialsKey)
	if err != nil {
		return nil, err
	}

	var credentials struct {
		Username     string `json:"username"`
		PasswordHash string `json:"passwordHash"`
	}

	if err := json.Unmarshal([]byte(data), &credentials); err != nil {
		return nil, err
	}

	// 验证用户名
	if credentials.Username != username {
		return nil, nil
	}

	// 验证密码哈希
	passwordHash := hashPassword(password)
	if credentials.PasswordHash != passwordHash {
		return nil, nil
	}

	// 获取管理员信息
	return m.getAdminByUsername(ctx, username)
}

// getAdmin 获取管理员信息
func (m *AdminAuthMiddleware) getAdmin(ctx context.Context, adminID string) (*Admin, error) {
	key := "admin:" + adminID
	data, err := m.redis.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	admin := &Admin{
		ID:       adminID,
		Username: data["username"],
		Email:    data["email"],
	}

	if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
		admin.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, data["lastLogin"]); err == nil {
		admin.LastLogin = t
	}

	return admin, nil
}

// getAdminByUsername 通过用户名获取管理员
func (m *AdminAuthMiddleware) getAdminByUsername(ctx context.Context, username string) (*Admin, error) {
	// 从用户名映射获取 ID
	key := "admin_username:" + username
	adminID, err := m.redis.Get(ctx, key)
	if err != nil {
		// 如果没有映射，创建默认管理员
		return &Admin{
			ID:        "admin_" + username,
			Username:  username,
			CreatedAt: time.Now(),
		}, nil
	}

	return m.getAdmin(ctx, adminID)
}

// validateSession 验证 session
func (m *AdminAuthMiddleware) validateSession(ctx context.Context, adminID, token string) bool {
	key := "admin_session:" + adminID
	storedToken, err := m.redis.Get(ctx, key)
	if err != nil {
		return false
	}
	return storedToken == token
}

// saveSession 保存 session
func (m *AdminAuthMiddleware) saveSession(ctx context.Context, adminID, token string, expiresAt time.Time) error {
	key := "admin_session:" + adminID
	ttl := time.Until(expiresAt)
	return m.redis.Set(ctx, key, token, ttl)
}

// deleteSession 删除 session
func (m *AdminAuthMiddleware) deleteSession(ctx context.Context, adminID string) {
	key := "admin_session:" + adminID
	m.redis.Del(ctx, key)
}

// updateLastLogin 更新最后登录时间
func (m *AdminAuthMiddleware) updateLastLogin(ctx context.Context, adminID string) {
	key := "admin:" + adminID
	m.redis.HSet(ctx, key, "lastLogin", time.Now().Format(time.RFC3339))
}

// hashPassword 计算密码哈希
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// GetAdminFromContext 从上下文获取管理员信息
func GetAdminFromContext(c *gin.Context) *Admin {
	if admin, exists := c.Get("admin"); exists {
		if a, ok := admin.(*Admin); ok {
			return a
		}
	}
	return nil
}

// GetAdminIDFromContext 从上下文获取管理员 ID
func GetAdminIDFromContext(c *gin.Context) string {
	if id, exists := c.Get("adminId"); exists {
		if adminID, ok := id.(string); ok {
			return adminID
		}
	}
	return ""
}
