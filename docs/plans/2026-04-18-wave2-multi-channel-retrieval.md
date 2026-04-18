# ZhituAgent Phase 2 Wave 2 实施计划：多通道并行检索 + 后处理器链

## Context

**痛点（Phase 2 Wave 1 结束后的 RAG 现状）**：`internal/rag/reranking_retriever.go` 只有一条"向量召回 → Qwen rerank → top-N"的管道。知识库主要是技术文档（API 名、错误码、产品型号），单路向量对精确关键词查询召回差；长句混短词时短词被稀释；`minScore` 过滤后零命中直接返空，用户体验差。Wave 1 的 `understand.IntentResult.Domain` 已经可以作为检索路由信号但还没人消费。

**目标**：把 RAG 升级为"多通道并行召回 + 后处理器链 + 零命中兜底"。设计细节见 `docs/research/phase2/interview-prep-wave2.md`。

**PR 切分（已与用户确认）**：
- **PR-A（本 Wave 骨架）**：Channel 接口 + Vector/BM25 两通道 + Dedup/RRF/Rerank 最小处理器链 + Pipeline 编排 + 30 条 seed golden set + scorecard 脚本，`rag.pipeline_mode` 默认 `legacy`
- **PR-B（体验与指标收尾）**：Phrase/Intent 通道 + MMR diversity + golden set 扩到 120 条 + A/B scorecard 对比报告

**范围外（本 plan 不做）**：gojieba 中文预分词（先用 RediSearch 默认，指标不达标再加）、双索引迁移（Wave 2 暂不改 schema，靠复用已存在的 `content TEXT` 字段直接上 BM25）、MMR（移到 PR-B）、Eval Center 自动化（Wave 4 P9）。

---

## 关键复用点（已探明）

- **Channel 替换点**：`internal/chat/workflow/nodes.go:36` 的 `deps.RAG.Retriever.Retrieve(ctx, e.Query)` 是唯一调用位，签名 `(ctx, query) ([]*schema.Document, error)`。新 `Pipeline` 实现同签名即可零改调用方
- **索引 schema 已够用**：`internal/rag/store.go` 的 `createIndexIfNotExists` 已经建了 `content TEXT` 和 `file_name TEXT` 字段（之前仅用于展示，未查询），直接可跑 `FT.SEARCH @content:(...)`，无需重建索引
- **Rerank 客户端复用**：`internal/rag/rerank_client.go:QwenRerankClient.Rerank(query, docs, topN) []int` 原样嵌入 `postprocessor/rerank.go`
- **QueryPreprocessor 复用**：`internal/rag/query_preprocessor.go:Preprocess(query)` 放到 Pipeline 入口
- **Redis 客户端复用**：`internal/rag/store.go:Store.RedisClient`（`Protocol:2, UnstableResp3:true`）直接传给 BM25 channel 跑 `FT.SEARCH`
- **Intent 信号**：`internal/understand.Result.Intent.Domain` 已在 workflow `enriched` 里传递，PR-B 的 IntentChannel 据此按文档 tag/category 筛选（PR-A 不用）

---

## 文件结构

**PR-A 新增**：
- `internal/rag/channel/channel.go` — Channel 接口 + Candidate 类型
- `internal/rag/channel/vector_channel.go` — 包装现有 `store.Retriever`（`redisretriever.Retriever`）实现 `Channel`
- `internal/rag/channel/bm25_channel.go` — `FT.SEARCH @content:(...) WITHSCORES LIMIT 0 20` 实现
- `internal/rag/postprocessor/processor.go` — Processor 接口
- `internal/rag/postprocessor/dedup.go` — 按 Doc.ID / content-hash 去重，合并 channel rank
- `internal/rag/postprocessor/rrf.go` — Reciprocal Rank Fusion，k=60，cross-channel 一致性奖励 1.3x
- `internal/rag/postprocessor/rerank.go` — 包装 `QwenRerankClient.Rerank`，带降级（失败返回输入）
- `internal/rag/pipeline.go` — `Pipeline.Retrieve(ctx, query)`：并行 errgroup + 2s 单通道超时 + legacy fallback
- `internal/rag/pipeline_test.go` — TDD 单测
- `docs/eval/rag/golden_set_seed.jsonl` — 30 条 seed（API 精确 / 错误码 / 概念问 / 长句混合各 ~7 条）
- `internal/rag/rag_eval_test.go` — `-tags=eval`，跑 golden set 输出 Recall@5 / MRR

