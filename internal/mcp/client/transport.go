package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

const (
	transportSSE   = "sse"
	transportStdio = "stdio"

	defaultInitTimeout = 10 * time.Second
)

// buildClient 按 config 构造并 Initialize 一个 mcp-go client。
// 返回 ready-to-use client（已完成握手）。
func buildClient(ctx context.Context, cfg config.MCPServerConfig) (*mcpclient.Client, error) {
	if cfg.Name == "" {
		return nil, errors.New("mcp server config missing name")
	}
	var (
		cli *mcpclient.Client
		err error
	)
	switch cfg.Transport {
	case transportSSE:
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp[%s]: sse transport requires url", cfg.Name)
		}
		cli, err = mcpclient.NewSSEMCPClient(cfg.URL)
	case transportStdio:
		if len(cfg.Command) == 0 {
			return nil, fmt.Errorf("mcp[%s]: stdio transport requires command", cfg.Name)
		}
		envList := make([]string, 0, len(cfg.Env))
		for k, v := range cfg.Env {
			envList = append(envList, k+"="+v)
		}
		cli, err = mcpclient.NewStdioMCPClient(cfg.Command[0], envList, cfg.Command[1:]...)
	default:
		return nil, fmt.Errorf("mcp[%s]: unsupported transport %q (want sse|stdio)", cfg.Name, cfg.Transport)
	}
	if err != nil {
		return nil, fmt.Errorf("mcp[%s]: new client: %w", cfg.Name, err)
	}

	timeout := defaultInitTimeout
	if cfg.TimeoutMs > 0 {
		timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Stdio client auto-starts; SSE client needs explicit Start.
	if cfg.Transport == transportSSE {
		if err := cli.Start(initCtx); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("mcp[%s]: start: %w", cfg.Name, err)
		}
	}

	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "zhitu-agent", Version: "1.0"}
	if _, err := cli.Initialize(initCtx, req); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp[%s]: initialize: %w", cfg.Name, err)
	}
	return cli, nil
}
