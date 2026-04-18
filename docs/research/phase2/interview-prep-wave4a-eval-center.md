# Wave 4A 面试向实施文档：RAG Eval Center

> 目标：建立 **evidence-first** RAG 评测中心，用场景包 + 可量化 scorecard 驱动持续优化。
> 覆盖优化点：**P9（RAG Eval Center）**，借鉴 **AgentX** 的 evidence/scorecard/report 三件套。
> 迭代时长：2 周。
> **战略地位**：这是 Wave 1/2/3 所有优化的**量化底座**——没有 Eval Center，简历里的"Recall@5 从 60% 提到 82%"就是拍脑袋数字。

---

## P9 · evidence-first RAG 评测中心

### 1. 业务背景 / 痛点

**当前现状**：所有的 RAG 改动靠"人眼看几个 case 感觉好了就合并"，没有量化基线，典型问题：
- 调 RRF 的 k 值，这次改 60、下次改 40，不知道效果到底怎么样
- 改 rerank topN 从 5 到 8，有人说"好像更好"但没数据
- 线上用户反馈"搜不到 X"，修完上线又有"丢失 Y"，来回拉扯
- 简历写"召回率提升 X%"却拿不出证据

**业务驱动**：简历和面试最容易被戳穿的就是"数据怎么来的"。**AgentX** 项目的 Eval Center 做得最好——每次变更产出 **三件套 artifact**（raw-evidence + scorecard + report），PR diff 直接能看指标对比。这是从 LeetCode 心态走向工业心态的分水岭。

### 2. 技术选型 & 对比

**选型 1：自建 vs 用 LangSmith / Phoenix 等**

| 方案 | 优点 | 缺点 | 决策 |
|---|---|---|---|
| LangSmith（LangChain 商业）| 开箱即用 | SaaS 锁定 + 数据合规 | ❌ 企业场景不合适 |
| Phoenix（Arize 开源）| 自部署 | Python 栈，和 Go 生态割裂 | ❌ |
| Ragas（评估库）| 指标全 | Python，偏向生成质量不是检索 | 🔸 借鉴指标定义 |
| **自建 Go-native Eval Center** | 贴合业务 + 数据不外流 | 要自己写 | ✅ |

**为什么自建**：评估逻辑本质是"跑流水线 + 计算指标"，核心代码 < 500 行，没必要引入外部服务。关键是**构建场景包 + 设计 scorecard schema**，这部分谁都绕不开。

**选型 2：评估指标体系**

借鉴 Ragas + 信息检索经典指标，按维度分层：

| 层级 | 指标 | 说明 |
|---|---|---|
| **检索层** | Recall@K, MRR, nDCG@K | 召回质量（Wave 2 已定义）|
| **生成层** | Answer Relevance, Faithfulness | LLM 回答贴合 query + 不 hallucinate |
| **端到端** | 任务成功率 | 完整场景下用户目标是否达成 |
| **成本** | P95 延迟、token 消耗 | 质量提升的代价 |

**选型 3：Judge 模型**

| 方案 | 特点 | 决策 |
|---|---|---|
| 人工标注 | 准确但慢 | Golden set 构建用 |
| qwen-max 自评 | 快、便宜、同栈 | ❌ 有偏（同家模型）|
| **gpt-4o / claude-3.5 作 Judge** | 跨家避偏、业界共识 | ✅ 选这个（付费 API）|
| 规则校验 | 客观指标（Recall）| ✅ 能用规则就不用 LLM |

**原则**：**客观指标（Recall/MRR/nDCG）用规则**，**主观指标（Faithfulness）用跨家 LLM Judge**。

### 3. 核心实现方案

**新目录结构**：

