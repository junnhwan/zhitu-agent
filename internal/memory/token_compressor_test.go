package memory

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestTokenCountCompressorCompress(t *testing.T) {
	c := NewTokenCountCompressor(2, 2000) // keep 2 recent rounds

	// Create 10 messages (5 rounds)
	var msgs []*schema.Message
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			msgs = append(msgs, schema.UserMessage("user message "+string(rune('0'+i))))
		} else {
			msgs = append(msgs, schema.AssistantMessage("assistant message "+string(rune('0'+i)), nil))
		}
	}

	compressed := c.Compress(context.Background(), msgs)

	// Should have: 1 summary + 4 recent (2 rounds)
	if len(compressed) != 5 {
		t.Errorf("len(compressed) = %d, want 5", len(compressed))
	}

	// First message should be system summary
	if compressed[0].Role != schema.System {
		t.Errorf("first message role = %v, want System", compressed[0].Role)
	}
}

func TestTokenCountCompressorNoCompress(t *testing.T) {
	c := NewTokenCountCompressor(5, 2000)

	// Fewer messages than threshold — should not compress
	msgs := []*schema.Message{
		schema.UserMessage("hello"),
		schema.AssistantMessage("hi", nil),
	}

	compressed := c.Compress(context.Background(), msgs)
	if len(compressed) != 2 {
		t.Errorf("len(compressed) = %d, want 2 (no compression)", len(compressed))
	}
}

func TestEstimateTokens(t *testing.T) {
	c := NewTokenCountCompressor(5, 2000)
	msgs := []*schema.Message{
		schema.UserMessage("hello"),  // 5 chars / 4 = 1
		schema.AssistantMessage("world", nil), // 5 chars / 4 = 1
	}

	tokens := c.EstimateTokens(msgs)
	if tokens != 2 { // 5/4 + 5/4 = 1 + 1 = 2
		t.Errorf("EstimateTokens = %d, want 2", tokens)
	}
}

func TestGenerateSummary(t *testing.T) {
	c := NewTokenCountCompressor(2, 2000)
	msgs := []*schema.Message{
		schema.UserMessage("What is Go?"),
		schema.AssistantMessage("Go is a programming language", nil),
		schema.UserMessage("How to install?"),
		schema.AssistantMessage("Download from golang.org", nil),
	}

	summary := c.generateSummary(msgs)
	if summary == "" {
		t.Error("generateSummary returned empty string")
	}
}
