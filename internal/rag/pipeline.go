package rag

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"golang.org/x/sync/errgroup"

	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/postprocessor"
)

type PipelineHooks struct {
	OnChannelFailed func(name string)
	OnZeroHit       func(fallback string)
}

type Pipeline struct {
	preprocessor   *QueryPreprocessor
	channels       []channel.Channel
	processors     []postprocessor.Processor
	channelTimeout time.Duration
	phraseFallback channel.Channel
	legacyFallback Retriever
	finalTopN      int
	hooks          PipelineHooks
}

func NewPipeline(
	pre *QueryPreprocessor,
	channels []channel.Channel,
	processors []postprocessor.Processor,
	channelTimeout time.Duration,
	legacyFallback Retriever,
	finalTopN int,
	hooks PipelineHooks,
) *Pipeline {
	if channelTimeout <= 0 {
		channelTimeout = 2 * time.Second
	}
	if finalTopN <= 0 {
		finalTopN = 5
	}
	return &Pipeline{
		preprocessor:   pre,
		channels:       channels,
		processors:     processors,
		channelTimeout: channelTimeout,
		legacyFallback: legacyFallback,
		finalTopN:      finalTopN,
		hooks:          hooks,
	}
}

// WithPhraseFallback 注入零命中第二级兜底（phrase 精确短语）。
// 三级兜底顺序：channels → phrase → legacy。
func (p *Pipeline) WithPhraseFallback(ch channel.Channel) *Pipeline {
	p.phraseFallback = ch
	return p
}

var _ Retriever = (*Pipeline)(nil)

func (p *Pipeline) Retrieve(ctx context.Context, query string) ([]*schema.Document, error) {
	if query == "" {
		return nil, nil
	}
	q := query
	if p.preprocessor != nil {
		q = p.preprocessor.Preprocess(query)
	}

	results := make([][]*channel.Candidate, len(p.channels))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	for i, ch := range p.channels {
		i, ch := i, ch
		g.Go(func() error {
			cctx, cancel := context.WithTimeout(gctx, p.channelTimeout)
			defer cancel()
			r, err := ch.Retrieve(cctx, q)
			if err != nil {
				log.Printf("[Pipeline] channel %s failed: %v", ch.Name(), err)
				if p.hooks.OnChannelFailed != nil {
					p.hooks.OnChannelFailed(ch.Name())
				}
				return nil
			}
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	all := flattenCandidates(results)

	if len(all) == 0 && p.phraseFallback != nil {
		log.Printf("[Pipeline] all channels empty, trying phrase fallback")
		cctx, cancel := context.WithTimeout(ctx, p.channelTimeout)
		phraseCands, err := p.phraseFallback.Retrieve(cctx, q)
		cancel()
		if err != nil {
			log.Printf("[Pipeline] phrase fallback failed: %v", err)
			if p.hooks.OnChannelFailed != nil {
				p.hooks.OnChannelFailed(p.phraseFallback.Name())
			}
		} else if len(phraseCands) > 0 {
			if p.hooks.OnZeroHit != nil {
				p.hooks.OnZeroHit("phrase")
			}
			all = phraseCands
		}
	}

	if len(all) == 0 {
		if p.legacyFallback != nil {
			if p.hooks.OnZeroHit != nil {
				p.hooks.OnZeroHit("legacy")
			}
			log.Printf("[Pipeline] all channels empty, falling back to legacy retriever")
			return p.legacyFallback.Retrieve(ctx, query)
		}
		if p.hooks.OnZeroHit != nil {
			p.hooks.OnZeroHit("none")
		}
		return nil, nil
	}

	for _, proc := range p.processors {
		all = proc.Process(ctx, all, q)
	}

	if len(all) > p.finalTopN {
		all = all[:p.finalTopN]
	}
	out := make([]*schema.Document, 0, len(all))
	for _, c := range all {
		if c != nil && c.Doc != nil {
			out = append(out, c.Doc)
		}
	}
	return out, nil
}

func flattenCandidates(results [][]*channel.Candidate) []*channel.Candidate {
	total := 0
	for _, r := range results {
		total += len(r)
	}
	out := make([]*channel.Candidate, 0, total)
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}
