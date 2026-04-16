package agent

import (
	"context"
	"log"
	"strings"
)

// Knowledge keywords — mirrors Java SimpleOrchestrator.KNOWLEDGE_KEYWORDS
var knowledgeKeywords = []string{
	"查询", "了解", "什么是", "介绍", "解释", "说明",
}

// SimpleOrchestrator coordinates multiple agents to handle user requests.
// Mirrors Java SimpleOrchestrator — keyword detection → KnowledgeAgent → ReasoningAgent.
type SimpleOrchestrator struct {
	knowledgeAgent *KnowledgeAgent
	reasoningAgent *ReasoningAgent
}

// NewSimpleOrchestrator creates an orchestrator with the given agents.
func NewSimpleOrchestrator(knowledgeAgent *KnowledgeAgent, reasoningAgent *ReasoningAgent) *SimpleOrchestrator {
	return &SimpleOrchestrator{
		knowledgeAgent: knowledgeAgent,
		reasoningAgent: reasoningAgent,
	}
}

// Process handles a user request by coordinating agents.
// Mirrors Java SimpleOrchestrator.process(sessionId, userInput).
func (o *SimpleOrchestrator) Process(ctx context.Context, sessionID int64, userInput string) string {
	log.Printf("[SimpleOrchestrator] processing request, sessionID: %d", sessionID)

	enhancedInput := userInput

	// Check if knowledge retrieval is needed
	if o.needKnowledgeRetrieval(userInput) {
		log.Printf("[SimpleOrchestrator] knowledge query detected, calling KnowledgeAgent")

		knowledgeResult := o.knowledgeAgent.Execute(ctx, sessionID, userInput)
		if knowledgeResult != "" {
			enhancedInput = "参考知识：\n" + knowledgeResult + "\n\n用户问题：" + userInput
			log.Printf("[SimpleOrchestrator] knowledge retrieval successful, enhanced input")
		} else {
			log.Printf("[SimpleOrchestrator] no knowledge results, using original input")
		}
	} else {
		log.Printf("[SimpleOrchestrator] no knowledge retrieval needed, direct reasoning")
	}

	// Call reasoning agent for final response
	finalResult := o.reasoningAgent.Execute(ctx, sessionID, enhancedInput)
	log.Printf("[SimpleOrchestrator] processing complete")

	return finalResult
}

// needKnowledgeRetrieval checks if the input contains any knowledge keywords.
// Mirrors Java SimpleOrchestrator.needKnowledgeRetrieval(input).
func (o *SimpleOrchestrator) needKnowledgeRetrieval(input string) bool {
	for _, keyword := range knowledgeKeywords {
		if strings.Contains(input, keyword) {
			return true
		}
	}
	return false
}
