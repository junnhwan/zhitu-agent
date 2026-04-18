package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMicroCompactSkipShort(t *testing.T) {
	mc := &MicroCompactor{Threshold: 2000}
	out := mc.Compact(context.Background(), "rag_search", "small result")
	if out != "small result" {
		t.Errorf("short result should pass through, got %q", out)
	}
}

func TestMicroCompactRagTopN(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10}
	segs := []string{}
	for i := 0; i < 10; i++ {
		segs = append(segs, "【来源：a.md | 相似度：0.9】\n段落内容")
	}
	huge := strings.Join(segs, "\n\n---\n\n")
	out := mc.Compact(context.Background(), "rag_search", huge)
	if n := strings.Count(out, "【来源："); n > 3 {
		t.Errorf("want ≤ 3 sources, got %d", n)
	}
}

func TestMicroCompactEmailSummary(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10}
	out := mc.Compact(context.Background(), "send_email", "邮件发送成功，收件人: foo@bar.com, 主题: hello world")
	if !strings.Contains(out, "邮件") {
		t.Errorf("expected email summary, got %q", out)
	}
	if len(out) > 50 {
		t.Errorf("email summary should be short, got %d chars", len(out))
	}
}

func TestMicroCompactUnknownFallback(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10}
	big := strings.Repeat("x", 100)
	out := mc.Compact(context.Background(), "weird_tool", big)
	if len(out) >= 100 {
		t.Errorf("no-LLM unknown tool must truncate, got %d", len(out))
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("truncation should end with ellipsis: %q", out)
	}
}

func TestMicroCompactUnknownWithLLM(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10, LLM: &fakeLLM{reply: "工具结果摘要：成功"}}
	big := strings.Repeat("x", 100)
	out := mc.Compact(context.Background(), "weird_tool", big)
	if !strings.Contains(out, "成功") {
		t.Errorf("LLM summary should be used, got %q", out)
	}
}

func TestMicroCompactLLMFailureFallsBackToTruncate(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10, LLM: &fakeLLM{err: errors.New("boom")}}
	big := strings.Repeat("x", 100)
	out := mc.Compact(context.Background(), "weird_tool", big)
	if len(out) >= 100 {
		t.Errorf("LLM error should fall back to truncate, got %d chars", len(out))
	}
}

func TestMicroCompactMessageForMemory(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10}
	msg := schema.ToolMessage(strings.Repeat("x", 100), "call-1", schema.WithToolName("weird_tool"))
	out := mc.MessageForMemory(context.Background(), msg)
	if out == msg {
		t.Error("expected a new message when content is compacted")
	}
	if out.ToolName != "weird_tool" || out.ToolCallID != "call-1" {
		t.Errorf("tool metadata not preserved: name=%q id=%q", out.ToolName, out.ToolCallID)
	}
	if out.Content == msg.Content {
		t.Errorf("content not compacted")
	}
}

func TestMicroCompactMessageForMemoryPassThroughNonTool(t *testing.T) {
	mc := &MicroCompactor{Threshold: 10}
	msg := schema.UserMessage(strings.Repeat("x", 100))
	if got := mc.MessageForMemory(context.Background(), msg); got != msg {
		t.Error("non-tool messages should be returned as-is")
	}
}
