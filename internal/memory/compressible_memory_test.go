package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

func newTestCompressibleMemory(t *testing.T, compressor Compressor, micro *MicroCompactor) (*CompressibleMemory, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.ChatMemoryConfig{
		MaxMessages: 20,
		Compression: config.ChatMemoryCompressionConfig{
			TokenThreshold:       6000,
			RecentRounds:         3,
			RecentTokenLimit:     2000,
			FallbackRecentRounds: 6,
		},
		Redis: config.ChatMemoryRedisConfig{
			TTLSeconds: 3600,
			Lock: config.ChatMemoryLockConfig{
				ExpireSeconds:   5,
				RetryTimes:      3,
				RetryIntervalMs: 10,
			},
		},
	}
	return NewCompressibleMemory(1, rdb, cfg, compressor, micro), mr
}

func TestCompressibleMemoryAddRoundTrip(t *testing.T) {
	mem, _ := newTestCompressibleMemory(t, NewTokenCountCompressor(3, 2000), nil)
	ctx := context.Background()

	mem.Add(ctx, schema.UserMessage("你好"))
	mem.Add(ctx, schema.AssistantMessage("你好，有什么可以帮你？", nil))

	got := mem.GetMessages(ctx)
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0].Content != "你好" || got[1].Content != "你好，有什么可以帮你？" {
		t.Errorf("unexpected content: %+v", got)
	}
}

func TestCompressibleMemoryTriggersCompression(t *testing.T) {
	llm := &fakeLLM{reply: "摘要：用户自我介绍叫小明"}
	compressor := NewLLMCompressor(llm, NewTokenCountCompressor(3, 2000), 6, 9, "")
	mem, _ := newTestCompressibleMemory(t, compressor, nil)
	ctx := context.Background()

	mem.Add(ctx, schema.UserMessage("我叫小明"))
	mem.Add(ctx, schema.AssistantMessage("你好小明", nil))
	// Push the history just past maxMessages so we observe the immediate
	// post-compression shape instead of re-growing the buffer afterward.
	for i := 0; i < mem.maxMessages-1; i++ {
		if i%2 == 0 {
			mem.Add(ctx, schema.UserMessage(fmt.Sprintf("question %d", i)))
		} else {
			mem.Add(ctx, schema.AssistantMessage(fmt.Sprintf("answer %d", i), nil))
		}
	}

	got := mem.GetMessages(ctx)
	if len(got) != 7 {
		t.Fatalf("want 1 summary + 6 recent = 7 after compression, got %d", len(got))
	}
	if got[0].Role != schema.System {
		t.Errorf("first msg role = %v, want System summary", got[0].Role)
	}
	if !strings.Contains(got[0].Content, "小明") {
		t.Errorf("summary lost key fact: %q", got[0].Content)
	}
}

func TestCompressibleMemoryMicroCompactsToolMessages(t *testing.T) {
	micro := &MicroCompactor{Threshold: 20}
	mem, _ := newTestCompressibleMemory(t, NewTokenCountCompressor(3, 2000), micro)
	ctx := context.Background()

	huge := strings.Repeat("【来源：a.md | 相似度：0.9】\n内容\n\n---\n\n", 10)
	toolMsg := schema.ToolMessage(huge, "call-1", schema.WithToolName("rag_search"))
	mem.Add(ctx, toolMsg)

	got := mem.GetMessages(ctx)
	if len(got) != 1 {
		t.Fatalf("want 1 message, got %d", len(got))
	}
	if n := strings.Count(got[0].Content, "【来源："); n > 3 {
		t.Errorf("tool message was not micro-compacted: %d sources remain", n)
	}
	if got[0].ToolName != "rag_search" || got[0].ToolCallID != "call-1" {
		t.Errorf("tool metadata lost: name=%q id=%q", got[0].ToolName, got[0].ToolCallID)
	}
}
