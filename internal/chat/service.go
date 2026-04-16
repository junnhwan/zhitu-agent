package chat

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// Service implements the core chat logic, mirroring Java AiChat + AiChatService.
// It loads system prompt, calls Qwen ChatModel, and will later integrate
// memory, RAG, and tools.
type Service struct {
	chatModel    model.ChatModel
	systemPrompt string
}

// NewService creates a ChatService with the given Qwen chat model.
// System prompt is loaded from the file specified in config.
func NewService(cfg *config.Config) (*Service, error) {
	ctx := context.Background()

	chatModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  cfg.DashScope.APIKey,
		Model:   cfg.DashScope.ChatModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create qwen chat model: %w", err)
	}

	// Load system prompt
	systemPrompt, err := loadSystemPrompt()
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %w", err)
	}

	return &Service{
		chatModel:    chatModel,
		systemPrompt: systemPrompt,
	}, nil
}

// Chat corresponds to Java aiChat.chat(sessionId, prompt).
// Returns the AI reply as plain text.
func (s *Service) Chat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	// Build message list: system prompt + user message
	// Memory will be injected here in Phase 3
	messages := []*schema.Message{
		schema.SystemMessage(s.systemPrompt),
		schema.UserMessage(prompt),
	}

	resp, err := s.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat model generate failed: %w", err)
	}

	return resp.Content, nil
}

// StreamChat corresponds to Java aiChat.streamChat(sessionId, prompt).
// Returns a StreamReader of message chunks for SSE streaming.
func (s *Service) StreamChat(ctx context.Context, sessionID int64, prompt string) (*schema.StreamReader[*schema.Message], error) {
	messages := []*schema.Message{
		schema.SystemMessage(s.systemPrompt),
		schema.UserMessage(prompt),
	}

	stream, err := s.chatModel.Stream(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("chat model stream failed: %w", err)
	}

	return stream, nil
}

// loadSystemPrompt reads the system prompt file.
// Mirrors Java @SystemMessage(fromResource = "system-prompt/chat-bot.txt")
func loadSystemPrompt() (string, error) {
	// Check env override first
	if path := os.Getenv("SYSTEM_PROMPT_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Default: system-prompt/chat-bot.txt relative to working directory
	data, err := os.ReadFile("system-prompt/chat-bot.txt")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
