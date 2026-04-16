package monitor

import (
	"fmt"
	"log"
)

// Logger mirrors Java ObservabilityLogger — structured request/response logging.
type Logger struct{}

// NewLogger creates a new observability logger.
func NewLogger() *Logger {
	return &Logger{}
}

// LogRequest logs the start of a request.
func (l *Logger) LogRequest(action string, params ...any) {
	mc := currentMonitorContext()
	if mc != nil {
		log.Printf("[%s] request_id=%s session_id=%d user_id=%d | %s",
			action, mc.RequestID, mc.SessionID, mc.UserID, formatParams(params...))
	} else {
		log.Printf("[%s] %s", action, formatParams(params...))
	}
}

// LogSuccess logs a successful response with duration.
func (l *Logger) LogSuccess(action string, durationMs int64, params ...any) {
	mc := currentMonitorContext()
	if mc != nil {
		log.Printf("[%s] SUCCESS duration=%dms request_id=%s session_id=%d user_id=%d | %s",
			action, durationMs, mc.RequestID, mc.SessionID, mc.UserID, formatParams(params...))
	} else {
		log.Printf("[%s] SUCCESS duration=%dms | %s", action, durationMs, formatParams(params...))
	}
}

// LogError logs a failed response with duration and error.
func (l *Logger) LogError(action string, durationMs int64, errMsg string) {
	mc := currentMonitorContext()
	if mc != nil {
		log.Printf("[%s] ERROR duration=%dms request_id=%s session_id=%d user_id=%d error=%s",
			action, durationMs, mc.RequestID, mc.SessionID, mc.UserID, errMsg)
	} else {
		log.Printf("[%s] ERROR duration=%dms error=%s", action, durationMs, errMsg)
	}
}

// currentMonitorContext tries to get MonitorContext from the holder map.
// Since we don't have goroutine-local storage in Go, this is a best-effort approach.
// The preferred path is to use FromContext(ctx) instead.
func currentMonitorContext() *MonitorContext {
	return nil // Placeholder — callers should use FromContext(ctx) for accurate context
}

// formatParams formats key-value pairs: ("model", "qwen", "status", "ok") → "model=qwen status=ok"
func formatParams(params ...any) string {
	if len(params) == 0 {
		return ""
	}
	var result string
	for i := 0; i+1 < len(params); i += 2 {
		if result != "" {
			result += " "
		}
		result += fmt.Sprintf("%v=%v", params[i], params[i+1])
	}
	return result
}