**PR-A 修改**：
- `internal/rag/rag.go` — `NewRAG` 按 `cfg.RAG.PipelineMode` 构造 `Retriever`（legacy=`*ReRankingRetriever`，hybrid=`*Pipeline`）
- `internal/rag/reranking_retriever.go` — 抽接口 `Retriever interface { Retrieve(ctx, query) ([]*schema.Document, error) }`（当前 struct 签名已匹配，只加接口声明）
- `internal/config/config.go` — `RAGConfig` 加 `PipelineMode string` / `ChannelTimeoutMs int` / `RRF struct { K int; ConsistencyBonus float64 }`
- `config.yaml` — 默认 `pipeline_mode: legacy`

**PR-B 新增/修改**（摘要，展开留到 PR-B plan）：
- `internal/rag/channel/phrase_channel.go` — 关键短语精确匹配，boost 3.0
- `internal/rag/channel/intent_channel.go` — 按 `understand.Intent.Domain` 筛选 doc tag
- `internal/rag/postprocessor/mmr.go` — MMR 多样性，同文件 cap 2
- `internal/chat/workflow/nodes.go` — `retrieveFn` 读 `e.Intent.Domain` 传入 Pipeline
- `docs/eval/rag/golden_set_v1.jsonl` — 扩到 120 条
- `docs/eval/reports/` — scorecard JSON 提交

---

## PR-A Tasks

### Task 1 — 抽 Retriever 接口 + RAGConfig 字段

**Files**: `internal/rag/reranking_retriever.go`, `internal/config/config.go`, `config.yaml`

- [ ] 新增 `type Retriever interface { Retrieve(ctx context.Context, query string) ([]*schema.Document, error) }`，放 `internal/rag/retriever.go`
- [ ] 确认 `*ReRankingRetriever.Retrieve` 已匹配（只加编译期断言 `var _ Retriever = (*ReRankingRetriever)(nil)`）
- [ ] `internal/rag/rag.go:RAG.Retriever` 字段类型从 `*ReRankingRetriever` 改 `Retriever`
- [ ] 全局 `grep -r "\.Retriever\."` 确认调用点只有 `internal/chat/workflow/nodes.go:36`，签名不变
- [ ] `RAGConfig` 加字段 + `config.yaml` 默认 `pipeline_mode: legacy`
- [ ] `go build ./...` + `go test ./... -race`
- [ ] Commit: `refactor(rag): extract Retriever interface + add pipeline config fields`

### Task 2 — Channel 接口 + Candidate 类型

**Files**: `internal/rag/channel/channel.go`

- [ ] 定义：
  ```go
  type Candidate struct {
      Doc           *schema.Document
      RankInChannel int     // 1-based
      RawScore      float64
      ChannelName   string
  }
  type Channel interface {
      Name() string
      Retrieve(ctx context.Context, query string) ([]*Candidate, error)
  }
  ```
- [ ] Commit: `feat(rag): add Channel interface`

### Task 3 — VectorChannel（包装现有 store）

**Files**: `internal/rag/channel/vector_channel.go`, `internal/rag/channel/vector_channel_test.go`

- [ ] TDD：构造 fake `redisretriever.Retriever` stub，断言返回 `[]*Candidate` 的 RankInChannel 从 1 开始、ChannelName="vector"、Doc.ID 透传
- [ ] 实现：内部持有 `eino-ext/components/retriever/redis.Retriever`（即现有 `store.Retriever`）和 `minScore`；调 `Retrieve(ctx, query)` 后按顺序包成 Candidate
- [ ] 复用 `store.Retriever` + `QueryPreprocessor` 由 pipeline 层统一调 preprocess，channel 内不做
- [ ] Commit: `feat(rag): add vector channel`

### Task 4 — BM25Channel（RediSearch TEXT）

**Files**: `internal/rag/channel/bm25_channel.go`, `internal/rag/channel/bm25_channel_test.go`

- [ ] TDD（集成测，`-tags=integration` 用本地 Redis Stack）：预先 `HSET zhitu:doc:test1 content "qwen3 chat model config"`，Retrieve("qwen3") 应返回该 doc，RankInChannel=1
- [ ] 实现：
  ```go
  cmd := rdb.Do(ctx, "FT.SEARCH", indexName,
      fmt.Sprintf("@content:(%s)", escapeQuery(query)),
      "LIMIT", "0", "20",
      "WITHSCORES",
      "DIALECT", "2")
  ```
  - 解析返回：`[total, id1, score1, fields1, id2, score2, fields2, ...]`
  - `escapeQuery`：转义 `,.<>{}[]"':;!@#$%^&*()-+=~` 等 RediSearch 保留字符
  - Query 长度 > 200 字符截断；纯空白/全停用词 query 返空（不打 Redis）
