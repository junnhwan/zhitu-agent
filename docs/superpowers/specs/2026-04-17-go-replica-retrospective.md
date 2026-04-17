# ZhituAgent Go 复刻复盘

> 日期：2026-04-17
> 版本：Phase 1 完整复刻

---

## 1. 项目概述

将原 Java + Spring Boot + LangChain4j 的 AI 智能体项目，用 Go + Eino 框架完整复刻，达成功能等价。

原项目核心能力：普通聊天、SSE 流式聊天、多 Agent 协调、RAG 文档检索增强、Redis 会话记忆与压缩、工具调用（时间/邮件/联网搜索）、系统提示词约束、输入 Guardrail、Prometheus 监控、前端演示页。

---

## 2. 架构差异

| 维度 | Java 版 | Go 版 | 理由 |
|---|---|---|---|
| 框架 | Spring Boot + LangChain4j | Gin + Eino | Go 生态对应选择 |
| 向量存储 | PostgreSQL + pgvector | Redis (RediSearch + HNSW) | Eino 有官方 Redis indexer/retriever，无 pgvector 适配器；少一个 PostgreSQL 依赖 |
| Embedding | text-embedding-v4 | text-embedding-v3 (dimensions=1024) | Eino dashscope embedding 仅支持到 v3 |
| 配置 | application.yml | config.yaml + 环境变量覆盖 (Viper) | Go 生态惯例 |
| 记忆 | LangChain4j ChatMemory | 自实现 RedisStore + Compressor | 避免 Eino 记忆接口适配复杂度 |
| MCP | LangChain4j MCP ToolProvider | 未接入 | 接入成本高，暂用等价工具替代 |

---

## 3. 模块完成度

### 3.1 已完成模块

| Java 模块 | Go 实现 | 文件 |
|---|---|---|
| `AiChatController` | ChatHandler | `internal/handler/chat_handler.go` |
| `AiChat` / `AiChatService` | ChatService (interface + impl) | `internal/chat/interface.go` + `service.go` |
| `SimpleOrchestrator` | Orchestrator | `internal/agent/orchestrator.go` |
| `KnowledgeAgent` | KnowledgeAgent | `internal/agent/knowledge_agent.go` |
| `ReasoningAgent` | ReasoningAgent | `internal/agent/reasoning_agent.go` |
| `RagConfig` / `DocumentSplitter` | RAG 全链路 | `internal/rag/` (9 文件) |
| `ChatMemoryConfig` / `RedisChatMemoryStore` | Redis 记忆 + 压缩 + 分布式锁 | `internal/memory/` (3 文件) |
| `TimeTool` | TimeTool | `internal/tool/time_tool.go` |
| `EmailTool` | EmailTool | `internal/tool/email_tool.go` |
| `RagTool` | RagTool.addKnowledge | `internal/tool/rag_tool.go` |
| `SafeInputGuardrail` | Guardrail 中间件 | `internal/middleware/guardrail.go` |
| `GlobalExceptionHandler` | 错误处理中间件 | `internal/middleware/error_handler.go` |
| CORS 配置 | CORS 中间件 | `internal/middleware/cors.go` |
| 配置类 | Config | `internal/config/config.go` + `config.yaml` |
| `Monitor` / `Interceptor` | 可观测 | `internal/monitor/` (5 文件) + `middleware/observability.go` |
| `RagAutoReloadJob` | 自动重载 | `internal/rag/auto_reload.go` |
| `DataLoadingJob` | 启动加载 | `internal/rag/data_loader.go` |
| `system-prompt/chat-bot.txt` | 知途技术顾问人设 | `system-prompt/chat-bot.txt` |
| `resources/front/gpt.html` | 演示页 | `static/gpt.html` |

### 3.2 未完成项

| 项目 | 优先级 | 说明 |
|---|---|---|
| WebSearchTool（联网搜索） | 高 | Java 版通过 MCP 接入联网搜索，Go 版暂无等价实现，影响实时问题回答能力 |
| `/actuator/prometheus` 别名路由 | 低 | 设计规格要求兼容此路径，当前仅有 `/metrics` |
| `qwen.html` / `gemini.html` 演示页 | 中 | Java 版有三个演示页（分别走 /api/chat 和 /api/streamChat），当前只有 gpt.html |
| MCP 工具协议集成 | 低 | 设计规格允许先用等价工具替代，后续再补 eino-ext/tool/mcp |

---

## 4. API 接口对照

| 接口 | Java 行为 | Go 实现 | 契约对齐 |
|---|---|---|---|
| `POST /api/chat` | 返回纯文本 | ✅ | ✅ |
| `POST /api/streamChat` | 返回 SSE 流式文本 | ✅ | ✅ |
| `POST /api/multiAgentChat` | 返回纯文本（知识增强 + 主链路叠加） | ✅ | ✅ |
| `POST /api/insert` | 返回纯文本状态消息 | ✅ | ✅ |
| `GET /healthz` | 健康检查 | ✅ | ✅ |
| `GET /metrics` | Prometheus 指标 | ✅ | ✅ |
| `GET /actuator/prometheus` | 兼容别名 | ❌ | 未实现 |

异常响应契约：所有接口异常均返回 `{"code": xxxxx, "data": null, "message": "错误信息"}` JSON — ✅ 对齐。

---

## 5. 关键参数对齐

