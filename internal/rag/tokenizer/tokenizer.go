package tokenizer

import (
	"log"
	"strings"
	"sync"

	"github.com/go-ego/gse"
)

// Tokenizer 封装 gse 中文分词器。空间连接 tokens 便于写入 RediSearch TEXT 字段。
type Tokenizer struct {
	seg gse.Segmenter
}

var (
	defaultOnce     sync.Once
	defaultTok      *Tokenizer
	defaultInitErr  error
)

// Default 返回进程级共享分词器（懒加载默认中文字典）。
func Default() (*Tokenizer, error) {
	defaultOnce.Do(func() {
		var seg gse.Segmenter
		if err := seg.LoadDict(); err != nil {
			defaultInitErr = err
			log.Printf("[tokenizer] gse LoadDict failed: %v", err)
			return
		}
		defaultTok = &Tokenizer{seg: seg}
		log.Printf("[tokenizer] gse default dict loaded")
	})
	return defaultTok, defaultInitErr
}

// Tokenize 切词后用单空格连接，供 RediSearch BM25 匹配。
// 空串返回空串，不报错。
func (t *Tokenizer) Tokenize(text string) string {
	if t == nil || strings.TrimSpace(text) == "" {
		return text
	}
	tokens := t.seg.Cut(text, true)
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}
