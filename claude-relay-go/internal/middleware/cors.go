package middleware

import (
	"strings"

	"github.com/catstream/claude-relay-go/internal/config"
	"github.com/gin-gonic/gin"
)

// CORSConfig CORS 配置
type CORSConfig struct {
	// EnableCors 启用通用 CORS（允许所有来源）
	EnableCors bool
	// AllowedOrigins 允许的来源列表
	AllowedOrigins []string
	// AllowedMethods 允许的方法
	AllowedMethods []string
	// AllowedHeaders 允许的请求头
	AllowedHeaders []string
	// ExposedHeaders 暴露的响应头
	ExposedHeaders []string
	// MaxAge 预检缓存时间（秒）
	MaxAge string
	// AllowCredentials 允许携带凭据
	AllowCredentials bool
}

// DefaultCORSConfig 默认 CORS 配置
var DefaultCORSConfig = CORSConfig{
	EnableCors: false,
	AllowedOrigins: []string{
		"http://localhost:3000",
		"https://localhost:3000",
		"http://127.0.0.1:3000",
		"https://127.0.0.1:3000",
	},
	AllowedMethods: []string{
		"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH",
	},
	AllowedHeaders: []string{
		"Origin",
		"X-Requested-With",
		"Content-Type",
		"Accept",
		"Authorization",
		"x-api-key",
		"x-goog-api-key",
		"api-key",
		"x-admin-token",
		"anthropic-version",
		"anthropic-dangerous-direct-browser-access",
		"x-session-hash",
		"x-sticky-session",
		"x-stainless-arch",
		"x-stainless-lang",
		"x-stainless-os",
		"x-stainless-package-version",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
	},
	ExposedHeaders: []string{
		"X-Request-ID",
		"Content-Type",
	},
	MaxAge:           "86400", // 24小时
	AllowCredentials: true,
}

// CORS 返回 CORS 中间件
func CORS() gin.HandlerFunc {
	return CORSWithConfig(DefaultCORSConfig)
}

// CORSWithConfig 使用指定配置返回 CORS 中间件
func CORSWithConfig(cfg CORSConfig) gin.HandlerFunc {
	// 检查全局配置
	enableCors := cfg.EnableCors
	if config.Cfg != nil {
		// 可以从配置中读取
		// enableCors = config.Cfg.Web.EnableCors
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// 如果启用了通用 CORS，允许所有来源
		if enableCors {
			c.Header("Access-Control-Allow-Origin", "*")
			setCORSHeaders(c, cfg)

			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(204)
				return
			}
			c.Next()
			return
		}

		// 检查是否为 Chrome 插件请求
		isChromeExtension := strings.HasPrefix(origin, "chrome-extension://")

		// 检查是否在允许列表中
		isAllowed := false
		for _, allowed := range cfg.AllowedOrigins {
			if origin == allowed {
				isAllowed = true
				break
			}
		}

		// 设置 Origin
		if isAllowed || origin == "" || isChromeExtension {
			if origin != "" {
				c.Header("Access-Control-Allow-Origin", origin)
			} else {
				c.Header("Access-Control-Allow-Origin", "*")
			}
		}

		setCORSHeaders(c, cfg)

		// 预检请求直接返回
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// setCORSHeaders 设置 CORS 响应头
func setCORSHeaders(c *gin.Context, cfg CORSConfig) {
	// Methods
	c.Header("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))

	// Headers
	c.Header("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))

	// Exposed Headers
	if len(cfg.ExposedHeaders) > 0 {
		c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
	}

	// Max Age
	if cfg.MaxAge != "" {
		c.Header("Access-Control-Max-Age", cfg.MaxAge)
	}

	// Credentials
	if cfg.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}
}

// NewCORSMiddleware 创建 CORS 中间件（兼容旧代码）
func NewCORSMiddleware() gin.HandlerFunc {
	return CORS()
}
