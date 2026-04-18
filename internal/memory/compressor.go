package memory

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

type Compressor interface {
	Compress(ctx context.Context, messages []*schema.Message) []*schema.Message
	EstimateTokens(messages []*schema.Message) int
}

type Config struct {
	Strategy           string
	RecentRounds       int
	RecentTokenLimit   int
	LLMModel           string
	APIKey             string
	BaseURL            string
	MicroCompactMinLen int
}

func NewCompressor(cfg Config) (Compressor, error) {
	switch cfg.Strategy {
	case "", "simple":
		return NewTokenCountCompressor(cfg.RecentRounds, cfg.RecentTokenLimit), nil
	case "llm_summary", "hybrid":
		return nil, fmt.Errorf("compressor strategy %q not implemented yet", cfg.Strategy)
	default:
		return nil, fmt.Errorf("unknown compressor strategy %q", cfg.Strategy)
	}
}
