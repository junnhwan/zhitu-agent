package postprocessor

import (
	"context"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

type Dedup struct{}

func NewDedup() *Dedup { return &Dedup{} }

func (d *Dedup) Name() string { return "dedup" }

// Process 合并 Doc.ID 相同的 Candidate：保留第一个出现的 Candidate，
// 并把所有 channel 的 rank 汇总到 RankByChannel。
func (d *Dedup) Process(_ context.Context, cands []*channel.Candidate, _ string) []*channel.Candidate {
	byID := make(map[string]*channel.Candidate, len(cands))
	order := make([]string, 0, len(cands))
	for _, c := range cands {
		if c == nil || c.Doc == nil {
			continue
		}
		id := c.Doc.ID
		existing, ok := byID[id]
		if !ok {
			fresh := *c
			if fresh.RankByChannel == nil {
				fresh.RankByChannel = map[string]int{}
			}
			fresh.RankByChannel[c.ChannelName] = c.RankInChannel
			byID[id] = &fresh
			order = append(order, id)
			continue
		}
		if existing.RankByChannel == nil {
			existing.RankByChannel = map[string]int{}
		}
		if _, seen := existing.RankByChannel[c.ChannelName]; !seen {
			existing.RankByChannel[c.ChannelName] = c.RankInChannel
		}
	}
	out := make([]*channel.Candidate, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}
