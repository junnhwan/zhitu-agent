# ragent 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/martin/ragent`
> 语言栈：Java 17 + Spring Boot 3.5 + MyBatis Plus + Milvus 2.6 + Redis/Redisson + RocketMQ + Tika + Sa-Token

## 1. 项目定位

**nageoffer（拿个 offer）社群的企业级 Agentic RAG 系统** —— 作者 martin 在公司实际落地过 RAG 系统。**定位和 ZhituAgent 高度相似**（都是"能写进简历的 AI 项目"），但功能维度和工程规范领先 2-3 个数量级：**40k 行 Java + 18k 行 TS**，20 张业务表，**8 个专用线程池**，三态熔断器，分布式队列限流，全链路 Trace，完整 React 管理后台。

**宁可参考它而不是复刻它**——这是 Phase 2 的"北极星图谱"。

## 2. 架构概览

```
ragent/
├── framework/      # 通用基础设施：三级异常体系、Snowflake、SSE 封装、Trace 透传
├── infra-ai/       # 屏蔽不同模型供应商（百炼/SiliconFlow/Ollama）
├── bootstrap/      # 业务模块（RAG 核心 + Admin + Ingestion）
│   └── rag/core/
│       ├── retrieve/{MultiChannelRetrievalEngine, channel/*, postprocessor/*}
│       ├── intent/{DefaultIntentClassifier, IntentResolver, IntentGuidanceService, IntentTreeCacheManager}
│       ├── mcp/{MCPToolRegistry, HttpMCPClient, LLMMCPParameterExtractor}
│       ├── memory/{ConversationMemoryService, ConversationMemorySummaryService, JdbcStore}
│       ├── rewrite/ # 问题重写 + 子问题拆分
│       └── router/  # 模型路由 + 熔断降级
└── mcp-server/     # ragent 自己作为 MCP Server 暴露能力
```

## 3. 亮点实现（核心 10 项）

### 3.1 多通道并行检索 + 后处理流水线 ★★★★★
`rag/core/retrieve/MultiChannelRetrievalEngine.java`

```java
// 阶段1：多个 SearchChannel 通过 CompletableFuture + 专用线程池并行
List<CompletableFuture<SearchChannelResult>> futures = enabledChannels.stream()
    .map(channel -> CompletableFuture.supplyAsync(() -> channel.search(context), ragRetrievalExecutor))
    .toList();

// 阶段2：后处理器链（rerank / dedup / filter / boost）依次串联
for (SearchResultPostProcessor processor : enabledProcessors) {
    chunks = processor.process(chunks, results, context);
}
```

**插件化**：`SearchChannel` 接口任意实现（Bean 自动注入）；`SearchResultPostProcessor` 接口任意实现（按 `getOrder()` 串联）。**单通道失败只影响本通道，整体降级不中断**。

### 3.2 树形意图识别 + 置信度引导 ★★★★★
`rag/core/intent/DefaultIntentClassifier.java`

- **树结构**：领域 → 类目 → 话题（三级），DB 存储 + Redis 缓存
- **LLM 打分**：把所有叶子节点列出来，要求 LLM 返回 `[{id, score, reason}]`
- **prompt 带示例问题**（`examples=xx / yy / zz`）和 **节点类型标识**（KB / MCP / SYSTEM）
- **阈值过滤**：`topN + minScore` 筛选
- **置信度不足时主动引导澄清**（`IntentGuidanceService`），不硬猜
- **参数冷热**：`temperature=0.1, topP=0.3, thinking=false` 避免发散

对比 ZhituAgent 的"关键词 switch-case"，这是**质的飞跃**。

### 3.3 意图驱动检索（IntentDirectedSearchChannel）★★★★
- 意图分类结果 → 选择启用哪些检索通道、用哪些 filter
- **KB 意图**：走向量检索通道 + 组织过滤
- **MCP 意图**：直接调 MCP 工具（不检索）
- **SYSTEM 意图**：走内置系统能力

**精华**：把"检索 vs 工具调用"的路由**上推到意图分类阶段**，避免 LLM 在 ReAct 里反复试错。

