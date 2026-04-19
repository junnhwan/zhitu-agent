package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag"
)

// RetrieveInput is the input for retrieveKnowledge.
type RetrieveInput struct {
	Query string `json:"query" jsonschema:"description=检索查询，例如问题或关键词"`
	TopK  int    `json:"topK,omitempty" jsonschema:"description=返回条数（v1 忽略，由 rag.retrieve_top_k 决定）"`
}

// NewRetrieveKnowledgeTool wraps rag.RAG.Retriever.Retrieve as an Eino InvokableTool
// so that MCP clients (or future chat flows) can pull RAG documents on demand.
func NewRetrieveKnowledgeTool(r *rag.RAG) (tool.InvokableTool, error) {
	return utils.InferTool[RetrieveInput, string](
		"retrieveKnowledge",
		"从智途知识库中检索与查询相关的文档片段。当需要引用项目文档、FAQ 或历史问答时调用此工具，返回按相关性排序的文档片段。",
		func(ctx context.Context, in RetrieveInput) (string, error) {
			if r == nil || r.Retriever == nil {
				return "", fmt.Errorf("rag retriever not initialized")
			}
			q := strings.TrimSpace(in.Query)
			if q == "" {
				return "", fmt.Errorf("query is required")
			}
			docs, err := r.Retriever.Retrieve(ctx, q)
			if err != nil {
				return "", fmt.Errorf("retrieve failed: %w", err)
			}
			return formatRetrievedDocs(docs), nil
		},
	)
}

func formatRetrievedDocs(docs []*schema.Document) string {
	if len(docs) == 0 {
		return "未检索到相关文档。"
	}
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		title := ""
		if d.MetaData != nil {
			if v, ok := d.MetaData["file_name"].(string); ok {
				title = v
			}
		}
		if title == "" {
			title = d.ID
		}
		fmt.Fprintf(&b, "[%d] %s\n%s", i+1, title, d.Content)
	}
	return b.String()
}
