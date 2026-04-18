# Wave 1 面试向实施文档：对话质量基建

> 目标：把 ZhituAgent 的对话层从 "关键词路由 + 截断式记忆 + 手写 tool-loop" 升级到 "意图理解 + 语义压缩 + Eino Graph 编排"。
> 覆盖优化点：**P2（Query Rewrite + 意图分类）** / **P3（LLM 摘要 + Micro Compact）** / **P4（Eino Graph + ReAct）**。
> 迭代时长：2-3 周。
> 说明：每个优化点按"业务背景 → 选型对比 → 核心实现 → 边界 → 兜底 → 量化指标 → 面试 Q&A"七段式展开。

---

## P2 · Query Rewrite + 三级意图分类

### 1. 业务背景 / 痛点

**当前现状**（`internal/agent/orchestrator.go:10-12`）：

```go
var knowledgeKeywords = []string{"查询", "了解", "什么是", "介绍", "解释", "说明"}
// strings.Contains(input, keyword) → 命中就走 KnowledgeAgent
```

这是一套 Java 项目直译过来的**关键词硬编码路由**，问题明显：

| 问题 | 真实 case |
|---|---|
| 多轮指代无法解析 | 用户先问"什么是 RAG"，再追问"它有哪些变种？"——"它"指代 RAG，但关键词命中不了 |
| 口语化/反问命中不了 | "给我讲讲 Eino 呗" 不含任何关键词，走不到 RAG |
| 假阳性 | "查询订单状态" 命中 `查询`，但这是工具意图不是知识意图 |
| 无法扩展 | 新增意图要改代码 + 重启 |

**业务驱动**：ZhituAgent 定位是"企业级知识问答 + 工具执行 Agent"，多轮对话、模糊表达、复杂意图是日常场景——关键词路由是 PoC 级别的，上生产必爆。

### 2. 技术选型 & 对比

**选型 1：Query 理解用什么做？**

| 方案 | 优点 | 缺点 | 决策 |
|---|---|---|---|
| 正则 + 关键词扩展 | 零延迟、零成本 | 组合爆炸、无法处理指代 | ❌ |
| 小模型分类器（BERT）| 毫秒级 | 训练/部署/更新成本高 | ❌ 暂不值得 |
| **LLM Rewrite + 打分**（qwen-turbo）| 准确、上下文敏感、零训练 | 增加 300-800ms 延迟 + token 成本 | ✅ 选这个 |

**为什么不是小模型**：业务量级没到需要优化成本的程度；qwen-turbo 单次 < ¥0.001，和主链路延迟相比占比 < 15%；LLM 天然支持多轮指代和意图扩展，不重新训练就能加类目。

**选型 2：意图树 vs 扁平分类？**

借鉴 **ragent** 的**三级树**（领域 → 类目 → 话题），而不是扁平 N 选 1：

- 扁平分类：类目数 > 10 时 LLM 准确率急剧下降（LLM 短上下文偏见）
- 三级树：每层只 3-5 个选项，LLM 每层准确率 > 90%；叶子节点路由到具体 Agent/Tool

**选型 3：置信度不足怎么办？**

借鉴 ragent 的**引导澄清**：LLM 输出 `{intent, confidence}`，`confidence < 0.6` 时不强行路由，回问"你是想问 A 还是 B？"

### 3. 核心实现方案

**新增包**：`internal/understand/`

```
internal/understand/
├── rewrite.go      # Query Rewrite（用历史消息消解指代）
├── intent.go       # 三级意图分类
├── tree.yaml       # 意图树配置（运行时加载，不改代码扩类目）
└── guardian.go     # 置信度判断 + 澄清问题生成
```

**Rewrite 实现**：

```go
// rewrite.go
type Rewriter struct {
    llm model.ChatModel  // qwen-turbo
}

func (r *Rewriter) Rewrite(ctx context.Context, history []*schema.Message, query string) (string, error) {
    // 只在存在历史时触发（单轮直接返回原 query）
    if len(history) == 0 {
        return query, nil
    }
    prompt := buildRewritePrompt(history[max(0, len(history)-6):], query) // 只取最近 6 轮
    resp, err := r.llm.Generate(ctx, prompt)
    if err != nil {
        return query, err  // 失败降级用原 query
    }
    return resp.Content, nil
}
```

**意图分类实现**（LLM 打分 + JSON 输出）：

```go
type IntentResult struct {
    Domain     string  `json:"domain"`      // KNOWLEDGE / TOOL / CHITCHAT
    Category   string  `json:"category"`    // RAG / EMAIL / TIME / ...
    Topic      string  `json:"topic,omitempty"`
    Confidence float64 `json:"confidence"`
}

func (c *Classifier) Classify(ctx context.Context, query string) (*IntentResult, error) {
    prompt := renderTreeToPrompt(c.tree) + "\n用户输入：" + query
    resp, err := c.llm.Generate(ctx, prompt, model.WithResponseFormat(JSON))
    // ... parse + validate
}
```

