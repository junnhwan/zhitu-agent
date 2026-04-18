package workflow

import (
	"context"
	"log"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
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
		docs, err := deps.RAG.Retriever.Retrieve(ctx, e.Query)
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
		msgs := make([]*schema.Message, 0, len(e.Request.History)+3)
		if deps.SystemPrompt != "" {
			msgs = append(msgs, schema.SystemMessage(deps.SystemPrompt))
		}
		msgs = append(msgs, e.Request.History...)

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
		e.Messages = msgs
		return msgs, nil
	}
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
