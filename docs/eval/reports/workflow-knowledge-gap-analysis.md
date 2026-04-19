# Workflow Mode Knowledge-Class Gap Analysis

**Source**: `docs/eval/reports/workflow-latest.json` (40 queries, 2026-04-19)
**Finding**: Graph 在 knowledge 类落败 10:1 (9 ties)。根因不是 RAG 召回（那是两路径共同盲区，已独立修复），而是 **RAG 注入模板不一致**。

## 数据

| 类别 | Legacy | Graph | Verdict |
|---|---|---|---|
| Knowledge (n=20) | 10 wins | 1 win | 9 ties |
| Knowledge reply_len avg | 621 chars | 553 chars | graph -11% |
| Knowledge completion_tokens avg | 321 | 289 | graph -10% |

10 个 legacy 胜案中 graph `reply_len` **全部**更短，judge verdict note 一致表述为 "A 更详细 / 更全面 / 举了例子 / 列出了结构"。k08 和 k15 砍了一半内容。

## 机制

两条路径拿到 RAG 检索结果后的拼装方式差异显著：

### Legacy — `internal/chat/service.go:495-510`

```go
kb.WriteString("参考知识：\n")
for i, doc := range docs {
    kb.WriteString(fmt.Sprintf("【来源：%s | 相似度：%.2f】\n%s",
        fileName, doc.Score(), doc.Content))
}
messages = append(messages, schema.UserMessage(kb.String()))
messages = append(messages, schema.AssistantMessage(
    "好的，我已了解相关知识，请继续提问。", nil))
messages = append(messages, schema.UserMessage(prompt))
```

注入方式：
- **独立 UserMessage** 承载参考知识，每条带 `【来源：<filename> | 相似度：<score>】` 元数据
- 后跟一条**假 AssistantMessage ack**（"好的，我已了解相关知识，请继续提问。"）
- 真 user prompt 单独作为最后一条消息

### Graph — `internal/chat/workflow/nodes.go:58-70`

```go
userContent := e.Query
if len(e.RAGDocs) > 0 {
    var b strings.Builder
    b.WriteString("参考知识：\n")
    for i, d := range e.RAGDocs {
        b.WriteString(d.Content)
        if i < len(e.RAGDocs)-1 {
            b.WriteString("\n---\n")
        }
    }
    b.WriteString("\n\n用户问题：")
    b.WriteString(e.Query)
    userContent = b.String()
}
msgs = append(msgs, schema.UserMessage(userContent))
```

注入方式：
- **单条 UserMessage** 里把知识和问题拼在一起
- **无文件名、无相似度、无 ack**
- 只有 `---` 做 doc 分隔

## 为什么这会让答案变短

1. **相似度信号缺失**：Legacy 告诉模型"这条 0.74 那条 0.62"，模型更倾向从高分段落展开叙述。Graph 给的是纯文本拼接，模型对每段的权重没有先验，倾向简短汇总。
2. **文件名缺失**：Legacy 让模型可以"据 X 文档所述..."引出详述；Graph 无法引用，回答更抽象。
3. **假 ack 缺失**：Legacy 的 `AssistantMessage("好的，我已了解相关知识")` 让接下来的真用户 prompt 处在"对话模式"，模型会展开解释。Graph 把知识糊进 user message，模型更像是在"完成 Q&A 任务"，回答紧凑。
4. **ReAct 固有倾向**：graph 路径走 ReAct Agent，即使 knowledge 类不调 tool，prompt 结构仍带规划框架，LLM 输出更收敛。

## 建议修复

对齐 `workflow/nodes.go:buildPromptFn` 的 RAG 注入格式到 legacy：
- 拆成独立 UserMessage（不混入 user prompt）
- 每条 doc 带 `【来源：<file_name> | 相似度：<score>】`
- 追加假 AssistantMessage ack
- 真 user prompt 作为最后一条 UserMessage

需要额外考虑：
- `file_name` 从 `doc.MetaData["file_name"]` 取（要确认 graph 路径的 Retriever 有填；DocumentConverter 默认不塞 file_name，legacy 靠 indexer HSET 直接写 hash 字段）
- `doc.Score()` 在 hybrid pipeline 里是 RRF 后的融合分数，含义和 legacy 的 distance→score 不完全一致；graph 跑 hybrid 时 score semantics 要对齐

## 次要观察

- RAG 检索 0 命中（40 queries 全程）是两路径共同盲区，已单独修复（见 `internal/rag/store.go` + `store_converter_test.go`）
- 本次 baseline 是 **RAG-broken** 状态下的对比，修复 RAG 后两路径差距可能收窄或放大，需重跑 baseline 确认