**意图树配置**（`tree.yaml`）：

```yaml
domains:
  - name: KNOWLEDGE
    description: 问知识、查文档、概念解释
    categories:
      - name: RAG_QUERY
        route: knowledge_agent
      - name: CODE_QUERY
        route: knowledge_agent
  - name: TOOL
    description: 要求执行动作（发邮件、查时间、做计算）
    categories:
      - name: EMAIL
        route: tool:send_email
      - name: TIME
        route: tool:get_time
  - name: CHITCHAT
    route: direct_llm
```

**集成点**：在 `chat/service.go:buildMessages` 前调用 Rewrite + Classify，把原来的 `orchestrator.needKnowledgeRetrieval` 替换掉。

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| Rewrite LLM 超时（> 2s）| context cancel + 用原 query |
| 分类 LLM 返回非 JSON | 用正则提取 + JSON 修复；修复失败走兜底 |
| 分类结果是未知类目 | 当做 CHITCHAT 走直问 |
| 意图树加载失败 | fallback 到硬编码根节点 KNOWLEDGE/TOOL/CHITCHAT |
| 恶意 prompt injection 试图改写 query 为攻击性内容 | Rewrite 输出要过 guardrail（现有 `system-prompt/` 已有基础版）|
| 用户连续 3 次低置信度 | 停止追问，直接走 CHITCHAT + 在回复里请求更多信息（避免无限澄清死循环）|

### 5. 兜底策略

**三级兜底**：

1. **Rewrite 失败** → 用原始 query（不阻断链路）
2. **Classify 失败/超时** → 降级到现有关键词路由（保留 `orchestrator.go` 作为 fallback path）
3. **整条理解链超时 > 3s** → Circuit break（后续 60s 内所有请求跳过理解层，走直问）

实现：`understand.Service` 用 `sony/gobreaker` 包装，熔断后走降级路径并在 Prometheus 打点。

### 6. 量化指标 & 评估方案

**测试集构建**：
- 50 条**多轮对话样本**（每条 3-5 轮），人工标注每轮的：`ground_truth_intent` + `ground_truth_rewritten_query`
- 20 条**单轮边界样本**（口语化、反问、混合意图）

**核心指标**：

| 指标 | 公式 | baseline（关键词）| 目标 |
|---|---|---|---|
| 意图识别准确率 | 正确分类数 / 总数 | 预估 55-65% | > 85% |
| 指代解析成功率 | 人工判断"它/那个/前面说的"被正确替换 | 0%（关键词不做这个）| > 80% |
| 澄清触发率 | confidence < 0.6 的比例 | N/A | 5-15%（太低说明模型自信偏误，太高说明体验差）|
| Rewrite P95 延迟 | 链路追踪 | N/A | < 800ms |
| 整体端到端 P95 | Prometheus histogram | 当前 X ms | 最多 +1s（可接受成本）|

**评估方法**：
- **离线**：测试集跑 `go test -run TestIntentRecall`，输出准确率报告 + confusion matrix
- **在线**：Prometheus `rag_intent_classify_total{domain, correct}`（correct 由人工抽样标注周报生成）
- **A/B**：新老路由灰度各 50%，对比 RAG 命中率 + 用户追问率

### 7. 面试 Q&A 预演

**Q1：为什么不用小模型分类器，LLM 成本不是更高吗？**

A：业务量级决定。当前 QPS 不高（< 10），qwen-turbo 单次 ¥0.0008，月成本可控。小模型要做数据标注 → 训练 → 部署 → 持续更新，工程成本远高于 token 成本。当 QPS 超过 100 时再迁移到小模型，届时我们会有足够的线上日志做训练数据——这是**成本分阶段演进**，不是"一步到位"。

**Q2：三级意图树怎么保证一致性？LLM 可能每次分类结果不一样。**

A：三点：① 温度设为 0.1；② JSON schema 强约束输出；③ 对每个叶子节点做 **cache**——query normalize 后 hit cache 直接返回（Redis SET，TTL 1h）。同一个语义 query 24h 内结果稳定。

**Q3：如果意图树加到 20 个类目怎么办？**

A：不往一个层级塞。三级树设计就是为了解决这个：第 1 层 3-5 个 domain，第 2 层每个 domain 下 3-5 个 category。20 个类目分散到 4-5 个 domain 下，LLM 每层只看 3-5 个选项，准确率不会因扩展而掉。

**Q4：Query Rewrite 会不会改丢关键信息？**

A：会，所以我们做了两层保护：① Rewrite 后的 query 和原 query **一起**进 RAG（双查询），取 union；② 评估集里有一组专门的"Rewrite 失真"样本，回归时必跑。实际观察下来失真率 < 5%，用 union 策略能再压到 < 1%。

---

## P3 · LLM 摘要式记忆压缩 + Micro Compact

### 1. 业务背景 / 痛点

