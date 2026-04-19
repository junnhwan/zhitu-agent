//go:build mcp

package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// TestStreamableHTTPRoundtrip spins up the MCP server inside httptest, drives it
// with mark3labs/mcp-go's streamable-http client, and verifies list_tools +
// call_tool work end to end.
// Run: go test -tags=mcp ./internal/mcp/server/ -v -run TestStreamableHTTPRoundtrip
func TestStreamableHTTPRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools := []tool.InvokableTool{newEchoTool(t)}
	srv, err := New(ctx, config.MCPServerSideConfig{Path: "/mcp"}, tools, Hooks{})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer srv.Shutdown(ctx)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cli, err := mcpclient.NewStreamableHttpClient(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cli.Close()

	if err := cli.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	listReq := mcp.ListToolsRequest{}
	listResp, err := cli.ListTools(ctx, listReq)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listResp.Tools) != 1 || listResp.Tools[0].Name != "echo" {
		t.Fatalf("tools=%+v", listResp.Tools)
	}
	t.Logf("listed tool: %s", listResp.Tools[0].Name)

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "echo"
	callReq.Params.Arguments = map[string]any{"msg": "hello from streamable"}
	callResp, err := cli.CallTool(ctx, callReq)
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if callResp.IsError {
		t.Fatalf("tool returned error: %+v", callResp.Content)
	}
	if len(callResp.Content) == 0 {
		t.Fatal("empty content")
	}
	tc, ok := callResp.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] type=%T", callResp.Content[0])
	}
	if !strings.Contains(tc.Text, "hello from streamable") {
		t.Errorf("text=%q, missing input echo", tc.Text)
	}
	t.Logf("echo response: %s", tc.Text)
}