```
eval/                               # 项目根新增（不放 internal）
├── datasets/
│   ├── rag_golden_v1.jsonl         # Wave 2 的 120 条（检索）
│   ├── e2e_scenarios_v1.jsonl      # 端到端场景（query + 期望回答要点）
│   └── regression_v1.jsonl         # 已知问题回归集（防退化）
├── runner/
│   ├── rag_runner.go               # 跑 RAG 流水线
│   ├── chat_runner.go              # 跑端到端 chat
│   └── judge.go                    # LLM Judge 封装
├── metrics/
│   ├── retrieval.go                # Recall/MRR/nDCG
│   ├── generation.go               # Faithfulness/Answer Relevance
│   └── cost.go                     # 延迟/token
├── artifacts/
│   ├── schema.go                   # 三件套 schema 定义
│   └── writer.go                   # 写 evidence/scorecard/report
├── cmd/
│   └── eval/main.go                # CLI 入口：eval run --set=rag_golden_v1
└── reports/                        # git-tracked 产物
    └── 2026-04-18_rag_golden_v1/
        ├── raw-evidence.json
        ├── scorecard.json
        └── report.md
```

**三件套 schema**：

```go
// artifacts/schema.go

// Evidence: 每条 query 的原始执行轨迹
type Evidence struct {
    QueryID      string            `json:"query_id"`
    Query        string            `json:"query"`
    ExpectedIDs  []string          `json:"expected_doc_ids,omitempty"`
    Retrieved    []RetrievedDoc    `json:"retrieved"`
    ChannelTrace map[string][]int  `json:"channel_trace"`      // channel_name -> doc indices
    LLMResponse  string            `json:"llm_response,omitempty"`
    Latency      LatencyBreakdown  `json:"latency_ms"`
    TokenUsage   TokenUsage        `json:"token_usage"`
    Timestamp    string            `json:"timestamp"`
}

// Scorecard: 聚合指标
type Scorecard struct {
    DatasetName   string             `json:"dataset"`
    DatasetSize   int                `json:"dataset_size"`
    Timestamp     string             `json:"timestamp"`
    GitCommit     string             `json:"git_commit"`
    Config        map[string]any     `json:"config"`  // RRF.k / topK / minScore 等
    Metrics       map[string]float64 `json:"metrics"`
    MetricsByTag  map[string]map[string]float64 `json:"metrics_by_tag"` // 按 intent/doc_type 分桶
    Delta         *Delta             `json:"delta,omitempty"`  // vs 上一版
}

// Report: 人读版 markdown
```

**Scorecard 例子**（`scorecard.json`）：

```json
{
    "dataset": "rag_golden_v1",
    "dataset_size": 120,
    "timestamp": "2026-04-18T10:00:00Z",
    "git_commit": "a1b2c3d",
    "config": {
        "pipeline_mode": "hybrid",
        "rrf_k": 60,
        "channel_timeout_ms": 2000,
        "rerank_final_topn": 5
    },
    "metrics": {
        "recall@5": 0.823,
        "recall@10": 0.908,
        "mrr": 0.67,
        "ndcg@5": 0.758,
        "zero_hit_rate": 0.025,
        "p95_latency_ms": 580,
        "avg_tokens_per_query": 420
    },
    "metrics_by_tag": {
        "intent=API_QUERY":    {"recall@5": 0.90},
        "intent=CONCEPT":      {"recall@5": 0.85},
        "intent=ERROR_CODE":   {"recall@5": 0.72}
    },
    "delta": {
        "baseline_commit": "xyz789",
        "baseline_metrics": {"recall@5": 0.608},
        "change": {"recall@5": "+0.215"}
    }
}
```

**Report**（`report.md`，给人看的）：

```markdown
# RAG Eval Report — 2026-04-18

**Commit**: a1b2c3d (vs baseline xyz789)
**Dataset**: rag_golden_v1 (120 queries)

## 核心指标

| 指标 | 当前 | Baseline | Δ |
|---|---|---|---|
| Recall@5 | **82.3%** | 60.8% | **+21.5pp** ✅ |
| MRR | 0.67 | 0.45 | +0.22 ✅ |
| 零命中率 | 2.5% | 10.8% | -8.3pp ✅ |
| P95 延迟 | 580ms | 400ms | +180ms ⚠️ |

## 分桶分析

- `ERROR_CODE` intent 召回仍偏低（72%）——BM25 对错误码命中不稳定
  - **行动**：加一条 `exact_match_channel` 专门处理 ID 类查询

## 退化警报

无退化（所有指标均不劣化）
```