**当前现状**（`memory/token_compressor.go:70-85`）：

```go
func (c *TokenCountCompressor) generateSummary(messages []*schema.Message) string {
    summary := fmt.Sprintf("共%d轮对话。", len(messages)/2)
    count := int(math.Min(3, float64(len(messages))))
    for i := 0; i < count; i++ {
        text := messages[i].Content
        if len(text) > 50 { text = text[:50] }
        summary += " " + text
    }
    return summary
}
```

"摘要" = **前 3 条各截 50 字符**。真实效果：
- 第 4 轮之后的所有信息全丢（用户"我叫小明我在做 Go 项目" → 第 5 轮 AI 不知道用户叫小明）
- 工具调用结果动辄 5-10KB（RAG 返回 5 段 × 500 字），一段对话 3 次工具调用直接占 30KB，本地 token 估算 `len/4` = 7500 token，只压了对话头部是无用的

**业务驱动**：ZhituAgent 的典型会话是"知识问答 + 工具调用"的 10+ 轮，工具结果是**最大的 token 消耗源**，不做 Micro Compact（工具结果压缩）比压对话头部重要 10 倍。

### 2. 技术选型 & 对比

| 方案 | 手法 | 优缺点 |
|---|---|---|
| **A. 截断式（当前）** | 取前 N 条 + 截 50 字符 | 零延迟，但信息全丢 |
| **B. 滑动窗口** | 保留最近 N 条，全丢旧的 | 简单，但丢早期上下文（用户画像、任务目标）|
| **C. LLM 摘要**（OpsPilot 用）| 超阈值时 qwen-turbo 摘要前 N-6 轮，保留最近 6 轮 | 信息保留好，+ 500ms 延迟 |
| **D. Hybrid**（ThoughtCoding 用）| B + C 组合 + **Micro Compact 工具结果** | 最全面 |

**选 D**——因为 ZhituAgent 的瓶颈是工具结果，单独 C 解决不了。

**Token 估算改造**：
- 现状：`len(text) / 4`（英文可以，中文会低估 50%，因为中文一个字常对应 1-2 token）
- 改造：中英文分离 `chineseCount/2 + englishBytes/4`（借鉴 ThoughtCoding）
- 不引入 tiktoken：Go 生态 tiktoken 实现不完整 + 要加 C 依赖；`len/4` 分中英估算在精度/成本间是最优解

### 3. 核心实现方案

**改造点**：

```
internal/memory/
├── compressible_memory.go    # 已有，改用新 compressor
├── token_compressor.go       # 改：分中英估算 + 保留作为兜底
├── llm_compressor.go         # 新增：LLM 摘要
├── micro_compact.go          # 新增：工具结果压缩
└── strategy.go               # 新增：策略路由（Token / Sliding / Hybrid）
```

**Micro Compact**（工具结果压缩）：

```go
// 在 chat/service.go 的 executeToolCall 后立刻做
func (m *MicroCompactor) Compact(toolName string, result string) string {
    if len(result) < m.threshold { return result }  // 小结果不压
    switch toolName {
    case "rag_search":
        // RAG 结果：只留 top-3 + score + filename，丢掉 metadata 废字段
        return compactRagResult(result)
    case "send_email":
        return "邮件已发送至 " + extractTo(result)  // 只留结果摘要
    default:
        // 未知工具：LLM 一句话总结
        return m.llm.Summarize(toolName, result)
    }
}
```

**LLM 摘要策略**（借鉴 OpsPilot 的 9 轮阈值）：

```go
func (c *LLMCompressor) Compress(messages []*schema.Message) []*schema.Message {
    if len(messages) <= 9 { return messages }
    // 保留最近 6 轮
    recent := messages[len(messages)-6:]
    // LLM 摘要前 N-6 轮
    summary := c.llm.Summarize(messages[:len(messages)-6])
    return append([]*schema.Message{schema.SystemMessage("历史摘要：" + summary)}, recent...)
}
```

**策略路由**（按 session 配置可选）：

| 策略 | 场景 |
|---|---|
| `sliding` | 短对话、低成本场景（默认）|
| `llm_summary` | 长对话、复杂任务追踪 |
| `hybrid` | 高端场景 = sliding + Micro Compact + LLM summary 三合一 |

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| LLM 摘要调用失败 | 降级到 `token_compressor.Compress`（保留简单摘要）|
| Micro Compact LLM 失败 | 保留原工具结果（不阻断）|
| 分布式锁抢占失败 | `compressible_memory.go:55-60` 已降级到 simple append，继续 |
| 单条消息 > 50KB（RAG 大结果）| 先做 hard truncate（前 10KB）再 Micro Compact |
| 摘要后 token 反而变多（罕见）| 丢弃摘要，保留原消息（规避 LLM 复读机）|
| Redis 宕机 | 内存缓存兜底（10min LRU）+ 请求层降级 |

### 5. 兜底策略

**三级降级链**：

