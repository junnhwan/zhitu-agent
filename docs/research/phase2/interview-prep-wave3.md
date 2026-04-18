# Wave 3 面试向实施文档：MCP 双端闭环

> 目标：让 ZhituAgent 既能**消费**外部 MCP 工具（Client），又能把自身 RAG 能力**暴露**为 MCP 工具（Server），形成生态闭环。
> 覆盖优化点：**P5（MCP Client + MCP Server）**。
> 迭代时长：2 周。

---

## P5 · MCP Client（SSE + Stdio）+ MCP Server 暴露 RAG

### 1. 业务背景 / 痛点

**当前现状**：工具是**硬编码**三件套（`internal/tool/time_tool.go` / `email_tool.go` / `rag_tool.go`），在 `chat/service.go:createTools` 里全部手动注册：

```go
timeTool, _ := ztool.NewTimeTool()
emailTool, _ := ztool.NewEmailTool(&cfg.Mail)
ragTool, _ := ztool.NewRagTool(r, cfg.RAG.DocsPath)
```

问题：

| 问题 | 影响 |
|---|---|
| 加工具要改代码 | 业务方要接私有 SaaS 工具（Jira / 飞书 / 内部系统），每个都写一个 tool 文件 |
| 无法对接 Claude Desktop / Cursor | 这些客户端只认 MCP 协议，ZhituAgent 的 RAG 能力是岛屿 |
| 没有动态发现 | 工具能力变化要重启服务 |
| 没有协议标准 | 工具通信用裸 Go interface，外部系统接不进来 |

**业务驱动**：2025-2026 年 MCP 是 Agent 工具协议事实标准（Anthropic 主导，OpenAI / Google / JetBrains 都支持）。做完 MCP 双端，ZhituAgent 就能：
- **消费**：接入开源 MCP 工具生态（filesystem / git / postgres / github 等 100+ 个现成工具）
- **输出**：被 Claude Desktop / Cursor / 其他 Agent 当知识源使用

### 2. 技术选型 & 对比

**选型 1：Go MCP SDK**

| 方案 | 维护方 | 特点 | 决策 |
|---|---|---|---|
| `mark3labs/mcp-go` | 社区 | 支持 Server/Client 双端 + SSE/Stdio | ✅ 选这个（OpsPilot 用的就是这个）|
| 自写协议实现 | - | 完全可控 | ❌ MCP spec 还在演进，重复造轮子 |
| `anthropic/mcp`（官方）| Anthropic | Python / TS 优先 | ❌ 没有 Go 实现 |

**为什么不自写**：MCP 协议基于 JSON-RPC 2.0 但有 handshake、notification、progress、sampling、resource 这些扩展，spec 还在演进，跟上 Anthropic 主线成本高。社区 SDK 已经覆盖主流用法。

**选型 2：传输协议**

| 传输 | 场景 | 实现难度 |
|---|---|---|
| **Stdio** | 本地工具（filesystem、git）| 简单，cmd.Exec + pipe |
| **SSE**（HTTP）| 远程工具（内部平台、云服务）| 中等，要处理 connection lifecycle |
| WebSocket | 低延迟双向 | MCP spec 不是主流传输，不做 |

**决策**：Client **两种都支持**（SSE 远程 + Stdio 本地），Server 先做 **SSE**（暴露给 Cursor/Claude Desktop 最通用）。

**选型 3：Client 架构**

借鉴 **ai-agent-scaffold-lite** 的 "**三传输抽象 + 动态发现**"：

```go
type MCPTransport interface {
    Connect(ctx context.Context) error
    ListTools(ctx context.Context) ([]*schema.ToolInfo, error)
    CallTool(ctx context.Context, name string, args map[string]any) (string, error)
    Close() error
}
```

不管 SSE 还是 Stdio，上层 `mcp.Client` 只认接口。

### 3. 核心实现方案

**新目录结构**：

```
internal/mcp/
├── client/
│   ├── client.go             # 统一 Client 接口
│   ├── transport_sse.go      # SSE transport
│   ├── transport_stdio.go    # Stdio transport
│   ├── registry.go           # 多个 MCP server 统一注册
│   └── eino_adapter.go       # 把 MCP tool 封装成 Eino tool.InvokableTool
├── server/
│   ├── server.go             # MCP server 启动
│   ├── rag_resource.go       # 把 RAG 暴露为 MCP Resource
│   └── rag_tool.go           # 把 RAG 检索暴露为 MCP Tool
└── config.go                 # 配置结构体

cmd/mcp-server/
└── main.go                   # 独立 MCP Server 进程（或主 server 加 /mcp 路由）
```

