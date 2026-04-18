package client

import (
	"context"
	"testing"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

func TestNewClientDisabled(t *testing.T) {
	c := NewClient(context.Background(), config.MCPClientConfig{Enabled: false})
	if c == nil {
		t.Fatal("nil client")
	}
	if len(c.Tools()) != 0 {
		t.Errorf("expected no tools, got %d", len(c.Tools()))
	}
	if err := c.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestNewClientSkipsBadServer(t *testing.T) {
	cfg := config.MCPClientConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{Name: "bad", Transport: "ws", Enabled: true}, // unsupported transport
			{Name: "no-name", Transport: "sse", URL: "http://127.0.0.1:1", Enabled: false}, // disabled
		},
	}
	c := NewClient(context.Background(), cfg)
	if len(c.Tools()) != 0 {
		t.Errorf("expected no tools, got %d", len(c.Tools()))
	}
	_ = c.Close()
}
