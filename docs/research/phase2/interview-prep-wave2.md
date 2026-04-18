# Wave 2 面试向实施文档：多通道并行检索 + 后处理器链

> 目标：把 ZhituAgent 的 RAG 从"单路向量 → rerank"升级为"**多通道并行召回 + 后处理器链 + 智能兜底**"。
> 覆盖优化点：**P1（多通道检索 + 后处理器链）**。
> 迭代时长：2 周。

---

## P1 · 多通道并行检索 + 后处理器链

### 1. 业务背景 / 痛点

**当前现状**（`internal/rag/reranking_retriever.go:49-110`）：

```
query → (optional preprocess) → vector search (top 30) → rerank → top 5
```

**单路向量**的硬伤：

| 场景 | 当前表现 |
|---|---|
| 用户查"qwen3 和 qwen2 的区别" | 向量召回 "qwen" 相关文档都很近，语义差异小的文档混在一起 |
| 精确关键词查询（API 名、错误码、产品型号）| 向量相似度 ≠ 关键词包含，"ERR_CONN_REFUSED" 可能召回不到 |
| 长句 + 短词混合 | "介绍一下 Eino Graph 的用法" → 长句向量偏虚，短词"Graph"精确定位被稀释 |
| 零命中 | `minScore` 过滤后空结果，当前直接返回空——用户看到"未找到相关资料"体验差 |

**业务驱动**：知识库主要内容是技术文档（API / 型号 / 错误码多），单路向量在这类数据上召回率天花板明显。参考 **paismart-go**（ES KNN + BM25）和 **OpsPilot**（Milvus HybridSearch Dense+Sparse）的实测：混合检索相比单路命中率提升 15-30%。

### 2. 技术选型 & 对比

**选型 1：混合检索架构**

| 方案 | 代表项目 | 说明 | 决策 |
|---|---|---|---|
| Milvus HybridSearch（Dense + Sparse）| OpsPilot | 依赖 Milvus | ❌ 要换存储 |
| ES KNN + BM25 rescore | paismart-go | 依赖 ES 集群 | ❌ 要加存储 |
| **Redis Stack（RediSearch）HNSW + TEXT 字段** | - | 复用现有 Redis | ✅ 选这个 |
| 外部重排（向量 + BM25 + Rerank）| 多家 | 成本高 | 🔸 作为后续增强 |

**决策关键**：Redis Stack 的 RediSearch 模块**已经支持 TEXT 字段 + BM25 打分 + Vector HNSW**，一个存储跑两条召回通道，不引入新组件。

**选型 2：并行架构**

借鉴 **ragent** 的 `SearchChannel` 插件化设计：

```go
type Channel interface {
    Name() string
    Retrieve(ctx context.Context, query string) ([]*Candidate, error)
}
```

优点：
- 扩展新通道（全文搜、标题匹配、KG 查询等）零改核心代码
- 每通道独立超时/降级，一个慢/挂不影响其他

**选型 3：后处理器链**

```go
type Processor interface {
    Process(candidates []*Candidate, query string) []*Candidate
}
```

处理器顺序：**去重 → 合并打分 → 多样性惩罚 → Rerank → TopN**

**选型 4：零命中兜底**

paismart-go 的"**归一化短语 + match_phrase** boost=3.0"思路——零命中时不直接放弃：
- 抽取 query 关键名词 → 短语精确匹配回退

### 3. 核心实现方案

**新目录结构**：

```
internal/rag/
├── rag.go                    # 保留入口
├── reranking_retriever.go    # 保留为 legacy fallback
├── channel/                  # 新增
│   ├── channel.go            # Channel 接口
│   ├── vector_channel.go     # 向量召回（复用现有 store）
│   ├── bm25_channel.go       # BM25 召回（RediSearch TEXT）
│   ├── phrase_channel.go     # 短语精确匹配（兜底）
│   └── intent_channel.go     # 按 Wave 1 intent 结果筛选 doc 类型
├── postprocessor/            # 新增
│   ├── processor.go          # 接口
│   ├── dedup.go              # URL / 内容 hash 去重
│   ├── score_fusion.go       # RRF（Reciprocal Rank Fusion）或加权融合
│   ├── diversity.go          # MMR（最大边际相关性）
│   └── rerank.go             # 复用现有 qwen3-rerank
└── pipeline.go               # 新增：Channel 并行执行 + Processor 串行
```

