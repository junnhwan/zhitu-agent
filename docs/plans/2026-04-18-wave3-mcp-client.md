# Phase 2 Wave 3 — MCP 双端闭环（PR-C1 Client 先行）

## Context

Wave 2 把 RAG 检索升级完后，工具层还是 Phase 1 的硬编码三件套（`internal/tool/time_tool.go` / `email_tool.go` / `rag_tool.go`）。要加新工具只能改代码 + 重启，不能对接外部 MCP 生态。MCP 是 2025-2026 的 Agent 工具事实标准。

**Wave 3 总目标**：接入 + 暴露 MCP，分两个 PR：
- **PR-C1（本 plan）**：MCP **Client**（SSE + Stdio 双传输），能消费外部 MCP server 提供的工具，透明注入到 ReAct Agent 的 tool loop
- **PR-C2（本 plan 末尾 roadmap）**：MCP **Server**，把 ZhituAgent 的现有工具 + RAG 暴露给 Claude Desktop / Cursor

**已与用户确认**：
- 拆 C1 / C2 两个 PR
- C1 联调对象：本地 stdio MCP server（`npx @modelcontextprotocol/server-filesystem /tmp` 或 `server-everything`）
- C2 安全：单 Bearer token + 默认只 bind 127.0.0.1（本 plan 暂不展开）

---

## 依赖现状（已探明）

- **`eino-ext/components/tool/mcp@v0.0.8`** 已在 GOMODCACHE，API：
  - `mcp.GetTools(ctx, &Config{Cli, ToolNameList, ToolCallResultHandler, CustomHeaders, Meta}) ([]tool.BaseTool, error)`
  - 直接返 Eino `tool.BaseTool`，可直接塞进 `chat/service.go:createTools` 的 `toolInfos` / `toolMap`
- **`mark3labs/mcp-go@v0.46.0`** 已在 GOMODCACHE，提供 `client.NewSSEMCPClient` + `client.NewStdioMCPClient`
- **`internal/common/errno.go:22`** 已预留 `MCPConnectionError = 80040`

## 关键复用点

- **Tool 注入单点**：`internal/chat/service.go:createTools()` 返回 `([]*schema.ToolInfo, map[string]tool.InvokableTool)`。PR-C1 在这里合并 MCP 工具即可，`Chat` / `StreamChat` / workflow 三条链路全受益
- **`chatModel.WithTools(toolInfos)`**（service.go:104-112）会把新工具列表绑到 model
- **Tool 执行路径**：legacy 手写 loop 和 graph ToolsNode 都调 `toolMap[name].InvokableRun(ctx, argsJSON)`，MCP adapter 只要实现 `InvokableTool` 接口即可无缝接入（eino-ext wrapper 已经帮我们做了）

---

## PR-C1 文件结构

**新增**：
- `internal/mcp/client/client.go` — `Client` 结构体：按 name 管理多个 MCP server 连接 + 合并后的 tools
- `internal/mcp/client/transport.go` — SSE / Stdio 两种 client 构造（薄 wrapper over mark3labs/mcp-go）
- `internal/mcp/client/registry.go` — 启动时从 config 读 servers，初始化失败的跳过，返回合并后的 `[]tool.BaseTool`
- `internal/mcp/client/client_test.go` — 单测（transport 构造参数、工具名冲突前缀）
- `internal/mcp/client/integration_test.go` — `-tags=mcp`，跑真实 `npx @modelcontextprotocol/server-everything` stdio 做端到端冒烟

**修改**：
- `internal/chat/service.go:createTools` — 构造 MCP Client，把返回的 tools 合并进 toolInfos/toolMap
- `internal/config/config.go` — 加 `MCPConfig{Client MCPClientConfig}` 字段
- `config.yaml` — `mcp.client: { enabled: false, servers: [] }` 默认关
- `cmd/server/main.go` — 进程退出时调用 MCP Client.Close()（graceful）

---

## PR-C1 Tasks（TDD 风格，每步独立 commit）

### T1 — Config schema + 默认关

**Files**: `internal/config/config.go`, `config.yaml`

