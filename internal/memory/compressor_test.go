package memory

import "testing"

func TestNewCompressorSimple(t *testing.T) {
	c, err := NewCompressor(Config{Strategy: "simple", RecentRounds: 2, RecentTokenLimit: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*TokenCountCompressor); !ok {
		t.Errorf("expected *TokenCountCompressor, got %T", c)
	}
}

func TestNewCompressorEmptyDefaultsToSimple(t *testing.T) {
	c, err := NewCompressor(Config{Strategy: "", RecentRounds: 2, RecentTokenLimit: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*TokenCountCompressor); !ok {
		t.Errorf("expected *TokenCountCompressor, got %T", c)
	}
}

func TestNewCompressorUnknown(t *testing.T) {
	if _, err := NewCompressor(Config{Strategy: "nope"}); err == nil {
		t.Errorf("expected error for unknown strategy")
	}
}

func TestNewCompressorLLMSummaryNeedsLLMOrCreds(t *testing.T) {
	if _, err := NewCompressor(Config{Strategy: "llm_summary"}); err == nil {
		t.Errorf("expected error when neither LLM nor creds are provided")
	}
}
