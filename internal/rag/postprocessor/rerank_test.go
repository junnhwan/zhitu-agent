package postprocessor

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

type fakeReranker struct {
	idxs []int
}

func (f *fakeReranker) Rerank(_ string, _ []string, _ int) []int { return f.idxs }

func mkCandWithContent(id, content string) *channel.Candidate {
	return &channel.Candidate{Doc: &schema.Document{ID: id, Content: content}}
}

func TestRerankHappyPath(t *testing.T) {
	in := []*channel.Candidate{
		mkCandWithContent("a", "aa"),
		mkCandWithContent("b", "bb"),
		mkCandWithContent("c", "cc"),
	}
	r := NewRerank(&fakeReranker{idxs: []int{2, 0}}, 5, nil)
	out := r.Process(context.Background(), in, "q")
	if len(out) != 2 || out[0].Doc.ID != "c" || out[1].Doc.ID != "a" {
		t.Errorf("bad: %+v", out)
	}
}

func TestRerankFallbackOnEmpty(t *testing.T) {
	in := []*channel.Candidate{
		mkCandWithContent("a", "aa"),
		mkCandWithContent("b", "bb"),
	}
	called := false
	r := NewRerank(&fakeReranker{idxs: nil}, 5, func() { called = true })
	out := r.Process(context.Background(), in, "q")
	if !called {
		t.Error("fallback not called")
	}
	if len(out) != 2 {
		t.Errorf("want 2, got %d", len(out))
	}
}
