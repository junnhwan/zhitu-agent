package understand

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestServiceHappyPath(t *testing.T) {
	rewriteLLM := &fakeLLM{reply: "rewritten query"}
	classifyLLM := &fakeLLM{reply: `{"domain":"KNOWLEDGE","category":"RAG_QUERY","confidence":0.9}`}
	tree := loadTestTree(t)

	svc := NewService(NewRewriter(rewriteLLM), NewClassifier(classifyLLM, tree), NewGuardian(0.6, 3), nil)

	out, err := svc.Understand(context.Background(), 1, []*schema.Message{schema.UserMessage("h")}, "原 query")
	if err != nil {
		t.Fatal(err)
	}
	if out.Route != "knowledge_agent" {
		t.Errorf("expected knowledge_agent route, got %s", out.Route)
	}
	if out.RewrittenQuery != "rewritten query" {
		t.Errorf("expected rewritten query, got %s", out.RewrittenQuery)
	}
	if out.Intent.Domain != "KNOWLEDGE" {
		t.Errorf("expected KNOWLEDGE, got %s", out.Intent.Domain)
	}
}

func TestServiceClarify(t *testing.T) {
	classifyLLM := &fakeLLM{reply: `{"domain":"KNOWLEDGE","category":"RAG_QUERY","confidence":0.2}`}
	svc := NewService(NewRewriter(&fakeLLM{}), NewClassifier(classifyLLM, loadTestTree(t)), NewGuardian(0.6, 3), nil)
	out, err := svc.Understand(context.Background(), 1, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !out.NeedsClarification {
		t.Errorf("expected NeedsClarification true")
	}
	if out.ClarifyQuestion == "" {
		t.Errorf("expected clarify question")
	}
}

func TestServiceFallbackOnClassifierError(t *testing.T) {
	classifyLLM := &fakeLLM{err: errors.New("classifier down")}
	svc := NewService(NewRewriter(&fakeLLM{}), NewClassifier(classifyLLM, loadTestTree(t)), NewGuardian(0.6, 3), nil)
	out, err := svc.Understand(context.Background(), 1, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out.Intent.Domain != "CHITCHAT" {
		t.Errorf("expected CHITCHAT fallback, got %s", out.Intent.Domain)
	}
}