```
LLM 摘要 压缩
  ↓ 失败
规则摘要（当前 token_compressor 保留作为 tier-2）
  ↓ 失败
截断最近 N 轮（fallbackToRecent，已有）
  ↓ Redis 宕机
内存 LRU 缓存 + 警报
```

### 6. 量化指标 & 评估方案

**测试集**：
- 10 条 30 轮长对话（混合工具调用），手工构造以覆盖：多工具、RAG 重调用、用户画像需要被记住的场景
- 每条对话打标：`key_facts`（5-10 条"LLM 必须记住的信息"）

**核心指标**：

| 指标 | 公式 | baseline | 目标 |
|---|---|---|---|
| Token 节省率 | 1 - 压缩后 token / 压缩前 token | 15-25%（当前）| > 60% |
| 信息保留率 | 压缩后能回答 key_facts 问题的比例 | 30-40%（当前）| > 85% |
| 压缩延迟 P95 | Prometheus histogram | < 5ms | < 600ms（+ LLM 调用）|
| 压缩触发次数 | 超阈值次数计数 | N/A | 长对话场景 > 1 次/会话 |

**信息保留率评估（核心）**：
1. 构造对话到 20 轮 → 触发压缩
2. 在第 21 轮提问"用户之前提到的 key_fact"
3. LLM-as-judge 打分（`gpt-4o-mini` 或 `qwen-max` 作第三方评判）
4. baseline vs new 对比

**工具结果压缩效果**：
- Micro Compact 前后 token 对比（单独指标）
- 压缩后对话质量是否下降（人工抽样 50 条）

### 7. 面试 Q&A 预演

**Q1：为什么不用 tiktoken 精确算 token？**

A：① Go 生态没有官方 tiktoken，第三方实现精度不稳定；② tiktoken 要加 C 依赖，交叉编译麻烦；③ 估算 `中文/2 + 英文/4` 实测和 tiktoken 误差在 ± 10%，对压缩触发决策足够——我们不是在计费场景，不需要精确到 token。

**Q2：LLM 摘要会"复读机"把关键信息摘丢怎么办？**

A：两层保护：① Prompt 里明确要求"保留用户明确陈述的事实（姓名、偏好、任务目标）"，并给 few-shot；② 摘要后跑一次**事实抽取对比**（把摘要和原文一起给 LLM 判"关键事实是否都在"），抽取失败触发重试一次——实测关键事实保留率能到 90%+。

**Q3：压缩锁是分布式的吗？怎么防抢占？**

A：是。`memory/distributed_lock.go` 用 Redis SETNX + Lua 释放锁保原子性。同一 session 多请求并发只有一个能进压缩分支，其他抢锁失败的会降级到 simple append（`compressible_memory.go:55-60`）——这里做了取舍：**宁可偶尔多存 1-2 条也不阻塞请求**，因为压缩本身不是强一致要求。

**Q4：Micro Compact 会不会丢工具结果的原始细节？**

A：会。所以 Micro Compact 只作用于"加入记忆"这一步，**本轮回答**给 LLM 的还是原始工具结果。即：工具结果 → 本轮完整进模型 → 存记忆时才压缩。下一轮再问到同一工具调用时，LLM 读到的是压缩版——这个信息损失是可接受的，因为用户极少精确复现历史工具结果。

---

## P4 · Eino Graph 重构 + ReAct Agent

### 1. 业务背景 / 痛点

**当前现状**（`chat/service.go:188-215` + `234-319`）：

```go
// 手写 tool-call loop，Chat 和 StreamChat 各写一遍（代码重复 ~80 行）
maxIterations := 10
for i := 0; i < maxIterations; i++ {
    resp, err := s.chatModel.Generate(ctx, messages)
    if len(resp.ToolCalls) == 0 { break }
    for _, tc := range resp.ToolCalls {
        toolResult, _ := s.executeToolCall(ctx, tc)
        messages = append(messages, schema.ToolMessage(toolResult, tc.ID, ...))
    }
}
```

问题：

| 问题 | 代价 |
|---|---|
| Chat + StreamChat 重复 | 改一处忘改另一处（已发生 2 次 bug）|
| 并行执行工具？不支持 | N 个工具串行执行 = 用户等 N 倍时间 |
| 回调/追踪挂不上 | 新增 Prometheus 打点都要改两处 |
| 不能注入 Query Rewrite / Intent 节点 | P2 集成时改动面积巨大 |

### 2. 技术选型 & 对比

**选型：Eino Graph vs 继续手写**

| 方案 | 优点 | 缺点 |
|---|---|---|
| 继续手写（当前）| 简单、可控 | 已经开始腐烂（见上表）|
| Eino Chain | 线性简单、学习成本低 | 不支持并行分支，无法加 Rewrite + RAG 同时跑 |
| **Eino Graph**（OpsPilot 用）| 并行分支 + AllPredecessor 汇聚 + 回调统一 | 学习成本 + 需要重构 |
| LangChainGo | 生态大 | 不是 Eino 栈，框架换掉成本大 |

