//go:build smoke

package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/schema"
)

// End-to-end smoke: ChatWorkflow.Invoke through real Qwen, no memory, no tools.
// Run: DASHSCOPE_API_KEY=xxx go test -tags=smoke ./internal/chat/workflow/ -v
func TestWorkflowSmokeInvoke(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cm, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  apiKey,
		Model:   "qwen-turbo",
	})
	if err != nil {
		t.Fatal(err)
	}

	wf, err := NewChatWorkflow(ctx, &Deps{
		ChatModel:    cm,
		SystemPrompt: "你是一个简洁的中文助手，回复不超过20字。",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := wf.Invoke(ctx, &Request{
		SessionID: 1,
		Prompt:    "用一句话说明什么是 RAG",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Message == nil || resp.Message.Content == "" {
		t.Fatalf("empty response: %+v", resp)
	}
	t.Logf("[smoke.Invoke] %s", resp.Message.Content)
}

func TestWorkflowSmokeStream(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cm, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  apiKey,
		Model:   "qwen-turbo",
	})
	if err != nil {
		t.Fatal(err)
	}

	wf, err := NewChatWorkflow(ctx, &Deps{
		ChatModel:    cm,
		SystemPrompt: "你是一个简洁的中文助手，回复不超过20字。",
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := wf.Stream(ctx, &Request{
		SessionID: 1,
		Prompt:    "你好",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	chunks := 0
	for {
		chunk, err := stream.Recv()
		if err != nil {
			break
		}
		sb.WriteString(chunk.Content)
		chunks++
	}
	if sb.Len() == 0 {
		t.Fatalf("no content streamed")
	}
	t.Logf("[smoke.Stream] %d chunks: %s", chunks, sb.String())

	_ = schema.Message{} // keep import
}
