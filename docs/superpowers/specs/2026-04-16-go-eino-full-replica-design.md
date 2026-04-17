# Go + Eino Full Replica Design

## 1. Background

当前仓库是一个基于 `Java + Spring Boot + LangChain4j` 的智能体项目，已经具备以下可见能力：

- 普通聊天接口
- SSE 流式聊天接口
- 简单多 Agent 协调接口
- RAG 文档加载、检索、重排、知识写回
- Redis 会话记忆与压缩
- 时间、邮件、联网搜索等工具调用
- 系统提示词驱动的人设与输出格式约束
- 输入 Guardrail 与全局异常处理
- Prometheus 指标与运行日志
- 三个前端演示页

本次目标不是重新设计产品，而是先用 `Go + Eino` 对当前项目做一版尽量完整的功能复刻，让 Go 版本先达到“可运行、可展示、可写进简历、可继续二次开发”的状态，后续再围绕性能、架构和交互做魔改。

## 2. Goals

### 2.1 Primary Goal

在保留 Java 版本作为参考实现的前提下，于同一仓库内新增一个 `Go` 实现，完成当前项目的核心功能与演示能力复刻：

- 后端能力尽量完整对齐
- 前端提供可演示页面
- Docker 编排可一键启动依赖
- 暴露 Prometheus 指标并提供 Grafana 面板

### 2.2 Success Criteria

Go 版本达到以下标准视为第一阶段完成：

- 能独立启动并提供 `/api/chat`、`/api/streamChat`、`/api/multiAgentChat`、`/api/insert`
- 能加载本地知识文档并写入向量库
- 能基于用户问题执行检索增强回答
- 能使用 Redis 保存会话上下文，并支持压缩与 TTL
- 能调用时间工具、邮件工具、联网搜索工具
- 能复刻当前系统提示词、输入 Guardrail 与错误返回契约
- 能暴露 Prometheus 指标，并由 Grafana 读取展示
- 能通过 Docker Compose 拉起 `app + redis + postgres/pgvector + prometheus + grafana`
- 能通过静态演示页完成普通对话和流式对话演示

## 3. Scope

### 3.1 In Scope

- 完整 Go 服务脚手架
- Eino 模型调用与工具编排
- 与 Java 版本等价的 API
- RAG 文档加载、切分、embedding、检索、rerank、写回
- 多轮会话记忆与压缩
- 基础多 Agent 编排
- 系统提示词、Guardrail、安全错误返回
- 运行日志、请求 ID、模型调用指标、RAG 指标
- `qwen.html / gpt.html / gemini.html` 三个演示页
- Docker Compose 与监控配置

### 3.2 Out of Scope for Phase 1

以下内容不进入第一阶段“完整复刻”的定义：

- 为了“更 Go 化”而大幅改变已有产品行为
- 自研复杂 Agent 规划器
- 微服务拆分
- 大规模前端重构
- 自定义权限系统、用户体系、数据库持久化业务表

## 4. Replication Principles

### 4.1 Functional Parity First

第一优先级是让 Go 版本在用户可见行为上尽量贴近当前 Java 版本，而不是一开始就追求架构创新。

### 4.2 Keep the Java Project as Reference

Java 目录保持不动，Go 版本单独放在新目录中实现，便于逐模块对照、迁移和回归验证。

### 4.3 Equivalent Behavior Over Identical Framework Semantics

LangChain4j 与 Eino 并非一一对应，因此优先保证“功能等价”而非“框架 API 等价”。

例如：

- 如果 Eino 的原生 MCP 能力覆盖不足，允许先用 Go 的工具适配层实现与 Java 可见效果等价的联网搜索/时间工具
- 如果 pgvector 某些接入细节不适合直接套 Eino 组件，则允许由普通 Go service 完成向量检索流程，再把结果接回 Eino 调用链

### 4.4 Safer Configuration

虽然目标是复刻，但敏感配置不再硬编码到源码，而是统一改成环境变量与 `.env.example`。

### 4.5 Preserve Current Runtime Semantics First

第一阶段需要优先保留当前 Java 代码的真实运行语义，而不是对照 README 或其他可能过期的说明文档做“想当然复刻”。

需要特别保留的现状包括：