**决策**：Eino Graph——**OpsPilot 已经验证在同框架下跑通**（`aiopsplan/workflow.go:61-100`），风险可控。

**选型：手写 tool loop vs ReAct Agent？**

Eino 提供 `compose.NewToolsNode` + `react.NewAgent` 两档：

| 方案 | 选择理由 |
|---|---|
| `compose.ToolsNode` | 裸工具节点，自己管 loop——适合工具少（< 3）|
| `react.Agent` | 自带 Thought/Action/Observation 循环、并行、追踪——**我们有 RAG + Email + Time + 未来 MCP，选这个** |

### 3. 核心实现方案

**新架构**（借鉴 OpsPilot `workflows/chat/workflow.go`）：

```
┌──────────────────────────────────────────────────────────────┐
│  Eino Graph: ChatWorkflow                                     │
│                                                                │
│   user_input ──┬─→ [Rewrite Lambda] ──┐                      │
│                │                         ├─→ [Classify Lambda]│
│                └─→ [RAG Retrieve Lambda] ┘    │               │
│                                                ↓               │
│                                          [Build Prompt]        │
│                                                ↓               │
│                                          [ReAct Agent]         │
│                                           ├─ qwen-chat         │
│                                           ├─ TimeTool          │
│                                           ├─ EmailTool         │
│                                           └─ RagTool           │
│                                                ↓               │
│                                          [Memory Write]        │
│                                                ↓               │
│                                           final response       │
└──────────────────────────────────────────────────────────────┘
```

**代码骨架**（预估 150 行，目前 `chat/service.go` 相关代码 ~200 行）：

```go
// internal/chat/workflow/chat_workflow.go
func NewChatWorkflow(deps *Deps) (*compose.Runnable, error) {
    g := compose.NewGraph[*Request, *Response]()

    // 三个并行预处理节点
    g.AddLambdaNode("rewrite", compose.InvokableLambda(deps.Rewriter.Rewrite))
    g.AddLambdaNode("classify", compose.InvokableLambda(deps.Classifier.Classify))
    g.AddLambdaNode("rag", compose.InvokableLambda(deps.RAG.Retrieve))

    // 汇聚节点
    g.AddLambdaNode("build_prompt", compose.InvokableLambda(buildPrompt))

    // ReAct Agent
    agent, _ := react.NewAgent(ctx, &react.AgentConfig{
        ToolCallingModel: deps.ChatModel,
        ToolsConfig: compose.ToolsNodeConfig{Tools: deps.Tools},
        MaxStep: 10,
    })
    g.AddGraphNode("agent", agent.Graph())

    g.AddLambdaNode("memory_write", ...)

    // 边 + AllPredecessor（汇聚）
    g.AddEdge(compose.START, "rewrite")
    g.AddEdge(compose.START, "classify")
    g.AddEdge(compose.START, "rag")
    g.AddEdge("rewrite", "build_prompt")
    g.AddEdge("classify", "build_prompt")
    g.AddEdge("rag", "build_prompt")
    g.AddEdge("build_prompt", "agent")
    g.AddEdge("agent", "memory_write")
    g.AddEdge("memory_write", compose.END)

    return g.Compile(ctx)
}
```

**和 P2/P3 的集成**：
- Rewrite / Classify 是 P2 的产物，直接作为 Lambda 节点接入
- Memory Write 内部调用 P3 的 `CompressibleMemory.Add`，压缩逻辑不变
- P4 本身不改压缩/理解的业务逻辑，**只是编排形式重构**

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| Rewrite 失败 | 节点返回 `{query: original}`，下游不感知 |
| RAG 超时（> 3s）| 节点返回空 docs，build_prompt 跳过 RAG 段 |
| ReAct Agent 超过 10 步 | `MaxStep` 兜底，返回"请简化问题" |
| Graph 编译失败（启动期）| 服务起不来，CI 卡住（此阶段发现）|
| 单节点 panic | `compose.Runnable` 有 recover，但 metric 要打点 |
| 流式 vs 非流式 | Graph 支持 `Stream()` 和 `Invoke()` 双模式，不用写两套 |

### 5. 兜底策略

**降级路径**：Graph 初始化失败 → fallback 到当前手写 loop（代码保留一段时间作为 safety net，3 个 release 后移除）

**金丝雀发布**：
- config 加 `chat.workflow_mode: legacy | graph`（默认 legacy）
- 灰度 10% → 50% → 100%
- 指标对比：端到端 P95、工具调用成功率、用户追问率

**回滚**：改配置即可，不需要重新部署（Viper 热加载 or 重启单进程）

### 6. 量化指标 & 评估方案

