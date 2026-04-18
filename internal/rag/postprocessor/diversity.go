package postprocessor

import (
	"context"
	"strings"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

// Diversity 限制每个来源文件最多保留 N 个 chunk，防止单文件霸榜。
// 严格 MMR（λ·query_sim - (1-λ)·doc_sim）需要 embedding 相似度，本实现是简化版：
// 按 file_name metadata 分组计数。
type Diversity struct {
	PerFileCap int
}

func NewDiversity(perFileCap int) *Diversity {
	if perFileCap <= 0 {
		perFileCap = 2
	}
	return &Diversity{PerFileCap: perFileCap}
}

func (d *Diversity) Name() string { return "diversity" }

func fileKey(c *channel.Candidate) string {
	if c == nil || c.Doc == nil {
		return ""
	}
	if c.Doc.MetaData != nil {
		if v, ok := c.Doc.MetaData["file_name"].(string); ok && v != "" {
			return v
		}
	}
	// fallback: 去掉 ID 末尾的 _<chunkIdx>
	id := c.Doc.ID
	if i := strings.LastIndex(id, "_"); i > 0 {
		return id[:i]
	}
	return id
}

func (d *Diversity) Process(_ context.Context, cands []*channel.Candidate, _ string) []*channel.Candidate {
	count := map[string]int{}
	out := make([]*channel.Candidate, 0, len(cands))
	for _, c := range cands {
		key := fileKey(c)
		if count[key] >= d.PerFileCap {
			continue
		}
		count[key]++
		out = append(out, c)
	}
	return out
}
