# ZhituAgent 项目启动指南

> 此文档是给 AI coding 助手的完整上下文，用于在另一个会话中指导 AI 从零完成整个 Go 项目的编码实现。

---

## 你的任务

在 `D:\dev\my_proj\go\ZhituAgent` 目录下，用 **Go + Eino 框架** 从零搭建一个 AI 智能体项目（知途/ZhituAgent），实现所有核心功能，确保项目可以独立运行、展示和写进简历。

---

## 参考文档位置

以下三份文档都在当前项目目录下，按优先级排列：

1. **`GO_IMPLEMENTATION_PLAN.md`**（最高优先级，与本文档同目录）
   - 包含原 Java 项目全部 43 个源文件的逐文件分析
   - 每个模块的 Go 实现代码骨架
   - 完整的 API 接口规格、配置参数、系统 Prompt
   - Eino 框架组件对应关系

2. **`docs/superpowers/specs/2026-04-16-go-eino-full-replica-design.md`**
   - 项目设计规格书，定义了功能边界、复刻原则和成功标准
   - 关键约束：功能等价优先、保留 Java 运行语义、成功返回纯文本/异常返回 JSON

3. **`docs/superpowers/plans/2026-04-16-go-eino-full-replica.md`**
   - 12 个 Task 的分步实现计划，含 TDD 测试代码
   - 推荐按此顺序实现，但目录结构需调整（见下文）

4. **原 Java 源码目录**（最终参考源，不在本项目中）
   - `D:\dev\learn_proj\hualvqing\QianyanAgent\src\main\java\`
   - `D:\dev\learn_proj\hualvqing\QianyanAgent\src\main\resources\`
   - 当文档与源码冲突时，以源码为准

---

## 关键设计决策（已确定，请遵守）

### 1. 项目目录

项目在 **独立目录** `D:\dev\my_proj\go\ZhituAgent` 中开发，不是原 Java 仓库的子目录。

目录结构采用以下布局（融合了三份文档的最佳方案）：

```
D:\dev\my_proj\go\ZhituAgent/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── handler/
│   │   └── chat_handler.go
│   ├── middleware/
│   │   ├── cors.go
│   │   ├── guardrail.go
│   │   ├── observability.go
│   │   └── error_handler.go
│   ├── agent/
│   │   ├── agent.go
│   │   ├── knowledge_agent.go
│   │   ├── reasoning_agent.go
│   │   └── orchestrator.go
│   ├── chat/
│   │   └── service.go
│   ├── memory/
│   │   ├── redis_store.go
│   │   ├── compressor.go
│   │   └── compressible_memory.go
│   ├── rag/
│   │   ├── store.go
│   │   ├── indexer.go
│   │   ├── retriever.go
│   │   ├── reranking_retriever.go
│   │   ├── rerank_client.go
│   │   ├── query_preprocessor.go
│   │   ├── document_splitter.go
│   │   ├── data_loader.go
│   │   ├── auto_reload.go
│   │   └── rerank_verifier.go
│   ├── tool/
│   │   ├── time_tool.go
│   │   ├── email_tool.go
│   │   └── rag_tool.go
│   ├── monitor/
│   │   ├── context.go
│   │   ├── ai_metrics.go
│   │   ├── rag_metrics.go
│   │   ├── model_listener.go
│   │   └── logger.go
│   └── common/
│       ├── errno.go
│       └── response.go
├── model/
│   └── dto.go
├── system-prompt/
│   └── chat-bot.txt
├── static/
│   ├── gpt.html
│   ├── ai.png
│   └── user.png
├── docs/
├── config.yaml
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

### 2. 向量存储：使用 Redis（不是 PgVector）

**原因：** Eino 框架没有 PgVector 适配器，但有官方的 `eino-ext/components/indexer/redis` 和 `eino-ext/components/retriever/redis`。使用 Redis 同时承担向量存储和会话记忆，架构更简洁，部署也更简单（少一个 PostgreSQL 依赖）。

这意味着：
- 使用 `eino-ext/components/indexer/redis` 做文档向量存储
- 使用 `eino-ext/components/retriever/redis` 做向量检索（FT.SEARCH）
- 使用 `eino-ext/components/embedding/dashscope` 做 Embedding（text-embedding-v3，1024 维度）
- docker-compose.yml 中不需要 PostgreSQL，只需要 Redis + Prometheus + Grafana

### 3. 配置方式：config.yaml + 环境变量覆盖

使用 Viper 加载 `config.yaml`，同时支持环境变量覆盖（敏感信息走 `.env`）。

### 4. 响应契约（必须遵守）

这是 Java 版的"混合契约"，必须完整保留：