- [ ] `MCPConfig{Client MCPClientConfig}` → `MCPClientConfig{Enabled bool, Servers []MCPServerConfig}`
- [ ] `MCPServerConfig{Name string, Transport string, // sse|stdio; URL, Command []string, Env map[string]string, Enabled bool, Timeout time.Duration}`
- [ ] `config.yaml` 加注释样例（stdio filesystem / sse 内网）
- [ ] 默认 `mcp.client.enabled: false`，零风险
- [ ] Commit: `feat(mcp): add MCP client config skeleton`

### T2 — Transport 构造

**Files**: `internal/mcp/client/transport.go`, `internal/mcp/client/transport_test.go`

- [ ] `buildClient(ctx, cfg MCPServerConfig) (client.MCPClient, error)`：
  - `transport == "sse"` → `client.NewSSEMCPClient(cfg.URL, client.WithHeaders(...))`
  - `transport == "stdio"` → `client.NewStdioMCPClient(cfg.Command[0], cfg.Env 转 []string, cfg.Command[1:]...)`
  - 其他 → error
- [ ] 调 `cli.Initialize(ctx, mcp.InitializeRequest{...})` 完成握手，`cfg.Timeout` 作为握手超时
- [ ] 单测：transport 参数校验（空 URL、空 Command → error）；Initialize 超时路径
- [ ] Commit: `feat(mcp): add SSE + Stdio transport builders`

### T3 — Registry：多 server 聚合 + 工具名冲突处理

**Files**: `internal/mcp/client/client.go`, `internal/mcp/client/registry.go`, `internal/mcp/client/client_test.go`

- [ ] `Client` 结构体：
  ```go
  type Client struct {
      servers map[string]client.MCPClient  // name -> raw mcp-go client
      tools   []tool.BaseTool              // eino-compatible tools, prefixed on collision
      mu      sync.RWMutex
  }
  ```
- [ ] `NewClient(ctx, cfg MCPClientConfig) (*Client, error)`：
  - 禁用时返回 no-op `&Client{}`，不报错
  - 逐个 server `buildClient` + `mcp.GetTools`；单个失败仅 WARN log，不阻断；最终 `c.tools = append(c.tools, tools...)`
  - 工具名冲突：第二次出现同名 → wrapper 加 `serverName__` 前缀，保留原 tool Info 不变但新建 adapter
- [ ] `Client.Tools() []tool.BaseTool` / `Client.ToolInfos(ctx) ([]*schema.ToolInfo, error)` / `Client.ToolMap(ctx) map[string]tool.InvokableTool`
- [ ] `Client.Close() error` — 逐个 close，汇总 error
- [ ] 单测：
  - Disabled → no-op，Tools() 返回空
  - 两个 fake MCPClient 名字冲突 → 第二个自动加前缀
  - 一个 server 初始化失败 → 其他正常返回
- [ ] Commit: `feat(mcp): add client registry with collision prefix & failure skip`

### T4 — 接入 chat/service.go

**Files**: `internal/chat/service.go`, `internal/chat/service_test.go` (轻补充)

- [ ] `NewService` 里构造 `mcpClient, _ := mcpclient.NewClient(ctx, cfg.MCP.Client)`，存到 `Service` 字段
- [ ] `createTools` 合并：
  ```go
  localInfos, localMap := buildLocalTools(...)
  mcpInfos, _ := s.mcpClient.ToolInfos(ctx)
  mcpMap := s.mcpClient.ToolMap(ctx)
  toolInfos := append(localInfos, mcpInfos...)
  for k, v := range mcpMap { localMap[k] = v }
  return toolInfos, localMap
  ```
- [ ] `Service.Shutdown()`（如不存在则新增）调用 `s.mcpClient.Close()`
- [ ] Commit: `feat(mcp): wire MCP client tools into chat service`

### T5 — 指标 + 可观测

**Files**: `internal/monitor/ai_metrics.go`, `internal/mcp/client/client.go`

- [ ] 加 metric：
  - `mcp_client_tools_total{server}` Gauge — 每个 server 贡献的 tool 数
  - `mcp_client_calls_total{server, tool, status}` Counter — 通过 `ToolCallResultHandler` 回调打点
  - `mcp_client_call_duration_seconds{server, tool}` Histogram