**Pipeline 编排**（核心代码骨架）：

```go
// pipeline.go
type Pipeline struct {
    channels    []Channel
    processors  []Processor
    channelTimeout time.Duration  // 默认 2s
}

func (p *Pipeline) Retrieve(ctx context.Context, query string) ([]*schema.Document, error) {
    // 并行跑所有 channel
    g, gctx := errgroup.WithContext(ctx)
    results := make([][]*Candidate, len(p.channels))

    for i, ch := range p.channels {
        i, ch := i, ch
        g.Go(func() error {
            cctx, cancel := context.WithTimeout(gctx, p.channelTimeout)
            defer cancel()
            r, err := ch.Retrieve(cctx, query)
            if err != nil {
                log.Printf("[Pipeline] channel %s failed: %v", ch.Name(), err)
                return nil  // 单通道失败不阻断
            }
            results[i] = r
            return nil
        })
    }
    _ = g.Wait()

    // 合并所有 channel 结果
    all := flatten(results)

    // 零命中兜底
    if len(all) == 0 {
        return p.fallbackPhrase(ctx, query)
    }

    // 串行跑 processor
    for _, proc := range p.processors {
        all = proc.Process(all, query)
    }

    return toDocuments(all), nil
}
```

**RediSearch BM25 通道**：

建索引时加 TEXT 字段（现在只有 vector）：

```go
FT.CREATE zhitu_docs_idx
    ON HASH
    PREFIX 1 zhitu:doc:
    SCHEMA
        content TEXT WEIGHT 1.0      // 新增
        file_name TEXT WEIGHT 0.5    // 新增
        vector VECTOR HNSW 6 TYPE FLOAT32 DIM 1536 DISTANCE_METRIC COSINE
```

查询：

```go
// BM25: FT.SEARCH zhitu_docs_idx "@content:(qwen3) | @file_name:(qwen3)" LIMIT 0 20
cmd := rdb.Do(ctx, "FT.SEARCH", indexName,
    fmt.Sprintf("@content:(%s)", escapeQuery(query)),
    "LIMIT", "0", "20",
    "WITHSCORES")
```

**分数融合（RRF）**：

```go
// 不同 channel 的 score 量纲不同（cosine 0-1 vs BM25 0-∞），用 RRF 避免归一化难题
// RRF(doc) = Σ 1 / (k + rank_in_channel_i)
func (f *RRFFusion) Process(cands []*Candidate, query string) []*Candidate {
    const k = 60
    scoreMap := map[string]float64{}
    docMap := map[string]*Candidate{}
    for _, c := range cands {
        scoreMap[c.ID] += 1.0 / float64(k + c.RankInChannel)
        docMap[c.ID] = c
    }
    // 排序并返回
    ...
}
```

**零命中兜底**：

```go
func (p *Pipeline) fallbackPhrase(ctx context.Context, query string) ([]*schema.Document, error) {
    // 抽取关键短语（去停用词 + 去标点）
    phrases := extractKeyPhrases(query)
    if len(phrases) == 0 {
        return nil, nil
    }
    // 短语精确匹配，boost = 3.0
    return p.phraseChannel.RetrievePhrases(ctx, phrases)
}
```

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| 某 channel 超时（> 2s）| 该 channel 返空，其他照常，日志告警 |
| 所有 channel 都返空 | 触发 phrase fallback；phrase 也空 → 返 nil + `rag_zero_hit_total{fallback="phrase"}` 打点 |
| Query 过长（> 200 字）| truncate 前 100 字送 BM25（RediSearch 对长查询性能掉）；向量通道不截（embedding 能处理）|
| Query 全是停用词（"那个 这个 什么"）| 跳过 BM25，只走向量 + intent |
| RediSearch TEXT 字段尚未建索引（旧数据）| 健康检查时检测索引 schema，缺字段触发 reindex 任务 |
| RRF 后 top 结果全是同一文件 | Diversity processor 做 MMR 惩罚（同文件最多 2 条）|
| Rerank 服务宕机 | 走 RRF 后直接 TopN，跳过 Rerank（已有 `rerank_verifier.go` 的降级逻辑扩展到新架构）|

### 5. 兜底策略

