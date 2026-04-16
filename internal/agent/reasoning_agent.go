package agent

import (
	"context"
	"log"
)

// ReasoningAgent is responsible for conversation and reasoning.
// Mirrors Java ReasoningAgent — delegates to AiChat.chat(sessionId, input).
type ReasoningAgent struct {
	chatFn func(ctx context.Context, sessionID int64, input string) (string, error)
}

// NewReasoningAgent creates a ReasoningAgent with the given chat function.
func NewReasoningAgent(chatFn func(ctx context.Context, sessionID int64, input string) (string, error)) *ReasoningAgent {
	return &ReasoningAgent{chatFn: chatFn}
}

// Execute runs the reasoning agent by calling the chat function.
// Mirrors Java ReasoningAgent.execute(sessionId, input) — calls aiChat.chat(sessionId, input).
func (a *ReasoningAgent) Execute(ctx context.Context, sessionID int64, input string) string {
	log.Printf("[ReasoningAgent] executing reasoning, sessionID: %d", sessionID)

	result, err := a.chatFn(ctx, sessionID, input)
	if err != nil {
		log.Printf("[ReasoningAgent] reasoning failed: %v", err)
		return "推理失败：" + err.Error()
	}

	log.Printf("[ReasoningAgent] reasoning complete")
	return result
}

// AgentName returns the agent name.
func (a *ReasoningAgent) AgentName() string {
	return "ReasoningAgent"
}