**Client 核心代码**（骨架）：

```go
// client/client.go
type Client struct {
    transports map[string]MCPTransport  // serverName -> transport
    tools      map[string]*RemoteTool   // toolName -> tool info
    mu         sync.RWMutex
}

// eino_adapter.go
// 把 MCP tool 封装成 Eino 认识的 tool.InvokableTool
type MCPToolAdapter struct {
    client     *Client
    serverName string
    info       *schema.ToolInfo
}

func (a *MCPToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return a.info, nil
}

func (a *MCPToolAdapter) InvokableRun(ctx context.Context, argumentsJSON string) (string, error) {
    var args map[string]any
    if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
        return "", err
    }
    return a.client.transports[a.serverName].CallTool(ctx, a.info.Name, args)
}
```

**Config**（`config.yaml` 新增）：

```yaml
mcp:
  client:
    enabled: true
    servers:
      - name: filesystem
        transport: stdio
        command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"]
        timeout: 5s
      - name: internal-jira
        transport: sse
        url: https://mcp.internal/jira/sse
        auth:
          type: bearer
          token_env: JIRA_MCP_TOKEN
        timeout: 3s
  server:
    enabled: true
    bind: :8788
    expose:
      - rag_search      # 默认把 RAG 暴露
      - rag_documents   # 文档列表
```

**启动集成**：

```go
// cmd/server/main.go
mcpClient, _ := mcpclient.New(cfg.MCP.Client)
mcpTools := mcpClient.ListAllEinoTools(ctx)  // 合并所有远程 server 的工具

// 和本地工具一起注册
allTools := append(localTools, mcpTools...)
chatService := chat.NewService(cfg, rag, allTools)

// 启 MCP Server 暴露 RAG
if cfg.MCP.Server.Enabled {
    go mcpserver.Start(cfg.MCP.Server, rag)
}
```

**MCP Server 暴露 RAG**（用 `mark3labs/mcp-go`）：

```go
// server/server.go
func Start(cfg *Config, rag *rag.RAG) error {
    s := mcp.NewServer("zhitu-agent", "1.0.0")
    s.RegisterTool("rag_search", "Search ZhituAgent knowledge base", rag_searchSchema,
        func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            query := args["query"].(string)
            topK := int(args["top_k"].(float64))
            docs, err := rag.Retriever.Retrieve(ctx, query)
            // ... 返回格式化结果
        })
    return s.ListenSSE(cfg.Bind)
}
```

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| 某个 MCP server 连不上（启动期）| 打日志 + 跳过注册该 server，不阻塞服务启动 |
| 运行期 MCP server 断连 | 自动重连（指数退避 1s → 30s），重连期间该 server 工具调用返 `{"error": "mcp server unavailable"}` |
| MCP tool 调用超时 | context 超时，上层 ReAct agent 收到 error message 继续推理 |
| MCP server 协议不兼容 | 启动期 `Initialize` 握手时检测 protocol version，不兼容跳过该 server |
| MCP tool schema 变化（热更新）| 定期（每 60s）重新 `ListTools`，比对差异，更新本地 registry |
| 工具名冲突（两个 server 都有 `search`）| 注册时自动加前缀 `{serverName}__search` |
| Stdio 子进程 crash | 父进程监听 child process exit，重启（最多 3 次，失败下线该 server）|
| 恶意 MCP server 返回过大结果 | 单次调用结果 > 1MB 截断 + 告警（防 OOM）|
| MCP Server 被爆破（暴露 RAG 给外部时）| HTTP 层加 bearer token auth + rate limit middleware |

### 5. 兜底策略

**Client 侧兜底**：
- 配置开关 `mcp.client.enabled: false` 禁用整个 MCP client 链路
- MCP 工具全部挂时，本地工具仍可用（不阻塞主业务）

**Server 侧兜底**：
- 独立 bind（8788）和主 API（8080）分开，MCP server 挂不影响主服务
- Server 侧 RAG 调用复用主服务的 RAG 实例（共享 Redis），不重复初始化

**协议演进**：
- `mcp-go` 库 minor version upgrade 走灰度 + 接口 adapter，隔离 SDK 变化

### 6. 量化指标 & 评估方案

| 指标 | 目标 |
|---|---|
| MCP Client 工具数（动态注册）| 接入 ≥ 3 个开源 MCP server（filesystem / git / 自建 Jira-like）|
| Client 工具调用 P95 | Stdio < 200ms，SSE < 1s |
| Client 重连成功率 | > 99%（单日）|
| MCP Server 被 Claude Desktop 成功调用 | 能做端到端演示（简历 talking point）|
| 工具配置化（零代码加工具）| 加新 server 只改 `config.yaml`，不 rebuild |
| 协议兼容性 | 支持 MCP spec 最新 minor version |

