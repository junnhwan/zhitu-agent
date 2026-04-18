package postprocessor

import (
	"context"
	"sort"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

type RRF struct {
	K                int
	ConsistencyBonus float64
}

func NewRRF(k int, bonus float64) *RRF {
	if k <= 0 {
		k = 60
	}
	if bonus <= 0 {
		bonus = 1.0
	}
	return &RRF{K: k, ConsistencyBonus: bonus}
}

func (r *RRF) Name() string { return "rrf" }

func (r *RRF) Process(_ context.Context, cands []*channel.Candidate, _ string) []*channel.Candidate {
	type scored struct {
		idx   int
		score float64
	}
	sc := make([]scored, len(cands))
	for i, c := range cands {
		var s float64
		for _, rank := range c.RankByChannel {
			s += 1.0 / float64(r.K+rank)
		}
		if len(c.RankByChannel) >= 2 {
			s *= r.ConsistencyBonus
		}
		c.RawScore = s
		sc[i] = scored{i, s}
	}
	sort.SliceStable(sc, func(i, j int) bool { return sc[i].score > sc[j].score })
	out := make([]*channel.Candidate, len(cands))
	for i, s := range sc {
		out[i] = cands[s.idx]
	}
	return out
}
