//go:build eval

package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/joho/godotenv"
)

// ---------- input schema ----------

type evalTemplate struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Messages    []evalMessage  `json:"messages"`
	Facts       []evalFact     `json:"facts,omitempty"`
}

type evalMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	ToolName string `json:"tool_name,omitempty"`
}

type evalFact struct {
	Question string `json:"question"`
	Expected string `json:"expected"`
}

func (em evalMessage) toSchema() *schema.Message {
	switch strings.ToLower(em.Role) {
	case "user":
		return schema.UserMessage(em.Content)
	case "assistant":
		return schema.AssistantMessage(em.Content, nil)
	case "system":
		return schema.SystemMessage(em.Content)
	case "tool":
		return schema.ToolMessage(em.Content, "", schema.WithToolName(em.ToolName))
	default:
		return &schema.Message{Role: schema.RoleType(em.Role), Content: em.Content}
	}
}

func loadMemoryTemplates(t *testing.T, path string) []*evalTemplate {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open templates: %v", err)
	}
	defer f.Close()
	var out []*evalTemplate
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var tmpl evalTemplate
		if err := json.Unmarshal([]byte(line), &tmpl); err != nil {
			t.Fatalf("bad template line: %v", err)
		}
		out = append(out, &tmpl)
	}
	return out
}

// ---------- output schema ----------

type templateScore struct {
	TemplateID       string         `json:"template_id"`
	Triggered        bool           `json:"triggered"`
	OriginalCount    int            `json:"original_count"`
	CompressedCount  int            `json:"compressed_count"`
	OriginalTokens   int            `json:"original_tokens"`
	CompressedTokens int            `json:"compressed_tokens"`
	CompressionRatio float64        `json:"compression_ratio"`
	RecentFidelity   bool           `json:"recent_fidelity"`
	FactRetention    *float64       `json:"fact_retention,omitempty"`
	FactDetails      []factResult   `json:"fact_details,omitempty"`
	Note             string         `json:"note,omitempty"`
}

type factResult struct {
	Question string `json:"question"`
	Expected string `json:"expected"`
	Answer   string `json:"answer"`
	Retained bool   `json:"retained"`
}

type strategyReport struct {
	Strategy  string           `json:"strategy"`
	Status    string           `json:"status"` // ok | skipped
	Skipped   string           `json:"skipped_reason,omitempty"`
	Templates []templateScore  `json:"templates,omitempty"`
	Aggregate map[string]any   `json:"aggregate,omitempty"`
}

type memoryReport struct {
	Timestamp     string           `json:"timestamp"`
	GoldenSource  string           `json:"golden_source"`
	TemplateCount int              `json:"template_count"`
	LLMJudge      bool             `json:"llm_judge_enabled"`
	Strategies    []strategyReport `json:"strategies"`
}

// ---------- harness ----------

const factJudgePrompt = `以下是某段对话的压缩版。请根据这段内容回答问题，不要编造。如果信息不在其中，回答"不知道"。

对话内容：
%s

问题：%s`

