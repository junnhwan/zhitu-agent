package postprocessor

import (
	"context"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

type Reranker interface {
	Rerank(query string, documents []string, topN int) []int
}

type Rerank struct {
	client      Reranker
	finalTopN   int
	maxBeforeRR int
	onFallback  func()
}

func NewRerank(client Reranker, finalTopN int, onFallback func()) *Rerank {
	if finalTopN <= 0 {
		finalTopN = 5
	}
	return &Rerank{client: client, finalTopN: finalTopN, maxBeforeRR: 20, onFallback: onFallback}
}

func (r *Rerank) Name() string { return "rerank" }

func (r *Rerank) Process(_ context.Context, cands []*channel.Candidate, query string) []*channel.Candidate {
	if len(cands) == 0 || r.client == nil {
		return cands
	}
	in := cands
	if len(in) > r.maxBeforeRR {
		in = in[:r.maxBeforeRR]
	}
	docs := make([]string, len(in))
	for i, c := range in {
		docs[i] = c.Doc.Content
	}
	idxs := r.client.Rerank(query, docs, r.finalTopN)
	if len(idxs) == 0 {
		if r.onFallback != nil {
			r.onFallback()
		}
		if len(in) > r.finalTopN {
			return in[:r.finalTopN]
		}
		return in
	}
	out := make([]*channel.Candidate, 0, len(idxs))
	for _, i := range idxs {
		if i >= 0 && i < len(in) {
			out = append(out, in[i])
		}
	}
	if len(out) == 0 {
		if r.onFallback != nil {
			r.onFallback()
		}
		if len(in) > r.finalTopN {
			return in[:r.finalTopN]
		}
		return in
	}
	return out
}
