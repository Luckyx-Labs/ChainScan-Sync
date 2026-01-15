package api

import (
	"strings"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware authentication middleware
// Using Bearer Token authentication
func AuthMiddleware(authToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authToken == "" {
			logger.Warn("API auth token not configured, skipping authentication")
			c.Next()
			return
		}

		// Get Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(401, gin.H{
				"success": false,
				"error":   "missing Authorization header",
			})
			return
		}

		// Support "Bearer <token>" or direct "<token>" format
		token := authHeader
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// Validate token
		if token != authToken {
			c.AbortWithStatusJSON(401, gin.H{
				"success": false,
				"error":   "invalid auth token",
			})
			return
		}

		c.Next()
	}
}

// RequestLogger request logging middleware
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		logger.Debugf("[API] %s %s %d %v", method, path, statusCode, latency)
	}
}
