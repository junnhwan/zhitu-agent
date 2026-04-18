package understand

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type IntentResult struct {
	Domain     string  `json:"domain"`
	Category   string  `json:"category"`
	Topic      string  `json:"topic,omitempty"`
	Confidence float64 `json:"confidence"`
}

const defaultClassifyPrompt = `你是意图分类器。根据下面的意图树，判断用户输入属于哪个 domain 和 category。
只输出 JSON，不要任何解释、Markdown 代码块或前后缀文本。

意图树：
%s

输出格式：{"domain":"...","category":"...","confidence":0.0-1.0}

用户输入：%s`

type Classifier struct {
	llm    model.BaseChatModel
	tree   *Tree
	prompt string
}

func NewClassifier(llm model.BaseChatModel, tree *Tree) *Classifier {
	return &Classifier{llm: llm, tree: tree, prompt: defaultClassifyPrompt}
}

var jsonExtractRe = regexp.MustCompile(`\{[^{}]*\}`)

func (c *Classifier) Classify(ctx context.Context, query string) (*IntentResult, error) {
	prompt := fmt.Sprintf(c.prompt, renderTreeToPrompt(c.tree), query)
	resp, err := c.llm.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil || resp == nil || resp.Content == "" {
		log.Printf("[Classifier] LLM error, fallback CHITCHAT: %v", err)
		return &IntentResult{Domain: "CHITCHAT", Confidence: 0}, nil
	}

	res := parseIntent(resp.Content)
	if res == nil || !c.isValidDomain(res.Domain) {
		log.Printf("[Classifier] invalid result %q, fallback CHITCHAT", resp.Content)
		return &IntentResult{Domain: "CHITCHAT", Confidence: 0}, nil
	}
	return res, nil
}

func parseIntent(text string) *IntentResult {
	text = strings.TrimSpace(text)
	if r := tryUnmarshal(text); r != nil {
		return r
	}
	if m := jsonExtractRe.FindString(text); m != "" {
		return tryUnmarshal(m)
	}
	return nil
}

func tryUnmarshal(s string) *IntentResult {
	var r IntentResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil
	}
	if r.Domain == "" {
		return nil
	}
	return &r
}

func (c *Classifier) isValidDomain(name string) bool {
	for _, d := range c.tree.Domains {
		if d.Name == name {
			return true
		}
	}
	return false
}

func renderTreeToPrompt(tree *Tree) string {
	var b strings.Builder
	for _, d := range tree.Domains {
		b.WriteString("- ")
		b.WriteString(d.Name)
		if d.Description != "" {
			b.WriteString("：")
			b.WriteString(d.Description)
		}
		b.WriteString("\n")
		for _, cat := range d.Categories {
			b.WriteString("  - ")
			b.WriteString(cat.Name)
			b.WriteString("\n")
		}
	}
	return b.String()
}