**CLI 入口**：

```bash
go run eval/cmd/eval -set=rag_golden_v1 -out=./eval/reports/$(date +%Y-%m-%d)_rag_golden_v1
```

**CI 集成**（`.github/workflows/eval.yaml`）：

```yaml
on: [pull_request]
jobs:
  rag-eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test ./eval/... -tags=eval
      - run: go run eval/cmd/eval -set=rag_golden_v1 -out=/tmp/pr-eval
      - run: node eval/scripts/compare-scorecard.js /tmp/pr-eval/scorecard.json ${{ baseline }}
      - name: Comment PR
        uses: marocchino/sticky-pull-request-comment@v2
        with:
          path: /tmp/pr-eval/report.md
```

效果：PR 合并前自动 post 一份 scorecard diff 到评论区。

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| Judge LLM 调用失败 | 重试 2 次，仍失败该条标记 `judge_failed: true`，不计入指标 |
| golden set 本身有错标 | 引入 `disputed: true` 字段人工复审时用；指标计算可选过滤 |
| 场景包过大跑一次要 30min | 提供 `--sample=20%` 快跑模式；CI 用 sample，手工 release 前跑全量 |
| LLM-based 指标方差大（同输入多次 judge 结果不同）| 每条 judge 跑 3 次取多数；记录方差到 evidence 里 |
| 新增 dataset 时没有 baseline | 首次 run 存 baseline，后续 delta 对比；baseline 3 个月 rotate 一次 |
| 评估跑偏报假阳性 | `metrics_by_tag` 分桶看哪个 segment 退化，定位是算法问题还是 dataset 问题 |

### 5. 兜底策略

- Judge LLM API 限流：队列 + backoff
- Scorecard 写失败：先写本地临时文件，后台 retry push
- CI 跑超时：降级到 20% 采样 + WARN，但不 block merge（防止误杀）

### 6. 量化指标 & 评估方案

**元指标**（Eval Center 本身的质量）：

| 指标 | 目标 |
|---|---|
| Eval 跑全量时长 | < 10 min（120 条 golden + 50 条 e2e）|
| CI 集成成功率 | > 99% |
| Judge 结果稳定性（3 次跑方差） | < 5% |
| Dataset 覆盖率 | 5 种 intent 各 ≥ 20 条 |
| 每周新增 / 更新 golden 样本 | ≥ 10 条 |

**业务产出**：
- Wave 1 / 2 / 3 每个优化**都有 scorecard 对比报告**作为简历数据源
- PR 合并门槛：`recall@5` / `mrr` 不劣化

### 7. 面试 Q&A 预演

**Q1：为什么不直接用 Ragas？**

A：Ragas 是 Python 库，我们是 Go 栈——引入会多一个进程 + 数据跨语言序列化。Ragas 的**指标定义**值得借鉴（Faithfulness、Answer Relevance 的 prompt 模板），但**执行器**自己写更贴合业务。Ragas 的 Faithfulness prompt 我们 port 成 Go + 改了几处适应中文。

**Q2：LLM-as-judge 不是"让 LLM 给自己打分"吗？可信吗？**

A：关键是**跨家 Judge** 和**规则验证**双轨：
- 客观指标（Recall / MRR / nDCG）**完全规则计算**，不信 LLM
- 主观指标（Faithfulness）用 gpt-4o 或 claude-3.5 作 Judge，而**被评估的**是 qwen（跨家）
- 抽样 20 条 Judge 结果人工复核，发现系统偏差就改 Judge prompt
- 单条 Judge 跑 3 次取多数，降低单次随机性