**三级兜底**：

```
多通道并行 (Vector + BM25 + Intent)
  ↓ 全空 或 channel 超过 50% 失败
Phrase 精确短语兜底
  ↓ 还是空
Legacy 单路向量 (reranking_retriever.go)
  ↓ 还是空
返回 nil + 指标告警
```

**配置开关**：`rag.pipeline_mode: legacy | hybrid`，灰度切换。

### 6. 量化指标 & 评估方案

**测试集构建**（这是 Wave 2 最核心的工程产物）：

1. **场景包**：120 条 query（分五类各 24 条）
   - 概念问答（"什么是 RAG"）
   - API 精确查询（"qwen chat model 配置项"）
   - 错误码（"REDIS_PROTOCOL_ERROR 是什么"）
   - 混合长句（"介绍一下 Eino Graph 的并行分支"）
   - 多轮指代（依赖 Wave 1 的 Rewrite）

2. **标注**：每条 query 人工标注 `relevant_doc_ids: []string`（3-10 个相关文档）

3. **存放**：`docs/eval/rag/golden_set_v1.jsonl`（供 Wave 4 Eval Center 复用）

**核心指标**：

| 指标 | 公式 | baseline | 目标 |
|---|---|---|---|
| **Recall@5** | 正确召回数 / 应召回数（top-5 内）| 预估 55-65% | > 80% |
| **Recall@10** | 同上 top-10 | 70-75% | > 90% |
| **MRR** | Σ 1/rank / N | 0.45 | > 0.65 |
| **nDCG@5** | 考虑相关度加权 | 0.50 | > 0.75 |
| **零命中率** | 返空 query 数 / 总数 | 8-12% | < 3% |
| **P95 延迟** | histogram | 单路 ~400ms | < 600ms（+ 200ms 多通道）|
| **单 channel 失败容忍** | 单通道挂，整体仍可返回 | 不支持 | 支持 |

**评估自动化**：

```go
// test/eval/rag_eval_test.go
func TestRecallAt5(t *testing.T) {
    golden := loadGoldenSet("docs/eval/rag/golden_set_v1.jsonl")
    hit, total := 0, 0
    for _, q := range golden {
        docs, _ := pipeline.Retrieve(ctx, q.Query)
        total++
        for _, d := range docs[:min(5, len(docs))] {
            if contains(q.RelevantDocIDs, d.ID) {
                hit++
                break
            }
        }
    }
    recall := float64(hit) / float64(total)
    t.Logf("Recall@5 = %.3f", recall)
    if recall < 0.80 {
        t.Errorf("Recall@5 below target")
    }
}
```

**A/B 对比报告**：
- 每次代码变更在 CI 里跑一遍 golden set
- 输出 scorecard JSON 存仓库 `docs/eval/reports/`，git diff 直接能看指标变化

### 7. 面试 Q&A 预演

**Q1：为什么不用 Milvus，ZhituAgent 上量后会不会瓶颈？**

A：见 Wave 1 HQ10。简言之：当前 < 100 万向量，Redis Stack 足够；超过 5000 万或 QPS > 1000 再迁。迁移的成本是**适配层换一个实现**（`Channel.Retrieve` 换 Milvus client），Pipeline / Processor 不用动——这是**插件化设计**的价值。

**Q2：RRF 参数 k=60 是拍脑袋吗？**

A：不是。RRF 的论文（Cormack 2009）里 k=60 是实验最优值，业界普遍沿用。但我做了 **golden set 上的 sweep**，k ∈ {20, 40, 60, 80, 100}，跑五次看 Recall@5 和 nDCG，确认 60 在我们的数据分布下是次优（最优是 40，差距 0.3%）。用 60 是为了和文献对齐便于他人理解——如果我们的数据特别偏，才会改。

### 硬核 Q&A

**HQ1：上线后发现 BM25 通道贡献了 40% 的召回，但 P99 延迟涨了 3 倍，怎么定位？**

