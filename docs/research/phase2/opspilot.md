# OpsPilot 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/xiaolin_go/OpsPilot`
> 语言栈：**Go 1.25 + CloudWeGo Eino（和 ZhituAgent 同框架！）** + Gin + GORM + Milvus + Redis

## 1. 项目定位

**智能运维 Agent（AIOps）**：聊天问答 + 知识库 RAG + Prometheus 告警分析 + MCP 日志工具，一条链路贯通。**和 ZhituAgent 用同一套 Eino 框架**，是**借鉴 Eino 高级用法的最佳对照项目**。

## 2. 架构概览

```
cmd/server/main.go
internal/
  ├── bootstrap/              # 统一装配入口（workflow 预编译）
  ├── interfaces/http/        # DTO + handler + SSE + middleware
  ├── application/            # 应用服务层（chat/auth/knowledge/aiops）
  ├── ai/
  │   ├── workflows/
  │   │   ├── chat/           # RAG + 工具调用 Chat Workflow（Eino Graph）
  │   │   ├── aiopsplan/      # Plan-Execute-Replan Agent
  │   │   ├── knowledgeindex/ # 文档索引流水线
  │   │   └── sessionsummary/ # LLM 摘要会话记忆压缩
  │   ├── tools/              # Eino 工具
  │   └── registry/           # 场景化工具注册
  ├── domain/                 # 领域模型（user/session/knowledge）
  └── infra/
      ├── config/             # Viper + OPSPILOT_ 环境变量
      ├── llm/                # Ark / OpenAI Factory
      ├── mcp/                # MCP SSE Client（mark3labs/mcp-go）
      ├── session/redis/      # Redis 会话 + 分布式锁
      └── vectorstore/milvus/ # Dense+Sparse 混合检索
```

**四层严格边界**：interfaces → application → ai/workflows → infra。

## 3. 亮点实现

### 3.1 Eino Plan-Execute-Replan Agent ★★★★★
`internal/ai/workflows/aiopsplan/workflow.go:61-100`

```go
planner, _ := planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
    ToolCallingChatModel: plannerModel,
})
executor, _ := planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
    Model: executorModel,
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
    },
    MaxIterations: 20,  // 执行器单步最多 20 次工具调用
})
replanner, _ := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
    ChatModel: plannerModel,
})
planExecuteAgent, _ := planexecute.New(ctx, &planexecute.Config{
    Planner: planner, Executor: executor, Replanner: replanner,
    MaxIterations: 10,  // 整体规划-执行循环最多 10 次
})
```

**精华**：Eino 内置了 `adk/prebuilt/planexecute` 三件套，直接拼装就能拿到完整的 Plan→Execute→Replan 闭环。执行结果通过 `runner.Query + iter.Next` 事件流拿到每一步的中间输出（`workflow.go:103-128`）。

**ZhituAgent 对比**：现在的 "multiAgent" 其实是关键词路由 + 主链路，**远谈不上 Plan-Execute**。

### 3.2 Eino Graph 编排（非 Chain）★★★★★
`internal/ai/workflows/chat/workflow.go:113-165`

```go
graph := compose.NewGraph[*Input, *schema.Message]()
graph.AddLambdaNode(inputToRAG, ...)
graph.AddChatTemplateNode(chatTemplate, template)
graph.AddLambdaNode(reactAgent, reactLambda)
graph.AddRetrieverNode(retrieverKey, w.retriever, compose.WithOutputKey("documents"))
graph.AddLambdaNode(inputToChat, ...)
graph.AddLambdaNode(rewriteModel, ...)

// 并行分流 → 汇聚
graph.AddEdge(compose.START, inputToChat)
graph.AddEdge(compose.START, rewriteModel)  // 🌟 同时启动两条
graph.AddEdge(rewriteModel, inputToRAG)
graph.AddEdge(inputToRAG, retrieverKey)
graph.AddEdge(retrieverKey, chatTemplate)
graph.AddEdge(inputToChat, chatTemplate)    // 🌟 两条边同时进入 chatTemplate
graph.AddEdge(chatTemplate, reactAgent)
graph.AddEdge(reactAgent, compose.END)

graph.Compile(ctx, compose.WithNodeTriggerMode(compose.AllPredecessor))
```

