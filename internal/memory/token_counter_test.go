package memory

import "testing"

func TestEstimateTokensCJK(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"pure ascii", "hello world", 2},
		{"pure chinese", "你好世界测试", 3},
		{"mixed", "hello 你好", 2},
		{"empty", "", 0},
		{"ascii with numbers", "abc123 def", 2},
		{"japanese hiragana", "こんにちは", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateTokens(tt.text); got != tt.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}
