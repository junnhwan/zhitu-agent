package postprocessor

import (
	"context"
	"math"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

func mkCand(id string, ranks map[string]int) *channel.Candidate {
	return &channel.Candidate{Doc: &schema.Document{ID: id}, RankByChannel: ranks}
}

func TestRRFSingleChannel(t *testing.T) {
	in := []*channel.Candidate{mkCand("a", map[string]int{"vector": 1})}
	out := NewRRF(60, 1.3).Process(context.Background(), in, "")
	want := 1.0 / 61.0
	if math.Abs(out[0].RawScore-want) > 1e-9 {
		t.Errorf("score=%v want %v", out[0].RawScore, want)
	}
}

func TestRRFCrossChannelBonus(t *testing.T) {
	in := []*channel.Candidate{
		mkCand("a", map[string]int{"vector": 1, "bm25": 1}),
		mkCand("b", map[string]int{"vector": 2}),
	}
	out := NewRRF(60, 1.3).Process(context.Background(), in, "")
	if out[0].Doc.ID != "a" {
		t.Errorf("a should rank first, got %s", out[0].Doc.ID)
	}
	wantA := (2.0 / 61.0) * 1.3
	if math.Abs(out[0].RawScore-wantA) > 1e-9 {
		t.Errorf("a score=%v want %v", out[0].RawScore, wantA)
	}
}

func TestRRFSortedDesc(t *testing.T) {
	in := []*channel.Candidate{
		mkCand("c", map[string]int{"vector": 10}),
		mkCand("a", map[string]int{"vector": 1}),
		mkCand("b", map[string]int{"vector": 5}),
	}
	out := NewRRF(60, 1.0).Process(context.Background(), in, "")
	ids := []string{out[0].Doc.ID, out[1].Doc.ID, out[2].Doc.ID}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("order = %v", ids)
	}
}
