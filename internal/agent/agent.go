package agent

import "context"

// Agent is the base interface for all agents.
// Mirrors Java Agent interface — execute(sessionId, input) and getAgentName().
type Agent interface {
	// Execute runs the agent task and returns the result.
	Execute(ctx context.Context, sessionID int64, input string) string

	// AgentName returns the name of the agent.
	AgentName() string
}
