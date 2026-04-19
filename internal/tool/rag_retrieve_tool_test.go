package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag"
)

type stubRetriever struct {
	docs []*schema.Document
	err  error
}

func (s *stubRetriever) Retrieve(ctx context.Context, q string) ([]*schema.Document, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.docs, nil
}

func TestRetrieveKnowledgeToolInfo(t *testing.T) {
	rk, err := NewRetrieveKnowledgeTool(&rag.RAG{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	info, err := rk.Info(context.Background())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Name != "retrieveKnowledge" {
		t.Errorf("name=%q", info.Name)
	}
	if info.ParamsOneOf == nil {
		t.Fatal("ParamsOneOf nil")
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("toJSONSchema: %v", err)
	}
	if _, ok := js.Properties.Get("query"); !ok {
		t.Error("schema missing query property")
	}
}

func TestRetrieveKnowledgeToolInvoke(t *testing.T) {
	stub := &stubRetriever{
		docs: []*schema.Document{
			{ID: "d1", Content: "alpha", MetaData: map[string]any{"file_name": "a.md"}},
			{ID: "d2", Content: "beta"},
		},
	}
	r := &rag.RAG{Retriever: stub}
	rk, err := NewRetrieveKnowledgeTool(r)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	out, err := rk.InvokableRun(context.Background(), `{"query":"hello"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "a.md") || !strings.Contains(out, "alpha") {
		t.Errorf("output missing expected fields: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("expected doc separator: %q", out)
	}
	if !strings.Contains(out, "[2] d2") {
		t.Errorf("expected ID fallback title: %q", out)
	}
}

func TestRetrieveKnowledgeToolEmptyQuery(t *testing.T) {
	rk, _ := NewRetrieveKnowledgeTool(&rag.RAG{Retriever: &stubRetriever{}})
	_, err := rk.InvokableRun(context.Background(), `{"query":"   "}`)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestRetrieveKnowledgeToolPropagatesError(t *testing.T) {
	r := &rag.RAG{Retriever: &stubRetriever{err: errors.New("boom")}}
	rk, _ := NewRetrieveKnowledgeTool(r)
	_, err := rk.InvokableRun(context.Background(), `{"query":"x"}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetrieveKnowledgeToolNoDocs(t *testing.T) {
	r := &rag.RAG{Retriever: &stubRetriever{docs: nil}}
	rk, _ := NewRetrieveKnowledgeTool(r)
	out, err := rk.InvokableRun(context.Background(), `{"query":"x"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "未检索到") {
		t.Errorf("expected empty-result hint: %q", out)
	}
}