| 参数 | Java 值 | Go 值 | 对齐 |
|---|---|---|---|
| 服务端口 | 10010 | 10010 | ✅ |
| 上下文路径 | /api | /api | ✅ |
| Qwen 对话模型 | qwen-max | qwen-max | ✅ |
| Embedding 模型 | text-embedding-v4 | text-embedding-v3 | ⚠️ 降级（Eino 限制） |
| 向量维度 | 1024 | 1024 | ✅ |
| 文档切片 maxChunkSize | 800 | 800 | ✅ |
| 文档切片 chunkOverlap | 200 | 200 | ✅ |
| 粗排 maxResults | 30 | 30 | ✅ |
| 粗排 minScore | 0.55 | 0.55 | ✅ |
| 精排 finalTopN | 5 | 5 | ✅ |
| 会话 maxMessages | 20 | 20 | ✅ |
| Token 阈值 | 6000 | 6000 | ✅ |
| 保留最近轮数 | 5 | 5 | ✅ |
| 最近对话 Token 上限 | 2000 | 2000 | ✅ |
| 降级保留轮数 | 10 | 10 | ✅ |
| Redis TTL | 3600s | 3600s | ✅ |
| 分布式锁过期 | 5s | 5s | ✅ |
| 锁重试次数 | 3 | 3 | ✅ |
| 锁重试间隔 | 100ms | 100ms | ✅ |
| Rerank 模型 | qwen3-rerank | qwen3-rerank | ✅ |
| 自动重载间隔 | 5 分钟 | 5 分钟 | ✅ |
| 知识检索关键词 | 查询/了解/什么是/介绍/解释/说明 | 同 | ✅ |
| 敏感词 | 死/杀 | 死/杀 | ✅ |
| Token 估算 | text.length / 4 | text.length / 4 | ✅ |
| 向量入库文本前缀 | file_name + "\n" | file_name + "\n" | ✅ |

---

## 6. 设计规格成功标准验收

| 成功标准 | 状态 |
|---|---|
| 能独立启动并提供 `/api/chat`、`/api/streamChat`、`/api/multiAgentChat`、`/api/insert` | ✅ |
| 能加载本地知识文档并写入向量库 | ✅ |
| 能基于用户问题执行检索增强回答 | ✅ |
| 能使用 Redis 保存会话上下文，并支持压缩与 TTL | ✅ |
| 能调用时间工具、邮件工具、联网搜索工具 | ⚠️ 时间✅ 邮件✅ 联网搜索❌ |
| 能复刻系统提示词、输入 Guardrail 与错误返回契约 | ✅ |
| 能暴露 Prometheus 指标，并由 Grafana 读取展示 | ✅ |
| 能通过 Docker Compose 拉起全栈 | ✅ |
| 能通过静态演示页完成普通对话和流式对话演示 | ⚠️ 仅 gpt.html，缺 qwen.html / gemini.html |

**9 条标准中 7 条完全通过，2 条部分通过。**

---

## 7. 测试覆盖

| 类型 | 文件数 | 用例数 | 状态 |
|---|---|---|---|
| 单元测试 | 7 | 24 | 全部通过 |
| 集成测试 (handler) | 1 | 7 | 全部通过 |
| **合计** | **8** | **31** | **全部通过** |

已覆盖模块：common/errno、model/dto、monitor/context、memory/token_compressor、agent/orchestrator、middleware、config、handler。

未覆盖模块：chat/service（需真实模型调用）、rag/（需 Redis + DashScope）、memory/redis_store（需 Redis）、tool/（需外部服务）。

---

## 8. 部署

- **Dockerfile**: 多阶段构建 (golang:1.26-alpine → alpine:3.20)，已配 GOPROXY=https://goproxy.cn,direct
- **docker-compose.yml**: 外部 Redis（zhitu-net 共享网络）+ zhitu-agent + Prometheus + Grafana
- **监控**: Prometheus 每 10s 抓取 /metrics，Grafana 预置 AI/RAG 指标面板
- **环境变量**: .env.example 提供 28 个配置项模板

---

## 9. 已知限制与后续方向

### 9.1 Phase 1 遗留项

1. **WebSearchTool 缺失** — 最影响功能完整性的缺口，建议优先补上
2. **演示页不完整** — 缺 qwen.html（走 /api/chat）和 gemini.html（走 /api/chat），可从 gpt.html 复制修改
3. **`/actuator/prometheus` 别名** — 一行路由即可补齐
4. **MCP 协议层** — 设计规格允许后续再补

### 9.2 Phase 2 优化方向（设计规格明确排到 Phase 2）

- 更 Go 化的架构调整（错误处理、并发模型）
- 自研更智能的 Agent 规划器
- 微服务拆分
- 前端重构
- 用户体系与权限系统
- 语义摘要替代简单文本截取压缩
- PgVector 支持作为可选向量后端
- 完整 MCP 协议集成

---

## 10. 代码统计

```
Go 源文件:     35 个 (.go)
测试文件:       8 个 (*_test.go)
配置/部署:      8 个 (yaml, Dockerfile, Makefile, .env.example)
前端/资源:      2 个 (html, txt)
核心代码行数:  ~3500 行（不含测试和空行）
测试代码行数:  ~800 行
```

---

## 11. 总结

Go + Eino 复刻版在 **核心功能层面** 已基本对齐 Java 原版：

- 4 个 API 接口行为完全对齐（含混合响应契约）
- RAG 全链路参数精确复刻
- 会话记忆语义一致（简化压缩 + 降级）
- 多 Agent 编排保留了"显式知识增强 + 主链路叠加"的完整语义
- 可观测指标名称与 Java 版一致

主要缺口集中在 **联网搜索工具** 和 **演示页数量**，这两个补齐后即可认为 Phase 1 完整复刻达成。