**演示视频**：
- Claude Desktop 连 ZhituAgent MCP Server → 问企业知识 → 看到 RAG 结果
- ZhituAgent 连 filesystem MCP → ReAct Agent 能读本地文件做增强

### 7. 面试 Q&A 预演

**Q1：为什么要做 MCP Server，不就是多一个端口暴露 RAG 吗？**

A：Server 端的价值不是"多一个 HTTP 接口"，是让 ZhituAgent 成为**生态消费者的工具**。Cursor/Claude Desktop 用户只需要配一行 JSON 就能把我们的知识库接入他们的工作流——这是**分发维度的乘数**。对比自己做 HTTP API：MCP 有标准协议 + 客户端生态现成，比自造轮子接入成本低 10 倍。

### 硬核 Q&A

**HQ1：Stdio transport 下子进程 crash，你说指数退避重启。但如果子进程每次启动都 crash（如配置错误），会无限重试，怎么办？**

A：三级保护：
1. **启动期健康检查**：首次启动时跑 `Initialize` 握手 + 简单 `ListTools`，失败直接不注册（不进重试逻辑）。
2. **运行期熔断**：连续重试 3 次失败 → 标记该 server `UNAVAILABLE` 状态 + 告警 + 停止重启。人工介入后改配置 + 通过 admin API 手动 `reload`。
3. **重启窗口限制**：每小时重启次数 > 10 直接下线，避免 crashloop 把日志/metric 打爆。

**实现**：用 `golang.org/x/time/rate` 的 `Limiter` 控制重启频率，结合 gobreaker 做状态机（CLOSED → HALF_OPEN → OPEN）。

**HQ2：MCP 协议是 JSON-RPC 2.0，但 SSE 是单向的，怎么做双向通信？**

A：关键是 **SSE 的单向 = server→client**，client→server 走 HTTP POST。具体：
1. Client 初始化：`POST /sse` 建立 SSE 连接（server 推响应）+ 记录 `session_id`。
2. Client 发消息：`POST /messages?session_id=xxx`，body 是 JSON-RPC request。
3. Server 响应：通过已建立的 SSE 连接推 event（`data: {...json-rpc...}`）。
4. Notification：server 主动推 event（如工具列表变化），client 不需回复。

**踩坑点**：
- SSE 连接有 Proxy / 负载均衡超时问题，要服务端心跳 `: keepalive\n\n` 每 15s
- 多实例部署时 `session_id` 要能路由回原实例（sticky session 或 Redis pub/sub 分发）——单实例可以不考虑

**HQ3：MCP server 暴露 RAG 给外部，会不会有数据泄露风险？**

A：**会**，所以要做分层防护：
1. **网络层**：MCP Server 只 bind 内网，不上公网；如需上公网走 IP 白名单 + WAF。
2. **认证**：`Authorization: Bearer <token>`，token 按 org/用户 签发，24h 过期。
3. **授权**：每个 token 关联 `allowedDocIDs` 或 `allowedTags`，Retriever 里做 post-filter，超出权限的文档不返回。
4. **审计**：每次 `rag_search` 调用记 `user + query + returned_doc_ids`，便于审计追溯。
5. **query 过滤**：敏感词/SQL injection 模式检测，防止通过 query 试探索引结构。
6. **结果脱敏**：返回前过一遍 DLP 规则（身份证、手机号、邮箱）mask。

**实现优先级**：1 → 2 → 4（MVP 做这三件），3 和 5/6 按业务需要加。

**HQ4：动态 ListTools 每 60 秒拉一次，如果 MCP server 频繁增减工具，会不会和 ReAct Agent 的 tool binding 冲突？**

A：会冲突：`chatModel.BindTools(toolInfos)` 是预绑定，动态改变后 model 不知道新工具。两种方案：
1. **重新绑定**（简单）：工具变化时 `chatModel.BindTools(newList)`——但 Eino 的 ChatModel 是否支持重复 BindTools 要看源码（有些实现会 panic）。
2. **不绑定，运行时传入**（推荐）：每次 Generate 时传 `tools` 参数而不是预绑定；Eino ReAct Agent 需要改造支持"每步动态 tool list"。

**更好的设计**：工具变化做**事件通知**（`chan struct{}`），重建 ReAct Agent 实例（轻量操作），灰度无损切换。

**HQ5：如果外部 MCP server 返回恶意工具（比如 `delete_all_files`），ReAct Agent 会不会被诱导调用？**

