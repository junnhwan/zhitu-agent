# ZhituAgent Phase 2 优化建议汇总

> 日期：2026-04-17
> 调研项目数：7 个
> 7 份单项笔记见同目录：`ragent.md` / `opspilot.md` / `paismart-go.md` / `ai-mcp-gateway.md` / `ai-agent-scaffold-lite.md` / `agentx.md` / `thought-coding.md`

## 1. 项目定位对照表

| 项目 | 语言栈 | 定位 | 借鉴价值 | 对 ZhituAgent 最独特的贡献 |
|---|---|---|---|---|
| **ragent** | Java + Milvus + 40k 行 | 企业级 Agentic RAG（**和 ZhituAgent 同定位**） | ⭐⭐⭐⭐⭐ | 多通道检索、树形意图、三态熔断、分布式排队限流 |
| **OpsPilot** | **Go + Eino（同框架！）** | AIOps + RAG | ⭐⭐⭐⭐⭐ | Eino Plan-Execute、Graph、ReAct、MCP Client、LLM 摘要 |
| **paismart-go** | Go + ES + Kafka | 企业级 RAG 知识库 | ⭐⭐⭐⭐ | Kafka 异步流水线、ES 混合检索、分片上传断点续传 |
| **ai-mcp-gateway** | Java + WebFlux | MCP 网关（暴露 HTTP 为 MCP） | ⭐⭐⭐⭐ | MCP Server、Guava 限流、Tree Strategy |
| **ai-agent-scaffold-lite** | Java + Spring AI + Google ADK | Agent 配置化装配脚手架 | ⭐⭐⭐ | MCP 三种传输（SSE/Stdio/Local）、Armory Pattern |
| **AgentX** | Java + LangGraph4j | Coding Agent 内核 | ⭐⭐⭐ | **Eval Center 独有**、Context Compilation Center |
| **ThoughtCoding** | Java + LangChain4j CLI | 终端版 Coding CLI | ⭐⭐⭐ | 三策略记忆压缩 + Micro Compact、工具确认 |

**核心结论**：**OpsPilot + ragent** 是两大北极星（同框架 + 同定位），其他项目按点取值。

## 2. 跨项目共性提炼（多个项目都推荐的才最值得做）

### 🔥 跨 5 个项目都出现：**MCP 生态**
| 项目 | 切面 |
|---|---|
| OpsPilot | MCP Client（SSE）+ 降级 |
| ai-mcp-gateway | **MCP Server**（HTTP → MCP 转换） |
| ai-agent-scaffold-lite | MCP Client 三传输（Local/SSE/Stdio） |
| ragent | MCP Client + **LLMMCPParameterExtractor** + 自带 MCP Server 模块 |
| ThoughtCoding | MCP Client 动态发现 |

**合成方案**：Go + Eino 用 `mark3labs/mcp-go` 同时做 **MCP Client（SSE + Stdio）** + **MCP Server（暴露 RAG 能力）**，形成完整双向 MCP 闭环。

### 🔥 跨 4 个项目都出现：**LLM 摘要式会话记忆**
| 项目 | 手法 |
|---|---|
| OpsPilot | 超过 9 轮时 LLM 压缩前 N-6 轮 + 保留最近 6 轮 |
| ragent | Redis 热 + JDBC 冷 + LLM 摘要 |
| ThoughtCoding | **Micro Compact 工具结果** + Token/Sliding/Hybrid 三策略 + 中英文分开估算 |
| paismart-go | Redis 存历史（无摘要） |

**ZhituAgent 当前**：`取前 3 条消息各截 50 字符`（CLAUDE.md 明确冻结）——**Phase 2 是协商解冻的时机**。

**合成方案**：**Micro Compact 工具结果 → Token 估算（中文/2 + 英文/4）→ 超阈值 LLM 摘要压缩 + 保留最近 N 轮**。