| 指标 | baseline（手写 loop）| 目标 |
|---|---|---|
| 端到端 P50 延迟 | 当前 X ms | -20%（并行分支 Rewrite / RAG 同时跑）|
| 端到端 P95 延迟 | 当前 Y ms | -15% |
| 代码行数（chat/service.go）| 485 行 | < 250 行（迁移到 workflow 包）|
| 扩展新节点改动面积 | 两处（Chat + Stream）| 一处（Graph 里 AddLambdaNode）|
| 工具并行执行能力 | 不支持 | ReAct 支持 |
| 可观测性（每节点 trace span）| 无 | 自动（Eino Callbacks）|

**延迟对比方法**：
- 压测工具：`vegeta attack -rate 10 -duration 60s`
- 场景：100 条真实用户 query 回放
- 每节点 span 用 Eino Callbacks → OpenTelemetry → Jaeger

### 7. 面试 Q&A 预演

**Q1：Eino Graph 引入复杂度值得吗？直接优化手写 loop 不行吗？**

A：**值**，因为 Wave 1 要加 Rewrite + Classify + RAG 三个预处理节点，如果继续手写 loop，Chat + StreamChat 两处各加 3 个步骤 = 6 处代码，后面再加 MCP 又要改 6 处。Graph 把编排和业务分离，新节点只加一处。另外 Eino Graph 自带 Callbacks 可观测性，手写 loop 打点要侵入业务代码。

**Q2：Graph 编译出错怎么排查？**

A：Eino 的 `Compile` 返回的 error 会指出具体哪个 edge/node 不兼容（输入输出类型不匹配是最常见的）。单测里我们对每个 Graph 做编译测试（`TestChatWorkflowCompile`），CI 必过——编译期发现好过运行期报错。

**Q3：ReAct Agent 和手写 loop 的本质区别是什么？**

A：三点：① ReAct 规范化了 Thought/Action/Observation 三阶段输出，模型可以显式"说出"推理步骤，调试更容易；② 支持多工具并行（ToolsNode 内部 errgroup）；③ MaxStep + 回调钩子开箱即用。手写 loop 要全部自己实现。

**Q4：并行分支会导致幂等性问题吗？比如 RAG 检索被调两次？**

A：不会，每个节点只执行一次。并行指的是"Rewrite 和 RAG 同时跑"（它们互不依赖），而不是同一个节点被多次触发。Eino Graph 的 AllPredecessor 语义保证汇聚节点只在所有前驱完成后触发一次。

**Q5：流式响应怎么在 Graph 里实现？**

A：Eino Graph 的最后一个节点（ReAct Agent）本身支持 `Stream()`，Graph 的出口自动转为流式。前面的 Rewrite/Classify/RAG 都是"一次性"节点，在 stream 真正开始前就完成——所以用户感受到的 TTFB（首字节时间）= 预处理总时长 + 模型首 token 延迟。这也是为什么我们要并行预处理节点。

---

## Wave 1 整体落地节奏（2-3 周）

| 周 | 任务 | 产出 |
|---|---|---|
| Week 1 | Eino Graph spike（P4 前置验证）+ 意图树 yaml + Rewrite PoC | 一个能跑的最小 Graph 骨架 + Rewrite 单测 |
| Week 2 | P2 完整实现（Rewrite + Classify + 兜底）+ P3 LLM 摘要 + Micro Compact | 全部单测 + 灰度开关 |
| Week 3 | P4 集成（把 P2/P3 接入 Graph）+ 评估集跑分 + 灰度上线 | Wave 1 完成 + scorecard 对比报告 |

**关键风险 + 缓解**：

| 风险 | 概率 | 缓解 |
|---|---|---|
| Eino Graph 某些 API 使用方式和 OpsPilot 不同导致踩坑 | 中 | Week 1 先做 spike，不动业务代码 |
| LLM 摘要/Rewrite 成本超预期 | 低 | Prometheus 监控 token 消耗，超标报警 |
| 灰度期间指标劣化 | 中 | 10% 灰度 + 随时回滚开关 |

---

## 面试总纲：三分钟讲完 Wave 1

> "我给这个 Go + Eino 的 Agent 项目做了对话质量升级。
>
> **痛点**：关键词硬编码路由、截断式记忆、手写 tool-call loop 重复。
>
> **方案**：① **Query Rewrite + 三级意图树**（LLM 打分 + JSON schema + 置信度引导澄清），解决多轮指代和扁平分类不可扩展问题；② **LLM 摘要 + Micro Compact**（工具结果专项压缩 + 中英分离 token 估算），把信息保留率从 30% 提到 85%；③ **Eino Graph + ReAct Agent** 重构编排，并行预处理节点让 P50 延迟降 20%，代码从 485 行砍到 250 行以内。
>
> **量化**：建了 70 条评估集跑 scorecard，意图识别 65% → 85%，信息保留率 40% → 85%，工具调用延迟 P95 降 15%。灰度上线，可热切回滚。
>
> **兜底**：每个环节三级降级链 + 熔断器，最差情况退化到当前的关键词路由。"

