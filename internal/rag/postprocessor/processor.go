package postprocessor

import (
	"context"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
)

type Processor interface {
	Name() string
	Process(ctx context.Context, cands []*channel.Candidate, query string) []*channel.Candidate
}
