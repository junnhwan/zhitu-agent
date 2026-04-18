package agent

import (
	"context"
	"log"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/zhitu-agent/zhitu-agent/internal/understand"
)

var knowledgeKeywords = []string{
	"查询", "了解", "什么是", "介绍", "解释", "说明",
}

type IntentRouter interface {
	Understand(ctx context.Context, sessionID int64, history []*schema.Message, query string) (*understand.Result, error)
}

type SimpleOrchestrator struct {
	knowledgeAgent *KnowledgeAgent
	reasoningAgent *ReasoningAgent
	intentRouter   IntentRouter
}

func NewSimpleOrchestrator(knowledgeAgent *KnowledgeAgent, reasoningAgent *ReasoningAgent) *SimpleOrchestrator {
	return &SimpleOrchestrator{
		knowledgeAgent: knowledgeAgent,
		reasoningAgent: reasoningAgent,
	}
}

func (o *SimpleOrchestrator) WithIntentRouter(r IntentRouter) *SimpleOrchestrator {
	o.intentRouter = r
	return o
}

func (o *SimpleOrchestrator) Process(ctx context.Context, sessionID int64, userInput string) string {
	log.Printf("[SimpleOrchestrator] processing request, sessionID: %d", sessionID)

	enhancedInput := userInput
	needKnowledge := o.routeNeedsKnowledge(ctx, sessionID, userInput)

	if needKnowledge {
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

	finalResult := o.reasoningAgent.Execute(ctx, sessionID, enhancedInput)
	log.Printf("[SimpleOrchestrator] processing complete")

	return finalResult
}

func (o *SimpleOrchestrator) routeNeedsKnowledge(ctx context.Context, sessionID int64, userInput string) bool {
	if o.intentRouter == nil {
		return o.needKnowledgeRetrieval(userInput)
	}
	res, err := o.intentRouter.Understand(ctx, sessionID, nil, userInput)
	if err != nil || res == nil || res.Fallback {
		log.Printf("[SimpleOrchestrator] intent router unavailable, falling back to keyword: %v", err)
		return o.needKnowledgeRetrieval(userInput)
	}
	log.Printf("[SimpleOrchestrator] intent: domain=%s category=%s confidence=%.2f", res.Intent.Domain, res.Intent.Category, res.Intent.Confidence)
	return res.Intent.Domain == "KNOWLEDGE"
}

func (o *SimpleOrchestrator) needKnowledgeRetrieval(input string) bool {
	for _, keyword := range knowledgeKeywords {
		if strings.Contains(input, keyword) {
			return true
		}
	}
	return false
}
