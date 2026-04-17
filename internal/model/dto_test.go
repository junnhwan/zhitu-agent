package model

import "encoding/json"
import "testing"

func TestChatRequestJSON(t *testing.T) {
	raw := `{"sessionId":123,"userId":456,"prompt":"hello"}`
	var req ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.SessionID != 123 {
		t.Errorf("SessionID = %d, want 123", req.SessionID)
	}
	if req.UserID != 456 {
		t.Errorf("UserID = %d, want 456", req.UserID)
	}
	if req.Prompt != "hello" {
		t.Errorf("Prompt = %q, want hello", req.Prompt)
	}
}

func TestKnowledgeRequestJSON(t *testing.T) {
	raw := `{"question":"Q","answer":"A","sourceName":"test.md"}`
	var req KnowledgeRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Question != "Q" {
		t.Errorf("Question = %q, want Q", req.Question)
	}
	if req.Answer != "A" {
		t.Errorf("Answer = %q, want A", req.Answer)
	}
	if req.SourceName != "test.md" {
		t.Errorf("SourceName = %q, want test.md", req.SourceName)
	}
}
