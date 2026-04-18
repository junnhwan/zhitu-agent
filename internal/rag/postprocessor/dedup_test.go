package postprocessor

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

func cand(id, ch string, rank int) *channel.Candidate {
	return &channel.Candidate{
		Doc:           &schema.Document{ID: id},
		ChannelName:   ch,
		RankInChannel: rank,
	}
}

func TestDedupMergesRanks(t *testing.T) {
	in := []*channel.Candidate{
		cand("a", "vector", 1),
		cand("b", "vector", 2),
		cand("c", "vector", 3),
		cand("b", "bm25", 1),
		cand("a", "bm25", 5),
		cand("d", "bm25", 2),
	}
	out := NewDedup().Process(context.Background(), in, "")
	if len(out) != 4 {
		t.Fatalf("want 4, got %d", len(out))
	}
	if out[0].Doc.ID != "a" || out[0].RankByChannel["vector"] != 1 || out[0].RankByChannel["bm25"] != 5 {
		t.Errorf("bad a: %+v", out[0].RankByChannel)
	}
	if out[1].Doc.ID != "b" || out[1].RankByChannel["vector"] != 2 || out[1].RankByChannel["bm25"] != 1 {
		t.Errorf("bad b: %+v", out[1].RankByChannel)
	}
	if out[3].Doc.ID != "d" || len(out[3].RankByChannel) != 1 {
		t.Errorf("bad d: %+v", out[3].RankByChannel)
	}
}