**精华**：用 `AllPredecessor` 触发模式实现**并行分支 + 同步汇聚**：RAG 检索路径和 chat template 组装路径并行，双方都完成才触发下游。Eino Graph 比 Chain 表达力强得多。

### 3.3 Eino ReAct Agent ★★★★
`workflow.go:168-181`

```go
config := &react.AgentConfig{
    MaxStep:            25,
    ToolReturnDirectly: map[string]struct{}{},  // 哪些工具返回即回答
    ToolCallingModel:   chatModel,
    ToolsConfig:        {Tools: tools},
}
agent, _ := react.NewAgent(ctx, config)
compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)  // 同时暴露同步+流式
```

**精华**：`flow/agent/react` 是 Eino 内置的 ReAct 实现，ZhituAgent 现在是**手写 tool-call loop**。切换到 `react.NewAgent` 可以少写很多胶水代码，而且支持 `ToolReturnDirectly`（某些工具执行完直接当答案返回，不再调模型）。

### 3.4 Query Rewrite（有历史时用 LLM 改写查询）★★★★
`workflow.go:184-206`

```go
func (w *Workflow) rewriteLambda(ctx, input) (*Input, error) {
    if len(input.History) == 0 {
        return input, nil  // 无历史不改写
    }
    messages := []*schema.Message{schema.SystemMessage(queryRewritePrompt)}
    messages = append(messages, input.History...)
    messages = append(messages, schema.UserMessage("请将当前问题改写为可直接用于知识库检索的查询：\n"+input.Query))
    msg, _ := w.chatModel.Generate(ctx, messages)
    return &Input{Query: msg.Content, History: input.History}, nil
}
```

**精华**：RAG 检索的老大难——多轮对话里的"它"、"那个"等代词会让向量检索失效。这里用**轻量 LLM 把代词还原为完整查询**再去检索。代价是多一次模型调用，收益是检索命中率显著提升。

### 3.5 LLM 摘要式会话记忆压缩 ★★★★
`internal/ai/workflows/sessionsummary/workflow.go:57-79`

```go
const defaultSystemPrompt = `你是会话记忆压缩助手...
要求：
1. 只保留与后续连续对话相关的事实、约束、偏好、待办、已确认结论和未解决问题。
2. 不要编造原对话中不存在的信息。
3. 输出纯文本，不要使用 markdown。
4. 控制在 300 字以内，内容紧凑。`

func (w *Workflow) Summarize(ctx, previousSummary, turns) (string, error) {
    // "已有摘要 + 待压缩对话" → LLM → 新摘要
}
```

`internal/application/chat/service.go:182-205`：
- **触发时机**：每轮结束后检查，`len(turns) > CompactThreshold (default 9)` 才触发。
- **压缩范围**：`turns[:trimCount]`（超过 retainTurns=6 的老轮次）合并进旧摘要。
- **结果**：Redis meta 存 summary，LTrim 保留最近 N 轮明细。

**ZhituAgent 对比**：现在是"前 3 条消息各截 50 字符"的字符级截取，**毫无语义保留**。这是简历上亮点能直接换真摘要。

### 3.6 Milvus Dense + Sparse 混合检索 ★★★★
`internal/infra/vectorstore/milvus/retriever.go:38-88`

```go
// Dense 向量用 Embedding 模型
denseVec := embedder.EmbedStrings(ctx, []string{query})

// Sparse 向量：gojieba 分词 → FNV 哈希 → 归一化词频（sparse.go:42-68）
sparseVec := buildSparseVector(query)  // 1M 桶

// 两路 ANN + 加权融合
requests := []*cli.ANNSearchRequest{
    cli.NewANNSearchRequest(DenseVectorField, entity.COSINE, ...),
    cli.NewANNSearchRequest(SparseVectorField, entity.IP, ...),
}
results, _ := client.HybridSearch(ctx, collection, nil, topK,
    outputFields,
    cli.NewWeightedReranker([]float64{0.5, 0.5}),  // dense 权重 0.5 / sparse 0.5
    requests)
```

