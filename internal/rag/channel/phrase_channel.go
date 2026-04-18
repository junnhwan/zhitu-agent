package channel

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

type PhraseChannel struct {
	rdb       *redis.Client
	indexName string
	topK      int
}

func NewPhraseChannel(rdb *redis.Client, indexName string, topK int) *PhraseChannel {
	if topK <= 0 {
		topK = 10
	}
	return &PhraseChannel{rdb: rdb, indexName: indexName, topK: topK}
}

func (c *PhraseChannel) Name() string { return "phrase" }

// ExtractPhrases 粗抽关键短语：按空格/标点切，ASCII token 保留 len>=3，CJK 保留 len>=2。
// 简陋的停用词过滤。
func ExtractPhrases(query string) []string {
	stop := map[string]struct{}{
		"的": {}, "是": {}, "了": {}, "吗": {}, "呢": {}, "啊": {},
		"what": {}, "how": {}, "why": {}, "the": {}, "is": {}, "a": {}, "an": {}, "of": {}, "to": {}, "for": {}, "in": {}, "on": {}, "and": {}, "or": {},
	}
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		s := cur.String()
		cur.Reset()
		sl := strings.ToLower(s)
		if _, isStop := stop[sl]; isStop {
			return
		}
		isCJK := false
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				isCJK = true
				break
			}
		}
		minLen := 3
		if isCJK {
			minLen = 2
		}
		if len([]rune(s)) < minLen {
			return
		}
		out = append(out, s)
	}
	for _, r := range query {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}

func (c *PhraseChannel) Retrieve(ctx context.Context, query string) ([]*Candidate, error) {
	phrases := ExtractPhrases(query)
	if len(phrases) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(phrases))
	for _, p := range phrases {
		// RediSearch 双引号短语匹配 — 内部单引号/双引号转义
		esc := strings.ReplaceAll(p, `"`, `\"`)
		parts = append(parts, fmt.Sprintf(`"%s"`, esc))
	}
	expr := fmt.Sprintf("@content:(%s)", strings.Join(parts, " | "))

	raw, err := c.rdb.Do(ctx, "FT.SEARCH", c.indexName,
		expr,
		"LIMIT", "0", strconv.Itoa(c.topK),
		"WITHSCORES",
		"DIALECT", "2",
	).Result()
	if err != nil {
		return nil, err
	}
	cands, err := parseFTSearch(raw)
	if err != nil {
		return nil, err
	}
	for _, ca := range cands {
		ca.ChannelName = "phrase"
	}
	return cands, nil
}

// satisfy unused import warn on fmt/strconv even if we branch — keep explicit.
var _ = schema.Document{}
