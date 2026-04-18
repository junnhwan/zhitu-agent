package understand

import "testing"

func TestGuardianHighConfidencePassthrough(t *testing.T) {
	g := NewGuardian(0.6, 3)
	d := g.Evaluate(1, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.9})
	if d.Action != ActionRoute {
		t.Errorf("expected route, got %v", d.Action)
	}
}

func TestGuardianLowConfidenceClarify(t *testing.T) {
	g := NewGuardian(0.6, 3)
	d := g.Evaluate(1, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.3})
	if d.Action != ActionClarify {
		t.Errorf("expected clarify, got %v", d.Action)
	}
	if d.ClarifyQuestion == "" {
		t.Errorf("expected clarify question")
	}
}

func TestGuardianGivesUpAfterRepeatedLowConfidence(t *testing.T) {
	g := NewGuardian(0.6, 3)
	for i := 0; i < 2; i++ {
		d := g.Evaluate(42, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
		if d.Action != ActionClarify {
			t.Errorf("attempt %d: expected clarify, got %v", i, d.Action)
		}
	}
	d := g.Evaluate(42, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	if d.Action != ActionRoute {
		t.Errorf("3rd low-confidence should give up and route, got %v", d.Action)
	}
	if d.Result.Domain != "CHITCHAT" {
		t.Errorf("expected CHITCHAT after giving up, got %s", d.Result.Domain)
	}
}

func TestGuardianResetsOnHighConfidence(t *testing.T) {
	g := NewGuardian(0.6, 3)
	g.Evaluate(7, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	g.Evaluate(7, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.9})
	d := g.Evaluate(7, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	if d.Action != ActionClarify {
		t.Errorf("counter should reset after success, got %v", d.Action)
	}
}

func TestGuardianSessionsIsolated(t *testing.T) {
	g := NewGuardian(0.6, 3)
	g.Evaluate(1, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	g.Evaluate(1, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	d := g.Evaluate(2, &IntentResult{Domain: "KNOWLEDGE", Confidence: 0.2})
	if d.Action != ActionClarify {
		t.Errorf("session 2 should not inherit session 1's count, got %v", d.Action)
	}
}
