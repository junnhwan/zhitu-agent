package memory

import (
	"fmt"
	"log"
	"math"

	"github.com/cloudwego/eino/schema"
)

// TokenCountCompressor compresses chat memory by estimating token count
// and generating a simple summary from old messages.
// Mirrors Java TokenCountChatMemoryCompressor — NOT LLM-based.
type TokenCountCompressor struct {
	recentRounds     int
	recentTokenLimit int
}

// NewTokenCountCompressor creates a compressor with the given parameters.
// recentRounds: number of recent message pairs to preserve (default 5)
// recentTokenLimit: token limit for recent messages (default 2000)
func NewTokenCountCompressor(recentRounds, recentTokenLimit int) *TokenCountCompressor {
	return &TokenCountCompressor{
		recentRounds:     recentRounds,
		recentTokenLimit: recentTokenLimit,
	}
}

// Compress splits messages into old and recent, generates a summary from old messages,
// and returns [summary + recent messages].
// Mirrors Java TokenCountChatMemoryCompressor.compress().
func (c *TokenCountCompressor) Compress(messages []*schema.Message) []*schema.Message {
	if len(messages) <= c.recentRounds*2 {
		return messages
	}

	splitIndex := len(messages) - c.recentRounds*2
	oldMessages := messages[:splitIndex]
	recentMessages := messages[splitIndex:]

	recentTokens := c.EstimateTokens(recentMessages)
	if recentTokens > c.recentTokenLimit {
		log.Printf("[Compressor] recent messages token count %d exceeds limit %d", recentTokens, c.recentTokenLimit)
	}

	log.Printf("[Compressor] history: %d msgs, keeping recent: %d msgs", len(oldMessages), len(recentMessages))

	summary := c.generateSummary(oldMessages)

	compressed := make([]*schema.Message, 0, 1+len(recentMessages))
	compressed = append(compressed, schema.SystemMessage("历史对话摘要: "+summary))
	compressed = append(compressed, recentMessages...)

	log.Printf("[Compressor] done — %d msgs -> %d msgs (1 summary + %d recent)", len(messages), len(compressed), len(recentMessages))
	return compressed
}

// EstimateTokens estimates token count using text.length/4.
// Mirrors Java estimateTokens().
func (c *TokenCountCompressor) EstimateTokens(messages []*schema.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
	}
	return total
}

// generateSummary generates a simple summary from old messages.
// Mirrors Java generateSummary() — takes first 3 messages, truncates each to 50 chars.
func (c *TokenCountCompressor) generateSummary(messages []*schema.Message) string {
	summary := fmt.Sprintf("共%d轮对话。", len(messages)/2)

	count := int(math.Min(3, float64(len(messages))))
	for i := 0; i < count; i++ {
		text := messages[i].Content
		if text != "" {
			if len(text) > 50 {
				text = text[:50]
			}
			summary += " " + text
		}
	}

	return summary
}
