package memory

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
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
	SummaryPrompt      string
	SummaryThreshold   int
	SummaryRecent      int
	MicroCompactMinLen int
	LLM                model.BaseChatModel
}

const (
	defaultSummaryThreshold = 9
	defaultSummaryRecent    = 6
)

func NewCompressor(cfg Config) (Compressor, error) {
	switch cfg.Strategy {
	case "", "simple":
		return NewTokenCountCompressor(cfg.RecentRounds, cfg.RecentTokenLimit), nil
	case "llm_summary", "hybrid":
		llm, err := resolveLLM(cfg)
		if err != nil {
			return nil, err
		}
		fallback := NewTokenCountCompressor(cfg.RecentRounds, cfg.RecentTokenLimit)
		threshold := cfg.SummaryThreshold
		if threshold <= 0 {
			threshold = defaultSummaryThreshold
		}
		recent := cfg.SummaryRecent
		if recent <= 0 {
			recent = defaultSummaryRecent
		}
		return NewLLMCompressor(llm, fallback, recent, threshold, cfg.SummaryPrompt), nil
	default:
		return nil, fmt.Errorf("unknown compressor strategy %q", cfg.Strategy)
	}
}

func resolveLLM(cfg Config) (model.BaseChatModel, error) {
	if cfg.LLM != nil {
		return cfg.LLM, nil
	}
	if cfg.APIKey == "" || cfg.LLMModel == "" {
		return nil, fmt.Errorf("strategy %q needs either cfg.LLM or (APIKey, LLMModel)", cfg.Strategy)
	}
	return qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.LLMModel,
	})
}
