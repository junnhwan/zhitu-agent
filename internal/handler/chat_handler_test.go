package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/zhitu-agent/zhitu-agent/internal/middleware"
	"github.com/zhitu-agent/zhitu-agent/internal/model"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockChatService implements chat.ChatService for testing.
type mockChatService struct {
	chatResult     string
	chatErr        error
	streamChunks   []string
	streamErr      error
	multiResult    string
	multiErr       error
	insertResult   string
	insertErr      error
	lastPrompt     string
	lastSessionID  int64
	lastInsertQ    string
	lastInsertA    string
	lastInsertSrc  string
}

func (m *mockChatService) Chat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	m.lastPrompt = prompt
	m.lastSessionID = sessionID
	return m.chatResult, m.chatErr
}

func (m *mockChatService) StreamChat(ctx context.Context, sessionID int64, prompt string, onChunk func(content string)) error {
	m.lastPrompt = prompt
	m.lastSessionID = sessionID
	if m.streamErr != nil {
		return m.streamErr
	}
	for _, chunk := range m.streamChunks {
		onChunk(chunk)
	}
	return nil
}

func (m *mockChatService) MultiAgentChat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	m.lastPrompt = prompt
	m.lastSessionID = sessionID
	return m.multiResult, m.multiErr
}

func (m *mockChatService) InsertKnowledge(ctx context.Context, question, answer, sourceName string) (string, error) {
	m.lastInsertQ = question
	m.lastInsertA = answer
	m.lastInsertSrc = sourceName
	return m.insertResult, m.insertErr
}

// setupRouter creates a test router with middleware + handler routes.
func setupRouter(svc *mockChatService) *gin.Engine {
	r := gin.New()
	r.Use(middleware.CORS())
	r.Use(middleware.Guardrail())
	r.Use(middleware.ErrorHandler())

	h := NewChatHandler(svc)
	api := r.Group("/api")
	RegisterRoutes(api, h)
	return r
}

func TestChatEndpointSuccess(t *testing.T) {
	svc := &mockChatService{chatResult: "Hello from AI"}
	r := setupRouter(svc)

	body := `{"sessionId":1,"userId":2,"prompt":"你好"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "Hello from AI" {
		t.Errorf("body = %q, want Hello from AI", w.Body.String())
	}
	if svc.lastPrompt != "你好" {
		t.Errorf("prompt = %q, want 你好", svc.lastPrompt)
	}
	if svc.lastSessionID != 1 {
		t.Errorf("sessionID = %d, want 1", svc.lastSessionID)
	}
}

func TestChatEndpointError(t *testing.T) {
	svc := &mockChatService{chatErr: context.DeadlineExceeded}
	r := setupRouter(svc)

	body := `{"sessionId":1,"userId":2,"prompt":"你好"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 80000 {
		t.Errorf("code = %v, want 80000", resp["code"])
	}
}

func TestChatEndpointBadJSON(t *testing.T) {
	svc := &mockChatService{}
	r := setupRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestChatEndpointGuardrail(t *testing.T) {
	svc := &mockChatService{chatResult: "should not reach"}
	r := setupRouter(svc)

	body := `{"sessionId":1,"userId":2,"prompt":"我想死"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestStreamChatEndpointSuccess(t *testing.T) {
	svc := &mockChatService{streamChunks: []string{"Hello", " world"}}
	r := setupRouter(svc)

	body := `{"sessionId":1,"userId":2,"prompt":"你好"}`
	req := httptest.NewRequest(http.MethodPost, "/api/streamChat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// SSE format: "data: Hello\n\n" + "data:  world\n\n"
	respBody := w.Body.String()
	if !strings.Contains(respBody, "Hello") {
		t.Errorf("SSE body should contain 'Hello', got: %q", respBody)
	}
	if !strings.Contains(respBody, "world") {
		t.Errorf("SSE body should contain 'world', got: %q", respBody)
	}
}

func TestMultiAgentChatEndpointSuccess(t *testing.T) {
	svc := &mockChatService{multiResult: "Multi-agent response"}
	r := setupRouter(svc)

	body := `{"sessionId":1,"userId":2,"prompt":"查询订单"}`
	req := httptest.NewRequest(http.MethodPost, "/api/multiAgentChat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "Multi-agent response" {
		t.Errorf("body = %q, want Multi-agent response", w.Body.String())
	}
}

func TestInsertKnowledgeEndpointSuccess(t *testing.T) {
	svc := &mockChatService{insertResult: "插入成功"}
	r := setupRouter(svc)

	body := `{"question":"Q1","answer":"A1","sourceName":"test.md"}`
	req := httptest.NewRequest(http.MethodPost, "/api/insert", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if svc.lastInsertQ != "Q1" {
		t.Errorf("question = %q, want Q1", svc.lastInsertQ)
	}
	if svc.lastInsertA != "A1" {
		t.Errorf("answer = %q, want A1", svc.lastInsertA)
	}
	if svc.lastInsertSrc != "test.md" {
		t.Errorf("sourceName = %q, want test.md", svc.lastInsertSrc)
	}
}

func TestInsertKnowledgeEndpointBadJSON(t *testing.T) {
	svc := &mockChatService{}
	r := setupRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/insert", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFullMiddlewareChain(t *testing.T) {
	svc := &mockChatService{chatResult: "ok"}
	r := gin.New()
	r.Use(middleware.CORS())
	r.Use(middleware.Observability())
	r.Use(middleware.Guardrail())
	r.Use(middleware.ErrorHandler())

	h := NewChatHandler(svc)
	api := r.Group("/api")
	RegisterRoutes(api, h)

	// Normal request should pass through all middleware
	body := `{"sessionId":42,"userId":7,"prompt":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// Observability middleware should set X-Request-ID
	if rid := w.Header().Get("X-Request-ID"); rid == "" {
		t.Error("X-Request-ID header should be set by Observability middleware")
	}
}

func TestChatRequestValidation(t *testing.T) {
	svc := &mockChatService{}
	r := setupRouter(svc)

	tests := []struct {
		name  string
		body  string
		want  int
	}{
		{"empty body", ``, http.StatusBadRequest},
		{"missing prompt", `{"sessionId":1,"userId":2}`, http.StatusOK}, // prompt defaults to "" which is valid JSON
		{"wrong type sessionId", `{"sessionId":"abc","userId":2,"prompt":"hi"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

// Verify the model DTOs match what the handler expects
func TestModelDTOsIntegration(t *testing.T) {
	chatJSON := `{"sessionId":100,"userId":200,"prompt":"test prompt"}`
	var chatReq model.ChatRequest
	if err := json.Unmarshal([]byte(chatJSON), &chatReq); err != nil {
		t.Fatalf("ChatRequest unmarshal: %v", err)
	}

	knowledgeJSON := `{"question":"What?","answer":"That.","sourceName":"doc.md"}`
	var knowledgeReq model.KnowledgeRequest
	if err := json.Unmarshal([]byte(knowledgeJSON), &knowledgeReq); err != nil {
		t.Fatalf("KnowledgeRequest unmarshal: %v", err)
	}
}
