# ZhituAgent Phase 2 Wave 1 实施计划：对话质量基建

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to execute task-by-task. Steps use `- [ ]` checkbox syntax.

**Goal:** 把对话层从"关键词路由 + 截断式记忆 + 手写 tool-loop"升级到"意图理解 + 语义压缩 + Eino Graph 编排"。

**Architecture:** 三个独立 PR 依次交付 —— **P3 记忆压缩** → **P2 Query 理解** → **P4 Eino Graph 重构**。P3 最独立最低成本先做；P2 需要 LLM 客户端和提示词工程；P4 把前两者作为 Lambda 节点接入 Eino Graph + ReAct Agent。每步后做灰度开关 + 可回滚。

**Tech Stack:** Go 1.26.1 · Eino 0.8.9（`compose.Graph` / `flow/agent/react`）· eino-ext/model/qwen 0.1.8 · Redis Stack (`redis/go-redis/v9`) · Viper 配置 · Prometheus 打点

**设计来源：** `docs/research/phase2/interview-prep-wave1.md`（七段式深设计）；两大北极星参考 `docs/research/phase2/opspilot.md`（同 Eino 栈 Graph/ReAct）+ `docs/research/phase2/ragent.md`（三级意图树 + 引导澄清）。

---

## Context（为什么现在做这个）

**Phase 1 复刻完成后的已知痛点：**

1. **记忆压缩是假摘要**（`internal/memory/token_compressor.go:70-85`）：
   - `generateSummary` 取前 3 条消息各截 50 字符，第 4 轮之后信息全丢
   - Token 估算 `len(text)/4` 对中文低估 50%
   - 工具结果（RAG 返回 5 段 × 500 字 ≈ 30KB）未做专项压缩
2. **路由是关键词硬编码**（`internal/agent/orchestrator.go:10-12`）：
   - `knowledgeKeywords = ["查询", "了解", "什么是", ...]` + `strings.Contains`
   - 多轮指代无法解析（"它有哪些变种？"）、口语化/反问漏召回、"查询订单"等假阳性
3. **手写 tool-call loop 重复**（`chat/service.go:188-215` + `254-319`）：
   - Chat + StreamChat 各写一遍，改一处忘另一处（已发生 2 次 bug）
   - 工具串行执行、无并行分支、回调追踪难以挂钩、P2/P3 接入改动面积巨大

**Phase 2 Wave 1 目标：** 2-3 周，以三个独立 PR 解除上述三点，为 Wave 2/3 铺平基础。

**CLAUDE.md 约束解冻说明：** 现 CLAUDE.md 明确写"记忆压缩是简单截取…不要擅自升级"和"Token 估算：len(text) / 4，不要换成 tiktoken 之类"。这些是 Phase 1 冻结的，`SUMMARY.md` 明确 **Phase 2 是协商解冻的时机**。**P3 第 0 步** 先更新 CLAUDE.md 解除这两条冻结，并记录新的契约。

---

## 整体文件结构

**P3 新增/修改：**
- `internal/memory/token_counter.go` **新增** — 中英分离 token 估算
- `internal/memory/llm_compressor.go` **新增** — LLM 摘要压缩
- `internal/memory/micro_compact.go` **新增** — 工具结果压缩
- `internal/memory/compressor.go` **新增** — `Compressor` 接口 + 策略路由
- `internal/memory/token_compressor.go` 修改 — 用新 token_counter；保留作为 fallback
- `internal/memory/compressible_memory.go` 修改 — 接收 `Compressor` 接口而非具体类型
- `internal/chat/service.go` 修改 — `executeToolCall` 后接入 Micro Compact
- `internal/config/config.go` 修改 — 增加 `compression.strategy`、`compression.llm_model` 字段
- `config.yaml` 修改 — 新字段默认值
- `CLAUDE.md` **修改（P3 第 0 步）** — 解冻两条约束

