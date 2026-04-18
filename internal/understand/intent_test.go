package understand

import (
	"context"
	"errors"
	"testing"
)

func loadTestTree(t *testing.T) *Tree {
	t.Helper()
	tree, err := LoadTree("tree.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestClassifyValidJSON(t *testing.T) {
	llm := &fakeLLM{reply: `{"domain":"KNOWLEDGE","category":"RAG_QUERY","confidence":0.9}`}
	c := NewClassifier(llm, loadTestTree(t))
	res, err := c.Classify(context.Background(), "什么是 RAG")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "KNOWLEDGE" || res.Category != "RAG_QUERY" || res.Confidence != 0.9 {
		t.Errorf("bad result: %+v", res)
	}
}

func TestClassifyJSONWithPrefix(t *testing.T) {
	llm := &fakeLLM{reply: "结果：\n{\"domain\":\"TOOL\",\"category\":\"EMAIL\",\"confidence\":0.8}\n好的"}
	c := NewClassifier(llm, loadTestTree(t))
	res, err := c.Classify(context.Background(), "发邮件")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "TOOL" || res.Category != "EMAIL" {
		t.Errorf("bad result: %+v", res)
	}
}

func TestClassifyUnknownDomainFallsBack(t *testing.T) {
	llm := &fakeLLM{reply: `{"domain":"WEIRD","category":"x","confidence":0.5}`}
	c := NewClassifier(llm, loadTestTree(t))
	res, err := c.Classify(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "CHITCHAT" {
		t.Errorf("expected CHITCHAT fallback, got %s", res.Domain)
	}
}

func TestClassifyMalformedFallsBack(t *testing.T) {
	llm := &fakeLLM{reply: "完全不是 JSON 的文本"}
	c := NewClassifier(llm, loadTestTree(t))
	res, err := c.Classify(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "CHITCHAT" {
		t.Errorf("expected CHITCHAT, got %s", res.Domain)
	}
	if res.Confidence != 0 {
		t.Errorf("expected confidence 0 for unparsed, got %v", res.Confidence)
	}
}

func TestClassifyLLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("boom")}
	c := NewClassifier(llm, loadTestTree(t))
	res, err := c.Classify(context.Background(), "test")
	if err != nil {
		t.Errorf("classifier should not return error, got %v", err)
	}
	if res.Domain != "CHITCHAT" {
		t.Errorf("expected CHITCHAT on LLM error, got %s", res.Domain)
	}
}
