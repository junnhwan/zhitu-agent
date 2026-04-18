# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 协作约定

- 用中文沟通，commit message 用英文
- 不要擅自优化或重构没被要求改的代码
- 不要加多余的注释、docstring、error handling
- 不要猜 Eino API，不确定就去读 eino/eino-ext 的源码
- 改代码之前先读，理解现有逻辑再动手
- 遇到重要的协作经验或踩坑教训，主动更新记忆和 CLAUDE.md

## 项目本质

Go + Eino AI 智能体项目。Phase 1（完整复刻）已以 v1.0.0 收口，后续进入自主优化阶段，不再参考原 Java 项目。

## 必须遵守的设计契约

- **混合响应**：成功返回纯文本，异常才返回 JSON。不要统一改成 JSON。
- **multiAgentChat 不是裸调模型**：它是"显式知识增强 + 主链路叠加"，最终必须经过 system prompt + guardrail + memory + RAG + tools 的完整链路。
- **记忆压缩策略可配置**（Wave 1 解冻）：由 `chat_memory.compression.strategy` 选：
  - `simple`（默认，兼容旧行为）：前 3 条消息各截 50 字符当摘要
  - `llm_summary`：消息数超 `max_messages` 或 token 超阈值时，用 LLM 摘要前 N-6 轮，保留最近 6 轮
  - `hybrid`：`llm_summary` + Micro Compact（工具结果专项压缩）
  降级链：LLM 失败 → simple 摘要 → `fallbackToRecent`
- **Token 估算分中英**（Wave 1 解冻）：CJK `(n+1)/2` + 其他 `(bytes+3)/4`，不引入 tiktoken
  （Go 无官方实现 + 要加 C 依赖 + 精度差异 ± 10% 对压缩触发决策足够）
- **向量存储用 Redis Stack**：不是 pgvector。Redis 客户端必须 `Protocol: 2, UnstableResp3: true`。
- **Embedding 用 v3**：Eino 只支持到 v3，不支持 v4。
- **分布式锁释放用 Lua 脚本**：保证原子性。
- **对话链路有两条**：`legacy`（`chat.Service.Chat` 手写 tool-loop，默认）和 `graph`（`internal/chat/workflow` Eino Graph + ReAct Agent）。由 `chat.workflow_mode` 切换。新功能加到 graph 路径，legacy 保留 3 个 release 作为 safety net。
- **MCP Client 工具动态注入**：由 `mcp.client.enabled` 开关（默认关）。启用后从配置的 MCP server（SSE / Stdio）拉取工具，与本地 tool 合并进 `chat/service.go:createTools`。工具名冲突时第二个自动加 `{serverName}__` 前缀。单 server 初始化失败只 WARN 跳过，不阻断主服务。`Service.Shutdown()` 负责关闭 stdio 子进程 / SSE 连接。

## 踩过的坑

- `REDIS_ADDR` 环境变量可能含端口号（如 `redis-stack:6379`），解析时要用 `strings.SplitN` 分离 host 和 port，不能直接当 host 拼接。
- Prometheus CounterVec 在首次 `.Inc()` 之前不会出现在 `/metrics` 输出中。注册指标后必须在 service 层调用 record 函数。
- Docker Compose 的 `environment` 优先级高于 `env_file`，不要在 compose 里硬编码本应走 `.env` 的变量。
