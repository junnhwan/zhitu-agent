package channel

import (
	"testing"
)

func TestEscapeBM25Query(t *testing.T) {
	got := escapeBM25Query("hello-world")
	if got != "hello\\-world" {
		t.Errorf("got %q", got)
	}
}

func TestIsAllSpaceOrPunct(t *testing.T) {
	if !isAllSpaceOrPunct("  , . ") {
		t.Error("expected true")
	}
	if isAllSpaceOrPunct("hello") {
		t.Error("expected false")
	}
}

func TestParseFTSearchEmpty(t *testing.T) {
	out, err := parseFTSearch([]any{int64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("want 0, got %d", len(out))
	}
}

func TestParseFTSearchTwoRows(t *testing.T) {
	raw := []any{
		int64(2),
		"zhitu:doc:a", "1.23", []any{"content", "hello world"},
		"zhitu:doc:b", "0.45", []any{"content", "foo"},
	}
	out, err := parseFTSearch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].Doc.ID != "zhitu:doc:a" || out[0].RankInChannel != 1 || out[0].Doc.Content != "hello world" {
		t.Errorf("bad first: %+v", out[0])
	}
	if out[1].Doc.ID != "zhitu:doc:b" || out[1].RankInChannel != 2 || out[1].RawScore != 0.45 {
		t.Errorf("bad second: %+v", out[1])
	}
}
