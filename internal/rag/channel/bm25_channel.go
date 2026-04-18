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

type BM25Channel struct {
	rdb       *redis.Client
	indexName string
	topK      int
	maxQuery  int
}

func NewBM25Channel(rdb *redis.Client, indexName string, topK int) *BM25Channel {
	if topK <= 0 {
		topK = 20
	}
	return &BM25Channel{rdb: rdb, indexName: indexName, topK: topK, maxQuery: 200}
}

func (c *BM25Channel) Name() string { return "bm25" }

// RediSearch 保留字符集，查询里出现要转义
var bm25Reserved = ",.<>{}[]\"':;!@#$%^&*()-+=~|/\\?"

func escapeBM25Query(q string) string {
	var b strings.Builder
	for _, r := range q {
		if strings.ContainsRune(bm25Reserved, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isAllSpaceOrPunct(q string) bool {
	for _, r := range q {
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
			return false
		}
	}
	return true
}

func (c *BM25Channel) Retrieve(ctx context.Context, query string) ([]*Candidate, error) {
	q := strings.TrimSpace(query)
	if q == "" || isAllSpaceOrPunct(q) {
		return nil, nil
	}
	if len([]rune(q)) > c.maxQuery {
		q = string([]rune(q)[:c.maxQuery])
	}
	esc := escapeBM25Query(q)
	expr := fmt.Sprintf("@content:(%s) | @file_name:(%s)", esc, esc)

	raw, err := c.rdb.Do(ctx, "FT.SEARCH", c.indexName,
		expr,
		"LIMIT", "0", strconv.Itoa(c.topK),
		"WITHSCORES",
		"DIALECT", "2",
	).Result()
	if err != nil {
		// 零命中 RediSearch 不应报错；报错则向上抛
		if strings.Contains(err.Error(), "no such index") {
			return nil, err
		}
		return nil, err
	}
	return parseFTSearch(raw)
}

// FT.SEARCH WITHSCORES DIALECT 2 返回：[total(int), id1, score1, fields1, id2, score2, fields2, ...]
func parseFTSearch(raw any) ([]*Candidate, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, nil
	}
	out := []*Candidate{}
	rank := 1
	// arr[0] = total (int64); 后续每 3 元组一条
	for i := 1; i+2 < len(arr); i += 3 {
		id, _ := arr[i].(string)
		if id == "" {
			continue
		}
		var score float64
		switch v := arr[i+1].(type) {
		case string:
			score, _ = strconv.ParseFloat(v, 64)
		case float64:
			score = v
		}
		content := ""
		if fields, ok := arr[i+2].([]any); ok {
			for j := 0; j+1 < len(fields); j += 2 {
				k, _ := fields[j].(string)
				if k == "content" {
					content, _ = fields[j+1].(string)
				}
			}
		}
		doc := &schema.Document{ID: id, Content: content}
		doc.WithScore(score)
		out = append(out, &Candidate{
			Doc:           doc,
			RankInChannel: rank,
			RawScore:      score,
			ChannelName:   "bm25",
		})
		rank++
	}
	return out, nil
}
