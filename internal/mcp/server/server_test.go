package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

type echoIn struct {
	Msg string `json:"msg" jsonschema:"description=回显内容"`
}

func newEchoTool(t *testing.T) tool.InvokableTool {
	t.Helper()
	eTool, err := utils.InferTool[echoIn, string](
		"echo",
		"回显传入的消息",
		func(ctx context.Context, in echoIn) (string, error) {
			if in.Msg == "boom" {
				return "", errors.New("boom")
			}
			return "echo:" + in.Msg, nil
		},
	)
	if err != nil {
		t.Fatalf("new echo tool: %v", err)
	}
	return eTool
}

func TestAdaptBuildsMCPToolWithSchema(t *testing.T) {
	mt, handler, err := adapt(context.Background(), newEchoTool(t))
	if err != nil {
		t.Fatalf("adapt: %v", err)
	}
	if mt.Name != "echo" {
		t.Errorf("name=%q, want echo", mt.Name)
	}
	if mt.Description == "" {
		t.Error("description empty")
	}
	if len(mt.RawInputSchema) == 0 {
		t.Fatal("RawInputSchema empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(mt.RawInputSchema, &schema); err != nil {
		t.Fatalf("schema not valid json: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type=%v, want object", schema["type"])
	}
	if handler == nil {
		t.Fatal("nil handler")
	}
}

func TestAdaptHandlerSuccess(t *testing.T) {
	_, handler, err := adapt(context.Background(), newEchoTool(t))
	if err != nil {
		t.Fatalf("adapt: %v", err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"msg": "hi"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] type=%T", res.Content[0])
	}
	if tc.Text != "echo:hi" {
		t.Errorf("text=%q, want echo:hi", tc.Text)
	}
}

func TestAdaptHandlerToolError(t *testing.T) {
	_, handler, err := adapt(context.Background(), newEchoTool(t))
	if err != nil {
		t.Fatalf("adapt: %v", err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"msg": "boom"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned go error (expected error result): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
}

func TestWithMetricsRecordsStatus(t *testing.T) {
	var gotTool, gotStatus string
	var gotDur time.Duration
	onCall := func(name, status string, d time.Duration) {
		gotTool, gotStatus, gotDur = name, status, d
	}

	okHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}
	withMetrics(okHandler, "t1", onCall)(context.Background(), mcp.CallToolRequest{})
	if gotTool != "t1" || gotStatus != "success" || gotDur < 0 {
		t.Errorf("success path: tool=%q status=%q dur=%v", gotTool, gotStatus, gotDur)
	}

	errHandler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("nope"), nil
	}
	withMetrics(errHandler, "t2", onCall)(context.Background(), mcp.CallToolRequest{})
	if gotTool != "t2" || gotStatus != "error" {
		t.Errorf("error path: tool=%q status=%q", gotTool, gotStatus)
	}
}

func TestNewRegistersTools(t *testing.T) {
	var registered int
	hooks := Hooks{OnToolsRegistered: func(n int) { registered = n }}
	srv, err := New(context.Background(), config.MCPServerSideConfig{Path: "/mcp"}, []tool.InvokableTool{newEchoTool(t)}, hooks)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if registered != 1 {
		t.Errorf("OnToolsRegistered got %d, want 1", registered)
	}
	if len(srv.Tools()) != 1 || srv.Tools()[0] != "echo" {
		t.Errorf("tools=%v", srv.Tools())
	}
	if srv.Handler() == nil {
		t.Fatal("nil handler")
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestMarshalArgsHandlesNilAndMap(t *testing.T) {
	req := mcp.CallToolRequest{}
	got, err := marshalArgs(req)
	if err != nil || got != "{}" {
		t.Errorf("nil args: got=%q err=%v", got, err)
	}
	req.Params.Arguments = map[string]any{"k": "v"}
	got, err = marshalArgs(req)
	if err != nil {
		t.Fatalf("map args: %v", err)
	}
	if got != `{"k":"v"}` {
		t.Errorf("map args: got=%q", got)
	}
}