### 🔥 跨 4 个项目都出现：**混合检索（语义 + 关键词）**
| 项目 | 手法 |
|---|---|
| paismart-go | ES KNN + BM25 rescore（query_weight=0.2/rescore_weight=1.0）+ match_phrase boost=3.0 兜底 + 零命中重试 |
| ragent | **多通道并行（SearchChannel 插件化）+ 后处理器链** |
| OpsPilot | Milvus HybridSearch（Dense + Sparse with gojieba FNV hash）+ WeightedReranker(0.5/0.5) |
| ZhituAgent 当前 | 单路向量 → rerank（最单薄） |

**合成方案**：**多通道并行检索 + 后处理器链**（向量 + BM25 + 短语兜底 + rerank + 去重），用 Redis Stack 内的 RediSearch 直接支持 KNN + TEXT 字段。

### 🔥 跨 3 个项目都出现：**Query 理解（Rewrite / Sub-question / Intent）**
| 项目 | 手法 |
|---|---|
| OpsPilot | 有历史时 LLM 改写查询用于检索 |
| ragent | **Rewrite + 子问题拆分 + 树形意图分类 + 引导澄清** |
| ZhituAgent 当前 | 关键词 switch-case（最原始） |

**合成方案**：**Query Rewrite Lambda 节点 + 树形意图分类 + 置信度不足时主动引导**。

### 🔥 跨 2 个项目都出现：**Eino Graph + Plan-Execute-Replan**
| 项目 | 手法 |
|---|---|
| OpsPilot | Eino Graph + `adk/prebuilt/planexecute` + ReAct Agent |
| ai-agent-scaffold-lite | Google ADK Loop/Parallel/Sequential |

**合成方案**：把 `multiAgentChat` 重构为 **Eino Graph 编排 + ReAct Agent + Plan-Execute-Replan**。

### 🔥 独一无二的差异化点

| 项目 | 独有能力 | 价值 |
|---|---|---|
| **AgentX** | **Eval Center**（evidence + scorecard + report 三件套） | ⭐⭐⭐⭐⭐ 其他项目全无 |
| **ragent** | **Redis ZSET + Pub/Sub 分布式排队限流** | ⭐⭐⭐⭐⭐ 面试爆点 |
| **ragent** | **三态熔断器 + 首包探测 + 模型路由** | ⭐⭐⭐⭐⭐ 生产级流式 AI 必备 |
| **paismart-go** | **Kafka 异步文档处理流水线** | ⭐⭐⭐⭐ 工程解耦典范 |
| **ai-mcp-gateway** | **MCP Server 协议实现** | ⭐⭐⭐⭐⭐ 2025-2026 最热协议 |

## 3. Phase 2 优化清单（按性价比排序）

> 性价比 = (简历价值 × 技术深度) / 实现成本。
> 所有建议均**明确标注借鉴来源**，不做"泛泛而谈的最佳实践"。

### ⚡ 第一梯队（必做 · Phase 2 核心）

| # | 功能 | 借鉴来源 | 成本 | 简历亮点 |
|---|---|---|---|---|
| **P1** | **多通道并行检索 + 后处理器链** | ragent + paismart-go + OpsPilot | 中 | 设计多通道并行检索引擎（errgroup 并发 + 插件化 SearchChannel），BM25 rescore + 短语兜底 + 零命中重试，检索命中率提升 X% |
| **P2** | **Query Rewrite + 树形意图分类 + 引导** | ragent + OpsPilot | 中 | 三级意图树（领域→类目→话题）LLM 打分，Query Rewrite 消解多轮指代，置信度不足主动引导澄清 |
| **P3** | **LLM 摘要式记忆压缩 + Micro Compact** | OpsPilot + ThoughtCoding | 低 | 超阈值（9 轮）触发 LLM 摘要压缩 + 保留最近 6 轮明细，工具结果做 Micro Compact，中英文分开估算 Token |
| **P4** | **Eino Graph + ReAct Agent 重构对话编排** | OpsPilot | 中 | 基于 Eino Graph 并行分支（RAG 检索 + Query Rewrite）AllPredecessor 汇聚，ReAct Agent 承载工具调用，替代手写 tool-call loop |
| **P5** | **MCP 双端闭环**（Client SSE+Stdio + Server） | OpsPilot + ai-mcp-gateway + ai-agent-scaffold-lite | 中 | 基于 mark3labs/mcp-go 实现 MCP 双端：客户端支持 SSE/Stdio 双传输动态加载远端工具，服务端暴露 RAG 能力为 MCP 工具供 Claude Desktop / Cursor 接入 |

