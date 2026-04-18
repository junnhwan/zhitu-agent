package workflow

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag"
	"github.com/zhitu-agent/zhitu-agent/internal/understand"
)

type Request struct {
	SessionID int64
	Prompt    string
	History   []*schema.Message
}

type Response struct {
	Message *schema.Message
}

type Deps struct {
	ChatModel      model.ToolCallingChatModel
	Tools          []tool.BaseTool
	IntentRouter   *understand.Service
	RAG            *rag.RAG
	SystemPrompt   string
	MaxReActSteps  int
}

type enriched struct {
	Request  *Request
	Query    string
	Intent   *understand.IntentResult
	RAGDocs  []*schema.Document
	Messages []*schema.Message
}
