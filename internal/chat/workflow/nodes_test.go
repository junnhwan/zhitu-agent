package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBuildPromptFnWithRAGMatchesLegacyTemplate(t *testing.T) {
	deps := &Deps{SystemPrompt: "你是助手"}
	doc1 := (&schema.Document{
		ID:       "a_0",
		Content:  "RAG 核心流程：召回 → rerank → 拼 prompt → 生成",
		MetaData: map[string]any{"file_name": "rag.md"},
	}).WithScore(0.82)
	doc2 := (&schema.Document{
		ID:      "b_1",
		Content: "Eino Graph 把节点用 AddLambdaNode 串起来",
	}).WithScore(0.61)
	e := &enriched{
		Request: &Request{Prompt: "什么是 RAG", History: nil},
		Query:   "什么是 RAG",
		RAGDocs: []*schema.Document{doc1, doc2},
	}
	msgs, err := buildPromptFn(deps)(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	// 期望 4 条：system + RAG user + 假 assistant ack + 真 user prompt
	if len(msgs) != 4 {
		t.Fatalf("msgs len=%d, want 4:\n%+v", len(msgs), msgs)
	}
	if msgs[0].Role != schema.System {
		t.Errorf("msgs[0] role=%v, want system", msgs[0].Role)
	}
	if msgs[1].Role != schema.User {
		t.Errorf("msgs[1] role=%v, want user (RAG context)", msgs[1].Role)
	}
	rag := msgs[1].Content
	if !strings.Contains(rag, "参考知识：") {
		t.Errorf("RAG msg missing heading:\n%s", rag)
	}
	if !strings.Contains(rag, "【来源：rag.md | 相似度：0.82】") {
		t.Errorf("RAG msg missing rag.md citation:\n%s", rag)
	}
	if !strings.Contains(rag, "【来源：未知文件 | 相似度：0.61】") {
		t.Errorf("RAG msg missing fallback filename for doc without file_name:\n%s", rag)
	}
	if !strings.Contains(rag, "\n\n---\n\n") {
		t.Errorf("RAG msg missing separator between docs:\n%s", rag)
	}
	if msgs[2].Role != schema.Assistant || !strings.Contains(msgs[2].Content, "我已了解") {
		t.Errorf("msgs[2] not the fake ack: role=%v content=%q", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != schema.User || msgs[3].Content != "什么是 RAG" {
		t.Errorf("msgs[3] should be the real user prompt, got role=%v content=%q", msgs[3].Role, msgs[3].Content)
	}
}

func TestBuildPromptFnNoRAGSkipsAckMessage(t *testing.T) {
	deps := &Deps{SystemPrompt: "你是助手"}
	e := &enriched{
		Request: &Request{Prompt: "你好", History: nil},
		Query:   "你好",
	}
	msgs, err := buildPromptFn(deps)(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	// 无 RAG 时：system + user prompt（没有 ack）
	if len(msgs) != 2 {
		t.Fatalf("msgs len=%d, want 2 (system + user)", len(msgs))
	}
	if msgs[0].Role != schema.System {
		t.Errorf("msgs[0] role=%v, want system", msgs[0].Role)
	}
	if msgs[1].Role != schema.User || msgs[1].Content != "你好" {
		t.Errorf("msgs[1] role=%v content=%q, want user '你好'", msgs[1].Role, msgs[1].Content)
	}
}

func TestBuildPromptFnHistoryBeforeRAG(t *testing.T) {
	deps := &Deps{SystemPrompt: "sys"}
	hist := []*schema.Message{
		schema.UserMessage("旧问"),
		schema.AssistantMessage("旧答", nil),
	}
	e := &enriched{
		Request: &Request{Prompt: "新问", History: hist},
		Query:   "新问",
		RAGDocs: []*schema.Document{
			(&schema.Document{Content: "k", MetaData: map[string]any{"file_name": "f.md"}}).WithScore(0.7),
		},
	}
	msgs, _ := buildPromptFn(deps)(context.Background(), e)
	// system + 2 history + RAG user + ack + real user prompt
	if len(msgs) != 6 {
		t.Fatalf("msgs len=%d, want 6", len(msgs))
	}
	if msgs[1].Content != "旧问" || msgs[2].Content != "旧答" {
		t.Errorf("history not preserved between system and RAG: %+v %+v", msgs[1], msgs[2])
	}
}

func TestFormatRAGContextMissingFilename(t *testing.T) {
	docs := []*schema.Document{
		(&schema.Document{Content: "X"}).WithScore(0.5),
	}
	out := formatRAGContext(docs)
	if !strings.Contains(out, "【来源：未知文件 | 相似度：0.50】") {
		t.Errorf("missing filename fallback:\n%s", out)
	}
}
