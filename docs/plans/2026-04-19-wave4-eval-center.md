# Wave 4 P9 — Eval Center 总规划

> 日期：2026-04-19
> 状态：规划中（未开工）
> 定位：把项目从"能跑"推到"能量化"——给 RAG / Memory / Workflow 三个子系统建立独立可跑、可复现、可对比的 eval harness。

## Context

Phase 2 Wave 1~3 一路加功能（记忆压缩、多通道检索、Eino Graph、MCP 双向），但**客观指标只有 RAG 一条线**（`docs/eval/reports/latest.json`，120 golden，关键词子串匹配）。

存在三个可见短板：

1. **RAG 的 ground truth 是代理指标**——`relevant_keywords` 子串命中当"相关"，不是真正的文档级 relevance。recall/MRR 数字能信到哪一步存疑。
2. **Memory 压缩零质量指标**——简单 / LLM摘要 / hybrid 三种策略谁好谁坏，现在只能靠 code review 判断。
3. **Workflow legacy vs graph 零 A/B**——`workflowMode` 切换后只能看日志，没延迟 / token / 答案质量的对比数据，灰度放量没依据。

Wave 4 P9 的目标就是补这三块，产出**可在本地复现、能灌进 CI 做 regression gate 的 eval harness**，不是一次性脚本。

## 三块 scope 总览

| 块 | 目标 | 核心指标 | 成本 | 依赖外部 API |
|---|---|---|---|---|
| **#1 真 doc_id 金标** | RAG eval 从关键词匹配升级到文档级 relevance | Recall@K / MRR（真 relevance） | 0.5~1 天 | 无 |
| **#2 Memory eval 集** | 三种压缩策略客观对比 | token 压缩比 / fact retention / 最近上下文保真度 | 1 天 | LLM-judge 可选 |
| **#3 Workflow benchmark** | legacy vs graph A/B | 端到端延迟 / token 花销 / 答案质量 | 1~2 天 | DashScope 必走 |

合计 2.5~4 天工程时间，不含样本标注 / query 设计的人工成本。

## 块 #1 — 真 doc_id 金标（RAG eval 重构）

### 现状

- 120 golden 在 `docs/eval/rag/golden_set_seed.jsonl`，schema `{query, relevant_keywords[]}`
- 评判函数 `internal/rag/rag_eval_test.go:sampleHit` 对每个候选 `doc.Content` 做关键词子串匹配
- 真实 doc ID 形态：`research/phase2/SUMMARY.md_3`（`{relpath}_{segmentIdx}`，见 `internal/rag/document_splitter.go:171`）

### 目标

- golden schema 扩成 `{query, relevant_doc_ids[], relevant_keywords[]?}`，`relevant_doc_ids` 优先，关键词作兜底或人工可读注释
- `sampleHit` 改成：`any(doc.ID has prefix in relevant_doc_ids[i])` —— 用 prefix 是为了 tolerate 切片粒度变化（相关是 `relpath`，实际命中可以是 `relpath_0` / `relpath_3`）
- Recall@5 / MRR 用新判据重新跑一次 baseline 并写回 `docs/eval/reports/latest.json`

### 开放问题

- **120 条怎么标？**
  - 选 a：手工逐条标，慢但准（半天~一天）
  - 选 b：LLM-assist——把 query + 所有 doc 段交给 qwen-max，让模型先给候选，再人工审（快但要 API 调用）
  - 选 c：**两者结合**——用现有 legacy retriever top-10 产生候选，人工挑其中真相关的当标注（最省时，前提是 legacy recall 足够撑住上位相关）
- 是否保留 `relevant_keywords`？建议保留（hybrid 命中：`doc_id OR keyword`），更鲁棒

### 交付物

- `docs/eval/rag/golden_set_seed.jsonl` 升级（schema 扩展 + relabel）
- `internal/rag/rag_eval_test.go:sampleHit` 改判据，测试通过
- `docs/eval/reports/latest.json` 写入新 baseline（legacy vs hybrid 在新判据下的数字）

## 块 #2 — Memory eval 集

### 现状

- `internal/memory/` 下 7 个 `_test.go`，全是单元 / 集成测试（策略分派、lock 幂等、Redis read/write），**没有质量指标**
- 三种策略：`simple`（截 50 字符）/ `llm_summary`（LLM 摘要前 N-6 轮）/ `hybrid`（llm_summary + MicroCompact）
- `MicroCompact` 对 `rag_search` 取 top-3、`send_email` 抽收件人

### 目标

三个客观指标，每个策略都跑：

1. **token 压缩比** —— 输入 N 轮对话，压缩后 token 数 / 原 token 数。最便宜，最可靠
2. **最近上下文保真度** —— 构造 20 轮对话，压缩后验证"最后 6 轮"是否逐字保留。验证"近端窗口"契约没被策略破坏
3. **fact retention**（可选，花钱）—— 在对话里埋 N 条关键信息（"我住北京"、"我叫 X"），压缩后用 qwen-turbo 判官"这条事实还能否从压缩结果里推出"，统计保留率