### 🔥 第二梯队（选做 · 挑 2 个当面试爆点）

| # | 功能 | 借鉴来源 | 成本 | 简历亮点 |
|---|---|---|---|---|
| **P6** | **多模型路由 + 三态熔断器 + 首包探测** | ragent | 中-高 | 三态熔断（CLOSED/OPEN/HALF_OPEN）+ ProbeBufferingCallback 首包探测，流式切换模型时用户端零脏数据 |
| **P7** | **Redis ZSET + Pub/Sub 分布式排队限流** | ragent | 高 | 分布式排队限流：Redis ZSET 排队 + Lua 原子判断窗口 + Semaphore 控并发 + 许可过期防死锁 + Pub/Sub 跨实例唤醒 + SSE 推排队状态 |
| **P8** | **Plan-Execute-Replan Agent** | OpsPilot | 低 | 用 Eino `adk/prebuilt/planexecute` 三件套重构多 Agent：Planner（qwen-max）拆解 → Executor（qwen-turbo + ReAct + 工具）执行 → Replanner 动态调整 |
| **P9** | **RAG Eval Center**（evidence + scorecard + report） | AgentX | 中 | evidence-first RAG 评测中心：场景包跑流水线 → 三件套 artifact（raw-evidence.json + scorecard.json + report.md），多轮对比驱动 prompt/检索持续优化 |

### 🌱 第三梯队（加分 · Phase 3 以后）

| # | 功能 | 借鉴来源 | 成本 |
|---|---|---|---|
| P10 | Kafka 异步文档处理流水线（替代定时扫目录）| paismart-go | 中 |
| P11 | Eino Callbacks 全链路追踪 + 前端 Trace 面板 | OpsPilot + ragent | 中 |
| P12 | 工具执行前确认（对 EmailTool 等副作用工具）| ThoughtCoding | 中 |
| P13 | 双模型策略（Think + Quick）| OpsPilot | 低 |
| P14 | Context Compilation Center + fingerprint | AgentX | 低 |
| P15 | 文件分片上传 + 秒传 + 断点续传（MinIO）| paismart-go | 中 |
| P16 | Tika 多格式文档解析（PDF/DOCX/PPTX）| paismart-go | 低 |
| P17 | Guava-style 本地 RateLimiter 限流中间件 | ai-mcp-gateway | 低 |
| P18 | 多租户 orgTag 权限模型（检索期过滤）| paismart-go | 中 |

## 4. Phase 2 建议路线图

**目标**：3-4 波迭代 = 4-8 周，让 ZhituAgent 从 v1.0（"能跑"）升级到 v2.0（"能写简历 + 面试能打"）。

### Wave 1：对话质量基建（2-3 周）⭐ 必做
**P2 + P3 + P4** 为一包：Query 理解 + 记忆压缩 + Eino Graph 重构。

- 新增 `internal/understand/` 做 Query Rewrite + Intent 分类
- 重构 `internal/memory/` 加 Micro Compact + LLM 摘要策略
- 重构 `internal/chat/` 用 Eino Graph 编排，砍掉手写 tool-call loop
- **产出**：对话能理解多轮代词、长对话不爆 Token、多路由不硬编码

### Wave 2：RAG 检索升级（2 周）⭐ 必做
**P1**：多通道检索 + 后处理器链。

- `internal/rag/` 增加 `channel/` + `postprocessor/` 两个包
- 实现 `VectorChannel`、`BM25Channel`（Redis Stack TEXT 字段）、`IntentChannel`
- 实现 `DedupProcessor`、`RerankProcessor`（复用现有 qwen3-rerank）
- **产出**：检索命中率上升，扩展新通道零改代码