- [ ] Commit: `feat(rag): add bm25 channel via RediSearch TEXT field`

### Task 5 — Processor 接口 + Dedup

**Files**: `internal/rag/postprocessor/processor.go`, `internal/rag/postprocessor/dedup.go`, `internal/rag/postprocessor/dedup_test.go`

- [ ] 接口：
  ```go
  type Processor interface {
      Name() string
      Process(ctx context.Context, cands []*Candidate, query string) []*Candidate
  }
  ```
- [ ] DedupProcessor：按 Doc.ID 去重；同一 doc 从多通道命中时合并（保留每个 channel 的 rank 用于 RRF），Candidate 加字段 `RankByChannel map[string]int`
- [ ] TDD：2 通道各返回 [A,B,C]，Dedup 后 3 条 + 每条 `RankByChannel` 双字段
- [ ] Commit: `feat(rag): add Processor interface + dedup`

### Task 6 — RRFProcessor

**Files**: `internal/rag/postprocessor/rrf.go`, `internal/rag/postprocessor/rrf_test.go`

- [ ] 公式：`score = Σ 1/(k + rank_i)`，k 从配置（默认 60）
- [ ] 一致性奖励：如果 `len(RankByChannel) >= 2` 乘 1.3（配置化）
- [ ] TDD：
  - 单通道 A rank1 → score = 1/61
  - 双通道 A 各 rank1 → score = 2/61 × 1.3
  - 排序稳定（score 相同时保持原顺序）
- [ ] Commit: `feat(rag): add RRF fusion with cross-channel bonus`

### Task 7 — RerankProcessor

**Files**: `internal/rag/postprocessor/rerank.go`, `internal/rag/postprocessor/rerank_test.go`

- [ ] 包装 `*QwenRerankClient`，`Process` 里：
  - 输入候选 > `finalTopN` × 4 时先截到 20（rerank 单次上限）
  - 调 `Rerank(query, contents, finalTopN) []int`，按返回 index 重排
  - rerank 错误 → 返回输入原样（已 sort by RRF），打点 `rag_rerank_fallback_total`
- [ ] TDD（用 fake rerank client）：正常路径 + 失败降级路径
- [ ] Commit: `feat(rag): add rerank postprocessor with fallback`

### Task 8 — Pipeline 编排

**Files**: `internal/rag/pipeline.go`, `internal/rag/pipeline_test.go`

- [ ] 结构：
  ```go
  type Pipeline struct {
      preprocessor   *QueryPreprocessor
      channels       []channel.Channel
      processors     []postprocessor.Processor
      channelTimeout time.Duration
      legacyFallback Retriever  // *ReRankingRetriever
      metrics        *monitor.AiMetrics
  }
  func (p *Pipeline) Retrieve(ctx context.Context, query string) ([]*schema.Document, error)
  ```
- [ ] 流程：
  1. `query = preprocessor.Preprocess(query)`
  2. errgroup 并行所有 channel，各自 `context.WithTimeout(ctx, channelTimeout)`
  3. 单 channel 失败/超时 → `log + metrics rag_channel_failed_total{name}`，不阻断
  4. 所有 channel 返空 → 调 `legacyFallback.Retrieve`（三级兜底第二级；PR-B 才加 phrase）
  5. 扁平化 Candidate → processor 链串行
  6. 取 top-N 转 `[]*schema.Document`
- [ ] TDD：
  - 两 channel 都正常 → 合并输出
  - 单 channel 超时 → 另一 channel 结果照常输出
  - 两个 channel 都返空 → 走 legacyFallback
  - legacyFallback 也空 → 返 nil + 打点
- [ ] Commit: `feat(rag): add pipeline orchestrator with parallel channels + fallback`

### Task 9 — 构造装配 + config 布线

**Files**: `internal/rag/rag.go`

- [ ] `NewRAG` 里按 `cfg.RAG.PipelineMode`：
  - `"legacy"`（默认）→ 原 `NewReRankingRetriever`
  - `"hybrid"` → `NewPipeline(preprocess, []Channel{vector, bm25}, []Processor{dedup, rrf, rerank}, legacyFallback=reranking)`
- [ ] Metrics：`internal/monitor/ai_metrics.go` 新增：
  - `rag_retrieve_duration_seconds{mode}` Histogram
  - `rag_channel_failed_total{channel}` Counter
  - `rag_zero_hit_total{fallback}` Counter
  - `rag_rerank_fallback_total` Counter