**精华**：**完全不依赖 ES，靠 Milvus 原生 HybridSearch + WeightedReranker 实现关键词+语义双召回**。Sparse 向量的技巧——`FNV32a % 1M` 桶作为稀疏维度，TF 作为值。轻量、无需 BM25 后端。

### 3.7 MCP SSE Client + 优雅降级 ★★★★★
`internal/infra/mcp/client.go`

```go
cli, _ := client.NewSSEMCPClient(p.url)
cli.Start(ctx)
request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
request.Params.ClientInfo = mcp.Implementation{Name: "opspilot", Version: "1.0.0"}
cli.Initialize(ctx, request)
return einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
```

依赖：`github.com/mark3labs/mcp-go`（Go MCP SDK）+ `cloudwego/eino-ext/components/tool/mcp`。

**降级策略**（AGENTS.md:49）：MCP 加载失败不阻塞启动，服务以"无日志工具模式"继续运行。

**这是 MCP 热点**——2025 年 Anthropic MCP 协议爆火，面试必问。

### 3.8 双模型策略（Think + Quick）★★★
- `think_chat_model`：规划、复杂推理（如 Planner / Replanner），一般用 Opus/Claude-Sonnet 量级
- `quick_chat_model`：执行、工具调用、Query Rewrite，用 Haiku / deepseek-chat 量级

配置里分开，装配时分别注入。**既控成本又保质量**。

### 3.9 分布式会话锁（Lua 释放 + Conflict Error）★★★
`internal/infra/session/redis/store.go:169-184`

```go
var unlockScript = goredis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`)

