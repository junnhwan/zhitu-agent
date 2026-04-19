//go:build eval

package rag

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestSampleHitDocIDPrefixMatch(t *testing.T) {
	docs := []*schema.Document{
		{ID: "research/phase2/SUMMARY.md_0", Content: "unrelated"},
		{ID: "design/rag.md_3", Content: "unrelated"},
	}
	s := goldenSample{Query: "q", RelevantDocIDs: []string{"design/rag.md"}}
	if !sampleHit(docs, s) {
		t.Error("expected prefix match on design/rag.md → design/rag.md_3")
	}
}

func TestSampleHitDocIDExactMatch(t *testing.T) {
	docs := []*schema.Document{{ID: "file.md_0"}}
	s := goldenSample{RelevantDocIDs: []string{"file.md_0"}}
	if !sampleHit(docs, s) {
		t.Error("expected exact ID match")
	}
}

func TestSampleHitDocIDNoMatch(t *testing.T) {
	docs := []*schema.Document{{ID: "a.md_0"}, {ID: "b.md_1"}}
	s := goldenSample{RelevantDocIDs: []string{"c.md"}}
	if sampleHit(docs, s) {
		t.Error("unexpected hit for disjoint doc IDs")
	}
}

// Prefix must be bounded by "_" — "a.md" should NOT match "a.md.bak_0".
func TestSampleHitDocIDPrefixBoundary(t *testing.T) {
	docs := []*schema.Document{{ID: "a.md.bak_0"}}
	s := goldenSample{RelevantDocIDs: []string{"a.md"}}
	if sampleHit(docs, s) {
		t.Error("prefix matched across non-boundary char")
	}
}

func TestSampleHitKeywordFallback(t *testing.T) {
	docs := []*schema.Document{
		{ID: "x", Content: "this doc talks about RAG and 检索"},
	}
	s := goldenSample{RelevantKeywords: []string{"RAG", "检索"}}
	if !sampleHit(docs, s) {
		t.Error("expected keyword hit")
	}
}

func TestSampleHitKeywordAllRequired(t *testing.T) {
	docs := []*schema.Document{
		{ID: "x", Content: "only RAG here"},
	}
	s := goldenSample{RelevantKeywords: []string{"RAG", "missing"}}
	if sampleHit(docs, s) {
		t.Error("expected miss when one keyword absent")
	}
}

// doc_id 非空时 keyword 被忽略 —— 即使 keyword 本可命中，doc_id 不命中就不算 hit。
func TestSampleHitDocIDTakesPriority(t *testing.T) {
	docs := []*schema.Document{
		{ID: "other.md_0", Content: "has the keyword RAG"},
	}
	s := goldenSample{
		RelevantDocIDs:   []string{"target.md"},
		RelevantKeywords: []string{"RAG"},
	}
	if sampleHit(docs, s) {
		t.Error("doc_id path should short-circuit keyword fallback")
	}
}

func TestSampleHitEmpty(t *testing.T) {
	if sampleHit([]*schema.Document{{ID: "x"}}, goldenSample{}) {
		t.Error("empty sample should not hit")
	}
}

func TestDocIDMatches(t *testing.T) {
	cases := []struct {
		actual, want string
		expect       bool
	}{
		{"a.md_0", "a.md", true},
		{"a.md", "a.md", true},
		{"a.md.bak_0", "a.md", false},
		{"b/a.md_0", "a.md", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := docIDMatches(c.actual, c.want); got != c.expect {
			t.Errorf("docIDMatches(%q, %q) = %v, want %v", c.actual, c.want, got, c.expect)
		}
	}
}

func TestPreviewTruncation(t *testing.T) {
	long := make([]rune, 500)
	for i := range long {
		long[i] = '字'
	}
	out := preview(string(long), 200)
	runes := []rune(out)
	// 200 rune + "..." 3 char = 203
	if len(runes) != 203 {
		t.Errorf("preview len = %d, want 203", len(runes))
	}
}

func TestPreviewCollapsesWhitespace(t *testing.T) {
	in := "hello\n\n  world\r\n\t\t end"
	out := preview(in, 100)
	// 允许 tab 保留（我们只折叠空格）—— 只要 \n 没了且多空格折叠
	if containsAny(out, "\n\r") {
		t.Errorf("still has newline: %q", out)
	}
	if containsSubstr(out, "  ") {
		t.Errorf("still has double space: %q", out)
	}
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, x := range s {
			if x == c {
				return true
			}
		}
	}
	return false
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
