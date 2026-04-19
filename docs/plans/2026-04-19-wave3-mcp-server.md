# Wave 3 PR-C2 — MCP Server

## Context

Phase 2 Wave 3 PR-C1 (MCP Client) shipped 2026-04-18: ZhituAgent can now pull external MCP tools into its chat loop. PR-C2 is the reverse direction — expose ZhituAgent's own capabilities as an MCP server so external clients like Claude Desktop can call them.

**Goal**: stand up an MCP HTTP server in the existing Gin process, bearer-token guarded, exposing 3 tools — `getCurrentTime`, `addKnowledgeToRag`, and a new `retrieveKnowledge` (wraps the RAG retriever). `sendEmail` is intentionally **not** exposed (side-effectful, no audit trail for remote callers).

**Why in-process + same port**: simplest lifecycle story, reuses graceful shutdown, no extra ports to manage. The MCP handler is just another `http.Handler` mounted under Gin via `gin.WrapH`.

**Why Streamable HTTP, not SSE or stdio**: Streamable HTTP is the current MCP spec direction (single endpoint, POST/GET/DELETE on `/mcp`). Stdio doesn't fit a long-running HTTP server. SSE is superseded.

**Disabled by default** (`mcp.server.enabled=false`). When enabled, `mcp.server.auth_token` must be set — fail-fast on empty token.

## Scope — what's exposed

| Tool | Source | Notes |
|---|---|---|
| `getCurrentTime` | existing `internal/tool/time_tool.go` | re-use as-is |
| `addKnowledgeToRag` | existing `internal/tool/rag_tool.go` | re-use as-is |
| `retrieveKnowledge` | **new** `internal/tool/rag_retrieve_tool.go` | wraps `rag.RAG.Retriever.Retrieve`, returns formatted docs |
| ~~`sendEmail`~~ | excluded | side effect, risk of abuse by remote clients |

Allow-list is hardcoded in v1 (`internal/mcp/server/tools.go:DefaultTools`). Config-driven allow-list can follow if needed.

## File changes

**New:**
- `internal/mcp/server/server.go` — `Server` type, `New()`, `Handler() http.Handler`, `Shutdown()`, `Hooks{OnToolsRegistered, OnCall}` mirroring client shape (`internal/mcp/client/client.go:19`)
- `internal/mcp/server/tools.go` — `DefaultTools(r *rag.RAG, cfg *config.Config) []tool.InvokableTool` + `adapt(t tool.InvokableTool) (mcp.Tool, server.ToolHandlerFunc, error)` converter
- `internal/mcp/server/server_test.go` — unit: adapter schema conversion, handler round-trip (no network)
- `internal/mcp/server/integration_test.go` (`-tags=mcp`) — spin up httptest server, drive via `mark3labs/mcp-go/client`, call all 3 tools
- `internal/tool/rag_retrieve_tool.go` — new `retrieveKnowledge` Eino tool using `utils.InferTool[RetrieveInput, string]`; calls `r.Retriever.Retrieve(ctx, query)` and formats docs as text
- `internal/middleware/bearer_auth.go` — `BearerAuth(token string, onReject func()) gin.HandlerFunc` with constant-time compare; empty header → 401, wrong token → 401 + metric

**Modified:**
- `internal/config/config.go:149-166` — add `MCPServerConfig{Enabled, Path, AuthToken}` inside `MCPConfig`; add default `mcp.server.enabled=false`, `mcp.server.path=/mcp`; add `ZHU_MCP_SERVER_AUTH_TOKEN` env override in `overrideFromEnv`
- `internal/monitor/ai_metrics.go:23-100` — add `mcpServerToolsGauge` (Gauge, no labels), `mcpServerCalls` (CounterVec: `tool,status`), `mcpServerCallDuration` (HistogramVec: `tool`), `mcpServerUnauth` (Counter); register in `NewAiMetrics`; add `SetMCPServerToolsCount`, `RecordMCPServerCall`, `RecordMCPServerUnauth`
- `cmd/server/main.go` — after `chatService.InitOrchestrator()`: if `cfg.MCP.Server.Enabled`, require non-empty token (else `log.Fatal`), build `mcpserver.New(...)`, mount `r.Any(cfg.MCP.Server.Path, middleware.BearerAuth(...), gin.WrapH(mcpSrv.Handler()))`; call `mcpSrv.Shutdown()` in the graceful-shutdown block alongside `ragSystem.Shutdown()`
- `config.yaml` — add commented `mcp.server:` block near existing `mcp.client:` example
- `CLAUDE.md` — append "MCP Server 可选对外暴露" contract under 必须遵守的设计契约

## Key implementation details

### Eino → MCP tool adapter (`internal/mcp/server/tools.go`)

