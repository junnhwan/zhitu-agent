package monitor

import (
	"context"
	"testing"
	"time"
)

func TestNewMonitorContext(t *testing.T) {
	mc := NewMonitorContext("req-1", 100, 200)
	if mc.RequestID != "req-1" {
		t.Errorf("RequestID = %q, want req-1", mc.RequestID)
	}
	if mc.SessionID != 100 {
		t.Errorf("SessionID = %d, want 100", mc.SessionID)
	}
	if mc.UserID != 200 {
		t.Errorf("UserID = %d, want 200", mc.UserID)
	}
	if mc.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}
}

func TestDurationMs(t *testing.T) {
	mc := NewMonitorContext("req-1", 0, 0)
	mc.StartTime = time.Now().Add(-100 * time.Millisecond)
	dur := mc.DurationMs()
	if dur < 90 {
		t.Errorf("DurationMs = %d, want >= 90", dur)
	}
}

func TestWithContext(t *testing.T) {
	mc := NewMonitorContext("req-1", 1, 2)
	ctx := WithContext(context.Background(), mc)

	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil")
	}
	if got.RequestID != "req-1" {
		t.Errorf("RequestID = %q, want req-1", got.RequestID)
	}
}

func TestFromContextMissing(t *testing.T) {
	got := FromContext(context.Background())
	if got != nil {
		t.Error("FromContext should return nil for empty context")
	}
}

func TestHolder(t *testing.T) {
	mc := NewMonitorContext("req-holder", 42, 0)
	SetHolder(42, mc)

	got := GetHolder(42)
	if got == nil || got.RequestID != "req-holder" {
		t.Errorf("GetHolder = %v, want req-holder", got)
	}

	ClearHolder(42)
	if got := GetHolder(42); got != nil {
		t.Error("GetHolder should return nil after ClearHolder")
	}
}