**不可信的场景**：如果 Judge 和被评估是同一模型（都是 qwen），必然有偏——这是我们特别避免的。

### 硬核 Q&A

**HQ1：Scorecard 说 Recall@5 从 60% → 82%，怎么证明不是 golden set 污染（开发时过拟合到 golden）？**

A：三道防线：
1. **Train/Eval split**：120 条里 **70/30 划分**，train 集用于调参（RRF k、minScore、channel 权重），eval 集 freeze 不给开发者看具体条目。最终报告用 eval 集指标。
2. **Hidden holdout**：另外藏 30 条 "**blind set**"，开发期间谁都看不到，上线前由 QA 跑一次验证——如果 eval 上涨但 blind set 没涨，说明过拟合。
3. **时间冻结**：季度 freeze，每次只加新样本不删旧——baseline 是同一个 commit 上的数字。

**如果被面试官追问**："如果我是评审，我会让你对**刚从线上采样的 30 条用户真实 query** 复算一次指标，和你报的对比——这是最硬的验证。"

**HQ2：你用 gpt-4o 做 Judge，但 gpt-4o 也会静默升级，怎么保证评估基线稳定？**

A：两手：
1. **锁版本**：用 `gpt-4o-2024-08-06` 这种带日期的 snapshot model ID，OpenAI 会保留这些 snapshot 至少 1 年。
2. **Judge 一致性监控**：每季度跑一组"标准测试对"（人工精标 50 条 expected Faithfulness 结果），Judge 输出和 ground truth 对比，一致率 < 90% 触发告警——说明 Judge 模型行为漂移。
3. **多 Judge 投票**：关键 release 用 gpt-4o + claude-3.5 双 Judge，结果不一致时人工仲裁——成本高只在大版本做。

**HQ3：eval_metrics 跑出来 Recall@5 涨了，但用户反馈说"答非所问"——指标和体验脱节怎么办？**

A：**指标脱节是评估体系最常见的陷阱**。要做的是：
1. **拆 funnel**：Retrieval Recall ↑ 但 Answer Relevance ↓，说明检索更多正确文档但 LLM 用不好——调 prompt 而不是调检索。
2. **加端到端任务完成指标**：用户提问到"获得可用答案"的成功率（e2e_scenarios 跑），这是最贴近体验的。
3. **线上 AB**：离线指标只是"可能"，上线小流量 A/B 看**真实用户的追问率、会话长度、满意度**（thumb up/down）。
4. **错配样本溯源**：挑 10 条"Recall 涨但 Answer 没变好"case 手工分析，通常是**排序问题**（相关文档在 top-10 但没进 top-3），加 Rerank 权重即可。

**核心观点**：**单一指标没有任何意义，指标体系 + 线上反馈 + 人工抽样三位一体才能判断。**

**HQ4：golden set 120 条够吗？业界标准多少？**

A：分阶段：
- **PoC 期（我们现在）**：120 条足够建流程，发现明显问题
- **成熟期**：业界 RAG 评估 dataset 通常 500-2000 条（如 BEIR benchmark、MS MARCO 子集）
- **规模化**：> 10 万条（Google SERP 标注规模），但那是搜索业务，Agent 场景 1000-5000 够用

**扩展路径**：
- 线上日志采样 + 人工标（最贴合）
- LLM 生成同义改写（扩广度）
- 对抗样本（扩鲁棒性）

**我敢写在简历上的规模**：500+ 条（Wave 2 落地 6 个月后可达）。**现在写 120 条也可以，但必须说"seed 集 + 持续扩展计划"，不能假装是最终规模**。

**HQ5：Eval CI 每次 PR 跑要 5-10 min，开发者抱怨慢怎么办？**

A：分层策略：
1. **Fast path**：PR 默认跑 **sample 20%**（约 1 min），作为 "cheap sanity check"，指标不能劣化 > 5%
2. **Slow path**：手动 trigger 全量跑（label `run-full-eval`），release 前必过
3. **缓存 embedding**：golden set 的 query embedding 入库时预计算 + 缓存，每次跑 eval 不重算，省 30-50%
4. **并行化**：120 条 query 并发跑（带 rate limit），不串行——eval 本身用 errgroup 并发