### Wave 3：MCP 生态闭环（2 周）⭐ 必做
**P5**：MCP Client（SSE + Stdio）+ MCP Server 暴露 RAG。

- `internal/mcp/client/` 新增，支持两种传输
- `cmd/mcp-server/` 新增（或在主 server 加 `/mcp/*` 路由）把 `rag.Retrieve` 暴露成 MCP 工具
- **产出**：ZhituAgent 能被 Claude Desktop / Cursor 接入 + 能消费外部 MCP 工具

### Wave 4：生产级差异化（2-3 周，二选一或全做）
从 **P6 / P7 / P8 / P9** 里**挑 1-2 个**做到简历能深入讲：

- **P9（Eval Center）** 是其他项目都没有的独特点，推荐优先
- **P7（分布式排队限流）** 是面试爆点，单独能讲 15 分钟
- **P6（模型路由熔断）** 是流式 AI 生产环境强题材
- **P8（Plan-Execute）** 成本低但没 P6/P7 亮眼

## 5. 简历写法总纲（3 段式）

把 Phase 2 做完后，可以写出这样的简历段：

> **ZhituAgent · 企业级 Agentic RAG 系统（Go + Eino）**
>
> **对话理解层**：树形意图分类（LLM 打分）+ Query Rewrite 消解多轮指代 + 置信度不足主动引导；基于 Eino Graph 并行分支（RAG 检索 + 历史改写）AllPredecessor 汇聚到 ReAct Agent，替代硬编码路由；LLM 摘要式记忆压缩（Micro Compact 工具结果 + 中英文分开 Token 估算）。
>
> **多通道检索层**：向量 KNN + BM25 rescore + 短语兜底 + Rerank 多路并行流水线，插件化 SearchChannel 接口扩展零改代码；零命中时用归一化短语自动重试。
>
> **生态 + 生产层**：基于 mark3labs/mcp-go 实现 **MCP 双端闭环**（Client 支持 SSE/Stdio 双传输，Server 暴露 RAG 能力供 Claude Desktop 接入）；【可选】基于 Redis ZSET + Pub/Sub 的分布式排队限流 / 三态熔断器 + 首包探测的多模型路由；【差异化】evidence-first RAG 评测中心（raw-evidence + scorecard + report）驱动持续优化。

## 6. 下一步

1. **确认优先级**：我推荐的 Wave 1-3 是否符合你的节奏？是否需要调整？
2. **起草 Phase 2 书面计划**：选定 Wave 后，进入 `writing-plans` skill 为每波写详细实施计划
3. **逐波实施**：每波独立 PR，做完一波更新 README / CLAUDE.md / 记忆

**我的个人建议**（性价比最高）：
- **Wave 1-3 全做** = 简历三段式成型
- **Wave 4 挑 P9（Eval Center）+ P7（排队限流）** = 两个差异化面试爆点

不推荐一次性推进到 P10 以后，那些放到 Phase 3 作为持续优化方向。

---

## Wave 1 进度

- ✅ **P3 记忆压缩升级** (PR 1) — LLM 摘要 + Micro Compact + CJK/ASCII token 估算，策略可配置可降级
- ✅ **P2 Query Rewrite + 三级意图分类** (PR 2) — `internal/understand/` 新包：Rewriter + Classifier(JSON 容错) + Guardian(置信度兜底) + Service(gobreaker 熔断)，接入 SimpleOrchestrator，关键词路由作 fallback；离线评估集 seed 20 条 + `-tags=eval` 框架
- ✅ **P4 Eino Graph + ReAct Agent 重构** (PR 3) — `internal/chat/workflow/` 新包：Graph 串行编排 (enrich→retrieve→build_prompt→react→wrap) + ReAct Agent 通过 `ExportGraph` 嵌入；`chat.workflow_mode: legacy|graph` 灰度开关，默认 legacy；手写 tool-loop 保留为 safety net
