package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DevelopmentOnly 中间件：仅允许在开发环境访问
func DevelopmentOnly(env string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if env == "production" {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "This endpoint is only available in development mode",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