**配合规则**：PR 改了 `internal/rag/` 强制跑全量；其他模块走 fast path——用 path filter 控制。

**HQ6：你的 report.md 说"ERROR_CODE intent 召回 72% 偏低"，面试官问"为什么低？"——这时候能不能答？**

A：这是 **Eval Center 价值的试金石**。我的答案应该基于 evidence 不是猜：
1. 打开 `raw-evidence.json` grep `intent=ERROR_CODE`
2. 看这 24 条的 `retrieved` 和 `expected`，看差在哪——是没召回到、还是召回了但 rank 靠后
3. 典型模式可能是：**错误码（如 ERR_CONN_REFUSED）是短 token，向量 embedding 稀释，BM25 命中但 rank 低**
4. **行动**：增加 `exact_id_channel` 用正则提取 query 中的错误码 → Redis 直接按 ID 精准查

**如果答不出来，说明 evidence 没存够**。这也是为什么 Evidence 必须把 `channel_trace` / `rank_per_channel` 都记下来——事后调查时就靠这些。

**HQ7：如果老板说"别搞评估了，先做业务"，你怎么说服？**

A：展示三张图：
1. **变更风险曲线**：没评估时，过去 3 个月改 RAG 平均引入 1-2 个 regression，开发者花 20% 时间修回退 bug
2. **简历含金量对比**：带数据的技术叙述（"Recall@5 +21.5pp"）vs 空洞形容（"优化了检索"）——面试通过率差 3 倍
3. **长期 ROI**：Eval Center 前期投入 2 周，此后每次变更节省 2-3 天验证时间，4-6 周回本

**退一步说服**：如果老板坚持砍，至少保留 **最小 scorecard 脚本 + 120 条 golden**，每次大 release 手动跑——别完全砍掉基线。

---

## Wave 4A 落地节奏（2 周）

| 周 | 任务 | 产出 |
|---|---|---|
| Week 1 | scheme 设计 + runner + metrics（retrieval 指标）+ 首版 120 条 golden | 能跑 `eval run -set=rag_golden_v1` 出 scorecard |
| Week 2 | generation 指标 + LLM Judge + CI 集成 + scorecard diff bot | PR 自动评论 scorecard |

**关键风险**：

| 风险 | 缓解 |
|---|---|
| LLM Judge 成本爆（跨家 API 贵）| 限频 + 缓存 + sample 模式 |
| Judge prompt 设计不稳定 | 前 50 条人工 vs Judge 对比校准，持续调 prompt |
| Golden set 标注成本 | LLM 生成候选 + 人工 Review 二阶段法，人效提升 3-5x |

---

## 面试总纲：三分钟讲完 Wave 4A

> "所有优化改动都要有评估数据，不然简历是拍脑袋。我借鉴 AgentX 搭了 RAG Eval Center：
>
> **三件套 artifact**：raw-evidence（原始执行轨迹含 channel_trace）、scorecard（聚合指标 + git commit + config）、report（人读版 markdown + 分桶分析）。
>
> **指标体系**：客观用规则（Recall@K / MRR / nDCG / 零命中率 / 延迟），主观用**跨家 LLM Judge**（gpt-4o 评 qwen，避免同家偏见），单条 Judge 跑 3 次取多数。
>
> **防过拟合**：Train/Eval 70/30 split + hidden blind set + 季度 freeze + 时间冻结 baseline。
>
> **CI 集成**：PR 自动跑 eval，sample 20% 作 cheap check，全量手动 trigger；scorecard diff bot 自动 comment 到 PR。
>
> **效果**：每次 RAG 改动产出一份 scorecard git-tracked，diff 直接看指标变化；Wave 1/2/3 所有简历数据来源于此。"
