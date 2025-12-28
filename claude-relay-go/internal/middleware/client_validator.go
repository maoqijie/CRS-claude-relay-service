package middleware

import (
	"net/http"
	"strings"

	"github.com/catstream/claude-relay-go/internal/pkg/clients"
	"github.com/catstream/claude-relay-go/internal/services/apikey"
	"github.com/gin-gonic/gin"
)

// ClientValidator 客户端验证器中间件
type ClientValidator struct {
	allowedClients   []string
	blockUnknown     bool
	customValidators []ClientValidatorFunc
}

// ClientValidatorFunc 自定义客户端验证函数
type ClientValidatorFunc func(userAgent string, clientType string) bool

// ClientValidatorOption 客户端验证器选项
type ClientValidatorOption func(*ClientValidator)

// NewClientValidator 创建客户端验证器
func NewClientValidator(opts ...ClientValidatorOption) *ClientValidator {
	cv := &ClientValidator{
		allowedClients: []string{},
		blockUnknown:   false,
	}

	for _, opt := range opts {
		opt(cv)
	}

	return cv
}

// WithAllowedClients 设置允许的客户端列表
func WithAllowedClients(clients []string) ClientValidatorOption {
	return func(cv *ClientValidator) {
		cv.allowedClients = clients
	}
}

// WithBlockUnknown 设置是否阻止未知客户端
func WithBlockUnknown(block bool) ClientValidatorOption {
	return func(cv *ClientValidator) {
		cv.blockUnknown = block
	}
}

// WithCustomValidator 添加自定义验证器
func WithCustomValidator(validator ClientValidatorFunc) ClientValidatorOption {
	return func(cv *ClientValidator) {
		cv.customValidators = append(cv.customValidators, validator)
	}
}

// Validate 返回验证中间件
func (cv *ClientValidator) Validate() gin.HandlerFunc {
	return func(c *gin.Context) {
		userAgent := c.GetHeader("User-Agent")
		clientType := cv.parseClientType(userAgent)

		// 检查是否在允许列表中
		if len(cv.allowedClients) > 0 {
			allowed := false
			clientLower := strings.ToLower(clientType)

			for _, ac := range cv.allowedClients {
				acLower := strings.ToLower(ac)
				if acLower == "*" || acLower == "all" || acLower == clientLower {
					allowed = true
					break
				}
				// 前缀匹配
				if strings.HasSuffix(acLower, "*") {
					prefix := strings.TrimSuffix(acLower, "*")
					if strings.HasPrefix(clientLower, prefix) {
						allowed = true
						break
					}
				}
			}

			if !allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":      "Client not allowed",
					"code":       "client_not_allowed",
					"clientType": clientType,
				})
				return
			}
		}

		// 阻止未知客户端
		if cv.blockUnknown && clientType == apikey.ClientUnknown {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Unknown client not allowed",
				"code":  "unknown_client",
			})
			return
		}

		// 运行自定义验证器
		for _, validator := range cv.customValidators {
			if !validator(userAgent, clientType) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Client validation failed",
					"code":  "client_validation_failed",
				})
				return
			}
		}

		// 设置客户端类型到上下文
		c.Set(string(ContextKeyClientType), clientType)

		c.Next()
	}
}

// parseClientType 解析客户端类型
func (cv *ClientValidator) parseClientType(userAgent string) string {
	return clients.ParseClientType(userAgent)
}

// RequireClaudeCode 要求 Claude Code 客户端
func RequireClaudeCode() gin.HandlerFunc {
	return NewClientValidator(
		WithAllowedClients([]string{apikey.ClientClaudeCode}),
	).Validate()
}

// RequireGeminiCLI 要求 Gemini CLI 客户端
func RequireGeminiCLI() gin.HandlerFunc {
	return NewClientValidator(
		WithAllowedClients([]string{apikey.ClientGeminiCLI}),
	).Validate()
}

// RequireKnownClient 要求已知客户端
func RequireKnownClient() gin.HandlerFunc {
	return NewClientValidator(
		WithBlockUnknown(true),
	).Validate()
}

// AllowAllClients 允许所有客户端（显式配置）
func AllowAllClients() gin.HandlerFunc {
	return NewClientValidator(
		WithAllowedClients([]string{"*"}),
	).Validate()
}

// ClientInfo 客户端信息
type ClientInfo struct {
	UserAgent  string `json:"userAgent"`
	ClientType string `json:"clientType"`
	IsKnown    bool   `json:"isKnown"`
}

// GetClientInfo 获取客户端信息
func GetClientInfo(c *gin.Context) *ClientInfo {
	userAgent := c.GetHeader("User-Agent")
	cv := &ClientValidator{}
	clientType := cv.parseClientType(userAgent)

	return &ClientInfo{
		UserAgent:  userAgent,
		ClientType: clientType,
		IsKnown:    clientType != apikey.ClientUnknown,
	}
}

// ValidateClaudeCodeHeaders 验证 Claude Code 特定头部
func ValidateClaudeCodeHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientType := GetClientTypeFromContext(c)
		if clientType != apikey.ClientClaudeCode {
			c.Next()
			return
		}

		// Claude Code 特定验证
		// 可以在这里添加对特定头部的检查

		c.Next()
	}
}
