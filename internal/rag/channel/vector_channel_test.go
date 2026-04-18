package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

type fakeVecSrc struct {
	docs []*schema.Document
	err  error
}

func (f *fakeVecSrc) Retrieve(ctx context.Context, query string, _ ...retriever.Option) ([]*schema.Document, error) {
	return f.docs, f.err
}

func docWithScore(id, content string, score float64) *schema.Document {
	d := &schema.Document{ID: id, Content: content}
	d.WithScore(score)
	return d
}

func TestVectorChannelRankAndFilter(t *testing.T) {
	src := &fakeVecSrc{docs: []*schema.Document{
		docWithScore("a", "x", 0.9),
		docWithScore("b", "y", 0.5),
		docWithScore("c", "z", 0.7),
	}}
	ch := NewVectorChannel(src, 0.6)
	got, err := ch.Retrieve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Doc.ID != "a" || got[0].RankInChannel != 1 || got[0].ChannelName != "vector" {
		t.Errorf("bad first: %+v", got[0])
	}
	if got[1].Doc.ID != "c" || got[1].RankInChannel != 2 {
		t.Errorf("bad second: %+v", got[1])
	}
}

func TestVectorChannelError(t *testing.T) {
	src := &fakeVecSrc{err: errors.New("boom")}
	ch := NewVectorChannel(src, 0)
	if _, err := ch.Retrieve(context.Background(), "q"); err == nil {
		t.Error("expected error")
	}
}

func TestVectorChannelName(t *testing.T) {
	if NewVectorChannel(&fakeVecSrc{}, 0).Name() != "vector" {
		t.Error("name mismatch")
	}
}
