package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
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
