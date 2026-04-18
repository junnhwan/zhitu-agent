package memory

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultMicroCompactThreshold = 2000
	ragTopN                      = 3
	ragSeparator                 = "\n\n---\n\n"
	truncatedFallbackLen         = 16
	microSummaryPrompt           = "用一句话概括工具 %s 的输出，不超过 50 字：\n%s"
)

type MicroCompactor struct {
	Threshold int
	LLM       model.BaseChatModel
}

func (c *MicroCompactor) Compact(ctx context.Context, toolName, result string) string {
	threshold := c.Threshold
	if threshold <= 0 {
		threshold = defaultMicroCompactThreshold
	}
	if len(result) < threshold {
		return result
	}

	switch toolName {
	case "rag_search", "ragTool", "RagTool":
		return compactRagTopN(result, ragTopN)
	case "send_email", "emailTool", "EmailTool":
		return summarizeEmail(result)
	default:
		return c.fallback(ctx, toolName, result, threshold)
	}
}

func (c *MicroCompactor) MessageForMemory(ctx context.Context, msg *schema.Message) *schema.Message {
	if msg == nil || msg.Role != schema.Tool {
		return msg
	}
	compacted := c.Compact(ctx, msg.ToolName, msg.Content)
	if compacted == msg.Content {
		return msg
	}
	clone := *msg
	clone.Content = compacted
	return &clone
}

func compactRagTopN(result string, n int) string {
	parts := strings.Split(result, ragSeparator)
	if len(parts) <= n {
		return result
	}
	return strings.Join(parts[:n], ragSeparator)
}

func summarizeEmail(result string) string {
	to := ""
	for _, kw := range []string{"收件人:", "收件人：", "to:", "To:"} {
		if idx := strings.Index(result, kw); idx >= 0 {
			tail := result[idx+len(kw):]
			if sep := strings.IndexAny(tail, ",，\n"); sep > 0 {
				to = strings.TrimSpace(tail[:sep])
			} else {
				to = strings.TrimSpace(tail)
			}
			break
		}
	}
	if to == "" {
		return "邮件已处理"
	}
	return "邮件已发送至 " + to
}

func (c *MicroCompactor) fallback(ctx context.Context, toolName, result string, threshold int) string {
	if c.LLM == nil {
		return truncateWithEllipsis(result, threshold)
	}
	resp, err := c.LLM.Generate(ctx, []*schema.Message{
		schema.UserMessage(fmt.Sprintf(microSummaryPrompt, toolName, result)),
	})
	if err != nil || resp == nil || resp.Content == "" {
		log.Printf("[MicroCompact] LLM summary failed for %s, fallback to truncate: %v", toolName, err)
		return truncateWithEllipsis(result, threshold)
	}
	return resp.Content
}

func truncateWithEllipsis(s string, threshold int) string {
	limit := truncatedFallbackLen
	if threshold > 0 && threshold/10 > limit {
		limit = threshold / 10
	}
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