func (s *Store) AcquireLock(ctx, sessionID) (UnlockFunc, error) {
    token := fmt.Sprintf("%d", time.Now().UnixNano())
    ok, _ := s.client.SetNX(ctx, lockKey, token, 30*time.Second).Result()
    if !ok { return nil, session.ErrSessionConflict }  // 语义化冲突错误
    return func(ctx) error {
        unlockScript.Run(ctx, client, []string{lockKey}, token)
    }, nil
}
```

**ZhituAgent 已有类似能力**，但 OpsPilot 把 `ErrSessionConflict` 上升到 domain 层，`application/chat` 捕获后返回 409——更清晰的错误语义。

### 3.10 Eino Callbacks Handler（链路日志）★★★
`internal/infra/logging/callback.go` 实现 `callbacks.Handler`，通过 `compose.WithCallbacks(...)` 注入到每条 workflow。

**价值**：标准化 Graph 每个节点的 onStart/onEnd/onError 钩子，天然支持 Tracing 埋点。ZhituAgent 的 Prometheus 埋点现在是手写 record 函数，可以迁移到 Callbacks 统一接入点。

### 3.11 Workflow 启动期预编译 ★★★
所有 workflow 在 `bootstrap.New` 阶段 `graph.Compile(...)`，**运行时直接 `runner.Invoke`**。Eino 的 `Compile` 做类型检查、拓扑排序、代码生成，放到请求路径会有明显延迟。

## 4. 对 ZhituAgent 的启示

### 候选 A：用 Eino Plan-Execute-Replan Agent 重构 multiAgentChat ★★★★★
- **描述**：现在的 `multiAgentChat` 是"关键词匹配 → 知识增强 → 主链路"，本质上是单轮的，称不上真 Agent。换成 `adk/prebuilt/planexecute` 后，模型会先**规划步骤**，再**执行工具**，遇阻时**重规划**。
- **借鉴位置**：`internal/ai/workflows/aiopsplan/workflow.go:61-100`。
- **Go+Eino 可行性**：**极低成本**——直接用 Eino 内置组件，主要是拼装。
- **升级 or 新增**：升级 `multiAgentChat`。
- **简历**：`基于 Eino ADK Plan-Execute-Replan 三件套重构多 Agent 对话：Planner 拆解任务 → Executor（ReAct Agent + 工具集，MaxStep=20）执行 → Replanner 根据中间结果动态调整计划，整体循环 MaxIterations=10`。
- **面试深入**：能讲 Plan-and-Execute vs ReAct vs CoT 的差别、Replanner 的触发条件、MaxIterations 设计的防失控作用、为什么 Planner 用强模型 Executor 用便宜模型。
- **深度评分**：5/5。

### 候选 B：Eino Graph 替换手写编排 + ReAct Agent 替换手写 tool-loop ★★★★★
- **描述**：ZhituAgent 的 tool 调用是**手写 loop**（memory 的 API 文档里有记载）。迁移到 `flow/agent/react.NewAgent` + `compose.NewGraph` 能吃到框架的所有能力（Stream、Callbacks、ToolReturnDirectly、MaxStep）。
- **借鉴位置**：`internal/ai/workflows/chat/workflow.go:113-181`。
- **Go+Eino 可行性**：中成本（重构现有 chat 链路）。
- **升级 or 新增**：升级 `ChatService`。
- **简历**：`基于 Eino Graph + ReAct Agent 重构对话编排：Graph 并行分支（RAG 检索 + 历史改写）AllPredecessor 汇聚到 ChatTemplate，ReAct Agent 统一承载工具调用与最终生成，支持流式响应`。
- **面试深入**：能讲 Eino Chain vs Graph 的表达力差异、AllPredecessor vs AnyPredecessor 的触发语义、ReAct 的 Thought-Action-Observation 循环。
- **深度评分**：5/5。

### 候选 C：Query Rewrite 改写用户查询再检索 ★★★★★
- **描述**：ZhituAgent 的 RAG 直接用原始用户 query 去检索，多轮代词场景命中率差。加一层 LLM 改写。
- **借鉴位置**：`workflow.go:184-206` 的 `rewriteLambda`。
- **Go+Eino 可行性**：**极低成本**，加一个 Lambda 节点即可。
- **升级 or 新增**：升级 `rag.Retrieve`。
- **简历**：`RAG 检索前置 Query Rewrite：多轮对话中有历史时调用轻量模型（Qwen-Turbo）将代词还原为完整查询，提升向量召回命中率`。
- **面试深入**：能讲多轮对话 RAG 的核心难题（指代消解）、Query Rewrite vs HyDE（假设文档扩展）的权衡、为什么用 quick 模型而不是 think 模型。
- **深度评分**：5/5（ROI 极高）。

### 候选 D：LLM 摘要式会话记忆压缩 ★★★★★
- **描述**：ZhituAgent 现在是 `取前 3 条消息各截 50 字符`（CLAUDE.md 明确写了"不要升级"——但 Phase 2 恰好是升级它的时机）。换成 `sessionsummary.Workflow`：超过 9 轮后 LLM 把前 `N-6` 轮压缩成 ≤300 字摘要，只保留最近 6 轮明细。
- **借鉴位置**：`internal/ai/workflows/sessionsummary/workflow.go` + `internal/application/chat/service.go:182-205` 的 `appendAndCompact`。
- **Go+Eino 可行性**：低成本（新增一个 workflow + 在 ChatService 里接入）。
- **升级 or 新增**：升级 `memory.Compress`（⚠️需要和你现在的契约 "简单截取"协商 —— 这是 Phase 2 的升级机会）。
- **简历**：`会话记忆压缩策略升级：从字符截取改为 LLM 摘要压缩，保留最近 6 轮明细 + 摘要合并策略，滚动压缩防止上下文膨胀，超阈值 9 轮触发`。
- **面试深入**：能讲 Token 成本 vs 上下文质量的权衡、Summary 的提示工程（"不编造、只保留关键事实"）、摘要-明细双层结构 vs 纯摘要的对比、长对话的 Context Window 膨胀问题。
- **深度评分**：5/5。

### 候选 E：MCP Client 接入 + 工具动态加载 ★★★★★
- **描述**：ZhituAgent 的工具是 **Go 代码里硬编码的 TimeTool/EmailTool/RagTool**。接入 MCP 后，工具可以来自**任何实现了 MCP 协议的外部服务**（你之前看到的 `ai-mcp-gateway` 就是做这个的），而且**Eino 原生支持**（`cloudwego/eino-ext/components/tool/mcp`）。
- **借鉴位置**：`internal/infra/mcp/client.go` 全文。
- **Go+Eino 可行性**：低成本。加一个 `internal/infra/mcp/client.go`，在工具注册时把 MCP 工具追加到 local 工具列表。
- **升级 or 新增**：新增（扩展 ZhituAgent 的工具生态）。
- **简历**：`接入 Model Context Protocol（MCP）SSE Client，动态拉取远端工具定义并并入 Eino Agent 的工具集；MCP 加载失败自动降级为本地工具模式，不阻塞服务启动`。
- **面试深入**：能讲 MCP 协议设计（SSE + Initialize 握手 + 工具发现）、为什么 Anthropic 推 MCP（解决 LLM 工具生态孤岛）、MCP vs OpenAPI vs Tool Schema 的差别、降级策略的必要性。
- **深度评分**：5/5（2025-2026 最热的 Agent 协议，面试强题材）。

### 候选 F：Eino Callbacks 统一可观测 ★★★★
- **描述**：ZhituAgent 的 Prometheus 指标是**手写在每个 service 的入口**（`record` 函数）。换成 Eino Callbacks 后，所有 Graph 节点自动带埋点。
- **借鉴位置**：`internal/infra/logging/callback.go` 实现 `callbacks.Handler`。
- **Go+Eino 可行性**：低-中成本。
- **升级 or 新增**：升级 `monitor`。
- **简历**：`基于 Eino Callbacks 接口实现统一可观测层，标准化 Graph 所有节点的 onStart/onEnd/onError 埋点，自动上报 Prometheus 指标和链路追踪 Span`。
- **面试深入**：能讲 Callbacks 模式 vs Middleware 模式、OTel Trace 的 Span 语义如何映射到 Graph 节点。
- **深度评分**：4/5。

### 候选 G：双模型策略（Think + Quick）★★★
- **描述**：ZhituAgent 现在只用一个 qwen-max。加个 `qwen-turbo` 或 `qwen-plus` 作为 quick model，路由：Planner / Rerank 走 think model，Query Rewrite / 工具参数填充走 quick model。
- **借鉴位置**：OpsPilot 配置里的 `think_chat_model` / `quick_chat_model`。
- **Go+Eino 可行性**：低成本。
- **升级 or 新增**：升级 LLM Provider。
- **简历**：`设计 Think/Quick 双模型分工架构：复杂规划走 qwen-max、轻量任务（Query Rewrite、摘要压缩）走 qwen-turbo，单次对话成本降低 X%`。
- **面试深入**：能讲成本/质量曲线、模型选型方法论。
- **深度评分**：3/5（偏工程优化）。

### 候选 H：AIOps 场景（Prometheus Alerts Tool）★★
- **描述**：如果你想给 ZhituAgent 加业务亮点，可以接一个 Prometheus Alerts 查询工具 + 多工具协作的告警分析流。**但这会扭转项目定位**，慎重。
- **深度评分**：2/5（场景绑定强，不通用）。

## 5. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **C. Query Rewrite** | ★★★★★ | **极低** | 加个 Lambda，RAG 命中率立刻上去，面试 1 分钟能说清 |
| 🥈 | **D. LLM 摘要记忆压缩** | ★★★★★ | 低 | 直接把 CLAUDE.md 里"简单截取"的契约升级，故事线完整 |
| 🥉 | **E. MCP Client** | ★★★★★ | 低 | 2025-2026 最热话题，Eino 原生支持零门槛 |
| 4 | **A. Plan-Execute-Replan** | ★★★★★ | 低 | Eino 内置，拼装即可；把 multiAgent 换成真 Agent |
| 5 | **B. Eino Graph + ReAct** | ★★★★★ | 中 | 大重构但收益极大；A/C/D 落地时可能自然带出 |
| 6 | **F. Callbacks 可观测** | ★★★★ | 中 | B 落地后顺带做 |
| 7 | **G. 双模型策略** | ★★★ | 低 | A/C/D 落地后顺便加一行配置 |

**建议组合**：**C + D + E + A 构成 Phase 2 第一轮**，然后**B 做第二轮架构重构**把 C/D/E/A 全部纳入 Graph 统一编排。**OpsPilot 是 ZhituAgent Phase 2 的北极星参考**——几乎每个亮点都能直接借鉴。
