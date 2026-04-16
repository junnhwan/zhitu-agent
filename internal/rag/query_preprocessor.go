package rag

import (
	"log"
	"strings"
)

// QueryPreprocessor removes Chinese stop words and punctuation from queries.
// Mirrors Java QueryPreprocessor.
type QueryPreprocessor struct {
	stopWords []string
}

// NewQueryPreprocessor creates a preprocessor with default Chinese stop words.
func NewQueryPreprocessor() *QueryPreprocessor {
	return &QueryPreprocessor{
		stopWords: []string{
			"的", "了", "是", "在", "我", "有", "和", "就", "不", "人", "都", "一", "一个",
			"上", "也", "很", "到", "说", "要", "去", "你", "会", "着", "没有", "看", "好",
			"吗", "呢", "吧", "啊", "哦", "嗯",
		},
	}
}

// Preprocess removes punctuation and stop words from the query.
// Returns the original query if the result would be empty.
func (p *QueryPreprocessor) Preprocess(originalQuery string) string {
	if len(originalQuery) < 3 {
		return originalQuery
	}

	processed := strings.TrimSpace(originalQuery)

	// Remove punctuation and collapse whitespace
	var b strings.Builder
	for _, r := range processed {
		if isPunct(r) || isWhitespace(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	processed = b.String()

	// Remove stop words
	for _, word := range p.stopWords {
		processed = strings.ReplaceAll(processed, word, " ")
	}

	// Collapse multiple spaces
	processed = strings.Join(strings.Fields(processed), " ")

	if processed != originalQuery {
		log.Printf("[QueryPreprocessor] [%s] -> [%s]", originalQuery, processed)
	}

	if processed == "" {
		return originalQuery
	}
	return processed
}

func isPunct(r rune) bool {
	// Unicode punctuation categories
	return (r >= 0x2000 && r <= 0x206F) || // General punctuation
		(r >= 0x3000 && r <= 0x303F) || // CJK punctuation
		(r >= 0xFF00 && r <= 0xFFEF) || // Halfwidth/Fullwidth
		(r >= 0x0021 && r <= 0x002F) || // Basic Latin punctuation
		(r >= 0x003A && r <= 0x0040) ||
		(r >= 0x005B && r <= 0x0060) ||
		(r >= 0x007B && r <= 0x007E)
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}