// TestMemoryEval runs each compression strategy over the conversation templates
// and writes a scorecard to docs/eval/reports/memory-latest.json.
//
// Run (offline, simple only):
//   go test -tags=eval ./internal/memory/ -run TestMemoryEval -v
// Run (with LLM strategies):
//   DASHSCOPE_API_KEY=... go test -tags=eval ./internal/memory/ -run TestMemoryEval -v
// Run (with fact-retention judge, costs API tokens):
//   DASHSCOPE_API_KEY=... MEM_EVAL_LLM_JUDGE=true go test -tags=eval ./internal/memory/ -run TestMemoryEval -v
func TestMemoryEval(t *testing.T) {
	_ = godotenv.Load("../../.env")

	templates := loadMemoryTemplates(t, "../../docs/eval/memory/conversation_seed.jsonl")
	t.Logf("loaded %d conversation templates", len(templates))

	apiKey := firstNonEmptyMem(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"))
	llmEnabled := apiKey != ""
	judgeEnabled := llmEnabled && os.Getenv("MEM_EVAL_LLM_JUDGE") == "true"

	var sharedLLM model.BaseChatModel
	if llmEnabled {
		summaryModel := firstNonEmptyMem(os.Getenv("MEM_EVAL_SUMMARY_MODEL"), "qwen-turbo")
		var err error
		sharedLLM, err = qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKey:  apiKey,
			Model:   summaryModel,
		})
		if err != nil {
			t.Fatalf("build summary LLM: %v", err)
		}
	}

	var judgeLLM model.BaseChatModel
	if judgeEnabled {
		judgeModel := firstNonEmptyMem(os.Getenv("MEM_EVAL_JUDGE_MODEL"), "qwen-turbo")
		var err error
		judgeLLM, err = qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKey:  apiKey,
			Model:   judgeModel,
		})
		if err != nil {
			t.Fatalf("build judge LLM: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	reports := []strategyReport{
		evalStrategy(t, ctx, "simple", templates, buildSimpleCompressor(), nil, judgeLLM),
	}

	if llmEnabled {
		reports = append(reports,
			evalStrategy(t, ctx, "llm_summary", templates, buildLLMCompressor(sharedLLM), nil, judgeLLM),
			evalStrategy(t, ctx, "hybrid", templates, buildLLMCompressor(sharedLLM), buildMicroCompactor(sharedLLM), judgeLLM),
		)
	} else {
		reports = append(reports,
			strategyReport{Strategy: "llm_summary", Status: "skipped", Skipped: "DASHSCOPE_API_KEY / QWEN_API_KEY not set"},
			strategyReport{Strategy: "hybrid", Status: "skipped", Skipped: "DASHSCOPE_API_KEY / QWEN_API_KEY not set"},
		)
	}

	report := memoryReport{
		Timestamp:     time.Now().Format("2006-01-02T15:04:05Z07:00"),
		GoldenSource:  "docs/eval/memory/conversation_seed.jsonl",
		TemplateCount: len(templates),
		LLMJudge:      judgeEnabled,
		Strategies:    reports,
	}
	writeMemoryScorecard(t, report)
}

func buildSimpleCompressor() Compressor {
	return NewTokenCountCompressor(5, 2000)
}

func buildLLMCompressor(llm model.BaseChatModel) Compressor {
	fallback := NewTokenCountCompressor(5, 2000)
	return NewLLMCompressor(llm, fallback, defaultSummaryRecent, defaultSummaryThreshold, "")
}

func buildMicroCompactor(llm model.BaseChatModel) *MicroCompactor {
	return &MicroCompactor{Threshold: 200, LLM: llm}
}

func evalStrategy(t *testing.T, ctx context.Context, name string, tmpls []*evalTemplate, c Compressor, mc *MicroCompactor, judge model.BaseChatModel) strategyReport {
	t.Helper()
	scores := make([]templateScore, 0, len(tmpls))
	for _, tmpl := range tmpls {
		scores = append(scores, evalOneTemplate(t, ctx, c, mc, tmpl, judge))
	}
	agg := aggregateStrategy(scores)
	t.Logf("[%s] %s", name, summaryLine(agg))
	return strategyReport{Strategy: name, Status: "ok", Templates: scores, Aggregate: agg}
}

func evalOneTemplate(t *testing.T, ctx context.Context, c Compressor, mc *MicroCompactor, tmpl *evalTemplate, judge model.BaseChatModel) templateScore {
	original := make([]*schema.Message, 0, len(tmpl.Messages))
	for _, em := range tmpl.Messages {
		original = append(original, em.toSchema())
	}

	// MicroCompact tool messages before feeding compressor (mirrors CompressibleMemory.Add path).
	feed := original
	if mc != nil {
		feed = make([]*schema.Message, 0, len(original))
		for _, m := range original {
			feed = append(feed, mc.MessageForMemory(ctx, m))
		}
	}

	compressed := c.Compress(ctx, feed)
	origTokens := c.EstimateTokens(original)
	compTokens := c.EstimateTokens(compressed)
	ratio := 1.0
	if origTokens > 0 {
		ratio = float64(compTokens) / float64(origTokens)
	}
	triggered := len(compressed) != len(original) || !reflect.DeepEqual(compressed, original)

	score := templateScore{
		TemplateID:       tmpl.ID,
		Triggered:        triggered,
		OriginalCount:    len(original),
		CompressedCount:  len(compressed),
		OriginalTokens:   origTokens,
		CompressedTokens: compTokens,
		CompressionRatio: round4(ratio),
		RecentFidelity:   recentFidelity(original, compressed),
	}

	if judge != nil && len(tmpl.Facts) > 0 {
		rate, details := evalFactRetention(ctx, compressed, tmpl.Facts, judge)
		score.FactRetention = &rate
		score.FactDetails = details
	}
	return score
}