### 3.4 问题重写 + 子问题拆分 ★★★★
- `多轮对话上下文补全`：用户说"报销咋整"→ 重写为"员工报销流程"
- `复杂问题拆分`：把复合问题拆为多个子问题，**每个子问题独立走一遍检索**，最后合并

`SubQuestionIntent` 数据结构承载拆分后的子问题 + 各自意图。这是 RAG 里最关键的 query understanding 环节。

### 3.5 模型路由 + 三态熔断器 + 首包探测 ★★★★★
- **优先级候选链**：百炼 > SiliconFlow > Ollama
- **三态熔断器**：CLOSED → OPEN（失败超阈值）→ HALF_OPEN（冷却期后放行探测）→ CLOSED/OPEN
- **ProbeBufferingCallback**（装饰器模式）：**流式输出时先缓冲 N 个事件确认模型健康，才把已缓冲+后续数据推给用户**。模型挂了可以回滚切换，用户端看到的是干净的首包。

**面试极强题材**：这是流式 AI 产品生产环境的核心挑战。

### 3.6 分布式队列式并发限流 ★★★★★
README 描述（`framework` 模块）：
- **Redis ZSET 排队**：请求入队带时间戳 score
- **Lua 原子判断**：当前请求是否在"队头窗口"内（前 N 个）
- **Semaphore 控制并发**：获得许可才能执行（带自动过期防死锁）
- **Pub/Sub 跨实例唤醒**：许可释放时通过频道广播，本地合并通知避免惊群
- **SSE 实时推送排队状态**：前端看到"前面还有 3 人"

**这是分布式系统的高阶题**，单机 RateLimiter 完全不是一个量级。

### 3.7 8 个专用线程池 + TTL 上下文透传 ★★★★
| 线程池 | 用途 |
|---|---|
| `mcpBatchThreadPool` | MCP 批量调用 |
| `ragContextThreadPool` | RAG 上下文组装 |
| `multiChannelThreadPool` | 多路检索 |
| `internalSearchThreadPool` | 内部检索 |
| `intentClassifyThreadPool` | 意图分类 |
| `memorySummaryThreadPool` | 记忆摘要 |
| `modelStreamThreadPool` | 模型流式输出 |
| `conversationThreadPool` | 对话入口 |

所有线程池包装 `TtlExecutors`（**TransmittableThreadLocal**），保证用户上下文 + Trace ID 在异步任务中不丢失。

### 3.8 MCP 客户端 + LLM 参数提取 ★★★★
`rag/core/mcp/`：
- **MCPToolRegistry**：注册表模式，发现所有 `MCPToolExecutor` Bean
- **HttpMCPClient**：远端 MCP 服务
- **LLMMCPParameterExtractor**：给 LLM 喂 "用户问题 + 工具 JSON Schema"，让 LLM 吐出调用参数

**这是 MCP 集成的工程精髓**：不是让主对话模型在 ReAct 里慢慢摸索，而是**小模型专职提参**，主模型专注回答。

### 3.9 会话记忆：Redis 热 + JDBC 冷 + LLM 摘要 ★★★★
`rag/core/memory/`：
- `ConversationMemoryStore`：近 N 轮 Redis 热存
- `JdbcConversationMemoryStore`：历史消息持久化
- `ConversationMemorySummaryService` + `JdbcConversationMemorySummaryService`：LLM 生成摘要 + 持久化
- `MemoryConfigValidator` + `@ValidMemoryConfig`：配置合法性校验

### 3.10 AOP 链路追踪 @RagTraceNode ★★★★
```java
@RagTraceNode(name = "multi-channel-retrieval", type = "RETRIEVE_CHANNEL")
public List<RetrievedChunk> retrieveKnowledgeChannels(...) { ... }
```

方法上加注解，AOP 自动埋点记录：入参 / 出参 / 耗时 / 异常。**管理后台有独立 Trace 页面**展示每次对话的完整链路。

### 3.11 MCP Server 模块 ★★★
独立 `mcp-server` Maven 模块，ragent 既是 MCP **客户端**（消费工具）也是 MCP **服务端**（暴露能力），**双向 MCP 闭环**。