- `AiChat` 统一承载 system prompt、input guardrail、memory、contentRetriever、tools 和 MCP tool provider
- `/multiAgentChat` 在 `SimpleOrchestrator` 中先显式做一次知识检索，但最终仍通过 `ReasoningAgent -> aiChat.chat()` 进入主链路，因此它是“显式知识增强 + 主链路内建 RAG/记忆/工具”的叠加
- 成功响应是原始文本或流式文本，异常响应才会进入 `BaseResponse` JSON 包装
- 当前 pgvector 初始化会 `dropTableFirst(true)`，也就是服务启动时会重建向量表

### 4.6 Source of Truth Precedence

本仓库中有若干补充说明文档，但其中部分参数已经与当前代码漂移。Go 复刻阶段一律按以下优先级取值：

1. 当前 Java 源码
2. 当前 `application.yml`
3. 测试代码
4. 其他说明文档

## 5. Source Project Inventory

当前 Java 项目的功能边界大致如下：

| Java 模块 | 责任 | Go 复刻策略 |
| --- | --- | --- |
| `controller/AiChatController` | 聊天、流式聊天、多 Agent、知识插入接口 | 用 Gin/Fiber handler 逐个复刻 |
| `ai/AiChat` | system prompt、guardrail 绑定、memoryId 入口 | 用 Go 的 prompt loader + guardrail + session binding 复刻 |
| `ai/AiChatService` | 组装模型、工具、记忆、检索 | 用 Eino chat orchestration + service layer 复刻 |
| `orchestrator/SimpleOrchestrator` | 简单知识路由与推理编排 | 原样保留为轻量编排器 |
| `agent/*` | KnowledgeAgent、ReasoningAgent | 保留角色边界，Go 中实现为 service |
| `rag/*` | 文档切分、预处理、检索、重排 | 用 Go service 实现，必要处接 Eino |
| `memory/*` | Redis 记忆、压缩、分布式锁 | 用 Go 封装 Redis memory service |
| `tool/*` | 时间、邮件、知识写回工具 | 在 Go 中定义工具接口并接入 Eino |
| `guardrail/*` | 输入敏感词拦截 | 用 Go middleware 或 pre-chat validator 复刻 |
| `Exception/*` + `common/*` | 全局异常与错误包裹 | 保留“成功文本，异常 JSON”的混合返回契约 |
| `config/*` | 模型、向量库、记忆、MCP、CORS 等配置 | Go 配置模块统一初始化 |
| `Monitor/*` + `interceptor/*` | 指标、日志、上下文 | Go middleware + Prometheus 指标注册 |
| `job/*` | 启动加载文档、定时自动重载 | Go 启动任务 + ticker job |
| `resources/system-prompt/*` | 人设与输出格式约束 | Go 中保留资源文件并在聊天链路加载 |
| `resources/front/*` | 简单演示页 | Go 静态文件服务复刻 |

## 6. Target Architecture

## 6.1 Repository Layout

为避免影响原 Java 项目，Go 复刻版放在仓库根目录下的 `go-port/`：

```text
go-port/
├─ cmd/server
├─ internal/app/chat
├─ internal/transport/http
├─ internal/agent/orchestrator
├─ internal/agent/knowledge
├─ internal/agent/reasoning
├─ internal/rag
├─ internal/memory
├─ internal/tools
├─ internal/observability
├─ internal/store/postgres
├─ internal/store/redis
├─ internal/config
├─ pkg/llm
├─ web
├─ deployments/docker
└─ monitoring
```

## 6.2 Runtime Components

- `HTTP Server`
  对外暴露 REST API、SSE、静态资源与监控路由
- `Chat Application Service`
  负责聚合请求上下文、调用 orchestrator、封装响应
- `Orchestrator`
  负责决定是否先检索知识、如何增强输入、何时调用工具
- `Knowledge Agent`
  负责 RAG 查询
- `Reasoning Agent`
  负责调用模型并输出最终回答
- `RAG Service`
  负责文档扫描、切分、向量化、检索、rerank 和写回
- `Memory Service`
  负责 Redis 会话存储、压缩、锁、TTL
- `Tool Registry`
  负责统一注册时间、邮件、知识写回、联网搜索等工具
- `Observability`
  负责 request ID、结构化日志、Prometheus 指标

## 7. Module Design

## 7.1 HTTP Transport

Go 版本将复刻以下接口：

- `POST /api/chat`
- `POST /api/streamChat`
- `POST /api/multiAgentChat`
- `POST /api/insert`
- `GET /healthz`
- `GET /metrics`
- `GET /actuator/prometheus`

设计上保留 Java 版本的接口语义：

- `chat` 返回普通文本结果
- `streamChat` 返回 SSE 流式分片
- `multiAgentChat` 先经过简易编排器
- `insert` 同时写入文档与向量库

