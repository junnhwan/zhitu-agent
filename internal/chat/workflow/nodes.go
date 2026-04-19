package workflow

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

func enrichFn(deps *Deps) func(context.Context, *Request) (*enriched, error) {
	return func(ctx context.Context, req *Request) (*enriched, error) {
		e := &enriched{Request: req, Query: req.Prompt}
		if deps.IntentRouter == nil {
			return e, nil
		}
		res, err := deps.IntentRouter.Understand(ctx, req.SessionID, req.History, req.Prompt)
		if err != nil {
			log.Printf("[workflow.enrich] understand failed, keep original: %v", err)
			return e, nil
		}
		if res.RewrittenQuery != "" {
			e.Query = res.RewrittenQuery
		}
		e.Intent = res.Intent
		return e, nil
	}
}

func retrieveFn(deps *Deps) func(context.Context, *enriched) (*enriched, error) {
	return func(ctx context.Context, e *enriched) (*enriched, error) {
		if deps.RAG == nil || e.Intent == nil || e.Intent.Domain != "KNOWLEDGE" {
			return e, nil
		}
		rctx := channel.WithDomain(ctx, e.Intent.Domain)
		docs, err := deps.RAG.Retriever.Retrieve(rctx, e.Query)
		if err != nil {
			log.Printf("[workflow.retrieve] rag failed, skip: %v", err)
			return e, nil
		}
		e.RAGDocs = docs
		return e, nil
	}
}

func buildPromptFn(deps *Deps) func(context.Context, *enriched) ([]*schema.Message, error) {
	return func(ctx context.Context, e *enriched) ([]*schema.Message, error) {
		// 对齐 legacy service.go:495-510 的 RAG 注入模板：
		// 独立 UserMessage 承载参考知识 + 每条带【来源 | 相似度】元数据 +
		// 假 AssistantMessage ack，让 LLM 进入"已读消化"态而非"裸拼接问答"态。
		// 缺了这套结构 graph 在 knowledge 类比 legacy 短 10%（见
		// docs/eval/reports/workflow-knowledge-gap-analysis.md）。
		msgs := make([]*schema.Message, 0, len(e.Request.History)+4)
		if deps.SystemPrompt != "" {
			msgs = append(msgs, schema.SystemMessage(deps.SystemPrompt))
		}
		msgs = append(msgs, e.Request.History...)

		if len(e.RAGDocs) > 0 {
			msgs = append(msgs, schema.UserMessage(formatRAGContext(e.RAGDocs)))
			msgs = append(msgs, schema.AssistantMessage("好的，我已了解相关知识，请继续提问。", nil))
		}
		msgs = append(msgs, schema.UserMessage(e.Query))
		e.Messages = msgs
		return msgs, nil
	}
}

func formatRAGContext(docs []*schema.Document) string {
	var b strings.Builder
	b.WriteString("参考知识：\n")
	for i, d := range docs {
		fileName := "未知文件"
		if v, ok := d.MetaData["file_name"]; ok {
			if fn, ok := v.(string); ok && fn != "" {
				fileName = fn
			}
		}
		fmt.Fprintf(&b, "【来源：%s | 相似度：%.2f】\n%s", fileName, d.Score(), d.Content)
		if i < len(docs)-1 {
			b.WriteString("\n\n---\n\n")
		}
	}
	return b.String()
}

func wrapResponseFn() func(context.Context, *schema.Message) (*Response, error) {
	return func(ctx context.Context, msg *schema.Message) (*Response, error) {
		return &Response{Message: msg}, nil
	}
}

func enrichNode(deps *Deps) *compose.Lambda      { return compose.InvokableLambda(enrichFn(deps)) }
func retrieveNode(deps *Deps) *compose.Lambda    { return compose.InvokableLambda(retrieveFn(deps)) }
func buildPromptNode(deps *Deps) *compose.Lambda { return compose.InvokableLambda(buildPromptFn(deps)) }
func wrapResponseNode() *compose.Lambda          { return compose.InvokableLambda(wrapResponseFn()) }