### 3.12 入库流水线（Configurable Pipeline）★★★
`ingestion/`：
- 节点配置存 DB（`IngestionPipelineDO` + `IngestionPipelineNodeDO`）
- 任务执行时动态装配 DAG
- 每个节点有独立执行日志
- **抓取 → 解析（Tika）→ 增强 → 分块 → 向量化 → 写 Milvus**，每步可插拔

## 4. 对 ZhituAgent 的启示

### 候选 A：多通道并行检索 + 后处理器链 ★★★★★
- **描述**：ZhituAgent 现在是单路向量检索 → rerank。升级为**多通道**：`VectorChannel`（现有）+ `KeywordChannel`（BM25）+ `IntentChannel`（按意图过滤的定向检索），并行执行 → 去重 → rerank → 截断。
- **借鉴位置**：`MultiChannelRetrievalEngine.java` 全文（架构可完美移植到 Go）。
- **Go+Eino 可行性**：中成本。用 `errgroup.Group` 或 `sync.WaitGroup` 替代 CompletableFuture，`SearchChannel` interface + 依赖注入。
- **升级 or 新增**：升级 `rag.Retrieve`（Phase 2 北极星候选）。
- **简历**：`设计多通道并行检索引擎：N 个 SearchChannel 通过 errgroup 并发执行（单通道失败自动降级不中断），结果经后处理器链（dedup → rerank → filter）精炼，支持插件化扩展新通道`。
- **面试深入**：能讲并行与降级的平衡、为什么后处理器要串行（依赖前序结果）、如何做到单点失败不影响整体。
- **深度评分**：5/5。

### 候选 B：树形意图分类 + LLM 打分 + 置信度引导 ★★★★★
- **描述**：给 ZhituAgent 加个意图树（知识类目 + MCP 工具 + 系统指令），LLM 打分后路由。**取代"关键词路由"**。
- **借鉴位置**：`DefaultIntentClassifier.java` + `IntentGuidanceService`。
- **Go+Eino 可行性**：中成本。新增意图表 + Redis 缓存 + Eino LLM 分类 + prompt 模板。
- **升级 or 新增**：升级 `multiAgentChat` 的路由层。
- **简历**：`实现树形意图识别系统：三级分类（领域→类目→话题）通过 LLM 打分，阈值过滤 + 置信度不足主动引导澄清，意图驱动后续检索/工具路由（替代关键词匹配的硬编码路由）`。
- **面试深入**：意图分类树 vs 规则引擎 vs 端到端 LLM 决策的利弊、低 temperature 打分的稳定性、零召回场景的引导话术设计。
- **深度评分**：5/5。

### 候选 C：三态熔断 + 模型路由 + 首包探测 ★★★★★
- **描述**：ZhituAgent 现在只用一个 qwen-max，挂了就挂了。接入候选链（Qwen / DeepSeek / Ollama 本地），三态熔断 + 首包探测。
- **借鉴位置**：`router/` + `ProbeBufferingCallback`。
- **Go+Eino 可行性**：中-高成本。Go 用 `sony/gobreaker` 现成熔断器；首包探测需要包装 Eino Stream。
- **升级 or 新增**：升级 `infra/llm`。
- **简历**：`多模型优先级路由 + 三态熔断（CLOSED/OPEN/HALF_OPEN），流式首包探测（ProbeBuffering）保证模型切换时用户端无脏数据，单模型故障业务无感`。
- **面试深入**：三态熔断每状态的转换条件、失败计数窗口（时间窗 vs 滑动窗）、流式首包探测的实现细节（缓冲多少帧、异常如何回滚）。
- **深度评分**：5/5。

### 候选 D：Redis ZSET + Pub/Sub 分布式排队限流 ★★★★★
- **描述**：ZhituAgent 目前无流量管控。引入**分布式排队限流**：用户并发请求多时自动排队，前端看到"前面还有 N 人"。
- **借鉴位置**：`framework/` 模块的队列限流（README 有详细描述）。
- **Go+Eino 可行性**：高成本。但"简历加分极强"。Redis ZSET + Lua 脚本 + go-redis Pub/Sub + Go Channel + SSE 推送。
- **升级 or 新增**：新增 `middleware/queue-limit`。
- **简历**：`基于 Redis ZSET + Pub/Sub 实现分布式排队限流：请求入队 Lua 原子判断窗口，信号量控制并发 + 许可自动过期防死锁，跨实例广播唤醒合并通知避免惊群，SSE 实时推送排队状态`。
- **面试深入**：令牌桶 vs 漏桶 vs 排队限流、分布式信号量的原子性保证、惊群效应的规避、许可过期防死锁的机制。
- **深度评分**：5/5（**单独拿出来就能讲 15 分钟**）。

