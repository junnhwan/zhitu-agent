//go:build eval

package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/joho/godotenv"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag"
)

// ---------- input schema ----------

type workflowQuery struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Query    string `json:"query"`
}

func loadWorkflowQueries(t *testing.T, path string) []workflowQuery {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open query set: %v", err)
	}
	defer f.Close()
	var out []workflowQuery
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var q workflowQuery
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Fatalf("bad query line: %v", err)
		}
		out = append(out, q)
	}
	return out
}

// ---------- output schema ----------

type perPathResult struct {
	Reply            string `json:"reply"`
	LatencyMs        int64  `json:"latency_ms"`
	ReplyLen         int    `json:"reply_len"`
	ToolCalls        int    `json:"tool_calls"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Err              string `json:"error,omitempty"`
}

type queryResult struct {
	QueryID     string        `json:"query_id"`
	Category    string        `json:"category"`
	Query       string        `json:"query"`
	Legacy      perPathResult `json:"legacy"`
	Graph       perPathResult `json:"graph"`
	Verdict     string        `json:"verdict"`      // A | B | T | ?
	VerdictNote string        `json:"verdict_note"` // judge 的一句话理由
}

type categoryAggregate struct {
	Count             int     `json:"count"`
	WinsLegacy        int     `json:"wins_legacy"`
	WinsGraph         int     `json:"wins_graph"`
	Ties              int     `json:"ties"`
	Undecided         int     `json:"undecided"`
	LegacyAvgLatency  float64 `json:"legacy_avg_latency_ms"`
	GraphAvgLatency   float64 `json:"graph_avg_latency_ms"`
	LegacyAvgTokens   float64 `json:"legacy_avg_total_tokens"`
	GraphAvgTokens    float64 `json:"graph_avg_total_tokens"`
	LegacyAvgToolCalls float64 `json:"legacy_avg_tool_calls"`
	GraphAvgToolCalls  float64 `json:"graph_avg_tool_calls"`
}

type workflowReport struct {
	Timestamp      string                       `json:"timestamp"`
	GoldenSource   string                       `json:"golden_source"`
	QueryCount     int                          `json:"query_count"`
	JudgeModel     string                       `json:"judge_model"`
	Categories     map[string]categoryAggregate `json:"categories"`
	Overall        categoryAggregate            `json:"overall"`
	Queries        []queryResult                `json:"queries"`
	Notes          []string                     `json:"notes,omitempty"`
}

// ---------- harness ----------

const workflowJudgePrompt = `你是一个客观的评审员。以下是用户的问题和两个系统的回答，请判断哪个回答更好。
评分维度：是否准确回答了问题、是否与问题相关、是否清晰完整、是否避免了编造。

用户问题：%s

系统 A 的回答：
%s

系统 B 的回答：
%s

请只输出一行，格式为 "X | 理由"，其中 X 是 A（A 更好）、B（B 更好）或 T（差不多）。理由不超过 30 字。`

// TestWorkflowBenchmark runs every query twice (legacy path + graph path),
// captures latency / token usage / tool-call count via Eino callbacks,
// then asks qwen-max to pairwise-judge which reply is better.
// Writes docs/eval/reports/workflow-latest.json.
//
// Required env:
//
//	DASHSCOPE_API_KEY (or QWEN_API_KEY)
//
// Optional:
//
//	WORKFLOW_EVAL_JUDGE_MODEL    default qwen-max
//	WORKFLOW_EVAL_LIMIT=N        only run first N queries (smoke)
//	WORKFLOW_EVAL_SKIP_JUDGE=1   skip LLM judge (use when just measuring latency)
//	RAG_RELOAD_DOCS=true         rebuild RediSearch index before run
//	SYSTEM_PROMPT_PATH           override system prompt location
func TestWorkflowBenchmark(t *testing.T) {
	_ = godotenv.Load("../../.env")
	apiKey := firstNonEmptyWF(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"))
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY / QWEN_API_KEY not set")
	}

	// Default SYSTEM_PROMPT_PATH so Service can find it from internal/chat cwd.
	if os.Getenv("SYSTEM_PROMPT_PATH") == "" {
		_ = os.Setenv("SYSTEM_PROMPT_PATH", "../../system-prompt/chat-bot.txt")
	}

	queries := loadWorkflowQueries(t, "../../docs/eval/workflow/query_set.jsonl")
	if limit := envInt("WORKFLOW_EVAL_LIMIT", 0); limit > 0 && limit < len(queries) {
		queries = queries[:limit]
		t.Logf("WORKFLOW_EVAL_LIMIT=%d — truncated query set", limit)
	}
	t.Logf("loaded %d queries", len(queries))

	cfg := buildBenchConfig(t, apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	ragSys, err := rag.NewRAG(ctx, cfg, monitor.DefaultRegistry.AiMetrics)
	if err != nil {
		t.Fatalf("build RAG: %v", err)
	}
	if os.Getenv("RAG_RELOAD_DOCS") == "true" {
		ragSys.Startup(ctx)
	}

	legacyCfg := *cfg
	legacyCfg.Chat.WorkflowMode = "legacy"
	graphCfg := *cfg
	graphCfg.Chat.WorkflowMode = "graph"

	legacySvc, err := NewService(&legacyCfg, ragSys)
	if err != nil {
		t.Fatalf("build legacy service: %v", err)
	}
	legacySvc.InitOrchestrator()
	defer legacySvc.Shutdown()

	graphSvc, err := NewService(&graphCfg, ragSys)
	if err != nil {
		t.Fatalf("build graph service: %v", err)
	}
	graphSvc.InitOrchestrator()
	defer graphSvc.Shutdown()

	skipJudge := os.Getenv("WORKFLOW_EVAL_SKIP_JUDGE") == "1"
	judgeModel := firstNonEmptyWF(os.Getenv("WORKFLOW_EVAL_JUDGE_MODEL"), "qwen-max")
	var judge model.BaseChatModel
	if !skipJudge {
		judge, err = qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKey:  apiKey,
			Model:   judgeModel,
		})
		if err != nil {
			t.Fatalf("build judge model: %v", err)
		}
	}

	var sessionCounter atomic.Int64
	results := make([]queryResult, 0, len(queries))
	for i, q := range queries {
		t.Logf("[%d/%d] %s (%s) — %s", i+1, len(queries), q.ID, q.Category, truncate(q.Query, 40))
		qr := queryResult{QueryID: q.ID, Category: q.Category, Query: q.Query}
		qr.Legacy = runOnePath(ctx, t, legacySvc, q.Query, sessionCounter.Add(1))
		qr.Graph = runOnePath(ctx, t, graphSvc, q.Query, sessionCounter.Add(1))
		if !skipJudge {
			qr.Verdict, qr.VerdictNote = judgePair(ctx, judge, q.Query, qr.Legacy.Reply, qr.Graph.Reply)
		} else {
			qr.Verdict = "?"
		}
		t.Logf("    legacy: %dms/%dtok/%dtc   graph: %dms/%dtok/%dtc   verdict=%s",
			qr.Legacy.LatencyMs, qr.Legacy.TotalTokens, qr.Legacy.ToolCalls,
			qr.Graph.LatencyMs, qr.Graph.TotalTokens, qr.Graph.ToolCalls,
			qr.Verdict)
		results = append(results, qr)
	}

	report := workflowReport{
		Timestamp:    time.Now().Format("2006-01-02T15:04:05Z07:00"),
		GoldenSource: "docs/eval/workflow/query_set.jsonl",
		QueryCount:   len(results),
		JudgeModel:   judgeModel,
		Categories:   aggregateByCategory(results),
		Overall:      aggregateResults("overall", results),
		Queries:      results,
	}
	if skipJudge {
		report.Notes = append(report.Notes, "judge skipped (WORKFLOW_EVAL_SKIP_JUDGE=1)")
	}
	writeWorkflowScorecard(t, report)
}

// runOnePath executes a single Chat call with callback-based token/tool accounting.
func runOnePath(ctx context.Context, t *testing.T, svc *Service, prompt string, sessionID int64) perPathResult {
	tracker := &callbackTracker{}
	handler := callbacks.NewHandlerBuilder().
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			if out := model.ConvCallbackOutput(output); out != nil {
				if out.TokenUsage != nil {
					tracker.promptTokens += out.TokenUsage.PromptTokens
					tracker.completionTokens += out.TokenUsage.CompletionTokens
					tracker.totalTokens += out.TokenUsage.TotalTokens
				}
				if out.Message != nil && len(out.Message.ToolCalls) > 0 {
					tracker.toolCalls += len(out.Message.ToolCalls)
				}
			}
			return ctx
		}).Build()
	cctx := callbacks.InitCallbacks(ctx, &callbacks.RunInfo{}, handler)

	start := time.Now()
	reply, err := svc.Chat(cctx, sessionID, prompt)
	elapsed := time.Since(start)

	res := perPathResult{
		Reply:            reply,
		LatencyMs:        elapsed.Milliseconds(),
		ReplyLen:         len([]rune(reply)),
		ToolCalls:        tracker.toolCalls,
		PromptTokens:     tracker.promptTokens,
		CompletionTokens: tracker.completionTokens,
		TotalTokens:      tracker.totalTokens,
	}
	if err != nil {
		res.Err = err.Error()
	}
	return res
}

type callbackTracker struct {
	toolCalls        int
	promptTokens     int
	completionTokens int
	totalTokens      int
}

func judgePair(ctx context.Context, judge model.BaseChatModel, query, a, b string) (string, string) {
	if judge == nil {
		return "?", ""
	}
	prompt := fmt.Sprintf(workflowJudgePrompt, query, a, b)
	resp, err := judge.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil || resp == nil {
		return "?", fmt.Sprintf("judge error: %v", err)
	}
	line := strings.TrimSpace(resp.Content)
	if line == "" {
		return "?", "empty judge reply"
	}
	verdict := "?"
	for _, ch := range []byte{'A', 'B', 'T'} {
		if strings.HasPrefix(line, string(ch)) || strings.Contains(line[:min(4, len(line))], string(ch)) {
			verdict = string(ch)
			break
		}
	}
	note := line
	if idx := strings.Index(line, "|"); idx >= 0 {
		note = strings.TrimSpace(line[idx+1:])
	}
	return verdict, note
}

// ---------- aggregation ----------

func aggregateByCategory(results []queryResult) map[string]categoryAggregate {
	buckets := map[string][]queryResult{}
	for _, r := range results {
		buckets[r.Category] = append(buckets[r.Category], r)
	}
	out := map[string]categoryAggregate{}
	for cat, rs := range buckets {
		out[cat] = aggregateResults(cat, rs)
	}
	return out
}

func aggregateResults(label string, rs []queryResult) categoryAggregate {
	_ = label
	agg := categoryAggregate{Count: len(rs)}
	if len(rs) == 0 {
		return agg
	}
	var ll, lg, tl, tg float64
	var tcl, tcg float64
	for _, r := range rs {
		ll += float64(r.Legacy.LatencyMs)
		lg += float64(r.Graph.LatencyMs)
		tl += float64(r.Legacy.TotalTokens)
		tg += float64(r.Graph.TotalTokens)
		tcl += float64(r.Legacy.ToolCalls)
		tcg += float64(r.Graph.ToolCalls)
		switch r.Verdict {
		case "A":
			agg.WinsLegacy++
		case "B":
			agg.WinsGraph++
		case "T":
			agg.Ties++
		default:
			agg.Undecided++
		}
	}
	n := float64(len(rs))
	agg.LegacyAvgLatency = round2(ll / n)
	agg.GraphAvgLatency = round2(lg / n)
	agg.LegacyAvgTokens = round2(tl / n)
	agg.GraphAvgTokens = round2(tg / n)
	agg.LegacyAvgToolCalls = round2(tcl / n)
	agg.GraphAvgToolCalls = round2(tcg / n)
	return agg
}

func writeWorkflowScorecard(t *testing.T, report workflowReport) {
	t.Helper()
	dir := "../../docs/eval/reports"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("mkdir reports failed: %v", err)
		return
	}
	body, _ := json.MarshalIndent(report, "", "  ")
	stamp := time.Now().Format("2006-01-02-1504")
	path := dir + "/workflow-" + stamp + ".json"
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Logf("write workflow scorecard failed: %v", err)
		return
	}
	_ = os.WriteFile(dir+"/workflow-latest.json", body, 0o644)
	t.Logf("workflow scorecard: %s (overall legacy avg %vms / graph avg %vms)",
		path, report.Overall.LegacyAvgLatency, report.Overall.GraphAvgLatency)
}

// ---------- helpers ----------

func buildBenchConfig(t *testing.T, apiKey string) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.DashScope.APIKey = apiKey
	cfg.DashScope.ChatModel = firstNonEmptyWF(os.Getenv("QWEN_CHAT_MODEL"), "qwen-max")
	cfg.DashScope.EmbeddingModel = firstNonEmptyWF(os.Getenv("QWEN_EMBEDDING_MODEL"), "text-embedding-v3")
	cfg.DashScope.EmbeddingDimensions = 1024
	cfg.DashScope.RerankModel = firstNonEmptyWF(os.Getenv("QWEN_RERANK_MODEL"), "qwen3-rerank")

	host, port := "127.0.0.1", 6379
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		parts := strings.SplitN(addr, ":", 2)
		host = parts[0]
		if len(parts) == 2 {
			fmt.Sscanf(parts[1], "%d", &port)
		}
	}
	if v := os.Getenv("REDIS_HOST"); v != "" {
		host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &port)
	}
	cfg.Redis.Host = host
	cfg.Redis.Port = port
	cfg.Redis.Password = os.Getenv("REDIS_PASSWORD")
	cfg.Redis.TTL = 3600

	cfg.RAG.DocsPath = "../../docs"
	cfg.RAG.RetrieveTopK = 3
	cfg.RAG.BaseRetriever.MaxResults = 30
	cfg.RAG.BaseRetriever.MinScore = 0.55
	cfg.RAG.Rerank.FinalTopN = 5
	cfg.RAG.PipelineMode = firstNonEmptyWF(os.Getenv("RAG_PIPELINE_MODE"), "legacy")
	cfg.RAG.ChannelTimeoutMs = 2000
	cfg.RAG.RRF.K = 60
	cfg.RAG.RRF.ConsistencyBonus = 1.3
	cfg.RAG.Diversity.PerFileCap = 2

	cfg.ChatMemory.MaxMessages = 20
	cfg.ChatMemory.Compression.Strategy = "simple"
	cfg.ChatMemory.Compression.RecentRounds = 5
	cfg.ChatMemory.Compression.RecentTokenLimit = 2000
	cfg.ChatMemory.Redis.TTLSeconds = 3600
	cfg.ChatMemory.Redis.Lock.ExpireSeconds = 5
	cfg.ChatMemory.Redis.Lock.RetryTimes = 3
	cfg.ChatMemory.Redis.Lock.RetryIntervalMs = 100

	cfg.Chat.MaxReActSteps = 10
	return cfg
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func firstNonEmptyWF(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
