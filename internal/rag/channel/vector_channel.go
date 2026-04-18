package channel

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

type VectorSource interface {
	Retrieve(ctx context.Context, query string) ([]*schema.Document, error)
}

type VectorChannel struct {
	src      VectorSource
	minScore float64
}

func NewVectorChannel(src VectorSource, minScore float64) *VectorChannel {
	return &VectorChannel{src: src, minScore: minScore}
}

func (c *VectorChannel) Name() string { return "vector" }

func (c *VectorChannel) Retrieve(ctx context.Context, query string) ([]*Candidate, error) {
	docs, err := c.src.Retrieve(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]*Candidate, 0, len(docs))
	rank := 1
	for _, d := range docs {
		if d.Score() < c.minScore {
			continue
		}
		out = append(out, &Candidate{
			Doc:           d,
			RankInChannel: rank,
			RawScore:      d.Score(),
			ChannelName:   "vector",
		})
		rank++
	}
	return out, nil
}
