//go:build integration

package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

func newIntegrationCompressibleMemory(t *testing.T, compressor Compressor, micro *MicroCompactor) *CompressibleMemory {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       15,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("integration Redis unavailable at %s: %v", addr, err)
	}

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

	sessionID := time.Now().UnixNano()
	mem := NewCompressibleMemory(sessionID, rdb, cfg, compressor, micro)
	mem.Clear(ctx)
	t.Cleanup(func() {
		mem.Clear(context.Background())
		_ = rdb.Close()
	})

	return mem
}

func TestCompressibleMemoryIntegrationTriggersCompression(t *testing.T) {
	llm := &fakeLLM{reply: "摘要：用户自我介绍叫小明"}
	compressor := NewLLMCompressor(llm, NewTokenCountCompressor(3, 2000), 6, 9, "")
	mem := newIntegrationCompressibleMemory(t, compressor, nil)
	ctx := context.Background()

	mem.Add(ctx, schema.UserMessage("我叫小明"))
	mem.Add(ctx, schema.AssistantMessage("你好小明", nil))
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
		t.Fatalf("first msg role = %v, want System summary", got[0].Role)
	}
	if !strings.Contains(got[0].Content, "小明") {
		t.Fatalf("summary lost key fact: %q", got[0].Content)
	}
}
