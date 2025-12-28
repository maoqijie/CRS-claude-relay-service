package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// User 用户信息
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	LastLogin time.Time `json:"lastLogin,omitempty"`
	IsActive  bool      `json:"isActive"`
}

// UserClaims JWT claims for user
type UserClaims struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	jwt.RegisteredClaims
}

// UserAuthMiddleware 用户认证中间件
type UserAuthMiddleware struct {
	redis     *redis.Client
	jwtSecret []byte
	enabled   bool
}

// NewUserAuthMiddleware 创建用户认证中间件
func NewUserAuthMiddleware(redisClient *redis.Client) (*UserAuthMiddleware, error) {
	secret, err := requiredJWTSecret()
	if err != nil {
		return nil, err
	}

	enabled := config.Cfg.UserManagement.Enabled

	return &UserAuthMiddleware{
		redis:     redisClient,
		jwtSecret: secret,
		enabled:   enabled,
	}, nil
}

// Authenticate 用户认证中间件
func (m *UserAuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果用户管理未启用，跳过认证
		if !m.enabled {
			c.Next()
			return
		}

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
			logger.Warn("User token validation failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
				"code":  "invalid_token",
			})
			return
		}

		// 3. 验证 session 是否有效
		if !m.validateSession(c.Request.Context(), claims.UserID, token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Session expired or invalid",
				"code":  "session_invalid",
			})
			return
		}

		// 4. 获取用户信息
		user, err := m.getUser(c.Request.Context(), claims.UserID)
		if err != nil || user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "User not found",
				"code":  "user_not_found",
			})
			return
		}

		// 5. 检查用户是否激活
		if !user.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "User account is disabled",
				"code":  "user_disabled",
			})
			return
		}

		// 6. 设置上下文
		c.Set("user", user)
		c.Set("userId", user.ID)
		c.Set("userEmail", user.Email)

		c.Next()
	}
}

// AuthenticateOptional 可选用户认证（不强制）
func (m *UserAuthMiddleware) AuthenticateOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.enabled {
			c.Next()
			return
		}

		token := m.extractToken(c)
		if token == "" {
			c.Next()
			return
		}

		claims, err := m.validateToken(token)
		if err != nil {
			c.Next()
			return
		}

		user, _ := m.getUser(c.Request.Context(), claims.UserID)
		if user != nil && user.IsActive {
			c.Set("user", user)
			c.Set("userId", user.ID)
			c.Set("userEmail", user.Email)
		}

		c.Next()
	}
}

// Register 用户注册
func (m *UserAuthMiddleware) Register(c *gin.Context) {
	if !m.enabled {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "User management is not enabled",
			"code":  "feature_disabled",
		})
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
		Username string `json:"username"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
			"code":  "invalid_request",
		})
		return
	}

	// 检查邮箱是否已存在
	exists, _ := m.emailExists(c.Request.Context(), req.Email)
	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Email already registered",
			"code":  "email_exists",
		})
		return
	}

	// 创建用户
	user := &User{
		ID:        uuid.New().String(),
		Email:     req.Email,
		Username:  req.Username,
		CreatedAt: time.Now(),
		IsActive:  true,
	}

	// 保存用户
	if err := m.saveUser(c.Request.Context(), user, req.Password); err != nil {
		logger.Error("Failed to create user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create user",
			"code":  "create_error",
		})
		return
	}

	logger.Info("User registered",
		zap.String("userId", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully",
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

// Login 用户登录
func (m *UserAuthMiddleware) Login(c *gin.Context) {
	if !m.enabled {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "User management is not enabled",
			"code":  "feature_disabled",
		})
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required"`
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
	user, err := m.verifyCredentials(c.Request.Context(), req.Email, req.Password)
	if err != nil || user == nil {
		logger.Warn("User login failed", zap.String("email", req.Email))
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid email or password",
			"code":  "invalid_credentials",
		})
		return
	}

	// 检查用户是否激活
	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "User account is disabled",
			"code":  "user_disabled",
		})
		return
	}

	// 生成 JWT token
	token, expiresAt, err := m.generateToken(user)
	if err != nil {
		logger.Error("Failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate token",
			"code":  "token_error",
		})
		return
	}

	// 保存 session
	if err := m.saveSession(c.Request.Context(), user.ID, token, expiresAt); err != nil {
		logger.Error("Failed to save session", zap.Error(err))
	}

	// 更新最后登录时间
	go m.updateLastLogin(context.Background(), user.ID)

	logger.Info("User logged in",
		zap.String("userId", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"token":     token,
		"expiresAt": expiresAt.Format(time.RFC3339),
		"user": gin.H{
			"id":       user.ID,
			"email":    user.Email,
			"username": user.Username,
		},
	})
}