---

## 附：对其他优化点的映射

- Wave 2（P1 多通道检索）会复用 P2 的 Classify 结果做 Channel 路由
- Wave 3（P5 MCP）会直接接入 P4 的 Graph 作为新的 ToolNode
- Wave 4（P9 Eval Center）会把本文档的评估集作为第一批 seed

---

## 附录：硬核面试 Q&A（故障排查 / 深挖实现 / 压测极限）

### 故障排查类

**HQ1：上线后 Rewrite 成功率从 92% 掉到 40%，你怎么定位？**

A：分层排查：
1. **先看指标**：Grafana 拉 `llm_request_duration_bucket{op="rewrite"}` 看是延迟上升（超时触发降级）还是**返回内容**出问题。
2. **如果是超时**：看 DashScope 上游有没有限流/429；看我们这边并发量（`llm_concurrent_requests` gauge）有没有突增；Redis 压力（`redis_commands_duration`）。
3. **如果返回内容非预期**：Sample 100 条 error 日志，看是 JSON 格式出错还是 LLM 输出了意料外的内容。如果是后者，大概率是**上游模型静默升级**（qwen-turbo → 新版）。回归 prompt 的 few-shot 是否还覆盖新模型的输出偏好。
4. **兜底**：把 Rewrite 开关置 off（Viper 热加载），用原 query，等定位完再打开。

**如果都查不出来，我会**：开启 Rewrite 节点的 Callback 全量采样（默认 1%），抓 10 分钟完整输入输出，离线 diff。

**HQ2：端到端 P95 从 2s 涨到 8s，但每个节点的 histogram 都正常，怎么办？**

A：这是经典"**总和大于部分之和**"问题，两个方向：
1. **排队延迟没打点**：Eino Graph 节点 histogram 只记录 execute 时长，不包含 goroutine 等待调度/锁等待。我会在 Graph Runnable 入口打"进队时间"，出口打"出队时间"，差值 - 节点总和 = 隐藏延迟。
2. **GC / CPU 抢占**：`GODEBUG=gctrace=1` + pprof 看 STW；如果是 GC 压力（大对象 `[]byte` 工具结果），改对象池或先截断再入参。

**实际踩坑案例举例**：曾经是 logger 同步 flush 阻塞 goroutine，但每节点计时不包含 logger 时间——改成 async buffer 后解决。

**HQ3：灰度期间 graph 分支的错误率比 legacy 高 0.3%，到底要不要回滚？**

A：不能拍脑袋，先做三件事：
1. **对齐 baseline**：legacy 的 0.05% 和 graph 的 0.35% 是不是**分布不一样的 query 类型**？灰度是按用户 hash 还是按 session，有没有系统偏差？
2. **看 error 分类**：是新架构引入的 bug（如 ReAct MaxStep 超限），还是原本就有只是 legacy 吞了？如果 legacy 本来就错，只是没抛出来，graph 这是**修复**不是劣化。
3. **用户影响**：0.3% 错误里多少是用户能感知（能看到错误提示）vs 能静默兜底的？

**决策原则**：如果错误有业务影响、且是新架构引入的未知 bug，回滚；如果是 legacy 掩盖问题被暴露、用户影响可控，修代码继续推。

### 深挖实现类

**HQ4：意图分类用 JSON schema 约束，但 LLM 还是偶尔返回非法 JSON，怎么办？**

A：三层防御：
1. **Prompt 里用 few-shot + "直接输出 JSON 不要加任何解释"**——从源头降低错误率（从 5% 降到 0.5%）。
2. **解析失败做正则抽取**：用 `regexp.MustCompile(\`\{[^}]+\}\`)` 抓第一段花括号，再 `json.Unmarshal`。处理 "这是你要的 json: {...}" 这种场景。
3. **还是失败**：走兜底分类 `CHITCHAT` + 打 `intent_parse_failed` 指标。每天抽样 10 条手工看，发现模式就更新 few-shot。

**为什么不用 function calling**：qwen 的 function calling 本质也是返回 JSON，同样有解析失败可能；且强制 function 会占用工具调用能力，后面 P4 的 ReAct 还要用。分开更干净。

**HQ5：你说分布式锁用 SETNX + Lua，Lua 脚本长什么样？怎么防止锁被误释放？**

A：释放脚本：
```lua
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
```

`ARGV[1]` 是获取锁时生成的 UUID。关键点：
- **原子性**：`GET + DEL` 必须在 Lua 里做，否则会出现"A 进程获取锁 → 过期 → B 进程获取同一把锁 → A 执行完 DEL 把 B 的锁删了"。
- **续期**：长任务要用 Lua watchdog 定期 `EXPIRE`（这里不需要，压缩 < 2s），但如果以后要做就得加。
- **锁超时 > 任务耗时 + 网络 RTT + 时钟漂移**——我们设 10s，压缩最坏 3s，留余量。

