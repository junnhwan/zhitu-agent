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
- ✅ **P1 PR-A 多通道检索骨架** (Wave 2) — `internal/rag/channel/` + `internal/rag/postprocessor/` + `rag.Pipeline`：Vector/BM25 双通道并行 (errgroup + 2s 超时) + Dedup/RRF/Rerank 处理器链 + 零命中回退 legacy；`rag.pipeline_mode: legacy|hybrid` 灰度开关，默认 legacy；30 条 seed golden set + `-tags=eval` A/B 框架
  - **2026-04-18 baseline**（30 条 seed，云 Redis Stack）：legacy Recall@5=0.967 MRR=0.933；hybrid Recall@5=0.967 MRR=0.917
  - 两侧同一条 miss（同 golden 盲区）；hybrid MRR 略低因 BM25 默认分词对中文不友好，RRF 融合稀释精准命中
  - PR-B 核心收益区间：gojieba 中文分词 + MMR 多样性 + Phrase 零命中兜底 + golden set 扩到 120 条
- ✅ **P1 PR-B1 三级兜底 + 多样性 + 短语兜底** (Wave 2) — 新增 PhraseChannel（RediSearch 精确短语匹配）+ DiversityProcessor（同文件 cap 2）+ 三级兜底（channels→phrase→legacy）+ Domain ctx 透传（预留 B2 消费）；golden set 扩到 80 条
  - **2026-04-18 扩量 baseline**（80 条）：legacy Recall@5=0.805 MRR=0.751；hybrid Recall@5=0.793 MRR=0.734
  - Recall 差距 1.2%（采样噪声内），phrase fallback 触发 3 次；差距主因仍是 BM25 中文分词差 —— PR-B2 上 go-ego/gse 后预期可拉平并超越
- ✅ **P1 PR-B2 gse 中文分词 + 双字段索引** (Wave 2) — 新包 `internal/rag/tokenizer/` (gse 纯 Go 懒加载) + Indexer 入库时注入 `content_tokenized` MetaData + Store schema 加 `content_tokenized TEXT` (weight 1.5) + BM25Channel.WithTokenizedField 切到新字段 + DataLoader ID 改相对路径（跨 CWD 一致）
  - **2026-04-18 干净重建索引**（82 条 golden）：legacy Recall@5=0.817 MRR=0.753；hybrid Recall@5=0.793 MRR=0.736
  - hybrid ≈ legacy（差距在 2% 采样噪声内），BM25 plumbing 验证通过；真实收益要等 golden set 补充更多 BM25 擅长的 query（错误码 / API 精确匹配 / 数字 ID）才能拉开差距
  - 切 hybrid 启动时 FT.INFO 检测 `content_tokenized` 字段缺失会自动 DROPINDEX+CREATE（不删 HASH，DataLoader 会重新 HSET 回写）
- ✅ **P1 PR-B 调优 — golden 扩到 120 + diversity 顺序** (Wave 2，2026-04-18)
  - 补 38 条 BM25-友好样本（API 名 / FT.SEARCH 命令 / 配置项 / 库版本号），golden set 共 120 条
  - **顺序调整**：DiversityProcessor 从 RRF 后挪到 Rerank 后 — 之前 diversity cap 2 在 rerank 前会丢同源相关 chunk，rerank 候选池被压缩
  - **120 条 baseline**：legacy Recall@5=0.758 MRR=0.702；hybrid Recall@5=0.750 MRR=**0.705 (反超 legacy)**
  - 结论：hybrid ≈ legacy（Recall 差 0.8% ~ 1 样本采样噪声），MRR 已反超 0.3%。完整骨架打通且不退化，进一步收益要靠 RRF 超参 sweep / 更大 golden / 更细 query 类型分桶
