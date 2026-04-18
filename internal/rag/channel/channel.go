package channel

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

type Candidate struct {
	Doc           *schema.Document
	RankInChannel int
	RawScore      float64
	ChannelName   string
	RankByChannel map[string]int
}

type Channel interface {
	Name() string
	Retrieve(ctx context.Context, query string) ([]*Candidate, error)
}
