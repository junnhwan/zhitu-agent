package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// Hooks 让调用方注入可观测回调，典型传 monitor.AiMetrics 的方法。
type Hooks struct {
	OnToolsRegistered func(count int)
	OnCall            func(toolName, status string, duration time.Duration)
}

// Server 包装 mcp-go 的 MCPServer + StreamableHTTPServer，
// 对外暴露 http.Handler 给 Gin 挂载，Shutdown 负责清理 session sweeper。
type Server struct {
	mcp     *mcpsrv.MCPServer
	shttp   *mcpsrv.StreamableHTTPServer
	handler http.Handler
	hooks   Hooks
	tools   []string
}

// New 构造一个 MCP Server，把 tools 里的每个 Eino InvokableTool 转换后注册到 mcp-go。
// cfg.Path 为 StreamableHTTP 的 endpoint 路径（默认 /mcp）。
func New(ctx context.Context, cfg config.MCPServerSideConfig, tools []tool.InvokableTool, hooks Hooks) (*Server, error) {
	m := mcpsrv.NewMCPServer("zhitu-agent", "1.0", mcpsrv.WithToolCapabilities(false))
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		mt, handler, err := adapt(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("adapt tool: %w", err)
		}
		wrapped := withMetrics(handler, mt.Name, hooks.OnCall)
		m.AddTool(mt, wrapped)
		names = append(names, mt.Name)
	}
	path := cfg.Path
	if path == "" {
		path = "/mcp"
	}
	shttp := mcpsrv.NewStreamableHTTPServer(m, mcpsrv.WithEndpointPath(path))
	if hooks.OnToolsRegistered != nil {
		hooks.OnToolsRegistered(len(tools))
	}
	log.Printf("[mcp.server] registered %d tools at path %s: %v", len(tools), path, names)
	return &Server{mcp: m, shttp: shttp, handler: shttp, hooks: hooks, tools: names}, nil
}

// Handler 返回挂到 Gin 的 http.Handler（StreamableHTTP 单端点，内部按 method 分派）。
func (s *Server) Handler() http.Handler { return s.handler }

// Tools 返回已注册的工具名列表（给日志/调试用）。
func (s *Server) Tools() []string {
	out := make([]string, len(s.tools))
	copy(out, s.tools)
	return out
}

// Shutdown 关闭内部 session sweeper 等后台协程。幂等。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.shttp == nil {
		return nil
	}
	return s.shttp.Shutdown(ctx)
}

// adapt 把 Eino tool.InvokableTool 转成 mcp-go 的 Tool + handler。
// Schema 直接用 ParamsOneOf.ToJSONSchema 序列化成 RawInputSchema，避开 mcp-go 的 typed InputSchema。
func adapt(ctx context.Context, eTool tool.InvokableTool) (mcp.Tool, mcpsrv.ToolHandlerFunc, error) {
	info, err := eTool.Info(ctx)
	if err != nil {
		return mcp.Tool{}, nil, fmt.Errorf("tool info: %w", err)
	}
	if info == nil || info.Name == "" {
		return mcp.Tool{}, nil, fmt.Errorf("tool info missing name")
	}

	var raw json.RawMessage
	if info.ParamsOneOf != nil {
		js, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return mcp.Tool{}, nil, fmt.Errorf("tool %q: to json schema: %w", info.Name, err)
		}
		raw, err = json.Marshal(js)
		if err != nil {
			return mcp.Tool{}, nil, fmt.Errorf("tool %q: marshal json schema: %w", info.Name, err)
		}
	} else {
		raw = json.RawMessage(`{"type":"object","properties":{}}`)
	}

	mt := mcp.Tool{
		Name:           info.Name,
		Description:    info.Desc,
		RawInputSchema: raw,
	}

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argsJSON, err := marshalArgs(req)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("invalid arguments", err), nil
		}
		out, err := eTool.InvokableRun(ctx, argsJSON)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("tool invocation failed", err), nil
		}
		return mcp.NewToolResultText(out), nil
	}
	return mt, handler, nil
}

// marshalArgs 把 CallToolRequest.Params.Arguments 序列化成 JSON 字符串，
// 兼容 map[string]any / json.RawMessage / nil 几种情况。
func marshalArgs(req mcp.CallToolRequest) (string, error) {
	raw := req.GetRawArguments()
	if raw == nil {
		return "{}", nil
	}
	if s, ok := raw.(json.RawMessage); ok {
		if len(s) == 0 {
			return "{}", nil
		}
		return string(s), nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// withMetrics 把 handler 包一层 timing + status 回调。
func withMetrics(h mcpsrv.ToolHandlerFunc, name string, onCall func(tool, status string, d time.Duration)) mcpsrv.ToolHandlerFunc {
	if onCall == nil {
		return h
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		res, err := h(ctx, req)
		status := "success"
		if err != nil || (res != nil && res.IsError) {
			status = "error"
		}
		onCall(name, status, time.Since(start))
		return res, err
	}
}