### 候选 E：AOP 链路追踪 @RagTraceNode ★★★★
- **描述**：ZhituAgent 有 Prometheus 指标但没有**链路追踪**（每次对话 → 看每个环节的输入输出耗时）。加个注解 + Redis 存 trace + 管理页面展示。
- **借鉴位置**：`rag/aop/RagTraceAspect.java` + `rag/controller/RagTraceController.java` + 前端 Trace 页。
- **Go+Eino 可行性**：中成本。Go 没有 AOP，但 Eino Callbacks 天然就是 AOP 切面。可以用 Callbacks + Redis Stream 存 trace。
- **升级 or 新增**：升级 `monitor`。
- **简历**：`RAG 全链路追踪：基于 Eino Callbacks 实现每个节点（Rewrite/Intent/Retrieve/Rerank/Generate）的耗时和输入输出记录，管理后台按会话维度展示完整 trace，便于效果调优`。
- **面试深入**：Trace 的数据模型（OpenTelemetry SpanContext）、高吞吐下的异步持久化、采样策略。
- **深度评分**：4/5。

### 候选 F：LLM 驱动的 MCP 参数提取 ★★★★
- **描述**：ZhituAgent 若接入 MCP（前面的候选），**不要让主对话模型在 ReAct 里摸索工具参数**。用小模型专职提参。
- **借鉴位置**：`LLMMCPParameterExtractor.java`。
- **深度评分**：4/5（和候选 G 双模型策略强相关）。

### 候选 G：专用线程池 + TTL 上下文透传 ★★★
- **描述**：Go 本身没 ThreadPool，但有**goroutine pool**（`ants`）和 **context.Context 透传**。对 Go 项目价值有限。
- **深度评分**：2/5（Go 不强依赖这种模式）。

### 候选 H：入库流水线（Configurable Pipeline）★★★
- **描述**：把 ZhituAgent 的"5 分钟扫目录"升级为 DB 配置的 DAG，每个节点（抓取/解析/分块/增强/向量化/写入）可插拔。
- **借鉴位置**：`ingestion/`。
- **Go+Eino 可行性**：中成本。
- **深度评分**：3/5。

## 5. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **A. 多通道并行检索** | ★★★★★ | 中 | RAG 命脉，面试必问 |
| 🥈 | **D. Redis ZSET 分布式排队限流** | ★★★★★ | 高 | **单点最能炸裂面试官**的题材 |
| 🥉 | **B. 树形意图识别** | ★★★★★ | 中 | 替代关键词路由，提升整体智能 |
| 4 | **C. 多模型路由 + 三态熔断** | ★★★★★ | 中-高 | 流式 AI 生产环境强题 |
| 5 | **E. AOP 链路追踪** | ★★★★ | 中 | 用 Eino Callbacks 实现 |
| 6 | **F. LLM 参数提取器** | ★★★★ | 低 | 跟 MCP 配对 |
| 7 | **H. 入库流水线 DAG** | ★★★ | 中 | 偏后台运维 |

## 6. 总结

ragent 的**完整度和工程深度远超 ZhituAgent 现阶段**。Phase 2 不需要全搬过来（那是 2-3 个月工作量），**按优先级挑 3-4 个攻坚**就能让简历上升一个档次：

- **Must have**：A（多通道检索）+ B（意图识别）
- **Highlight**：D（分布式排队限流）或 C（模型路由熔断） —— 二选一当面试爆点
- **Bonus**：E（全链路 trace）作为可观测性差异化

配合前面项目（OpsPilot 的 Eino 高级用法、ai-mcp-gateway 的 MCP Server、paismart-go 的 Kafka pipeline），可以组合出 **"Agentic RAG + 生产级可观测 + MCP 生态"**的完整简历故事。