- [ ] 在 `mcp.Config.ToolCallResultHandler` 里埋点（eino-ext 提供的钩子）
- [ ] Commit: `feat(mcp): add client Prometheus metrics`

### T6 — 集成测试（`-tags=mcp`）

**Files**: `internal/mcp/client/integration_test.go`

- [ ] 用 `npx -y @modelcontextprotocol/server-everything` 作为 stdio server（零 state，自带 echo / add 等 demo tools）
- [ ] 测试断言：
  1. `NewClient` 成功，`len(tools) > 0`
  2. Tool schema 能映射到 `tool.InvokableTool.Info`
  3. 随便调一个 tool（如 `echo`），结果 JSON 含预期字段
- [ ] 跳过条件：`command -v npx` 失败 → t.Skip
- [ ] Commit: `test(mcp): add stdio integration smoke against server-everything`

### T7 — 文档 + 收尾

- [ ] `go test ./... -race` 全绿；`-tags=mcp` 手动跑一次记 log
- [ ] `CLAUDE.md` 加一条"MCP Client 由 `mcp.client.enabled` 开关，默认关；工具名冲突自动加 `{serverName}__` 前缀"
- [ ] `docs/research/phase2/SUMMARY.md` 加 "✅ P5 PR-C1 MCP Client"
- [ ] PR title: `feat(wave3/p5c1): MCP client (SSE + Stdio) with multi-server registry`

---

## PR-C1 范围外（显式不做）

- **运行期动态 ListTools 热更新**：当前为静态注册，工具变更需重启。Wave 3 后期或 PR-C2 之后再加
- **断线重连 + 指数退避 + 三态熔断**：启动期失败跳过即可；运行期断线留到后续
- **白名单 + 副作用工具确认**：stdio 本地场景默认信任；PR-C2 的 server 端防护单独做
- **MCP Resource / Prompt / Sampling**：本 PR 只做 Tool 一种 MCP primitive

---

## PR-C1 验证

```bash
# 1. 单测
go test ./... -race -count=1

# 2. 集成（需 Node.js + npx）
go test -tags=mcp ./internal/mcp/client/ -v

# 3. 手动端到端
# config.yaml:
#   mcp.client.enabled: true
#   mcp.client.servers:
#     - name: fs
#       transport: stdio
#       command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
#       enabled: true
go run ./cmd/server
curl -X POST http://localhost:10010/api/multiAgentChat \
     -H "Content-Type: application/json" \
     -d '{"userId":1,"sessionId":1,"prompt":"列一下 /tmp 下的文件"}'
# 日志应见 MCP 工具被调用，返回文件列表
```

---

## 风险登记

| 风险 | 概率 | 缓解 |
|---|---|---|
| `mark3labs/mcp-go` minor version breaking | 中 | 锁 v0.46.0；升级走独立 PR |
| Windows stdio 子进程行为差异 | 中 | 集成测试本地跑 Windows + WSL 各一次 |
| `npx` 拉包慢 / 断网 | 高 | T6 集成测试 skip on missing npx；CI 不跑 `-tags=mcp` |
| 工具名加前缀破坏 LLM 已学习的命名 | 低 | 只在冲突时加前缀，无冲突保持原名 |
| MCP tool 返回结果过大撑爆内存 | 低 | eino-ext wrapper 默认透传；后续在 ToolCallResultHandler 截断 |

---

## Roadmap — PR-C2（后续 PR，不在本 plan 范围）

- `internal/mcp/server/` — 用 `mark3labs/mcp-go/server.NewMCPServer` 包装现有 3 个 local tool 为 MCP tool
- 路由：gin `gin.WrapH` mount `/mcp/sse` + `/mcp/message`，必须 **跳过 Guardrail 中间件**（SSE body 读不了 JSON）
- 安全：单 Bearer token + 默认 `bind: 127.0.0.1`；config `mcp.server: { enabled, bind, auth_token_env, expose_tools[] }`
- Claude Desktop 联调：提供 `mcp.json` sample config
- 估计 2-3 天

PR-C1 做完再展开 C2 的详细 task 列表。
