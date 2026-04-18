package postprocessor

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

func mkFileDoc(id string, fileName string) *channel.Candidate {
	return &channel.Candidate{Doc: &schema.Document{
		ID:       id,
		MetaData: map[string]any{"file_name": fileName},
	}}
}

func TestDiversityCap(t *testing.T) {
	in := []*channel.Candidate{
		mkFileDoc("a_0", "a.md"),
		mkFileDoc("a_1", "a.md"),
		mkFileDoc("a_2", "a.md"),
		mkFileDoc("b_0", "b.md"),
		mkFileDoc("a_3", "a.md"),
		mkFileDoc("b_1", "b.md"),
		mkFileDoc("b_2", "b.md"),
	}
	out := NewDiversity(2).Process(context.Background(), in, "")
	if len(out) != 4 {
		t.Fatalf("want 4, got %d", len(out))
	}
	want := []string{"a_0", "a_1", "b_0", "b_1"}
	for i, c := range out {
		if c.Doc.ID != want[i] {
			t.Errorf("[%d] got %s want %s", i, c.Doc.ID, want[i])
		}
	}
}

func TestDiversityFallbackFileKey(t *testing.T) {
	in := []*channel.Candidate{
		{Doc: &schema.Document{ID: "docs/a.md_0"}},
		{Doc: &schema.Document{ID: "docs/a.md_1"}},
		{Doc: &schema.Document{ID: "docs/a.md_2"}},
	}
	out := NewDiversity(2).Process(context.Background(), in, "")
	if len(out) != 2 {
		t.Errorf("want 2 via ID prefix key, got %d", len(out))
	}
}
