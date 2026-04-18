package memory

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const defaultSummaryPrompt = `请对以下对话历史做简洁摘要。要求：
- 保留用户明确陈述的事实（姓名、偏好、任务目标）
- 保留关键工具调用结果
- 去除寒暄与重复
- 不超过 300 字

对话历史：
%s`

type LLMCompressor struct {
	llm       model.BaseChatModel
	fallback  *TokenCountCompressor
	recent    int
	threshold int
	prompt    string
}

func NewLLMCompressor(llm model.BaseChatModel, fallback *TokenCountCompressor, recent, threshold int, prompt string) *LLMCompressor {
	if prompt == "" {
		prompt = defaultSummaryPrompt
	}
	return &LLMCompressor{llm: llm, fallback: fallback, recent: recent, threshold: threshold, prompt: prompt}
}

func (c *LLMCompressor) Compress(ctx context.Context, messages []*schema.Message) []*schema.Message {
	if len(messages) <= c.threshold {
		return messages
	}

	split := len(messages) - c.recent
	old := messages[:split]
	recent := messages[split:]

	var oldText strings.Builder
	for _, m := range old {
		oldText.WriteString(string(m.Role))
		oldText.WriteString(": ")
		oldText.WriteString(m.Content)
		oldText.WriteString("\n")
	}

	resp, err := c.llm.Generate(ctx, []*schema.Message{
		schema.UserMessage(fmt.Sprintf(c.prompt, oldText.String())),
	})
	if err != nil || resp == nil || resp.Content == "" {
		log.Printf("[LLMCompressor] summary failed, fallback: %v", err)
		return c.fallback.Compress(ctx, messages)
	}

	out := make([]*schema.Message, 0, 1+len(recent))
	out = append(out, schema.SystemMessage("历史摘要："+resp.Content))
	out = append(out, recent...)
	log.Printf("[LLMCompressor] %d msgs -> %d msgs (1 summary + %d recent)", len(messages), len(out), len(recent))
	return out
}

func (c *LLMCompressor) EstimateTokens(messages []*schema.Message) int {
	return c.fallback.EstimateTokens(messages)
}