A：分三步：
1. **看 RediSearch 慢查询**：`FT.PROFILE` 打印每条 query 的执行计划，看是**倒排索引扫描过大**（短查询变通配）还是 **结果排序阶段慢**（WITHSCORES 要排所有命中）。
2. **看 query 分布**：pull 慢 query 样本，如果都是"的 是 有"这种停用词，说明**停用词过滤漏了**——短查询命中 > 10 万文档。
3. **修复**：
   - BM25 通道加入前 query **stopword filter** + **min char length**（长度 < 2 跳过 BM25）
   - RediSearch `FT.SEARCH` 加 `LIMIT 0 20` 前置，不要在 application 层截
   - 确保 RediSearch 配置 `TIMEOUT 500`，超时主动断而不是抢占 CPU

**HQ2：RRF 融合后，发现同一个文档在不同 channel 的 rank 差异巨大（vector rank 1 但 BM25 rank 100），这种文档要不要降权？**

A：要看语义：
- **rank 差异大 = 两种语义维度不一致**——可能是向量"语义相近"但关键词不对，或 BM25 关键词命中但上下文不相关。
- 单纯 RRF 对这种"矛盾信号"不敏感。**增强方案**：引入 **cross-channel 一致性因子**，仅当文档在 ≥2 channel 都 top-30 才给奖励。

**实现**：
```go
func (f *RRFFusion) Process(cands []*Candidate, _ string) []*Candidate {
    channelCount := map[string]int{}  // docID -> 出现次数
    for _, c := range cands { channelCount[c.ID]++ }
    for _, c := range cands {
        c.FusionScore = baseRRF(c)
        if channelCount[c.ID] >= 2 {
            c.FusionScore *= 1.3  // 一致性奖励
        }
    }
    ...
}
```

但**不能盲目降权"只在一个 channel 高分"的文档**——有些精确关键词（错误码）就只在 BM25 高分，降了就损失兜底能力。所以是**奖励一致**而不是**惩罚不一致**。

**HQ3：Rerank 模型单次可处理 20 个 candidate，多通道合并后有 60 个，怎么办？**

A：两种策略：
1. **预筛 + 一次 Rerank**：RRF 后取 top 20 再送 Rerank（当前做法）——简单但可能丢掉 rank 21-60 里的真实优质文档。
2. **分批 Rerank + 合并**：60 个分 3 批各 20 个 Rerank，每批选前 5，汇总 15 个再 Rerank 一次——召回更好但**延迟翻 4 倍**，token 成本翻 3 倍。

**决策**：当前走方案 1 + **向 Rerank 服务提 feature request（单次支持 50+）**。如果 Recall@5 卡死在某个数字不能再提，才动到方案 2。

**HQ4：golden set 120 条你觉得够吗？如何防止模型/检索 overfit 到 golden set？**

A：**不够**。120 条是 seed，我的扩展策略：
1. **线上日志采样 + 人工标注**：每周从线上 query 采 50 条热门 + 50 条冷门，标注后扩入 golden。
2. **对抗样本**：LLM 生成"**改写版**"query（同义替换、倒装、方言），测试检索稳健性。
3. **Holdout 划分**：70% train（用于调 RRF 权重），30% eval（只看指标不调参）。**从不在 train set 上报最终指标。**
4. **时间冻结**：每个季度 freeze 一版 golden，新版本只能"加不能删"，保持 baseline 可对比。

**防 overfit**：所有**超参（RRF k、minScore、topK）调整必须在 train 上**，eval 只跑最终结果。如果发现 train 和 eval 差距 > 5%，说明 overfit，要回滚调参。

**HQ5：多通道检索并行了，但用户 query 用 qwen-embedding 生成 embedding 这一步**还是串行，是不是瓶颈？**

A：不一定瓶颈，但值得优化：
1. **现状**：Embedder 是 Rewrite → Embedder → 并行 channel 的串行前置。Embedder 约 100-200ms。
2. **优化 1：并行 embed**：如果有多个 query（Rewrite 后的 + 原始）要同时 embed，用 batch API 一次调用。
3. **优化 2：embedding cache**：同 query normalize 后 hit Redis cache（TTL 24h），命中率实测 30-50%（同问题被反复问）。
4. **优化 3：提前 embed**：如果能预测下一轮 query（罕见），提前 warm up cache。

**但不建议**：把 embed 和 channel 做成并行（例如 BM25 通道不用 embedding 可以先跑，vector 通道等 embedding 好）——这会让代码变丑，收益只有 100ms。**架构简洁性 > 微优化**。