- ✅ **P5 PR-C1 MCP Client** (Wave 3，2026-04-18) — `internal/mcp/client/`：SSE + Stdio 双传输（mark3labs/mcp-go v0.46 + eino-ext tool/mcp v0.0.8），多 server 注册表，工具名冲突自动加 `{serverName}__` 前缀，单 server 失败跳过不阻断。`chat/service.go` 在 `createTools` 里合并本地 tool + MCP tool 一起 `WithTools` 绑 model。`mcp.client.enabled` 默认关，零风险。Prometheus 指标：`mcp_client_tools_total{server}` / `mcp_client_calls_total{server,tool,status}` / `mcp_client_call_duration_seconds`。`-tags=mcp` 集成测试对 `@modelcontextprotocol/server-everything` stdio 跑通：拉到 13 个工具，`echo` 调用端到端 5.9s。
- ✅ **P5 PR-C2 MCP Server** (Wave 3，2026-04-19) — `internal/mcp/server/`：Streamable HTTP 单端点挂到现有 Gin 同端口（默认 `/mcp`），`middleware.BearerAuth` 用 constant-time 校验 `Authorization: Bearer <token>`。默认暴露 3 个工具：`getCurrentTime` / `addKnowledgeToRag` / 新增 `retrieveKnowledge`（包 `rag.RAG.Retriever`，给 Claude Desktop 直接查知识库）。**`sendEmail` 不暴露**（副作用 + 无远端审计）。适配器用 `ParamsOneOf.ToJSONSchema` 把 Eino tool 的 JSON Schema 直接灌到 mcp-go 的 `RawInputSchema`，零损。`mcp.server.enabled` 默认关；启用时 `auth_token` 空启动 fail-fast（推荐走 `ZHU_MCP_SERVER_AUTH_TOKEN`）。Prometheus 指标：`mcp_server_tools_total` / `mcp_server_calls_total{tool,status}` / `mcp_server_call_duration_seconds{tool}` / `mcp_server_unauth_total`。`-tags=mcp` 集成测试用 `httptest.Server` + mcp-go 客户端 list + call 端到端跑通。双向 MCP 闭环（Client + Server）至此打通。
- 🚧 **P9 Wave 4 Eval Center 启动** (2026-04-19) — 总规划 `docs/plans/2026-04-19-wave4-eval-center.md`，三块活儿：真 doc_id 金标 / Memory eval / Workflow benchmark。#1a 代码改造落地：`goldenSample` 加 `relevant_doc_ids`，`sampleHit` 双判据（doc_id prefix 优先、keyword 回退），scorecard 增加 `judgment.{by_doc_id,by_keyword,unlabelled}` 统计，`TestDumpCandidates` 跑 legacy top-N 输出 `docs/eval/rag/candidates-<ts>.jsonl` 供人工挑。Makefile 加 `make eval` / `make eval-rag` / `make eval-dump-candidates` / `make eval-mcp`。11 条 sampleHit 单测覆盖前缀边界 + 优先级。**120 条实际 relabel 还要人工跑 `make eval-dump-candidates` 再挑，这一步没做。**
- 🚧 **P9 #2 Memory eval 集** (2026-04-19) — `internal/memory/memory_eval_test.go` (-tags=eval)：3 策略 × 5 合成对话模板，指标：`compression_ratio`（仅 triggered 时有意义）/ `recent_fidelity`（压缩后近端是否逐字保真）/ `fact_retention`（可选，`MEM_EVAL_LLM_JUDGE=true` 才跑，qwen-turbo 做判官）。模板 `docs/eval/memory/conversation_seed.jsonl` 5 条：short_chat (6 msgs, neither triggers) / medium_task (12) / long_mixed (20) / facts_bio (16 + 4 facts) / rag_tool_result (10)。Scorecard 写 `docs/eval/reports/memory-latest.json`。无 API key 时 LLM 策略显式 skip，simple 策略独立可跑。Makefile `make eval-memory`。离线 smoke 跑通：simple 策略 3/5 triggered，100% fidelity，triggered 时 avg ratio=0.79（long_mixed 压到 0.54 最狠）。
- 🚧 **P9 #3 Workflow benchmark** (2026-04-19) — `internal/chat/workflow_eval_test.go` (-tags=eval)：两条 Service 共享一个 RAG，`cfg.chat.workflow_mode=legacy` vs `graph` 分别构造。每 query 走两路径，用 Eino `callbacks.InitCallbacks` 零侵入收 `TokenUsage` + `ToolCalls`（利用 openai ACL 层已写入的 `ResponseMeta.Usage`）。Pairwise LLM-judge 用 qwen-max，输出 `A|B|T` + 一句理由。40 条 query 集 `docs/eval/workflow/query_set.jsonl`：20 knowledge / 10 tool / 10 chit-chat。指标：latency_ms、reply_len、tool_calls、prompt/completion/total_tokens、verdict。按 category 聚合 wins_legacy/wins_graph/ties + avg latency/tokens/tool_calls。`WORKFLOW_EVAL_LIMIT=N`/`WORKFLOW_EVAL_SKIP_JUDGE=1` 便于 smoke。`make eval-workflow`（timeout 60m）。Scorecard 写 `docs/eval/reports/workflow-latest.json`。无 API key 时 skip 测试不失败。**实际 baseline 得跑起 Redis + DashScope 才出，40 条预估成本 < ￥1。**
