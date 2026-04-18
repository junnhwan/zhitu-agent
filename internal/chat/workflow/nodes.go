package workflow

import (
	"context"
	"log"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// enrichNode runs rewrite + classify in one step to keep the graph serial.
// Produces *enriched with Query/Intent populated; RAGDocs/Messages left for later nodes.
func enrichNode(deps *Deps) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, req *Request) (*enriched, error) {
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
	})
}

// retrieveNode does RAG retrieval only when intent is KNOWLEDGE.
func retrieveNode(deps *Deps) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, e *enriched) (*enriched, error) {
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
	})
}

// buildPromptNode assembles system + history + rag context + user query into
// a []*schema.Message ready for the ReAct agent.
func buildPromptNode(deps *Deps) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, e *enriched) ([]*schema.Message, error) {
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
	})
}

// wrapResponse converts the ReAct agent's *schema.Message output into *Response.
func wrapResponseNode() *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (*Response, error) {
		return &Response{Message: msg}, nil
	})
}
