package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/understand"
)

func TestNeedKnowledgeRetrieval(t *testing.T) {
	orch := &SimpleOrchestrator{}

	tests := []struct {
		input string
		want  bool
	}{
		{"查询最近的订单", true},
		{"了解Python的基础知识", true},
		{"什么是微服务", true},
		{"介绍一下Redis", true},
		{"解释一下Docker", true},
		{"说明这个设计", true},
		{"帮我写一段代码", false},
		{"今天天气怎么样", false},
		{"Hello world", false},
	}

	for _, tt := range tests {
		got := orch.needKnowledgeRetrieval(tt.input)
		if got != tt.want {
			t.Errorf("needKnowledgeRetrieval(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestKnowledgeAgentName(t *testing.T) {
	a := &KnowledgeAgent{}
	if a.AgentName() != "KnowledgeAgent" {
		t.Errorf("AgentName() = %q, want KnowledgeAgent", a.AgentName())
	}
}

func TestReasoningAgentName(t *testing.T) {
	a := &ReasoningAgent{}
	if a.AgentName() != "ReasoningAgent" {
		t.Errorf("AgentName() = %q, want ReasoningAgent", a.AgentName())
	}
}

func TestReasoningAgentExecute(t *testing.T) {
	a := NewReasoningAgent(func(ctx context.Context, sessionID int64, input string) (string, error) {
		return "mocked: " + input, nil
	})

	result := a.Execute(context.Background(), 1, "test input")
	if result != "mocked: test input" {
		t.Errorf("Execute() = %q, want mocked: test input", result)
	}
}

func TestReasoningAgentExecuteError(t *testing.T) {
	a := NewReasoningAgent(func(ctx context.Context, sessionID int64, input string) (string, error) {
		return "", fmt.Errorf("something went wrong")
	})

	result := a.Execute(context.Background(), 1, "test")
	if result == "" {
		t.Error("Execute() should return error message, got empty")
	}
	if !strings.Contains(result, "推理失败") {
		t.Errorf("Execute() = %q, should contain 推理失败", result)
	}
}

type stubRouter struct {
	domain   string
	fallback bool
	err      error
}

func (s *stubRouter) Understand(ctx context.Context, sessionID int64, history []*schema.Message, query string) (*understand.Result, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &understand.Result{
		Intent:   &understand.IntentResult{Domain: s.domain, Confidence: 0.9},
		Fallback: s.fallback,
	}, nil
}

func TestRouteWithIntentRouter(t *testing.T) {
	o := &SimpleOrchestrator{}
	o.WithIntentRouter(&stubRouter{domain: "KNOWLEDGE"})
	if !o.routeNeedsKnowledge(context.Background(), 1, "随便问个问题") {
		t.Errorf("KNOWLEDGE intent should route to knowledge")
	}
	o.WithIntentRouter(&stubRouter{domain: "CHITCHAT"})
	if o.routeNeedsKnowledge(context.Background(), 1, "查询订单") {
		t.Errorf("CHITCHAT intent should not route to knowledge even if keyword present")
	}
}

func TestRouteFallsBackToKeywordOnRouterError(t *testing.T) {
	o := &SimpleOrchestrator{}
	o.WithIntentRouter(&stubRouter{err: errors.New("boom")})
	if !o.routeNeedsKnowledge(context.Background(), 1, "查询订单") {
		t.Errorf("router error should fall back to keyword routing")
	}
}

func TestRouteFallsBackOnRouterFallbackFlag(t *testing.T) {
	o := &SimpleOrchestrator{}
	o.WithIntentRouter(&stubRouter{fallback: true})
	if !o.routeNeedsKnowledge(context.Background(), 1, "什么是 Redis") {
		t.Errorf("router fallback flag should defer to keyword routing")
	}
}