A：有风险。**MCP Client 工具必须做白名单 + 确认机制**：
1. **白名单**：只注册配置文件明确列出的工具名，未列出的工具即便 server 返回也忽略。
2. **副作用工具确认**（借鉴 ThoughtCoding）：带 `sideEffect: true` 标的工具，调用前让用户显式确认（CLI 场景）或走审批流（B端场景）。
3. **沙箱隔离**：文件系统类工具限制在特定目录（`@modelcontextprotocol/server-filesystem /data` 的 `/data` 参数就是做这个的）。
4. **Prompt injection 防护**：工具描述（description）进 system prompt 前先扫关键词（`ignore previous instructions` 等）。

**最坏情况**：出了事故，**审计日志**能定位哪个 tool 在什么时间被谁的 query 触发——这是事后追责的保底。

**HQ6：MCP 协议 vs OpenAPI / gRPC，本质区别是什么？**

A：

| 维度 | MCP | OpenAPI | gRPC |
|---|---|---|---|
| **定位** | LLM 工具协议 | REST API 规范 | RPC 协议 |
| **发现** | 运行时 `ListTools` | 静态 spec 文件 | 静态 .proto |
| **双向** | 是（notification） | 否 | 双向流 |
| **Schema** | JSON Schema（LLM 友好）| OpenAPI Schema | Protobuf |
| **Streaming** | 是（progress、stream result） | SSE 扩展可选 | 原生 |
| **面向** | **LLM Agent 消费** | Web 前端 / 移动端 | 微服务内部 |

**核心差异**：MCP 的 tool description 是 **LLM-first** 设计（用 natural language + JSON schema，方便 LLM 理解和调用），而 OpenAPI / gRPC 是 **human developer-first**。对 Agent 场景，MCP 的 schema + description 能直接喂给 LLM 作为 tool calling 入参——这是 OpenAPI 需要额外 adapter 层才能做的。

**HQ7：做 MCP 会让架构变复杂，如果项目没几个外部工具需求，是不是过度设计？**

A：诚实回答——**是，但值得**。理由：
1. **简历价值 >> 实现成本**：MCP 是 2025-2026 最热协议，面试官认知度高，能体现"跟进前沿标准"。
2. **生态效应**：即便当前只接 3 个工具，未来加工具边际成本接近 0（改配置）。
3. **Server 端是单向增益**：把 ZhituAgent 知识库开放给 Claude Desktop，零改业务代码就多一个分发渠道。
4. **可退出**：MCP 有开关，真不用一关就等于没做——没有长期负担。

**如果项目连 Wave 1+2 都没做扎实**，MCP 是过度设计，应该先做前面。但**前两波做完 MCP 是自然的下一步**。

---

## Wave 3 落地节奏（2 周）

| 周 | 任务 | 产出 |
|---|---|---|
| Week 1 | Client 侧：SSE + Stdio 双 transport、registry、Eino adapter | 能连 filesystem MCP server 走 ReAct Agent |
| Week 2 | Server 侧：暴露 `rag_search` / `rag_documents` + auth middleware + Claude Desktop 联调 | 可演示的端到端闭环 |

**关键风险**：

| 风险 | 缓解 |
|---|---|
| `mark3labs/mcp-go` API 有 breaking change | 锁版本 + 封装 adapter 层 |
| Stdio 子进程在 Windows 行为差异 | 上 CI 跑 win/linux/mac 三平台冒烟 |
| Claude Desktop 联调期间 spec 解读歧义 | 准备一个 `curl`-based 手动测试集，逐个工具验证 |

---

## 面试总纲：三分钟讲完 Wave 3

> "MCP 是 2025-2026 的 Agent 工具事实标准，我做了双端闭环：
>
> **Client 端**：基于 `mark3labs/mcp-go` 抽象 `MCPTransport` 接口，同时支持 **Stdio（本地工具）** 和 **SSE（远程服务）**；把远程工具封装成 Eino 的 `tool.InvokableTool`，透明接入 ReAct Agent。支持动态 `ListTools` 热更新、指数退避重连、工具名冲突前缀隔离、熔断防 crashloop。
>
> **Server 端**：把 RAG 暴露成 MCP `rag_search` / `rag_documents` 工具，Cursor/Claude Desktop 配一行 JSON 就能接入；bearer token + 文档级授权 + 审计日志三层防护。
>
> **价值**：接入新工具从"写 Go 代码 + 重启"变成"改一行 yaml"；把 ZhituAgent 知识库变成生态节点，被外部 Agent 消费。
>
> **边界考虑**：恶意工具白名单、副作用确认、prompt injection 扫描、query 审计——生产级而非 demo 级。"
