package channel

import (
	"context"
	"testing"
)

func TestDomainCtx(t *testing.T) {
	ctx := WithDomain(context.Background(), "KNOWLEDGE")
	if d := DomainFromContext(ctx); d != "KNOWLEDGE" {
		t.Errorf("got %q", d)
	}
	if d := DomainFromContext(context.Background()); d != "" {
		t.Errorf("empty ctx got %q", d)
	}
}