### 设计取舍

- `-tags=eval` 模式跑，不污染 `go test ./...`
- 用 `httptest` + mock model，**不依赖 Redis**（memory 的压缩逻辑本身与存储解耦，直接测 `Compressor.Compress(messages)` 即可）
- fact-retention 可关（`MEM_EVAL_LLM_JUDGE=true` 才跑）

### 开放问题

- **基准对话从哪来？** 三种来源：
  - a. 手工合成（固定 20~50 轮）——复现性好，但合成感强
  - b. 历史对话采样（从 Redis 拉几个真实 session）——真但不稳定
  - c. 合成为主 + 真实对话作 smoke——建议选这个
- 要不要跑 fact-retention（花 API 钱）？**默认关，留开关**

### 交付物

- `internal/memory/memory_eval_test.go`（`-tags=eval`）
- `docs/eval/memory/conversation_seed.jsonl`（基准对话集）
- `docs/eval/reports/memory-latest.json`（每次跑自动覆盖）

## 块 #3 — Workflow benchmark（legacy vs graph）

### 现状

- 两条链路在 `internal/chat/service.go:Chat`（legacy 手写 tool-loop）vs `chatViaWorkflow`（Eino Graph 5 节点）
- 切换点：`cfg.chat.workflow_mode`
- 没有 A/B，只有 `ai_workflow_requests_total{mode,entry}` 记分发，灰度放量没有决策依据

### 目标

query 集 20~50 条，每条两路径都跑一次，对比三指标：

1. **端到端延迟** —— Go 侧 `time.Since`，一次 Chat() 入到出
2. **token 花销** —— prompt + completion 合计（从 `resp.ResponseMeta.Usage` 读）
3. **答案质量** —— 两路径回答 + 参考答案（query 集携带）给 qwen-max 判官打分 1~5

### 设计取舍

- **必依赖 DashScope**，无法离线跑 —— `-tags=eval` + 真 API
- query 集分三类平衡覆盖：
  - Knowledge 类（走 RAG）：10 条
  - Tool 类（触发 getCurrentTime / addKnowledgeToRag）：5 条
  - Chit-chat 类（不触发 RAG / tool）：5~10 条
- Legacy 的 tool-loop 和 graph 的 ReAct 行为不完全等价，要记录 tool 调用次数差异（debug 用）

### 开放问题

- **参考答案怎么来？** query 集如果标注了"期望结论"，LLM-judge 能给出相对分；否则只能两路径互比（pairwise），判官说哪个更好
- 多次跑取平均？建议 3 次，避免 LLM 波动
- **这玩意儿花多少钱？** 40 条 × 2 路径 × 3 次 × (~500 input + ~300 output) × qwen-max ≈ 24k prompt tokens + 14.4k completion，按官网价 ~￥0.5 一次全量跑。可接受

### 交付物

- `internal/chat/workflow/workflow_eval_test.go`（`-tags=eval`）
- `docs/eval/workflow/query_set.jsonl`
- `docs/eval/reports/workflow-latest.json`

## 建议 PR 顺序

```
#1 真 doc_id 金标  →  [RAG eval 判据可信]
       ↓
#2 Memory eval        [独立，可并行]
       ↓
#3 Workflow benchmark [最重，收尾]
```

**理由**：
- #1 先做，因为它**修复已有 eval 的信号质量**，后续所有"提升 RAG"的 PR 都能用上新判据
- #2 并行也行（零依赖 #1），但单独做产出高，**优先级 = #1 > #2**
- #3 放最后——它是三块里最重 + 要钱的，且它的"知识类答案质量"判据可以复用 #1 的 doc_id 标签（问答题变填空题，judge 的不确定度更低）

## 跨块共性设计

- 所有 eval 走 `-tags=eval`，与日常 `go test ./...` 解耦
- scorecard 统一写 `docs/eval/reports/{sys}-latest.json` + 带时间戳的归档副本
- 每个 scorecard 带 `git_sha`、`config_snapshot`、`timestamp`，便于回溯
- 先本地跑、手动检查，**不进 CI 门禁**——等三块都稳定至少两周再考虑 CI gate

## 下一步决策点（开工前要回答）

- [x] #1 的 120 条 relabel：**选 c —— legacy retriever top-10 dump 出来人工挑**
- [x] #2 是否跑 fact-retention LLM-judge：**默认关，留 `MEM_EVAL_LLM_JUDGE=true` 开关**
- [x] #3 的 query 集：**40 手写为主 + 5~10 历史会话 smoke**
- [x] 加 `make eval` 汇总入口：**做**

开工 #1。