// Logout 用户登出
func (m *UserAuthMiddleware) Logout(c *gin.Context) {
	token := m.extractToken(c)
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
		return
	}

	claims, err := m.validateToken(token)
	if err == nil && claims != nil {
		m.deleteSession(c.Request.Context(), claims.UserID)
		logger.Info("User logged out", zap.String("userId", claims.UserID))
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// GetProfile 获取用户资料
func (m *UserAuthMiddleware) GetProfile(c *gin.Context) {
	user := GetUserFromContext(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Not authenticated",
			"code":  "not_authenticated",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":        user.ID,
			"email":     user.Email,
			"username":  user.Username,
			"createdAt": user.CreatedAt.Format(time.RFC3339),
			"lastLogin": user.LastLogin.Format(time.RFC3339),
		},
	})
}

// extractToken 从请求中提取 token
func (m *UserAuthMiddleware) extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}

	if token, err := c.Cookie("user_token"); err == nil && token != "" {
		return token
	}

	return ""
}

// validateToken 验证 JWT token
func (m *UserAuthMiddleware) validateToken(tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		return m.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrTokenInvalidClaims
}

// generateToken 生成 JWT token
func (m *UserAuthMiddleware) generateToken(user *User) (string, time.Time, error) {
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 天过期

	claims := &UserClaims{
		UserID:   user.ID,
		Email:    user.Email,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "claude-relay-go",
			Subject:   user.ID,
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

// verifyCredentials 验证用户凭据
func (m *UserAuthMiddleware) verifyCredentials(ctx context.Context, email, password string) (*User, error) {
	// 获取用户 ID
	userID, err := m.redis.Get(ctx, "user_email:"+email)
	if err != nil {
		return nil, err
	}

	// 获取用户信息
	user, err := m.getUser(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}

	// 获取密码哈希
	storedHash, err := m.redis.Get(ctx, "user_password:"+userID)
	if err != nil {
		return nil, err
	}

	// 验证密码
	passwordHash := hashUserPassword(password)
	if storedHash != passwordHash {
		return nil, nil
	}

	return user, nil
}

// getUser 获取用户信息
func (m *UserAuthMiddleware) getUser(ctx context.Context, userID string) (*User, error) {
	key := "user:" + userID
	data, err := m.redis.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	user := &User{
		ID:       userID,
		Email:    data["email"],
		Username: data["username"],
		IsActive: data["isActive"] == "true" || data["isActive"] == "1",
	}

	if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
		user.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, data["lastLogin"]); err == nil {
		user.LastLogin = t
	}

	return user, nil
}

// emailExists 检查邮箱是否存在
func (m *UserAuthMiddleware) emailExists(ctx context.Context, email string) (bool, error) {
	exists, err := m.redis.Exists(ctx, "user_email:"+email)
	return exists, err
}

// saveUser 保存用户
func (m *UserAuthMiddleware) saveUser(ctx context.Context, user *User, password string) error {
	// 保存用户信息
	userKey := "user:" + user.ID
	err := m.redis.HSet(ctx, userKey,
		"email", user.Email,
		"username", user.Username,
		"createdAt", user.CreatedAt.Format(time.RFC3339),
		"isActive", "true",
	)
	if err != nil {
		return err
	}

	// 保存邮箱映射
	err = m.redis.Set(ctx, "user_email:"+user.Email, user.ID, 0)
	if err != nil {
		return err
	}

	// 保存密码哈希
	passwordHash := hashUserPassword(password)
	err = m.redis.Set(ctx, "user_password:"+user.ID, passwordHash, 0)
	if err != nil {
		return err
	}

	return nil
}

// validateSession 验证 session
func (m *UserAuthMiddleware) validateSession(ctx context.Context, userID, token string) bool {
	key := "user_session:" + userID
	storedToken, err := m.redis.Get(ctx, key)
	if err != nil {
		return false
	}
	return storedToken == token
}

// saveSession 保存 session
func (m *UserAuthMiddleware) saveSession(ctx context.Context, userID, token string, expiresAt time.Time) error {
	key := "user_session:" + userID
	ttl := time.Until(expiresAt)
	return m.redis.Set(ctx, key, token, ttl)
}

// deleteSession 删除 session
func (m *UserAuthMiddleware) deleteSession(ctx context.Context, userID string) {
	key := "user_session:" + userID
	m.redis.Del(ctx, key)
}

// updateLastLogin 更新最后登录时间
func (m *UserAuthMiddleware) updateLastLogin(ctx context.Context, userID string) {
	key := "user:" + userID
	m.redis.HSet(ctx, key, "lastLogin", time.Now().Format(time.RFC3339))
}

// hashUserPassword 计算用户密码哈希
func hashUserPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// GetUserFromContext 从上下文获取用户信息
func GetUserFromContext(c *gin.Context) *User {
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*User); ok {
			return u
		}
	}
	return nil
}

// GetUserIDFromContext 从上下文获取用户 ID
func GetUserIDFromContext(c *gin.Context) string {
	if id, exists := c.Get("userId"); exists {
		if userID, ok := id.(string); ok {
			return userID
		}
	}
	return ""
}

// IsUserManagementEnabled 检查用户管理是否启用
func (m *UserAuthMiddleware) IsUserManagementEnabled() bool {
	return m.enabled
}
