package monitor

import (
	"context"
	"sync"
	"time"
)

// contextKey is the key type for MonitorContext in context.Context.
type contextKey struct{}

// MonitorContext mirrors Java MonitorContext — carries request-level observability data.
type MonitorContext struct {
	RequestID string
	SessionID int64
	UserID    int64
	StartTime time.Time
}

// NewMonitorContext creates a MonitorContext with the current time as StartTime.
func NewMonitorContext(requestID string, sessionID, userID int64) *MonitorContext {
	return &MonitorContext{
		RequestID: requestID,
		SessionID: sessionID,
		UserID:    userID,
		StartTime: time.Now(),
	}
}

// DurationMs returns elapsed milliseconds since StartTime.
func (m *MonitorContext) DurationMs() int64 {
	return time.Since(m.StartTime).Milliseconds()
}

// WithContext stores MonitorContext in the given context.Context.
func WithContext(ctx context.Context, mc *MonitorContext) context.Context {
	return context.WithValue(ctx, contextKey{}, mc)
}

// FromContext retrieves MonitorContext from context.Context.
func FromContext(ctx context.Context) *MonitorContext {
	if mc, ok := ctx.Value(contextKey{}).(*MonitorContext); ok {
		return mc
	}
	return nil
}

// holder mirrors Java MonitorContextHolder — goroutine-safe context holder.
var (
	holderMu  sync.RWMutex
	holderMap = make(map[int64]*MonitorContext) // keyed by goroutine is impractical; use sessionID
)

// SetHolder stores MonitorContext for the given session.
func SetHolder(sessionID int64, mc *MonitorContext) {
	holderMu.Lock()
	defer holderMu.Unlock()
	holderMap[sessionID] = mc
}

// GetHolder retrieves MonitorContext for the given session.
func GetHolder(sessionID int64) *MonitorContext {
	holderMu.RLock()
	defer holderMu.RUnlock()
	return holderMap[sessionID]
}

// ClearHolder removes MonitorContext for the given session.
func ClearHolder(sessionID int64) {
	holderMu.Lock()
	defer holderMu.Unlock()
	delete(holderMap, sessionID)
}
