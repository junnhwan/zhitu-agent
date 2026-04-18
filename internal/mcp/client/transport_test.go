package client

import (
	"context"
	"strings"
	"testing"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

func TestBuildClientMissingName(t *testing.T) {
	_, err := buildClient(context.Background(), config.MCPServerConfig{})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("got %v", err)
	}
}

func TestBuildClientUnsupportedTransport(t *testing.T) {
	_, err := buildClient(context.Background(), config.MCPServerConfig{Name: "x", Transport: "ws"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("got %v", err)
	}
}

func TestBuildClientSSENoURL(t *testing.T) {
	_, err := buildClient(context.Background(), config.MCPServerConfig{Name: "x", Transport: "sse"})
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Errorf("got %v", err)
	}
}

func TestBuildClientStdioNoCommand(t *testing.T) {
	_, err := buildClient(context.Background(), config.MCPServerConfig{Name: "x", Transport: "stdio"})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("got %v", err)
	}
}
