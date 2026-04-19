# Workflow Baseline Re-run After RAG Score Fix + Template Alignment

**Timestamp**: 2026-04-19T17:30:45+08:00
**Scorecard**: `docs/eval/reports/workflow-latest.json` (40 queries)
**Baseline before**: commit `77b5557` (RAG 0 命中 + 模板 drift 状态)
**Fixes applied since**:
- `ad52382` RAG `doc.Score()` 修复（`vectorScoreConverter`）
- `863b1f0` graph RAG 注入模板对齐 legacy（独立 UserMessage + 【来源 | 相似度】+ 假 ack）

## 前后对比

| 维度 | Before (RAG broken) | After (fixed) | Δ |
|---|---|---|---|
| Overall verdict (L:G:T) | 13 : 4 : 23 | **19 : 9 : 12** | 两边都赢更多，tie 减半 |
| Overall latency (L / G) | 19.9s / 20.2s | 18.2s / **13.9s** | graph -31% |
| Overall tokens (L / G) | 1148 / 1168 | **2191** / 1120 | legacy +91%, graph -4% |
| Knowledge verdict (L:G:T) | 10 : 1 : 9 | **15 : 3 : 2** | legacy 差距拉大 |
| Knowledge latency | 33.1s / 31.0s | 27.1s / **21.5s** | 两边都快，graph 更快 |
| Knowledge tokens | — | 2624 / 1112 | legacy 用 graph **2.4×** token |
| Chitchat verdict | 1 : 2 : 7 | 0 : 4 : 6 | graph 小胜 |
| Tool verdict | 1 : 2 : 7 | 4 : 2 : 4 | 基本打平 |
| Coarse 0-hit 次数 | 40/40 | 13/40 | 修复生效 |

## 关键结论

### 1. RAG score 修复是这次最大的净收益
13/40 仍有 0-hit，都是合理情形（chitchat 类如 `你好` / `谢谢`，tool 类如 `现在几点了`——这些 query 本就不该召回知识库）。20 knowledge query 里 legacy 全部走到 rerank，graph 按 intent 路由在 KNOWLEDGE domain 都走到 rerank。

### 2. legacy 受益最大
拿到真 `file_name` + 真 `score` 后 legacy 答案涨了 ~50% token。`【来源：<file.md> | 相似度：0.74】` 的元数据让 LLM 愿意展开"据 X.md 所述…"的引用叙述。knowledge wins 从 10 → 15。

### 3. graph 的 knowledge 劣势不是模板问题，是 ReAct 架构固有倾向
模板已对齐（4 msg 结构 + ack + 元数据都在），但 graph 在 knowledge 类仍输 15:3:2。真正原因在 `chat_workflow.go:36-43`:

```go
agent, err := react.NewAgent(ctx, &react.AgentConfig{
    ToolCallingModel: deps.ChatModel,
    ToolsConfig:      compose.ToolsNodeConfig{Tools: deps.Tools},
    MaxStep:          maxStep,
})
```

graph 所有查询（含 knowledge）都经 `react.Agent`。ReAct 的 "Thought → Action → Observation → Finish" 循环即使没调 tool 也让 LLM 进入"最简收敛"模式：
- graph knowledge reply 平均 598 chars vs legacy 694 chars
- graph knowledge tokens 1112 vs legacy 2624（**一半多**）

LLM judge 偏好"更详细 + 具体代码路径 + 结构化解释"——这正是 ReAct 收敛模式会砍掉的部分。

### 4. graph 的优势：chitchat / latency / 成本
- chitchat 4 wins vs 0 legacy —— 对日常对话 ReAct 简洁反而更合口味
- 全域 latency -31%、token -48%——运营成本视角 graph 优势明显
- 适合成本敏感或流量大的入口

## 后续选项（不在本次 commit 范围）

要让 graph 在 knowledge 类追上 legacy，可选：
- **A. 分域路由**：KNOWLEDGE domain 绕开 ReAct，直接 `ChatModel.Generate(messages)`，保留 enrich/retrieve/build_prompt。改 `chat_workflow.go` 加条件边 `nodeBuildPrompt -> nodeReAct | nodeDirectGen`。
- **B. ReAct system prompt 调优**：在 `deps.SystemPrompt` 里追加 "回答 knowledge 类问题时展开解释并引用来源" 指令。成本最小，效果有限。
- **C. 灰度策略**：根据 category 灰度切 workflow_mode（knowledge → legacy，tool/chitchat → graph）。运营决策而非代码决策。

A 最彻底但改动大；B 最便宜可先试；C 是上层决策层面的灰度。建议下一 wave 再评估。

## 本次修复的客观价值

- **bug 消除**：RAG 真的起作用了（从 100% 0-hit 到 67% 成功召回）
- **质量下界抬升**：legacy 在知识类答得更详细更引用出处，两路径都比之前好
- **判断基础修正**：原 baseline 在 RAG broken 状态下的 A/B 对比结论不再成立；新 baseline 才是 workflow_mode 灰度决策的真实起点
