package tokenizer

import (
	"strings"
	"testing"
)

func TestTokenizeChinese(t *testing.T) {
	tk, err := Default()
	if err != nil {
		t.Skipf("gse init failed: %v", err)
	}
	out := tk.Tokenize("什么是 Eino Graph")
	// gse 默认转小写；BM25 侧也 case-insensitive
	if !strings.Contains(strings.ToLower(out), "eino") {
		t.Errorf("missing eino: %q", out)
	}
	// 切完应该有空格分隔的多个 token
	if !strings.Contains(out, " ") {
		t.Errorf("expected multi-token output: %q", out)
	}
}

func TestTokenizeEmpty(t *testing.T) {
	tk, err := Default()
	if err != nil {
		t.Skip()
	}
	if got := tk.Tokenize(""); got != "" {
		t.Errorf("empty should be empty, got %q", got)
	}
	if got := tk.Tokenize("   "); strings.TrimSpace(got) != "" {
		t.Errorf("whitespace should collapse, got %q", got)
	}
}

func TestTokenizeNil(t *testing.T) {
	var tk *Tokenizer
	if got := tk.Tokenize("hello"); got != "hello" {
		t.Errorf("nil tokenizer should pass-through, got %q", got)
	}
}
