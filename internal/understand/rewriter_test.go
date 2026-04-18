package understand

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeLLM struct {
	reply string
	err   error
	calls int
}

func (f *fakeLLM) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.reply, nil), nil
}

func (f *fakeLLM) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func TestRewriterNoHistory(t *testing.T) {
	llm := &fakeLLM{reply: "should not be called"}
	r := NewRewriter(llm)
	got, err := r.Rewrite(context.Background(), nil, "什么是 RAG")
	if err != nil {
		t.Fatal(err)
	}
	if got != "什么是 RAG" {
		t.Errorf("expected original query, got %q", got)
	}
	if llm.calls != 0 {
		t.Errorf("LLM should not be called when history is empty")
	}
}

func TestRewriterWithHistory(t *testing.T) {
	llm := &fakeLLM{reply: "RAG 有哪些变种"}
	r := NewRewriter(llm)
	history := []*schema.Message{
		schema.UserMessage("什么是 RAG"),
		schema.AssistantMessage("RAG 是检索增强生成", nil),
	}
	got, err := r.Rewrite(context.Background(), history, "它有哪些变种")
	if err != nil {
		t.Fatal(err)
	}
	if got != "RAG 有哪些变种" {
		t.Errorf("expected rewritten query, got %q", got)
	}
}

func TestRewriterLLMErrorFallback(t *testing.T) {
	llm := &fakeLLM{err: errors.New("boom")}
	r := NewRewriter(llm)
	history := []*schema.Message{schema.UserMessage("x"), schema.AssistantMessage("y", nil)}
	got, err := r.Rewrite(context.Background(), history, "原始 query")
	if err != nil {
		t.Errorf("rewriter must not return error on LLM failure, got %v", err)
	}
	if got != "原始 query" {
		t.Errorf("expected fallback to original, got %q", got)
	}
}

func TestRewriterTrimsHistory(t *testing.T) {
	llm := &fakeLLM{reply: "rewritten"}
	r := NewRewriter(llm)
	history := make([]*schema.Message, 20)
	for i := range history {
		history[i] = schema.UserMessage("h")
	}
	_, err := r.Rewrite(context.Background(), history, "q")
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
}