- `/api/chat` 成功 → 返回**纯文本**
- `/api/streamChat` 成功 → 返回 **SSE 流式文本**
- `/api/multiAgentChat` 成功 → 返回**纯文本**
- `/api/insert` 成功 → 返回**纯文本**
- **任何接口异常** → 返回 `{"code": xxxxx, "data": null, "message": "错误信息"}` JSON

### 5. multiAgentChat 的完整语义（容易做错）

`/api/multiAgentChat` **不是**简单的"知识检索 + 裸调模型"，而是：

1. Orchestrator 判断是否命中知识关键词
2. 命中时：先调用 KnowledgeAgent 检索知识，拼接到增强 prompt 中
3. **然后通过 ReasoningAgent 进入与 /chat 相同的主链路**（包含 system prompt + guardrail + memory + contentRetriever + tools + MCP tools）

这意味着 multiAgentChat 的最终行为 = 显式知识增强 + 主链路内建 RAG/记忆/工具的叠加。

---

## 实现顺序（严格按此顺序推进）

### Phase 1: 项目骨架 + 基本对话
1. `go mod init` + 目录结构
2. `internal/config/config.go` + `config.yaml`
3. `internal/common/errno.go` + `response.go`
4. `internal/model/dto.go`
5. `internal/middleware/cors.go` + `observability.go` + `error_handler.go`
6. `internal/handler/chat_handler.go`（占位路由）
7. `cmd/server/main.go`（Gin 启动）
8. `system-prompt/chat-bot.txt`（复制 Java 版内容）
9. `internal/chat/service.go`（接入 eino-ext/model/qwen）
10. 实现 `/api/chat` 基本对话

**验收：** `curl -X POST http://localhost:10010/api/chat -d '{"sessionId":1,"userId":1,"prompt":"你好"}'` 能返回 AI 回复

### Phase 2: RAG 全链路
1. `internal/rag/store.go`（Redis Indexer + Retriever 初始化）
2. `internal/rag/document_splitter.go`（递归切片 800/200）
3. `internal/rag/query_preprocessor.go`（停用词 + 标点）
4. `internal/rag/rerank_client.go`（DashScope Rerank HTTP API）
5. `internal/rag/reranking_retriever.go`（两阶段检索 + 降级）
6. `internal/rag/indexer.go`（文档入库编排）
7. `internal/rag/data_loader.go`（启动加载）
8. `internal/rag/auto_reload.go`（定时扫描）
9. `internal/rag/rerank_verifier.go`（启动验证）
10. RAG 注入 ChatService
11. 实现 `/api/insert` 知识插入接口

**验收：** 上传文档后对话能检索到知识，Rerank 精排生效

### Phase 3: 会话记忆 + 工具调用
1. `internal/memory/redis_store.go`（Redis CRUD）
2. `internal/memory/compressor.go`（Token 计数 + 摘要）
3. `internal/memory/compressible_memory.go`（分布式锁 + 压缩 + 降级）
4. `internal/tool/time_tool.go`
5. `internal/tool/email_tool.go`
6. `internal/tool/rag_tool.go`（检索 + 动态添加知识）
7. MCP 工具集成（eino-ext/tool/mcp）
8. 工具注册到 Agent

**验收：** 多轮对话有上下文记忆，能调用时间和邮件工具

### Phase 4: 多 Agent 编排
1. `internal/agent/agent.go`（接口定义）
2. `internal/agent/knowledge_agent.go`
3. `internal/agent/reasoning_agent.go`
4. `internal/agent/orchestrator.go`（关键词分流）
5. 实现 `/api/multiAgentChat` 接口

**验收：** 包含"查询"/"什么是"等关键词的请求走知识检索路径

### Phase 5: 安全 + 可观测 + 流式
1. `internal/middleware/guardrail.go`（敏感词拦截）
2. `internal/monitor/ai_metrics.go`（Prometheus AI 模型指标）
3. `internal/monitor/rag_metrics.go`（Prometheus RAG 指标）
4. `internal/monitor/context.go` + `logger.go`
5. `/metrics` 端点
6. 实现 `/api/streamChat` SSE 流式输出
7. 前端页面（复制 Java 版 gpt.html）

**验收：** 敏感词被拦截，Prometheus 能采集指标，流式输出正常

### Phase 6: 部署 + 测试
1. `docker-compose.yml`（Redis + Prometheus + Grafana）
2. `Dockerfile`
3. `.env.example`
4. `Makefile`
5. 单元测试
6. 集成测试

**验收：** `docker compose up` 一键启动，Grafana 能看到指标

---

## Eino 框架核心依赖

