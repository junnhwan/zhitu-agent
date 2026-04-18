package understand

import (
	"context"
	"log"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/sony/gobreaker/v2"
)

type Result struct {
	Route              string
	RewrittenQuery     string
	Intent             *IntentResult
	NeedsClarification bool
	ClarifyQuestion    string
	Fallback           bool
}

type Service struct {
	rewriter   *Rewriter
	classifier *Classifier
	guardian   *Guardian
	cb         *gobreaker.CircuitBreaker[*IntentResult]
}

type BreakerConfig struct {
	Name        string
	Timeout     time.Duration
	MaxRequests uint32
	Interval    time.Duration
	ResetAfter  time.Duration
	ErrorRate   float64
}

func defaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		Name:        "understand",
		MaxRequests: 5,
		Interval:    5 * time.Minute,
		ResetAfter:  60 * time.Second,
		ErrorRate:   0.5,
	}
}

func NewService(rewriter *Rewriter, classifier *Classifier, guardian *Guardian, cfg *BreakerConfig) *Service {
	if cfg == nil {
		c := defaultBreakerConfig()
		cfg = &c
	}
	settings := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.ResetAfter,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.Requests < 5 {
				return false
			}
			rate := float64(counts.TotalFailures) / float64(counts.Requests)
			return rate >= cfg.ErrorRate
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("[understand] circuit breaker %s: %s -> %s", name, from, to)
		},
	}
	return &Service{
		rewriter:   rewriter,
		classifier: classifier,
		guardian:   guardian,
		cb:         gobreaker.NewCircuitBreaker[*IntentResult](settings),
	}
}

func (s *Service) Understand(ctx context.Context, sessionID int64, history []*schema.Message, query string) (*Result, error) {
	rewritten, _ := s.rewriter.Rewrite(ctx, history, query)

	intent, cbErr := s.cb.Execute(func() (*IntentResult, error) {
		res, err := s.classifier.Classify(ctx, rewritten)
		if err != nil {
			return nil, err
		}
		return res, nil
	})

	if cbErr != nil {
		log.Printf("[understand] breaker open or classifier failed: %v", cbErr)
		return &Result{
			Route:          findRoute(s.classifier.tree, "CHITCHAT", ""),
			RewrittenQuery: rewritten,
			Intent:         &IntentResult{Domain: "CHITCHAT"},
			Fallback:       true,
		}, nil
	}

	decision := s.guardian.Evaluate(sessionID, intent)
	if decision.Action == ActionClarify {
		return &Result{
			RewrittenQuery:     rewritten,
			Intent:             intent,
			NeedsClarification: true,
			ClarifyQuestion:    decision.ClarifyQuestion,
		}, nil
	}

	final := decision.Result
	return &Result{
		Route:          findRoute(s.classifier.tree, final.Domain, final.Category),
		RewrittenQuery: rewritten,
		Intent:         final,
	}, nil
}

func findRoute(tree *Tree, domain, category string) string {
	for _, d := range tree.Domains {
		if d.Name != domain {
			continue
		}
		for _, c := range d.Categories {
			if c.Name == category {
				return c.Route
			}
		}
		if d.Route != "" {
			return d.Route
		}
	}
	return "direct_llm"
}
