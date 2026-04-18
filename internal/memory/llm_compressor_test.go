package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeLLM struct {
	reply string
	err   error
	seen  []*schema.Message
}

func (f *fakeLLM) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.seen = input
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.reply, nil), nil
}

func (f *fakeLLM) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not used")
}

func makeMsgs(n int) []*schema.Message {
	out := make([]*schema.Message, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			out[i] = schema.UserMessage("user turn " + string(rune('0'+i%10)))
		} else {
			out[i] = schema.AssistantMessage("assistant turn "+string(rune('0'+i%10)), nil)
		}
	}
	return out
}

func TestLLMCompressorBelowThreshold(t *testing.T) {
	c := NewLLMCompressor(&fakeLLM{reply: "summary"}, NewTokenCountCompressor(3, 2000), 6, 9, "")
	msgs := makeMsgs(8)
	out := c.Compress(context.Background(), msgs)
	if len(out) != 8 {
		t.Errorf("below threshold should pass through, got %d", len(out))
	}
}

func TestLLMCompressorHappyPath(t *testing.T) {
	llm := &fakeLLM{reply: "用户自我介绍叫小明，在做 Go 项目"}
	c := NewLLMCompressor(llm, NewTokenCountCompressor(3, 2000), 6, 9, "")
	msgs := makeMsgs(10)
	out := c.Compress(context.Background(), msgs)
	if len(out) != 7 {
		t.Fatalf("want 1 summary + 6 recent = 7, got %d", len(out))
	}
	if out[0].Role != schema.System {
		t.Errorf("first role = %v, want System", out[0].Role)
	}
	if !strings.Contains(out[0].Content, "小明") {
		t.Errorf("summary missing fact: %q", out[0].Content)
	}
	if llm.seen == nil {
		t.Error("LLM was not called")
	}
}

func TestLLMCompressorFallbackOnError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("boom")}
	c := NewLLMCompressor(llm, NewTokenCountCompressor(3, 2000), 6, 9, "")
	msgs := makeMsgs(10)
	out := c.Compress(context.Background(), msgs)
	if len(out) == 0 {
		t.Fatal("fallback produced empty output")
	}
	if out[0].Role != schema.System {
		t.Errorf("fallback first role = %v, want System summary", out[0].Role)
	}
}

func TestNewCompressorLLMSummaryWired(t *testing.T) {
	llm := &fakeLLM{reply: "ok"}
	c, err := NewCompressor(Config{
		Strategy:         "llm_summary",
		RecentRounds:     3,
		RecentTokenLimit: 2000,
		LLM:              llm,
	})
	if err != nil {
		t.Fatalf("llm_summary with injected LLM should work: %v", err)
	}
	if _, ok := c.(*LLMCompressor); !ok {
		t.Errorf("want *LLMCompressor, got %T", c)
	}
}