```
github.com/cloudwego/eino                                    # 核心框架
github.com/cloudwego/eino/adk                                 # Agent 开发套件
github.com/cloudwego/eino/compose                              # Graph 编排
github.com/cloudwego/eino-ext/components/model/qwen             # Qwen 对话模型
github.com/cloudwego/eino-ext/components/embedding/dashscope   # DashScope Embedding
github.com/cloudwego/eino-ext/components/indexer/redis         # Redis 向量存储
github.com/cloudwego/eino-ext/components/retriever/redis       # Redis 向量检索
github.com/cloudwego/eino-ext/components/tool/mcp              # MCP 工具协议
github.com/cloudwego/eino-ext/components/document/transformer/splitter  # 文档切片
github.com/gin-gonic/gin
github.com/redis/go-redis/v9
github.com/prometheus/client_golang
github.com/spf13/viper
github.com/google/uuid
```

---

## Java 版核心参数（Go 版必须对齐）

| 参数 | 值 | 来源 |
|---|---|---|
| 服务端口 | 10010 | application.yml |
| 上下文路径 | /api | application.yml |
| Qwen 对话模型 | qwen-max | DashScopeModelConfig.java |
| Embedding 模型 | text-embedding-v3（Java 用 v4，Eino 只支持到 v3） | EmbeddingStoreConfig.java |
| 向量维度 | 1024 | EmbeddingStoreConfig.java |
| 文档切片 maxChunkSize | 800 | RagConfig.java |
| 文档切片 chunkOverlap | 200 | RagConfig.java |
| 粗排 maxResults | 30 | RagConfig.java |
| 粗排 minScore | 0.55 | RagConfig.java |
| 精排 finalTopN | 5 | RagConfig.java |
| 会话记忆 maxMessages | 20 | ChatMemoryConfig.java |
| Token 阈值 | 6000 | ChatMemoryConfig.java |
| 保留最近轮数 | 5 | ChatMemoryConfig.java |
| 最近对话 Token 上限 | 2000 | ChatMemoryConfig.java |
| 降级保留轮数 | 10 | ChatMemoryConfig.java |
| Redis TTL | 3600 秒 | application.yml |
| 分布式锁过期 | 5 秒 | ChatMemoryConfig.java |
| 锁重试次数 | 3 | ChatMemoryConfig.java |
| 锁重试间隔 | 100ms | ChatMemoryConfig.java |
| Rerank 模型 | qwen3-rerank | application.yml |
| Rerank API | https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank | QwenRerankClient.java |
| 自动重载间隔 | 5 分钟 | RagAutoReloadJob.java |
| 知识检索关键词 | 查询/了解/什么是/介绍/解释/说明 | SimpleOrchestrator.java |
| 敏感词 | 死/杀 | SafeInputGuardrail.java |
| Token 估算 | text.length / 4 | TokenCountChatMemoryCompressor.java |
| 文本转换 | 向量入库前在文本前拼接 file_name + "\n" | RagConfig.java |

---

## 实现注意事项

1. **先读 GO_IMPLEMENTATION_PLAN.md**：里面有每个模块的 Go 代码骨架，直接参考实现
2. **遇到框架 API 不确定时**：读 eino 和 eino-ext 的源码或 README，不要猜
3. **Redis 向量检索需要预先创建索引**：参考 `eino-ext/components/retriever/redis` 的 README，Redis 客户端需设置 `Protocol: 2` + `UnstableResp3: true`
4. **压缩器不要升级**：Java 版用的是简单文本截取摘要（取前3条消息各截50字符），不是 LLM 摘要。第一阶段原样复刻
5. **dashscope embedding 版本**：Eino 的 dashscope embedding 只支持 v1/v2/v3（不支持 v4），使用 v3 + dimensions=1024 对齐 Java 版
6. **锁释放用 Lua 脚本**：与 Java 版一致，保证原子性
7. **配置不要硬编码密钥**：所有 API Key 走环境变量或 .env
8. **RagTool 有两个方法**：retrieve(query) 供 KnowledgeAgent 调用；addKnowledge(question, answer, fileName) 供 LLM 工具调用自动写入知识
9. **前端页面**：从原 Java 项目 `D:\dev\learn_proj\hualvqing\QianyanAgent\src\main\resources\front\gpt.html` 复制到本项目的 `static/gpt.html`，修改 `API_URL` 即可
10. **MCP 工具**：如果 eino-ext/tool/mcp 接入成本高，优先用等价的普通 Go 工具实现（如内置 WebSearchTool），后续再补 MCP 协议层

---

## 开始实现

请先阅读以上三份参考文档（尤其是 GO_IMPLEMENTATION_PLAN.md），然后从 Phase 1 开始逐步实现。每完成一个 Phase，运行验收标准确认后再进入下一个 Phase。

**现在就开始 Phase 1。**
