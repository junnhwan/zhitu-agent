package understand

import (
	"context"
	"log"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const defaultRewritePrompt = `你是 query 改写助手。根据对话历史，把用户的最新输入改写为可独立理解的完整 query。
要求：
- 消解指代（"它"/"这个"/"上面那个"）为具体名词
- 不改变原意，不增加新信息
- 直接输出改写后的 query，不要任何解释、引号、前缀

对话历史：
%s

用户最新输入：%s`

const maxHistoryMessages = 6

type Rewriter struct {
	llm    model.BaseChatModel
	prompt string
}

func NewRewriter(llm model.BaseChatModel) *Rewriter {
	return &Rewriter{llm: llm, prompt: defaultRewritePrompt}
}

func (r *Rewriter) Rewrite(ctx context.Context, history []*schema.Message, query string) (string, error) {
	if len(history) == 0 {
		return query, nil
	}

	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}

	var buf strings.Builder
	for _, m := range history {
		buf.WriteString(string(m.Role))
		buf.WriteString(": ")
		buf.WriteString(m.Content)
		buf.WriteString("\n")
	}

	prompt := strings.Replace(r.prompt, "%s", buf.String(), 1)
	prompt = strings.Replace(prompt, "%s", query, 1)

	resp, err := r.llm.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil || resp == nil || resp.Content == "" {
		log.Printf("[Rewriter] failed, fallback to original: %v", err)
		return query, nil
	}
	return strings.TrimSpace(resp.Content), nil
}
