package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Observability mirrors Java ObservabilityInterceptor
// Generates X-Request-ID and sets it in both context and response header
func Observability() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = strings.ReplaceAll(uuid.New().String(), "-", "")
		}

		// Store in gin context for downstream handlers
		c.Set("request_id", requestID)
		c.Set("user_id", int64(0))
		c.Set("session_id", int64(0))

		// Set response header
		c.Header("X-Request-ID", requestID)

		c.Next()
	}
}
