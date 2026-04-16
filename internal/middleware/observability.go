package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
)

// Observability mirrors Java ObservabilityInterceptor
// Generates X-Request-ID, creates MonitorContext, and sets it in both context and response header
func Observability() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = strings.ReplaceAll(uuid.New().String(), "-", "")
		}

		// Set defaults in gin context — downstream handlers (e.g. ChatHandler) will override
		c.Set("request_id", requestID)
		c.Set("user_id", int64(0))
		c.Set("session_id", int64(0))

		// Create MonitorContext and store in request context
		mc := monitor.NewMonitorContext(requestID, 0, 0)
		ctx := monitor.WithContext(c.Request.Context(), mc)
		c.Request = c.Request.WithContext(ctx)

		// Set response header
		c.Header("X-Request-ID", requestID)

		c.Next()

		// Update MonitorContext with final values from gin context
		if sessionID, ok := c.Get("session_id"); ok {
			if id, ok := sessionID.(int64); ok {
				mc.SessionID = id
			}
		}
		if userID, ok := c.Get("user_id"); ok {
			if id, ok := userID.(int64); ok {
				mc.UserID = id
			}
		}
	}
}
