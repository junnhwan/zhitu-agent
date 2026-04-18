package understand

import "sync"

type Action int

const (
	ActionRoute Action = iota
	ActionClarify
)

type Decision struct {
	Action          Action
	Result          *IntentResult
	ClarifyQuestion string
}

type Guardian struct {
	threshold       float64
	maxLowAttempts  int
	mu              sync.Mutex
	lowCount        map[int64]int
}

func NewGuardian(threshold float64, maxLowAttempts int) *Guardian {
	return &Guardian{
		threshold:      threshold,
		maxLowAttempts: maxLowAttempts,
		lowCount:       make(map[int64]int),
	}
}

func (g *Guardian) Evaluate(sessionID int64, result *IntentResult) *Decision {
	g.mu.Lock()
	defer g.mu.Unlock()

	if result.Confidence >= g.threshold {
		delete(g.lowCount, sessionID)
		return &Decision{Action: ActionRoute, Result: result}
	}

	g.lowCount[sessionID]++
	if g.lowCount[sessionID] >= g.maxLowAttempts {
		delete(g.lowCount, sessionID)
		return &Decision{
			Action: ActionRoute,
			Result: &IntentResult{Domain: "CHITCHAT", Confidence: result.Confidence},
		}
	}

	return &Decision{
		Action:          ActionClarify,
		Result:          result,
		ClarifyQuestion: "我不太确定你的意图，能再说得具体一点吗？",
	}
}
