package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zhitu-agent/zhitu-agent/internal/chat"
	"github.com/zhitu-agent/zhitu-agent/internal/common"
	"github.com/zhitu-agent/zhitu-agent/internal/model"
)

// ChatHandler handles the 4 core API endpoints, mirroring Java AiChatController.
type ChatHandler struct {
	chatService *chat.Service
}

// NewChatHandler creates a ChatHandler with the given chat service.
func NewChatHandler(chatService *chat.Service) *ChatHandler {
	return &ChatHandler{
		chatService: chatService,
	}
}

// RegisterRoutes registers all API routes on the given router group.
// Mirrors Java controller endpoints under /api context path.
func RegisterRoutes(api *gin.RouterGroup, h *ChatHandler) {
	api.POST("/chat", h.Chat)
	api.POST("/streamChat", h.StreamChat)
	api.POST("/multiAgentChat", h.MultiAgentChat)
	api.POST("/insert", h.InsertKnowledge)
}

// Chat handles POST /api/chat — returns plain text on success.
// Mirrors Java: aiChat.chat(sessionId, prompt)
func (h *ChatHandler) Chat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.Error(common.ParamsError, err.Error()))
		return
	}

	// Set monitoring context values
	c.Set("user_id", req.UserID)
	c.Set("session_id", req.SessionID)

	ctx := c.Request.Context()
	result, err := h.chatService.Chat(ctx, req.SessionID, req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, common.Error(common.AIModelError, err.Error()))
		return
	}

	// Mixed response contract: success → plain text
	c.String(http.StatusOK, result)
}

// StreamChat handles POST /api/streamChat — returns SSE stream on success.
// Mirrors Java: aiChat.streamChat(sessionId, prompt)
// Enhanced with tool call support: tool calls are handled internally by the service,
// and only text content is streamed to the client.
func (h *ChatHandler) StreamChat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.Error(common.ParamsError, err.Error()))
		return
	}

	c.Set("user_id", req.UserID)
	c.Set("session_id", req.SessionID)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx := c.Request.Context()
	err := h.chatService.StreamChat(ctx, req.SessionID, req.Prompt, func(content string) {
		c.SSEvent("", content)
		c.Writer.Flush()
	})
	if err != nil {
		// Error during stream setup — return JSON error per mixed contract
		c.JSON(http.StatusInternalServerError, common.Error(common.AIModelError, err.Error()))
		return
	}
}

// MultiAgentChat handles POST /api/multiAgentChat — returns plain text on success.
// Mirrors Java: simpleOrchestrator.process(sessionId, prompt)
func (h *ChatHandler) MultiAgentChat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.Error(common.ParamsError, err.Error()))
		return
	}

	c.Set("user_id", req.UserID)
	c.Set("session_id", req.SessionID)

	ctx := c.Request.Context()
	result, err := h.chatService.MultiAgentChat(ctx, req.SessionID, req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, common.Error(common.AIModelError, err.Error()))
		return
	}

	c.String(http.StatusOK, result)
}

// InsertKnowledge handles POST /api/insert — returns plain text on success.
// Mirrors Java: insertKnowledge(knowledgeRequest)
func (h *ChatHandler) InsertKnowledge(c *gin.Context) {
	var req model.KnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.Error(common.ParamsError, err.Error()))
		return
	}

	ctx := c.Request.Context()
	result, err := h.chatService.InsertKnowledge(ctx, req.Question, req.Answer, req.SourceName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, common.Error(common.AIModelError, err.Error()))
		return
	}

	// Mixed response contract: success → plain text
	c.String(http.StatusOK, result)
}