**陷阱**：不要用 `SET key value EX 10 NX` 搭配非 Lua 的 `DEL`——必须 value 是 UUID + 用 Lua 删。`memory/distributed_lock.go` 里用的就是这套。

**HQ6：Graph 并行分支 Rewrite 和 RAG，但如果我的 Classify 依赖 Rewrite 的结果呢？**

A：那就不能三路并行，要改成：
```
Rewrite ──┬─→ Classify ──┐
          └──────────────┤─→ build_prompt
       RAG（用 Rewrite 后 query）─┘
```

也就是 Rewrite 做完后 Classify 和 RAG 并行（依赖于 rewritten query），这两个再汇聚。Eino Graph 支持任意 DAG，只要在 AddEdge 里声明依赖关系即可。

**进一步权衡**：也可以让 RAG 用原 query，Classify 用 Rewrite query——**RAG 做 union（原 + 改写）能提命中率**（Wave 2 会讨论）。设计上这是"选择并行还是选择准确"的 tradeoff。

### 压测极限 / 扩展性

**HQ7：当前日活 1 万，如果变成 100 万，这套架构哪里先崩？**

A：按资源梯度排序：
1. **LLM 上游限流**：DashScope 单 key QPM 有限制，先挂。需要做 **多 key 轮询 + 退避重试 + 本地限流**（P7 要做的事）。
2. **Redis 单点**：当前 Redis Stack 单实例，压缩锁 + vector search + memory 都压在一台。`100万 DAU × 5 轮平均` ≈ 500万 key 写入/天，Redis 内存爆。需要**拆实例**：memory、vector store、锁分三个 Redis。
3. **Go 进程 goroutine 数**：ReAct Agent 并行工具调用会放大 goroutine 数，内存和调度压力。要加**信号量限制同时工具调用并发**。
4. **RAG embedder 上游**：embedding 调用频次 × 10，也可能被限流。要加本地缓存（相同 query 24h 不重算 embedding）。

**不会先崩的**：Gin handler / Eino Graph 本身（纯 CPU 活，scale out 就能扛）。

**HQ8：如果要支持多租户（多公司各用各的知识库），当前架构哪里要改？**

A：改动点：
1. **RAG Store**：加 `orgID` 维度，Redis key 加前缀 `org:{id}:doc:...`；检索时 `FT.SEARCH` 加 `@org_id:{id}` filter（RediSearch 原生支持）。
2. **Memory key**：`chat:memory:{orgID}:{sessionID}`，防串数据。
3. **意图树**：每个 org 可能有定制类目，`tree.yaml` 改成 `tree/{orgID}.yaml` 动态加载 + 默认 fallback。
4. **Tool 权限**：Email / 内部系统工具按 org 白名单，Tool 注册时加 `allowedOrgs` 字段。
5. **鉴权 middleware**：从 token 解析 orgID 放到 `context.Context`，所有下游从 ctx 取。
6. **监控指标**：Prometheus label 加 `org_id`，但注意高基数会打爆时序库——用户量级大时改成 hash 分桶或只记 top-N。

**这不是一个简单的"加字段"，而是一个 tenancy 贯穿所有层的设计决策。**

### 对比陷阱类

**HQ9：为什么不直接用 LangChain4j 或 Spring AI？已经很成熟了。**

A：技术选型是业务 + 团队的综合结果：
1. **语言栈**：团队主栈是 Go，LangChain4j / Spring AI 是 Java，上线就要多养一个 JVM 运维栈。
2. **框架成熟度 vs 灵活性**：LangChain4j 的 Agent 抽象是 Java 对象模型，改起来掣肘；Eino Graph 是函数式 DAG，更适合 Go 习惯。
3. **参照项目**：OpsPilot 已经用 Eino 跑通同类场景，验证了可行性——有 reference impl 比"生态大"更重要。
4. **但是** Spring AI 的 prompt template engine 和 chat memory 抽象确实比 Eino 成熟，所以我们**借鉴其设计思想**（compressible memory、system prompt 文件化）而不是换栈。

**HQ10：为什么向量库不用 Milvus / Qdrant / pgvector，非要用 Redis Stack？**

A：不是"最好"，是"最合适"：
1. **团队已有 Redis**：Redis 已经用于 session / 缓存 / 限流，向量再复用不增加运维对象。
2. **数据量级**：当前 < 100 万向量，Redis Stack 的 HNSW 索引完全够用（Milvus 是 1 亿级以上才显优势）。
3. **混合存储**：memory、锁、向量都在 Redis，事务/原子操作更简单（Wave 2 的多通道检索会利用这点）。
4. **运维成本**：Milvus/Qdrant 是**额外的有状态服务**，要做备份、升级、监控。

**什么时候换**：向量数 > 5000 万 或 查询 QPS > 1000，Redis 的内存开销和 CPU 占用会让 embedding 之外的缓存业务受影响——届时拆到 Milvus。这是**成本/性能拐点**决定的演进路径。