**P2 新增/修改：**
- `internal/understand/rewriter.go` **新增** — Query Rewrite
- `internal/understand/intent.go` **新增** — 三级意图分类
- `internal/understand/guardian.go` **新增** — 置信度判断 + 澄清
- `internal/understand/tree.yaml` **新增** — 意图树配置
- `internal/understand/service.go` **新增** — 统一入口 + `sony/gobreaker` 熔断
- `internal/agent/orchestrator.go` 修改 — 新入口走 understand，keyword routing 保留作为 fallback
- `internal/config/config.go` 修改 — 增加 `understand.*` 段
- `go.mod` — 新增 `github.com/sony/gobreaker/v2`

**P4 新增/修改：**
- `internal/chat/workflow/chat_workflow.go` **新增** — Eino Graph 编排
- `internal/chat/workflow/nodes.go` **新增** — Lambda 节点工厂
- `internal/chat/workflow/spike_test.go` **新增** — Week 1 spike 验证
- `internal/chat/service.go` 修改 — 按 config `workflow_mode: legacy|graph` 路由
- `internal/config/config.go` 修改 — 增加 `chat.workflow_mode`
- 现有 `service.go` 手写 loop 保留 3 个 release 作为 safety net

**参考现有代码（只读，不改）：**
- `internal/memory/distributed_lock.go` — 分布式锁
- `internal/memory/compressible_memory.go:44-46, 68-75` — 锁 + 压缩触发条件
- `internal/chat/service.go:344-397` — buildMessages（P4 会重写到 Graph）
- `internal/rag/` — P4 的 RAG Lambda 节点复用 `rag.Retriever.Retrieve`
- `internal/tool/` — P4 的 ToolsNode 复用现有 Tool 实例
- `docs/research/phase2/interview-prep-wave1.md` — 每个设计决策的业务背景/选型/面试 Q&A

---

## PR 1 — P3 记忆压缩升级（成本最低、最独立）

**产出：** LLM 摘要 + Micro Compact + 中英分离 token 估算，策略可配置可降级。

### Task 0: 解冻 CLAUDE.md + 搭建 config 骨架

**Files:**
- Modify: `CLAUDE.md`
- Modify: `internal/config/config.go:72-79`
- Modify: `config.yaml`

- [ ] **Step 1: 更新 CLAUDE.md 解除 P3 冻结**

把"记忆压缩是简单截取..."与"Token 估算：len(text)/4..."两条整段替换为：

```markdown
- **记忆压缩策略可配置**：策略由 `chat_memory.compression.strategy` 选择：
  - `simple`（默认兼容旧行为）：前 3 条各截 50 字符摘要
  - `llm_summary`：超 9 轮时 qwen-turbo 摘要前 N-6 轮，保留最近 6 轮
  - `hybrid`：llm_summary + Micro Compact（工具结果压缩）
  降级链：LLM 失败 → simple 摘要 → fallbackToRecent
- **Token 估算分中英**：`tokenCountChinese(s)/2 + tokenCountEnglish(s)/4`，不引入 tiktoken
  （Go 无官方实现 + 要加 C 依赖 + 精度差异 ± 10% 对压缩触发足够）
```

- [ ] **Step 2: 加 config 字段**

在 `ChatMemoryCompressionConfig` 里新增：

```go
Strategy   string `mapstructure:"strategy"`      // simple | llm_summary | hybrid
LLMModel   string `mapstructure:"llm_model"`     // qwen-turbo
MicroCompactThreshold int `mapstructure:"micro_compact_threshold"` // 默认 2000 chars
```

