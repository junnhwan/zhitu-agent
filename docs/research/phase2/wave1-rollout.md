# Wave 1 灰度发布方案

## 三个 PR 的开关

| PR | 配置项 | 默认值 | 启用方式 |
|---|---|---|---|
| P3 记忆压缩 | `chat_memory.compression.strategy` | `simple` | 改 `llm_summary` 或 `hybrid` |
| P2 意图理解 | `understand.enabled` | `false` | 改 `true` + 确认 `tree_path` |
| P4 Graph 工作流 | `chat.workflow_mode` | `legacy` | 改 `graph` |

三档独立，可任意组合。P4=graph 时自动消费 P2/P3 的能力（P2 通过 `IntentRouter` 传入 workflow.Deps；P3 继续由 chat service 在 graph 入口前后做 memory I/O）。

## 推荐灰度节奏

1. **Week 1**：只开 P3 (`llm_summary` or `hybrid`)，观察 LLM 摘要成本、Redis TTL、命中率
2. **Week 2**：加开 P2，观察 `understand.*` Prometheus 指标 + 熔断器 state 日志
3. **Week 3**：小流量开 P4=graph（建议先 10% → 50% → 100%），对比 legacy vs graph 的：
   - P50 / P95 延迟
   - 工具调用成功率
   - 输出质量（手工抽样 100 条）

## 回滚步骤

- 任何一档出问题：把对应 config 改回默认值，热重启服务即可
- P4 回滚不会丢数据（memory 读写在 graph 外）
- P2 熔断器本身是自动回滚（5 min 错误率 ≥ 50% 自动 fallback_keyword），不需要人工干预

## 关键监控指标

- `memory_compress_duration_seconds{strategy}` — P3 压缩耗时
- `understand_intent_classify_total{domain, correct}` — P2 分类准确率（需 eval 集跑）
- `circuit_breaker_state{name="understand"}` — P2 熔断器状态
- `chat_workflow_mode{mode}` — P4 legacy vs graph 请求量分布（需新增埋点）
- `react_agent_step_count` — ReAct 步数分布（需新增埋点）

**埋点 TODO**：`chat_workflow_mode` 和 `react_agent_step_count` 两个新指标尚未实现，在 Wave 1 收尾合并前补。

## 已知限制 / Wave 2 衔接

- Graph 当前是**纯串行**（enrich → retrieve → build_prompt → react → wrap）。Plan 原设计的 `[rewrite | classify | rag]` 三路并行因为 Eino 合并语义复杂，留到 Wave 2 演进
- StreamChat 还没走 graph（`workflow.ChatWorkflow.Invoke` only）。Wave 2 起做流式版本
- Memory 写入仍在 `chatViaWorkflow` 里手工调用，不在 Graph 内。这样保留 P3 的压缩/micro-compact 逻辑完全可用，但如果 Wave 2 要加 state graph / 回调追踪，需要把 memory 搬进 graph
