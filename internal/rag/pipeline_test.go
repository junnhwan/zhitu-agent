package rag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/postprocessor"
)

type stubChannel struct {
	name  string
	cands []*channel.Candidate
	err   error
	delay time.Duration
}

func (s *stubChannel) Name() string { return s.name }
func (s *stubChannel) Retrieve(ctx context.Context, _ string) ([]*channel.Candidate, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.cands, s.err
}

type stubLegacy struct {
	docs []*schema.Document
	err  error
}

func (s *stubLegacy) Retrieve(_ context.Context, _ string) ([]*schema.Document, error) {
	return s.docs, s.err
}

func mkCandWithID(id, ch string, rank int, content string) *channel.Candidate {
	return &channel.Candidate{
		Doc:           &schema.Document{ID: id, Content: content},
		ChannelName:   ch,
		RankInChannel: rank,
	}
}

func TestPipelineTwoChannelsMerge(t *testing.T) {
	v := &stubChannel{name: "vector", cands: []*channel.Candidate{mkCandWithID("a", "vector", 1, "x"), mkCandWithID("b", "vector", 2, "y")}}
	b := &stubChannel{name: "bm25", cands: []*channel.Candidate{mkCandWithID("a", "bm25", 1, "x"), mkCandWithID("c", "bm25", 2, "z")}}
	p := NewPipeline(nil, []channel.Channel{v, b},
		[]postprocessor.Processor{postprocessor.NewDedup(), postprocessor.NewRRF(60, 1.3)},
		time.Second, nil, 5, PipelineHooks{})
	out, err := p.Retrieve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0].ID != "a" {
		t.Errorf("want 3 with a first, got len=%d first=%s", len(out), func() string {
			if len(out) > 0 {
				return out[0].ID
			}
			return ""
		}())
	}
}

func TestPipelineChannelTimeout(t *testing.T) {
	slow := &stubChannel{name: "slow", delay: 200 * time.Millisecond, cands: []*channel.Candidate{mkCandWithID("x", "slow", 1, "x")}}
	fast := &stubChannel{name: "fast", cands: []*channel.Candidate{mkCandWithID("y", "fast", 1, "y")}}
	p := NewPipeline(nil, []channel.Channel{slow, fast},
		[]postprocessor.Processor{postprocessor.NewDedup()},
		50*time.Millisecond, nil, 5, PipelineHooks{})
	out, _ := p.Retrieve(context.Background(), "q")
	if len(out) != 1 || out[0].ID != "y" {
		t.Errorf("expected only fast=y, got %+v", out)
	}
}

func TestPipelineChannelError(t *testing.T) {
	bad := &stubChannel{name: "bad", err: errors.New("boom")}
	ok := &stubChannel{name: "ok", cands: []*channel.Candidate{mkCandWithID("y", "ok", 1, "y")}}
	failed := ""
	p := NewPipeline(nil, []channel.Channel{bad, ok},
		[]postprocessor.Processor{postprocessor.NewDedup()},
		time.Second, nil, 5, PipelineHooks{OnChannelFailed: func(n string) { failed = n }})
	out, _ := p.Retrieve(context.Background(), "q")
	if failed != "bad" {
		t.Errorf("failed hook = %q", failed)
	}
	if len(out) != 1 || out[0].ID != "y" {
		t.Errorf("bad out: %+v", out)
	}
}

func TestPipelineFallbackLegacy(t *testing.T) {
	empty := &stubChannel{name: "empty"}
	legacy := &stubLegacy{docs: []*schema.Document{{ID: "legacy1"}}}
	zeroHit := ""
	p := NewPipeline(nil, []channel.Channel{empty}, nil, time.Second, legacy, 5,
		PipelineHooks{OnZeroHit: func(fb string) { zeroHit = fb }})
	out, _ := p.Retrieve(context.Background(), "q")
	if zeroHit != "legacy" {
		t.Errorf("zeroHit = %q", zeroHit)
	}
	if len(out) != 1 || out[0].ID != "legacy1" {
		t.Errorf("bad fallback: %+v", out)
	}
}

func TestPipelineAllEmpty(t *testing.T) {
	empty := &stubChannel{name: "empty"}
	legacy := &stubLegacy{}
	zeroHit := ""
	p := NewPipeline(nil, []channel.Channel{empty}, nil, time.Second, legacy, 5,
		PipelineHooks{OnZeroHit: func(fb string) { zeroHit = fb }})
	out, _ := p.Retrieve(context.Background(), "q")
	if len(out) != 0 {
		t.Errorf("want empty, got %+v", out)
	}
	if zeroHit != "legacy" {
		t.Errorf("zeroHit = %q", zeroHit)
	}
}
