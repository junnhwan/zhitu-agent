package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/zhitu-agent/zhitu-agent/internal/rag"
)

// KnowledgeAgent is responsible for knowledge retrieval.
// Mirrors Java KnowledgeAgent — calls RagTool.retrieve(input).
type KnowledgeAgent struct {
	rag *rag.RAG
}

// NewKnowledgeAgent creates a KnowledgeAgent with the given RAG system.
func NewKnowledgeAgent(r *rag.RAG) *KnowledgeAgent {
	return &KnowledgeAgent{rag: r}
}

// Execute performs knowledge retrieval and returns formatted results.
// Mirrors Java KnowledgeAgent.execute(sessionId, input) — calls ragTool.retrieve(input).
func (a *KnowledgeAgent) Execute(ctx context.Context, sessionID int64, input string) string {
	log.Printf("[KnowledgeAgent] executing knowledge retrieval, sessionID: %d", sessionID)

	if a.rag == nil || a.rag.Retriever == nil {
		log.Printf("[KnowledgeAgent] RAG not available, returning empty result")
		return ""
	}

	docs, err := a.rag.Retriever.Retrieve(ctx, input)
	if err != nil {
		log.Printf("[KnowledgeAgent] retrieval failed: %v", err)
		return ""
	}

	if len(docs) == 0 {
		log.Printf("[KnowledgeAgent] no results found")
		return ""
	}

	var sb strings.Builder
	for i, doc := range docs {
		fileName := "未知文件"
		if v, ok := doc.MetaData["file_name"]; ok {
			if fn, ok := v.(string); ok && fn != "" {
				fileName = fn
			}
		}
		sb.WriteString(fmt.Sprintf("【来源：%s | 相似度：%.2f】\n%s", fileName, doc.Score(), doc.Content))
		if i < len(docs)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}

	result := sb.String()
	logPreview := result
	if len(logPreview) > 100 {
		logPreview = logPreview[:100] + "..."
	}
	log.Printf("[KnowledgeAgent] retrieval result: %s", logPreview)
	return result
}

// AgentName returns the agent name.
func (a *KnowledgeAgent) AgentName() string {
	return "KnowledgeAgent"
}