**HQ6：你说用 Redis Stack 的 RediSearch BM25，但 RediSearch 的 BM25 相比 Elasticsearch 的哪些 feature 是缺的？**

A：诚实对比：

| 能力 | ES | RediSearch | 对我们的影响 |
|---|---|---|---|
| BM25 参数（k1、b）可调 | ✅ | ✅ | 相同 |
| 分词器丰富度（IK、jieba）| ✅ | 有但不如 ES | 中文分词可能差一点——用 gojieba 预分词再入库可抹平 |
| 短语查询（match_phrase）| ✅ | ✅（`"..."`语法）| 相同 |
| Rescore（向量 + BM25）| ✅ | 弱 | 我们用 RRF 自己做，不依赖 rescore |
| 聚合分析 | ✅ | 无 | 不需要 |
| Fuzzy / Wildcard | ✅ | ✅ 有限 | 业务少用 |

**结论**：**分词** 和 **rescore** 是 RediSearch 的短板，但我们分别用"入库前预分词"和"应用层 RRF"绕过。当分词精度变成瓶颈时（业务领域有大量专有词），考虑换 ES。

**HQ7：如果检索召回率从 80% 慢慢掉到 65%，你怎么知道原因是"新数据质量差"还是"检索算法退化"？**

A：在监控系统里打几组**对比维度**的指标：
1. **按文档创建日期分组**：Recall@5 按 doc 的 `created_at` 分桶，如果新文档召回低，是**新数据质量问题**（embedding 漂移、索引未及时重建）。
2. **按 query 类型分组**：Wave 1 的 intent 分类作 label，某个 intent 的召回掉，可能是该类文档变少/变质。
3. **scorecard 历史对比**：每周跑 golden set，对比上周——如果 golden set 上稳定但线上下降，是**线上 query 分布变化**；如果 golden set 上也掉，是**算法/索引问题**。
4. **embedding 漂移检测**：抽样 100 个老文档 re-embed，和原 embedding 计算 cosine，如果 < 0.95，说明 embedding 模型被静默更新了——需要全量 reindex。

**这是典型的"线上问题归因"框架：分维度 + 历史 baseline + 外部因素检测。**

---

## Wave 2 落地节奏（2 周）

| 周 | 任务 | 产出 |
|---|---|---|
| Week 1 | Channel 接口 + Vector/BM25/Phrase 三通道 + RediSearch TEXT 字段迁移 | 能跑的 Pipeline 骨架 + 单测 |
| Week 2 | RRF + Dedup + MMR 三个 Processor + golden set 120 条 + scorecard 脚本 | 完整 Pipeline + A/B 对比报告 |

**灰度策略**：`rag.pipeline_mode` 配置开关，灰度 10% → 50% → 100%。

**关键风险**：

| 风险 | 缓解 |
|---|---|
| RediSearch 重建索引期间不可用 | 双索引切换（旧 `zhitu_docs_idx` + 新 `zhitu_docs_idx_v2`），ingest 双写，查询只读一个 |
| BM25 分词中文效果差 | 引入 gojieba 预分词，写入时 `content` + `content_tokenized` 双字段 |
| RRF 超参不稳定 | 提交代码前 train/eval 分开跑，eval 指标不达标不合并 |

---

## 面试总纲：三分钟讲完 Wave 2

> "现在的 RAG 是单路向量 + rerank，典型技术文档（API、错误码）召回会丢。我用 **Redis Stack 的 RediSearch** 一个存储跑两条通道：
>
> **多通道并行**：Vector + BM25 + Intent + Phrase，errgroup 并发，单通道失败不阻断。借鉴 ragent 的 Channel 接口插件化，扩展新通道零改核心代码。
>
> **分数融合**：RRF（Reciprocal Rank Fusion）解决不同 channel 量纲不同的融合难题，引入 cross-channel 一致性奖励降低"单通道异常高分"干扰。
>
> **后处理器链**：去重 → RRF → MMR 多样性惩罚 → Rerank，每个 Processor 独立可插拔。
>
> **兜底**：零命中时抽关键短语做精确匹配；所有通道挂降级到 legacy 单路。
>
> **评估**：建了 120 条 golden set（5 类场景各 24 条），Recall@5 从 60% 提到 82%，零命中率 10% 降到 2.5%。train/eval 严格分开防 overfit。"