```
adapt(eTool tool.InvokableTool) (mcp.Tool, server.ToolHandlerFunc, error):
  info := eTool.Info(ctx)
  jsonSchema, _ := info.ParamsOneOf.ToJSONSchema()  // *jsonschema.Schema
  rawSchema, _ := json.Marshal(jsonSchema)
  mcpTool := mcp.Tool{
    Name: info.Name,
    Description: info.Desc,
    RawInputSchema: rawSchema,  // bypass typed InputSchema — use raw
  }
  handler := func(ctx, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    argsJSON, _ := json.Marshal(req.Params.Arguments)
    out, err := eTool.InvokableRun(ctx, string(argsJSON))
    if err != nil { return mcp.NewToolResultError(err.Error()), nil }
    return mcp.NewToolResultText(out), nil
  }
  return mcpTool, handler, nil
```

Schema source: `eino@v0.8.9/schema/tool.go:138` — `(*ParamsOneOf).ToJSONSchema() (*jsonschema.Schema, error)`. Eino tool's `ParamsOneOf` already encodes the same JSON Schema MCP expects.

### Server wrapper (`internal/mcp/server/server.go`)

```
type Server struct {
  mcp     *mcpsrv.MCPServer
  handler http.Handler  // *StreamableHTTPServer
  hooks   Hooks
}

func New(ctx context.Context, cfg config.MCPServerConfig, tools []tool.InvokableTool, hooks Hooks) (*Server, error):
  mcp := mcpsrv.NewMCPServer("zhitu-agent", "1.0", mcpsrv.WithToolCapabilities(false))
  for _, t := range tools {
    mt, handler, err := adapt(t)
    if err != nil { return nil, err }
    wrapped := withMetrics(handler, mt.Name, hooks.OnCall)
    mcp.AddTool(mt, wrapped)
  }
  shttp := mcpsrv.NewStreamableHTTPServer(mcp, mcpsrv.WithEndpointPath(cfg.Path))
  hooks.OnToolsRegistered?(len(tools))
  return &Server{mcp: mcp, handler: shttp, hooks: hooks}, nil
```

`withMetrics` wraps the handler to time + record status (mirrors `client.metered`).

### Bearer auth middleware (`internal/middleware/bearer_auth.go`)

```
func BearerAuth(expected string, onReject func()) gin.HandlerFunc {
  expBytes := []byte("Bearer " + expected)
  return func(c *gin.Context) {
    got := c.GetHeader("Authorization")
    if subtle.ConstantTimeCompare([]byte(got), expBytes) != 1 {
      if onReject != nil { onReject() }
      c.AbortWithStatus(http.StatusUnauthorized)
      return
    }
    c.Next()
  }
}
```

`onReject` wired to `AiMetrics.RecordMCPServerUnauth()` in main.go.

### retrieveKnowledge tool (`internal/tool/rag_retrieve_tool.go`)

```
type RetrieveInput struct {
  Query string `json:"query" jsonschema:"description=检索查询"`
  TopK  int    `json:"topK,omitempty" jsonschema:"description=返回条数,默认使用 rag.retrieve_top_k"`
}

func NewRetrieveKnowledgeTool(r *rag.RAG) (tool.InvokableTool, error):
  return utils.InferTool[RetrieveInput, string](
    "retrieveKnowledge",
    "从知识库检索与查询相关的文档片段...",
    func(ctx, in RetrieveInput) (string, error) {
      docs, err := r.Retriever.Retrieve(ctx, in.Query)
      if err != nil { return "", err }
      // format: 一行标题 + "\n" + content 片段，多条用 "---" 分隔
      return formatDocs(docs), nil
    },
  )
```

TopK is accepted but currently ignored (retriever uses config default) — document it for forward-compat.

## Verification

**Unit tests (no build tag):**
```
go test ./internal/mcp/server/ ./internal/middleware/ ./internal/tool/ -race
```
Covers: adapter schema conversion, handler happy-path + error, BearerAuth allow/reject, retrieveKnowledge formatting.

**Integration (`-tags=mcp`):**
```
go test -tags=mcp ./internal/mcp/server/ -v
```
Spawn `httptest.Server` wrapping the MCP handler; drive via `mark3labs/mcp-go/client.NewStreamableHttpClient`. Call `list_tools` (expect 3), `call_tool getCurrentTime` (non-empty string), `call_tool retrieveKnowledge{query:"zhitu"}` (empty RAG → empty-ish result, no error), `call_tool addKnowledgeToRag{...}` (file written + re-indexed in temp docs dir).

**Manual smoke:**
```
ZHU_MCP_SERVER_ENABLED=true ZHU_MCP_SERVER_AUTH_TOKEN=dev123 ./server
curl -X POST http://127.0.0.1:10010/mcp \
  -H "Authorization: Bearer dev123" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
# expect 200 + initializeResult
curl -X POST http://127.0.0.1:10010/mcp -H "Authorization: Bearer WRONG" -d '{}'
# expect 401
```

**Claude Desktop smoke (optional, post-merge):**
Add to `claude_desktop_config.json`:
```json
{"mcpServers":{"zhitu":{"url":"http://127.0.0.1:10010/mcp","transport":"streamable_http","headers":{"Authorization":"Bearer dev123"}}}}
```
Verify tools appear + each executes.

**Prometheus check:**
```
curl http://127.0.0.1:10010/metrics | grep mcp_server
# expect mcp_server_tools_total 3, mcp_server_calls_total{...} after a call
```