- [ ] **Step 3: config.yaml 默认值 + 运行 `go build ./...` 验证编译通过**
- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md internal/config/config.go config.yaml
git commit -m "chore(memory): unfreeze compression & token-estimate contracts for Wave 1"
```

### Task 1: 中英分离 token 估算

**Files:**
- Create: `internal/memory/token_counter.go`
- Create: `internal/memory/token_counter_test.go`
- Modify: `internal/memory/token_compressor.go:60-66`

- [ ] **Step 1: 先写 failing test**

```go
// token_counter_test.go
func TestEstimateTokensCJK(t *testing.T) {
    tests := []struct {
        name string
        text string
        want int
    }{
        {"pure ascii", "hello world", 2},          // 11/4 = 2
        {"pure chinese", "你好世界测试", 3},         // 6/2 = 3
        {"mixed", "hello 你好", 3},                  // 6/4 + 2/2 = 1 + 1 = 2 → 向上取整 3
        {"empty", "", 0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := EstimateTokens(tt.text); got != tt.want {
                t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails** — `go test ./internal/memory/ -run TestEstimateTokensCJK` → 期望 "undefined: EstimateTokens"

- [ ] **Step 3: 实现 `EstimateTokens`**

```go
// token_counter.go
package memory

import "unicode"

func EstimateTokens(text string) int {
    cjkRunes := 0
    otherBytes := 0
    for _, r := range text {
        if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
            cjkRunes++
        } else {
            otherBytes += len(string(r))
        }
    }
    return (cjkRunes + 1) / 2 + (otherBytes + 3) / 4 // 向上取整
}
```

- [ ] **Step 4: 改造 `TokenCountCompressor.EstimateTokens` 调用新函数**

```go
// token_compressor.go:60-66
func (c *TokenCountCompressor) EstimateTokens(messages []*schema.Message) int {
    total := 0
    for _, msg := range messages {
        total += EstimateTokens(msg.Content)
    }
    return total
}
```

- [ ] **Step 5: Run all memory tests** — `go test ./internal/memory/... -v` 全绿
- [ ] **Step 6: Commit**

```bash
git add internal/memory/token_counter.go internal/memory/token_counter_test.go internal/memory/token_compressor.go
git commit -m "feat(memory): separate CJK/ASCII token estimation"
```

### Task 2: Compressor 接口 + 策略路由

**Files:**
- Create: `internal/memory/compressor.go`
- Create: `internal/memory/compressor_test.go`

- [ ] **Step 1: 定义接口 + strategy 工厂（写失败测试）**

```go
// compressor_test.go
func TestNewCompressorSimple(t *testing.T) {
    c, err := NewCompressor(Config{Strategy: "simple", RecentRounds: 2, RecentTokenLimit: 2000})
    if err != nil { t.Fatal(err) }
    if _, ok := c.(*TokenCountCompressor); !ok { t.Errorf("expected TokenCountCompressor") }
}

func TestNewCompressorUnknown(t *testing.T) {
    if _, err := NewCompressor(Config{Strategy: "nope"}); err == nil {
        t.Errorf("expected error for unknown strategy")
    }
}
```

- [ ] **Step 2: Run failing**
- [ ] **Step 3: 实现接口 + 工厂**

```go
// compressor.go
type Compressor interface {
    Compress(ctx context.Context, messages []*schema.Message) []*schema.Message
    EstimateTokens(messages []*schema.Message) int
}

type Config struct {
    Strategy          string
    RecentRounds      int
    RecentTokenLimit  int
    LLMModel          string
    APIKey, BaseURL   string
    MicroCompactMinLen int
}

func NewCompressor(cfg Config) (Compressor, error) { /* switch cfg.Strategy */ }
```

- [ ] **Step 4: 改 `TokenCountCompressor.Compress` 签名加 `ctx context.Context`**（不使用但实现接口）
- [ ] **Step 5: Run** `go test ./internal/memory/... -v` 全绿
- [ ] **Step 6: Commit**

```bash
git commit -am "refactor(memory): introduce Compressor interface + strategy factory"
```

### Task 3: LLM 摘要 Compressor

**Files:**
- Create: `internal/memory/llm_compressor.go`
- Create: `internal/memory/llm_compressor_test.go`

- [ ] **Step 1: 写失败测试（用 ChatModel 接口的 mock）**

```go
// 借鉴 internal/chat/service.go 里如何构造 qwen model
type fakeLLM struct{ reply string; err error }
func (f *fakeLLM) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
    if f.err != nil { return nil, f.err }
    return schema.AssistantMessage(f.reply, nil), nil
}

func TestLLMCompressorHappyPath(t *testing.T) {
    c := &LLMCompressor{llm: &fakeLLM{reply: "用户自我介绍叫小明，在做 Go 项目"}, fallback: NewTokenCountCompressor(3, 2000), recent: 6, threshold: 9}
    msgs := make([]*schema.Message, 10) // > 9 触发压缩
    for i := range msgs { msgs[i] = schema.UserMessage(fmt.Sprintf("msg%d", i)) }
    out := c.Compress(context.Background(), msgs)
    // 期望：1 条 summary system msg + 6 条 recent
    if len(out) != 7 { t.Errorf("len = %d, want 7", len(out)) }
    if out[0].Role != schema.System { t.Errorf("first role = %v", out[0].Role) }
    if !strings.Contains(out[0].Content, "小明") { t.Errorf("summary missing fact") }
}

func TestLLMCompressorFallback(t *testing.T) {
    c := &LLMCompressor{llm: &fakeLLM{err: errors.New("boom")}, fallback: NewTokenCountCompressor(3, 2000), recent: 6, threshold: 9}
    msgs := make([]*schema.Message, 10)
    for i := range msgs { msgs[i] = schema.UserMessage(fmt.Sprintf("m%d", i)) }
    out := c.Compress(context.Background(), msgs)
    // 兜底到 simple summary + recent
    if out[0].Role != schema.System { t.Errorf("expected fallback summary, got %v", out[0].Role) }
}
```

- [ ] **Step 2: Run failing**
- [ ] **Step 3: 实现 LLMCompressor**（只在 `len(messages) > threshold` 时触发，prompt 要求保留"用户明确陈述的事实"，参考 `interview-prep-wave1.md:269-275`）
- [ ] **Step 4: 接入 `NewCompressor` 工厂：`case "llm_summary"` / `case "hybrid"`**
- [ ] **Step 5: Run tests**
- [ ] **Step 6: Commit** — `feat(memory): add LLM summary compressor with simple fallback`

### Task 4: Micro Compact（工具结果压缩）

**Files:**
- Create: `internal/memory/micro_compact.go`
- Create: `internal/memory/micro_compact_test.go`
- Modify: `internal/chat/service.go:199-207, 307-314`（两处 tool result 入口）

- [ ] **Step 1: 失败测试**

```go
func TestMicroCompactSkipShort(t *testing.T) {
    mc := &MicroCompactor{Threshold: 2000}
    out := mc.Compact(context.Background(), "rag_search", "small result")
    if out != "small result" { t.Errorf("short result should pass through") }
}

func TestMicroCompactRagTopN(t *testing.T) {
    mc := &MicroCompactor{Threshold: 10}
    huge := strings.Repeat("【来源：a.md | 相似度：0.9】\n段落内容\n\n---\n\n", 10) // 10 段
    out := mc.Compact(context.Background(), "rag_search", huge)
    // 期望：只保留 top-3
    if strings.Count(out, "【来源：") > 3 { t.Errorf("top-3 not applied") }
}
```

- [ ] **Step 2-5: 实现 + 失败/通过/绿色（ragSearch 取 top-3；email 提取收件人；未知工具走 LLM 一句话总结，失败时返回原始）**
- [ ] **Step 6: 在 `chat/service.go:199` 和 `:307`（`executeToolCall` 返回后立刻）接入 Micro Compact —— 但只压"进内存"的版本，当前轮给 LLM 的仍用原始**

```go
// 两处都改成：
toolResult, err := s.executeToolCall(ctx, tc)
// ...原逻辑拼 messages...
// 新增：记忆版本做 Micro Compact
compactedForMem := s.microCompactor.Compact(ctx, tc.Function.Name, toolResult)
messages = append(messages, schema.ToolMessage(toolResult, tc.ID, ...)) // 本轮还是原始
// 但写入 mem 时要用 compactedForMem —— 具体落在 mem.Add 之前，或在 Task 5 改 compressible_memory.go 时用 metadata 区分
```

**实施要点：** 最小改动 = 保存到 memory 前替换内容。具体做法：新增 `ToolMessageForMemory(content string)` helper 或在 `CompressibleMemory.Add` 入口判断 `Role == schema.Tool` 时 Compact。

- [ ] **Step 7: Run full `go test ./...`**
- [ ] **Step 8: Commit** — `feat(memory): add Micro Compact for tool results`

### Task 5: 接入 CompressibleMemory + 端到端验证

**Files:**
- Modify: `internal/memory/compressible_memory.go:26, 36-47, 71-72, 129-135`
- Modify: `internal/chat/service.go:72-76`（NewService 里构造 Compressor）

- [ ] **Step 1: `CompressibleMemory` 的 `compressor *TokenCountCompressor` 字段改为 `compressor Compressor`**
- [ ] **Step 2: `NewCompressibleMemory` 接口签名同步（保持向后兼容）**
- [ ] **Step 3: `chat/service.go:72-76` 用 `NewCompressor(Config{Strategy: cfg.ChatMemory.Compression.Strategy, ...})`**
- [ ] **Step 4: 新增端到端测试 `compressible_memory_integration_test.go`（需要本地 Redis）**：
  - 构造 20 轮对话 → 触发压缩
  - 第 21 轮询问早期 key_fact
  - 断言压缩后消息数 = 7（1 summary + 6 recent）
- [ ] **Step 5: `go test -tags=integration ./internal/memory/...`**
- [ ] **Step 6: Commit** — `feat(memory): wire Compressor interface + end-to-end integration test`

### Task 6: PR1 收尾

- [ ] 运行 `go test ./... -race -count=1` 全绿
- [ ] 在 `config.yaml` 把 `chat_memory.compression.strategy` 默认值设 **`simple`**（保留旧行为，不默认启用 LLM）
- [ ] 更新 `docs/research/phase2/SUMMARY.md` 末尾加 "✅ P3 Wave 1 已实现 (PR #xxx)"
- [ ] 提 PR：`feat(wave1/p3): memory compression upgrade — LLM summary + micro compact + CJK token`

---

## PR 2 — P2 Query Rewrite + 三级意图分类

**产出：** `internal/understand/` 新包 + 替换 `orchestrator.needKnowledgeRetrieval`。

### Task 7: understand 包骨架 + 意图树

**Files:**
- Create: `internal/understand/tree.yaml`
- Create: `internal/understand/tree.go`（加载 yaml）
- Create: `internal/understand/tree_test.go`
- Modify: `go.mod`（可能已有 goccy/go-yaml，复用）

- [ ] **Step 1: 写 failing test：LoadTree 返回 3 个 domain**
- [ ] **Step 2: 写 tree.yaml**（见 `interview-prep-wave1.md:108-126`）：KNOWLEDGE / TOOL / CHITCHAT
- [ ] **Step 3: 实现 `LoadTree(path string) (*Tree, error)`**（`goccy/go-yaml` 已在间接依赖）
- [ ] **Step 4-6: 测试通过 + Commit**

### Task 8: Rewriter

**Files:**
- Create: `internal/understand/rewriter.go`
- Create: `internal/understand/rewriter_test.go`

- [ ] **Step 1: 失败测试（用 fakeLLM 同 Task 3）**
  - 无历史 → 返回原 query
  - 有历史 → 返回 LLM reply
  - LLM 错误 → 返回原 query（不阻断）
- [ ] **Step 2-5: 实现 `Rewriter.Rewrite(ctx, history, query) (string, error)`**（最多取 history 最后 6 条）
- [ ] **Step 6: Commit**

### Task 9: Classifier（三级意图分类 + JSON 容错）

**Files:**
- Create: `internal/understand/intent.go`
- Create: `internal/understand/intent_test.go`

- [ ] **Step 1: 失败测试**
  - 合法 JSON → 解析成功
  - 带解释文本的 JSON（"这是结果：{...}"）→ 正则提取成功
  - 全非法 → 返回 CHITCHAT + 打点
- [ ] **Step 2-5: 实现 Classify + `renderTreeToPrompt(tree)` + 正则兜底**（参考 `interview-prep-wave1.md:92-104`）
- [ ] **Step 6: Commit**

### Task 10: Guardian（置信度 + 澄清）

**Files:**
- Create: `internal/understand/guardian.go`
- Create: `internal/understand/guardian_test.go`

- [ ] **Step 1-5: TDD**
  - confidence ≥ 0.6 → 直接路由
  - 0.6 > confidence → 生成澄清问题（对话级状态记录"低置信度次数"）
  - 连续 3 次低置信度 → 停止追问走 CHITCHAT
- [ ] **Step 6: Commit**

### Task 11: understand.Service 统一入口 + 熔断

**Files:**
- Create: `internal/understand/service.go`
- Create: `internal/understand/service_test.go`
- Modify: `go.mod` — `go get github.com/sony/gobreaker/v2`

- [ ] **Step 1-5:**
  - Service 串联 Rewriter → Classifier → Guardian
  - 用 `gobreaker.CircuitBreaker` 包装整条链路，整条链超时 > 3s 或 5 分钟内错误率 > 50% 时熔断 60s
  - 熔断时返回 `{Route: "fallback_keyword", OriginalQuery: q}`
- [ ] **Step 6: Commit**

### Task 12: 接入 Orchestrator

**Files:**
- Modify: `internal/agent/orchestrator.go:31-56`
- Modify: `internal/chat/service.go:109-114`（InitOrchestrator 注入 understand.Service）

- [ ] **Step 1: `SimpleOrchestrator` 加字段 `understand *understand.Service`**
- [ ] **Step 2: `Process` 先走 understand，拿不到结果（熔断 / fallback_keyword）才走 `needKnowledgeRetrieval`**
- [ ] **Step 3: 改 existing `orchestrator_test.go` — 注入 stub understand.Service 测试两条路径**
- [ ] **Step 4: Run** `go test ./internal/agent/... -v`
- [ ] **Step 5: Commit**

### Task 13: PR2 收尾 + 离线评估集

**Files:**
- Create: `docs/research/phase2/eval/wave1-intent.jsonl`（50 多轮 + 20 单轮边界样本）
- Create: `internal/understand/offline_eval_test.go`（读 jsonl、跑 Classify、输出准确率 + confusion matrix）

- [ ] **Step 1: 手工造 50 + 20 测试样本**（可先 20 + 10，后续补齐）
- [ ] **Step 2: 跑 `go test -tags=eval ./internal/understand/...` 得到 baseline 数字**
- [ ] **Step 3: 在 `docs/research/phase2/SUMMARY.md` 末尾 append scorecard**
- [ ] **Step 4: Commit + 开 PR：`feat(wave1/p2): query rewrite + 3-level intent tree + circuit breaker`**

---

## PR 3 — P4 Eino Graph + ReAct Agent 重构

**产出：** `internal/chat/workflow/` 新包用 Eino Graph 重写 Chat / StreamChat / MultiAgentChat，config 切换 `legacy | graph` 灰度。

### Task 14: Eino Graph API spike（Week 1 必做）

**Files:**
- Create: `internal/chat/workflow/spike_test.go`

⚠️ **关键前置：不要猜 Eino API。** 去读 `docs/research/phase2/opspilot.md` 与 `vendor/github.com/cloudwego/eino/compose/` + `vendor/github.com/cloudwego/eino/flow/agent/react/` 源码（或 `go doc github.com/cloudwego/eino/compose.Graph`），确认：

- [ ] **Step 1: `go doc github.com/cloudwego/eino/compose Graph | tee /tmp/eino-graph.txt`** 确认节点类型、输入输出约束
- [ ] **Step 2: `go doc github.com/cloudwego/eino/flow/agent/react`** 确认 ReAct Agent 构造与 `AgentConfig` 字段
- [ ] **Step 3: 写最小 Graph spike —— 1 个 Lambda 节点读字符串 → ReAct Agent → 返回**
- [ ] **Step 4: 跑通 spike → 记下实际 API 写法（与 interview-prep-wave1.md:434-470 示例做对照，修正差异）**
- [ ] **Step 5: Commit** — `chore(wave1/p4): eino graph api spike`

### Task 15: 定义 Request/Response + 依赖结构

**Files:**
- Create: `internal/chat/workflow/types.go`

- [ ] **Step 1-4: 定义**

```go
type Request struct { SessionID int64; Prompt string }
type Response struct { Text string; TokensUsed int }
type Deps struct {
    ChatModel  model.ChatModel
    Tools      []tool.InvokableTool
    Rewriter   *understand.Rewriter
    Classifier *understand.Classifier
    RAG        rag.Retriever
    MemProvider func(sessionID int64) *memory.CompressibleMemory
    SystemPrompt string
}
```

- [ ] **Step 5: Commit**

### Task 16: Lambda 节点工厂

**Files:**
- Create: `internal/chat/workflow/nodes.go`
- Create: `internal/chat/workflow/nodes_test.go`

- [ ] **TDD 每个节点：**
  - `RewriteNode` — 调 Rewriter，失败返回原 query
  - `ClassifyNode` — 调 Classifier，失败返回 CHITCHAT
  - `RAGNode` — 调 Retriever，失败/超时返回空 docs
  - `BuildPromptNode` — AllPredecessor 汇聚：拼 system + memory + rag docs + rewritten query
  - `MemoryWriteNode` — 调 `mem.Add(user)` + `mem.Add(assistant)`
- [ ] **Commit** — `feat(wave1/p4): add graph lambda nodes`

### Task 17: 组装 ChatWorkflow

**Files:**
- Create: `internal/chat/workflow/chat_workflow.go`
- Create: `internal/chat/workflow/chat_workflow_test.go`

- [ ] **Step 1: 失败测试 — Compile 不 panic**
- [ ] **Step 2-4: 按 `interview-prep-wave1.md:434-470` 骨架 + spike 修正后的 API 组装：`[rewrite | classify | rag] → build_prompt → react_agent → memory_write`**
- [ ] **Step 5: 流式版本单独 `StreamWorkflow`（或同一个 Graph 同时支持 `Invoke` / `Stream`）—— 根据 spike 结果决定**
- [ ] **Step 6: Commit**

### Task 18: Service 切换 + 灰度开关

**Files:**
- Modify: `internal/config/config.go` —— 加 `Chat.WorkflowMode string` (`legacy | graph`)
- Modify: `internal/chat/service.go:160-341` —— `Chat` / `StreamChat` / `MultiAgentChat` 三个入口根据 `workflowMode` 选择 legacy 或 graph
- Modify: `config.yaml` —— 默认 `workflow_mode: legacy`

- [ ] **Step 1: 加 config 字段 + `NewService` 时若 `workflow_mode: graph` 则构造 Runnable**
- [ ] **Step 2: 路由逻辑**

```go
func (s *Service) Chat(ctx context.Context, sessionID int64, prompt string) (string, error) {
    if s.workflow != nil && s.workflowMode == "graph" {
        resp, err := s.workflow.Invoke(ctx, &Request{SessionID: sessionID, Prompt: prompt})
        // ... metrics + fallback on error
        return resp.Text, err
    }
    // 原有手写 loop 保留
}
```

- [ ] **Step 3: `go test ./internal/chat/...` 双路都绿**
- [ ] **Step 4: Commit** — `feat(wave1/p4): wire graph workflow behind config flag`

### Task 19: 回归 + 灰度文档 + PR3 收尾

- [ ] `go test ./... -race -count=1` 全绿
- [ ] 手动 smoke：本地起服务，`workflow_mode: legacy` 与 `graph` 各跑 10 条样本对比
- [ ] 写 `docs/research/phase2/wave1-rollout.md`（灰度方案 + 回滚步骤 + 监控关键指标）
- [ ] 更新 `CLAUDE.md`：增加"对话链路有两条 —— legacy（`service.Chat`）和 graph（`workflow.Runnable`），新功能加到 graph"
- [ ] 更新 `docs/research/phase2/SUMMARY.md`："✅ P4 Wave 1 已实现"
- [ ] 提 PR：`feat(wave1/p4): eino graph + react agent orchestration with legacy fallback`

---

## Wave 1 完成标准（所有 PR 合并后）

- [ ] `go test ./... -race` 全绿
- [ ] Grafana 指标可见：`memory_compress_duration_seconds` / `understand_intent_classify_total{domain, correct}` / `chat_workflow_mode{mode}`
- [ ] `docs/research/phase2/SUMMARY.md` P3/P2/P4 三行都标 ✅
- [ ] CLAUDE.md 同步更新两处：压缩策略（Task 0 已改）+ 对话链路（Task 19 已改）
- [ ] 记忆 `current_progress.md` 更新：Phase 2 Wave 1 DONE，下一步 Wave 2（P1 多通道检索）

---

## 验证（端到端）

```bash
# 1. 单测全绿
go test ./... -race -count=1

# 2. 集成测试（需要 Redis）
docker-compose up -d redis-stack
go test -tags=integration ./internal/memory/... -v
go test -tags=eval ./internal/understand/... -v  # 跑离线意图评估

# 3. 服务级 smoke
docker-compose up -d
# 在 config.yaml 切 workflow_mode
curl -X POST http://localhost:8080/ai/multiAgentChat -d '{"sessionId":1,"prompt":"什么是 RAG"}'
curl -X POST http://localhost:8080/ai/multiAgentChat -d '{"sessionId":1,"prompt":"它有哪些变种？"}'  # 多轮指代
# 观察：日志应见 [Rewriter]/[Classifier]；memory 应 ≤ 7 条（9 轮触发）

# 4. 灰度对比
# legacy vs graph 各跑 100 条 eval set，对比端到端 P50/P95、意图准确率、工具调用成功率
```

---

## 风险登记

| 风险 | 概率 | 缓解 |
|---|---|---|
| Eino Graph API 与 interview-prep 示例不一致 | 中 | Task 14 spike 先跑通再动业务 |
| LLM 摘要 / Rewrite 成本超预期 | 低 | Prometheus `llm_tokens_total{op}` 监控，超标报警 |
| 灰度期间指标劣化 | 中 | `workflow_mode` config 热切，10% → 50% → 100% |
| CLAUDE.md 解冻后团队共识偏差 | 低 | Task 0 改动单独提前，和 reviewer 显式沟通 |
| 手写 loop 保留期内被遗忘 | 低 | 在 `service.go:160` 顶部加 TODO 注释记录清理时间点 |

---

## 下一步（Wave 2/3/4 衔接）

- Wave 2 复用 **P2 Classifier 结果** 做多通道路由（`understand.IntentResult.Domain` → `VectorChannel / BM25Channel / IntentChannel`）
- Wave 3 直接在 **P4 Graph** 新增 MCP Client ToolNode（与现有 RagTool / EmailTool 平级）
- Wave 4 P9 Eval Center 第一批 seed = Task 13 的 `wave1-intent.jsonl` + 新增 `wave1-memory.jsonl`
