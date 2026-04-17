package chat

import "context"

// ChatService defines the interface for chat operations.
// Extracted from Service to enable handler testing with mocks.
type ChatService interface {
	Chat(ctx context.Context, sessionID int64, prompt string) (string, error)
	StreamChat(ctx context.Context, sessionID int64, prompt string, onChunk func(content string)) error
	MultiAgentChat(ctx context.Context, sessionID int64, prompt string) (string, error)
	InsertKnowledge(ctx context.Context, question, answer, sourceName string) (string, error)
}

// Verify Service implements ChatService at compile time.
var _ ChatService = (*Service)(nil)