- [ ] `config.yaml`：
  ```yaml
  rag:
    pipeline_mode: legacy  # legacy | hybrid
    channel_timeout_ms: 2000
    rrf:
      k: 60
      consistency_bonus: 1.3
  ```
- [ ] Commit: `feat(rag): wire pipeline behind rag.pipeline_mode flag`

### Task 10 — Golden seed set + eval 脚本

**Files**: `docs/eval/rag/golden_set_seed.jsonl`, `internal/rag/rag_eval_test.go`

- [ ] 手工造 30 条 seed（覆盖 `docs/` 里已有文档）：
  - API 精确 7 条（"qwen chat model 配置"等）
  - 错误码/技术名 7 条
  - 概念问答 8 条（"什么是 RAG"等）
  - 长句混合 8 条
  - 每条标 `relevant_doc_ids: []string`（从 `store.go:redisKeyPrefix` 键里选）
- [ ] `-tags=eval` 测试：对同一 golden set 分别跑 `legacy` 和 `hybrid` 两个 pipeline，打印 Recall@5 / MRR 对比表
- [ ] 跑一遍，数字记到 `docs/research/phase2/SUMMARY.md`
- [ ] Commit: `test(rag): add 30-sample golden seed + A/B eval harness`

### Task 11 — PR-A 收尾

- [ ] `go test ./... -race -count=1` 全绿
- [ ] 手动 smoke：`pipeline_mode: hybrid` 启动，curl 跑 3 条 query，日志看 `vector` / `bm25` 两通道命中数
- [ ] 更新 `CLAUDE.md`：加"RAG 检索链路有两条 —— legacy（单路向量 + rerank）和 hybrid（多通道 + RRF + rerank），由 `rag.pipeline_mode` 切换"
- [ ] 更新 `docs/research/phase2/SUMMARY.md`：加 "✅ P1 PR-A (Wave 2 骨架) 已实现"
- [ ] 开 PR: `feat(wave2/p1a): multi-channel retrieval skeleton — vector + bm25 + RRF`

---

## 验证（端到端）

```bash
# 1. 单测全绿
go test ./... -race -count=1

# 2. 集成（需本地 Redis Stack）
docker compose up -d redis-stack
go test -tags=integration ./internal/rag/channel/... -v

# 3. Golden set A/B
DASHSCOPE_API_KEY=xxx go test -tags=eval ./internal/rag/... -v
# 期望：hybrid Recall@5 > legacy Recall@5（哪怕 1-2% 也算 PR-A 成功，大头留 PR-B MMR + phrase）

# 4. 服务级 smoke
# config.yaml 切 pipeline_mode: hybrid
curl -X POST http://localhost:8080/ai/multiAgentChat \
     -d '{"sessionId":1,"prompt":"qwen3 chat model 有哪些配置项"}'
# 日志应见两通道命中 + RRF 融合后 top-5

# 5. 回滚验证
# 改回 pipeline_mode: legacy，重启服务，behavior 与 Wave 1 一致
```

---

## 风险登记

| 风险 | 概率 | 缓解 |
|---|---|---|
| RediSearch `FT.SEARCH` 返回格式解析踩坑 | 中 | Task 4 集成测试先跑通，不猜格式；必要时查 RediSearch 文档 + `go-redis` issue |
| `content TEXT` 字段无中文分词，BM25 中文效果一般 | 高 | 接受 PR-A 中文 BM25 召回不强；PR-B 评估后决定是否上 gojieba |
| 多通道并行后 P95 延迟涨 | 中 | 2s 单通道超时硬卡；`rag_retrieve_duration_seconds` 打点监控；legacy 回滚 1 秒切换 |
| RRF 超参 k=60 对本项目数据不合适 | 低 | seed golden set 上 sweep k∈{40,60,80} 记录差异；不调参就用文献默认 |
| 现有 `content TEXT` 字段未被索引（只在 schema 定义但未 `HSET` content 字段）| 高 | **Task 4 开工前先 `HGET zhitu:doc:<id>` 验证 content 字段真的入库**，否则 BM25 恒空；若未入库需回改 `indexer.go` 写入 content（属于小改）|

---

## 下一步 (PR-B 衔接)

- 加 PhraseChannel（零命中兜底）+ IntentChannel（`understand.Intent.Domain` 筛选）
- 加 MMRProcessor（同文件 cap 2）
- Golden set 扩到 120 条（5 类 × 24 条）
- A/B scorecard 自动化 + `docs/eval/reports/` 归档
- 评估 PR-A 中文 BM25 效果，决定是否引入 gojieba 预分词