// recentFidelity 检查压缩后的"近端"是否与原始消息的末尾逐字相等。
// 未触发压缩：完全相等 → true。
// 触发压缩（典型：summary + last-N）：compressed[1:] 必须 == original 的末尾 N 条。
// 若 compressed 与 original 长度相同但内容不同，视为 fidelity 破损。
func recentFidelity(original, compressed []*schema.Message) bool {
	if len(compressed) == len(original) {
		return reflect.DeepEqual(compressed, original)
	}
	if len(compressed) == 0 {
		return false
	}
	tail := len(compressed) - 1 // 减去 summary
	if tail <= 0 || tail > len(original) {
		return false
	}
	return reflect.DeepEqual(compressed[1:], original[len(original)-tail:])
}

func evalFactRetention(ctx context.Context, compressed []*schema.Message, facts []evalFact, judge model.BaseChatModel) (float64, []factResult) {
	history := formatForJudge(compressed)
	out := make([]factResult, 0, len(facts))
	retained := 0
	for _, f := range facts {
		prompt := fmt.Sprintf(factJudgePrompt, history, f.Question)
		resp, err := judge.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
		answer := ""
		if err == nil && resp != nil {
			answer = strings.TrimSpace(resp.Content)
		}
		hit := strings.Contains(strings.ToLower(answer), strings.ToLower(f.Expected))
		if hit {
			retained++
		}
		out = append(out, factResult{Question: f.Question, Expected: f.Expected, Answer: answer, Retained: hit})
	}
	return float64(retained) / float64(len(facts)), out
}

func formatForJudge(msgs []*schema.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func aggregateStrategy(scores []templateScore) map[string]any {
	if len(scores) == 0 {
		return nil
	}
	triggered := 0
	ratioSum := 0.0
	fidelityOK := 0
	var factSum float64
	factN := 0
	for _, s := range scores {
		if s.Triggered {
			triggered++
			ratioSum += s.CompressionRatio
		}
		if s.RecentFidelity {
			fidelityOK++
		}
		if s.FactRetention != nil {
			factSum += *s.FactRetention
			factN++
		}
	}
	agg := map[string]any{
		"triggered_count":      triggered,
		"triggered_rate":       round4(float64(triggered) / float64(len(scores))),
		"recent_fidelity_rate": round4(float64(fidelityOK) / float64(len(scores))),
	}
	if triggered > 0 {
		agg["avg_ratio_when_triggered"] = round4(ratioSum / float64(triggered))
	}
	if factN > 0 {
		agg["avg_fact_retention"] = round4(factSum / float64(factN))
		agg["fact_retention_samples"] = factN
	}
	return agg
}

func summaryLine(agg map[string]any) string {
	if agg == nil {
		return "(no scores)"
	}
	parts := []string{
		fmt.Sprintf("triggered=%v", agg["triggered_count"]),
		fmt.Sprintf("fidelity=%v", agg["recent_fidelity_rate"]),
	}
	if r, ok := agg["avg_ratio_when_triggered"]; ok {
		parts = append(parts, fmt.Sprintf("avg_ratio=%v", r))
	}
	if r, ok := agg["avg_fact_retention"]; ok {
		parts = append(parts, fmt.Sprintf("fact_retention=%v", r))
	}
	return strings.Join(parts, " ")
}

func writeMemoryScorecard(t *testing.T, report memoryReport) {
	t.Helper()
	dir := "../../docs/eval/reports"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("mkdir reports failed: %v", err)
		return
	}
	body, _ := json.MarshalIndent(report, "", "  ")
	stamp := time.Now().Format("2006-01-02-1504")
	path := dir + "/memory-" + stamp + ".json"
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Logf("write memory scorecard failed: %v", err)
		return
	}
	_ = os.WriteFile(dir+"/memory-latest.json", body, 0o644)
	t.Logf("memory scorecard written: %s", path)
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func firstNonEmptyMem(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