请求体 DTO 与 Java 对齐：

- `ChatRequest`
  - `sessionId: Long`
  - `userId: Long`
  - `prompt: String`
- `KnowledgeRequest`
  - `question: String`
  - `answer: String`
  - `sourceName: String`

响应契约也要按当前实现复刻：

- 正常情况下：
  - `/api/chat` 返回纯文本
  - `/api/multiAgentChat` 返回纯文本
  - `/api/insert` 返回纯文本状态消息
  - `/api/streamChat` 返回流式文本
- 异常情况下：
  - 由全局异常处理器返回 `BaseResponse` 风格 JSON

这是一个需要显式保留的“混合契约”，不能被新实现默认改造成统一 JSON。

## 7.2 Application Service

`internal/app/chat` 作为统一业务入口，职责包括：

- 请求参数校验
- 监控上下文初始化
- 调用普通聊天或多 Agent 流程
- 在流式响应结束时清理上下文

这样可以避免把过多业务逻辑直接塞进 HTTP handler。

## 7.3 AI Contract Layer

Go 版本必须单独复刻 Java 当前的“AI 合同层”：

- `system-prompt/chat-bot.txt`
  - 规定知途的身份
  - 要求回答简明扼要、逻辑清晰
  - 要求输出纯文本
  - 要求代码片段用 ``` 包裹
- `SafeInputGuardrail`
  - 当前只拦截包含 `死`、`杀` 的输入
- `GlobalExceptionHandler`
  - 对 Guardrail 异常返回统一错误码

这里不要提前“升级成更聪明的安全系统”。第一阶段要先复刻当前的简单规则和资源文件，再在第二阶段扩展。

## 7.4 Agent Layer

为了贴近原项目，Go 版本保留“简单多 Agent”结构，但不把它做成过重的多智能体系统。

- `KnowledgeAgent`
  接收原始问题，调用 RAG 检索，返回知识片段
- `ReasoningAgent`
  接收增强后的输入，调用 Eino ChatModel 输出结果
- `SimpleOrchestrator`
  使用与 Java 类似的关键词判断是否触发知识检索，并拼装增强提示

这是一个轻量编排层，本质上是“规则路由 + 模型推理”。

但要注意：

- `ReasoningAgent` 并不是“裸调模型”，而是要进入与普通 `/chat` 相同的主链路
- 因此 `multiAgentChat` 的最终行为应包含：
  - system prompt
  - input guardrail
  - session memory
  - content retriever
  - tools
  - MCP tools

这意味着 Go 版不能把 `multiAgentChat` 简化成“知识检索后直接调用一个不带上下文的模型方法”。

## 7.5 RAG Layer

RAG 是本项目的核心复刻对象，第一阶段的行为目标如下：

- 启动时扫描 `docs` 目录并加载现有文档
- 对文档执行切分
- 使用 embedding 模型写入 pgvector
- 检索时召回较多候选
- 使用 rerank 缩小最终结果集
- 将命中内容拼接回模型上下文
- 支持用户通过 `/api/insert` 写回新知识
- 支持定时扫描目录变更并自动增量加载

当前代码中的关键参数也要直接落进 Go 版默认值：

- 向量维度：`1024`
- 文档切分：`maxChunkSize=800`、`chunkOverlap=200`
- 检索粗排：`maxResults=30`、`minScore=0.55`
- rerank 输出：`finalTopN=5`
- 自动重载频率：`300000ms`，即每 5 分钟
- 查询预处理：移除标点、停用词、规范化空格
- 文本转换：写入向量前在段落文本前拼上 `file_name + "\n"`

还需要保留两个容易被忽略的现状：

- `EmbeddingStoreConfig` 当前会 `dropTableFirst(true)`，启动会重建向量表
- `RerankVerifier` 会在 `rerank.test.enabled=true` 时启动自检

建议实现：

- `DocumentLoader`
- `DocumentSplitter`
- `EmbeddingIndexer`
- `Retriever`
- `Reranker`
- `KnowledgeWriter`
- `AutoReloadJob`

## 7.6 Memory Layer

Go 版记忆系统尽量贴近 Java 版本：

- 以 `sessionId` 为记忆隔离键
- 消息达到数量阈值或 token 阈值后触发压缩
- 使用 Redis TTL 控制会话过期
- 使用 Redis 分布式锁避免并发覆盖
- 压缩失败时降级保留最近 N 轮消息

这里不强依赖 Eino 内建记忆能力，优先用普通 Go service 实现，避免复杂框架适配。

还要保留当前压缩器的“简化实现”语义：

- 不是调用 LLM 生成摘要
- 而是把早期消息做简单文本摘要
- 最近对话窗口默认保留 5 轮
- token 估算使用“字符数 / 4”的近似值

不要在第一阶段擅自升级成更复杂的语义摘要方案。

## 7.7 Tools

第一阶段保留以下工具能力：

- `TimeTool`
- `EmailTool`
- `RagTool.addKnowledge`
- `WebSearchTool`

实现策略：

- 与模型对话直接相关的工具通过 Eino tool 接口接入
- 对外部服务的调用封装为普通 Go client
- MCP 若能快速接通则保留；若接入成本高，先以等价搜索工具完成功能复刻

还需要保留当前工具返回风格：

- `TimeTool` 返回上海时区格式化时间字符串
- `EmailTool` 返回发送成功/失败文案
- `RagTool.addKnowledge` 追加 Markdown 文件并同步向量库
- `RagTool.retrieve` 输出 `"【来源：%s | 相似度：%.2f】\n%s"` 风格文本

## 7.8 Observability

复刻目标不是只打印日志，而是保留 Java 版的核心指标意识：

- 请求总数
- 请求成功/失败数
- 模型响应耗时
- token 使用量
- RAG 查询次数、命中数、重排耗时
- 工具调用次数、成功率
- request ID 和 session ID 透传

Go 版将提供：

- HTTP middleware 注入 request ID
- chat 调用前后埋点
- SSE 生命周期埋点
- `/metrics` 暴露给 Prometheus
- 兼容 `/actuator/prometheus` 别名，方便展示“与原项目接口形态接近”

指标名称也建议直接延续当前代码：

- `ai_model_requests_total`
- `ai_model_errors_total`
- `ai_model_tokens_total`
- `ai_model_response_duration_seconds`
- `rag_retrieval_hit_total`
- `rag_retrieval_miss_total`
- `rag_retrieval_duration_seconds`

## 7.9 Frontend

前端不是本项目重点，但需要服务于简历展示和录屏：

- 保留一个轻量聊天页作为主入口
- 额外可提供 `qwen.html / gpt.html / gemini.html` 三个演示页面名称，保持与原项目资源结构接近
- 支持普通请求与流式请求
- 支持输入 `userId` 与 `sessionId`
- 展示响应分段、错误提示、基础 loading 状态

前端复刻要点：

- 会话列表保存在浏览器 localStorage
- 页面会自动生成 6 位 `userId`
- `qwen.html` 和 `gemini.html` 当前都走 `/api/chat`
- `gpt.html` 当前走 `/api/streamChat`
- 主题切换、会话删除、新建会话等交互都属于现有演示的一部分

前端不必完全追求 CSS 像素级一致，但交互结构和页面文件名应尽量保留。

## 7.10 Deployment

Go 版提供 `docker-compose.yml`，至少拉起：

- `go-app`
- `redis`
- `postgres + pgvector`
- `prometheus`
- `grafana`

其中：

- 文档目录通过 volume 映射
- 配置走 `.env`
- Grafana 默认加载预置 dashboard

## 8. Request Flows

## 8.1 Chat Flow

`POST /api/chat`

1. handler 解析请求并注入监控上下文
2. application service 获取/构造 session memory
3. 加载 system prompt 并执行 input guardrail
4. reasoning agent 调用主聊天链路
5. 主聊天链路执行 memory + contentRetriever + tools + MCP tools
6. 将新消息写回 Redis
7. 记录模型与请求指标
8. 返回最终文本

## 8.2 Stream Chat Flow

`POST /api/streamChat`

1. handler 建立 SSE 响应
2. application service 加载 system prompt 并执行 input guardrail
3. 调用 streaming chat 主链路
4. 每个 token/chunk 通过 SSE 写回前端
5. 结束后写回完整 assistant 消息
6. 记录耗时和 token 指标
7. 清理上下文

## 8.3 Multi-Agent Chat Flow

`POST /api/multiAgentChat`

1. orchestrator 判断是否命中知识查询关键词
2. 若命中，先调用 knowledge agent 获取知识内容
3. 将知识拼接进增强 prompt
4. 调用 reasoning agent 进入主聊天链路
5. 主聊天链路继续执行 system prompt、guardrail、memory、contentRetriever、tools
6. 写回会话与指标

## 8.4 Insert Knowledge Flow

`POST /api/insert`

1. 将问答格式化为 Markdown 内容
2. 追加写入指定知识文件
3. 生成 embedding 并写入 pgvector
4. 返回成功/部分成功/失败结果

## 8.5 Auto Reload Flow

定时任务每隔一段时间扫描文档目录：

1. 对比文件更新时间戳
2. 找到新增或变更文档
3. 增量切分与 embedding
4. 更新向量索引

## 9. Data and Configuration

## 9.1 External Dependencies

- DashScope/Qwen：聊天、流式聊天、embedding、rerank
- Redis：会话存储、锁、TTL
- PostgreSQL + pgvector：向量存储
- SMTP：邮件发送
- Web Search API/MCP：联网搜索
- Prometheus + Grafana：监控

## 9.2 Environment Variables

统一通过环境变量管理：

- `APP_PORT`
- `APP_CONTEXT_PATH`
- `QWEN_API_KEY`
- `QWEN_CHAT_MODEL`
- `QWEN_EMBEDDING_MODEL`
- `QWEN_RERANK_MODEL`
- `QWEN_RERANK_VERIFY_ON_STARTUP`
- `REDIS_ADDR`
- `REDIS_PASSWORD`
- `POSTGRES_DSN`
- `PGVECTOR_DIMENSION`
- `PGVECTOR_TABLE`
- `PGVECTOR_DROP_TABLE_FIRST`
- `RAG_DOCS_PATH`
- `RAG_RETRIEVE_TOP_K`
- `RAG_MIN_SCORE`
- `RAG_FINAL_TOP_N`
- `RAG_AUTO_RELOAD_INTERVAL`
- `SMTP_HOST`
- `SMTP_PORT`
- `SMTP_USER`
- `SMTP_PASS`
- `WEB_SEARCH_API_KEY`
- `SYSTEM_PROMPT_PATH`
- `GUARDRAIL_SENSITIVE_WORDS`
- `MEMORY_MAX_MESSAGES`
- `MEMORY_TOKEN_THRESHOLD`
- `MEMORY_RECENT_ROUNDS`
- `MEMORY_RECENT_TOKEN_LIMIT`
- `MEMORY_FALLBACK_RECENT_ROUNDS`

## 10. Testing Strategy

第一阶段测试强调“能稳定复刻”，而不是极度追求测试花活。

测试分层：

- 单元测试
  - 配置加载
  - orchestrator 路由判断
  - guardrail 拦截逻辑
  - system prompt 资源加载
  - memory 压缩触发逻辑
  - RAG 查询参数拼装
  - 工具输入输出
- 集成测试
  - Redis 会话读写
  - pgvector 检索
  - `/api/chat`
  - `/api/streamChat`
  - `/api/insert`
  - Guardrail 异常返回 JSON 包裹
  - `/api/multiAgentChat` 的显式知识增强 + 主链路叠加行为
- 端到端验证
  - Docker Compose 启动全链路
  - 演示页成功对话
  - Prometheus 抓到指标

## 11. Risks and Mitigations

### 11.1 Eino and LangChain4j Are Not 1:1

风险：
部分 Java 中由 LangChain4j 自动完成的行为，在 Go 中需要自己补 service 层。

应对：
坚持“业务流程自己掌握，Eino 只负责模型与工具编排”的思路。

### 11.2 MCP Compatibility Cost

风险：
Go 端如果真实接 MCP transport，调通成本可能高于项目收益。

应对：
优先实现功能等价的 Web Search Tool，后续再补协议层一致性。

### 11.3 Full Replica Scope Expansion

风险：
用户容易在“先复刻”阶段加入过多新能力，导致完工时间不可控。

应对：
明确第一阶段以复刻为准，新增优化全部排到第二阶段。

## 12. Delivery Plan

建议按以下顺序完成：

1. Go 项目脚手架与配置加载
2. HTTP API 与模型调用
3. Redis 会话记忆
4. pgvector 与 RAG
5. 工具调用与多 Agent 编排
6. 观测指标与日志
7. 演示前端
8. Docker Compose 与监控
9. 端到端验收

## 13. Final Recommendation

这次 Go 复刻应明确采用以下策略：

- 先完整复刻当前项目，不抢跑优化
- 允许框架实现方式不同，但用户可见功能尽量等价
- 保留 Java 版本作为对照参考
- 在 Go 版完成后，再逐步做更 Go 化、更工程化、更适合面试讲解的第二阶段优化

这条路线最适合当前目标：先拿到一个完整、可信、好讲、好继续扩展的 Go Agent 项目。
