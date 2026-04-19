//go:build eval

package rag

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/cloudwego/eino/schema"
	"github.com/joho/godotenv"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/postprocessor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/tokenizer"
)

type goldenSample struct {
	Query            string   `json:"query"`
	RelevantKeywords []string `json:"relevant_keywords,omitempty"`
	RelevantDocIDs   []string `json:"relevant_doc_ids,omitempty"`
}

func loadGoldenSet(t *testing.T, path string) []goldenSample {
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden set: %v", err)
	}
	defer f.Close()
	var out []goldenSample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s goldenSample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		out = append(out, s)
	}
	return out
}

// sampleHit returns true if the retrieved docs satisfy the sample's relevance criteria.
// 判据优先级：relevant_doc_ids > relevant_keywords。
//   - relevant_doc_ids 非空：只要有一个检索 doc.ID 以任一 relevant_doc_id 为前缀即命中
//     （前缀匹配是为了兼容切片粒度——relevant_doc_id 通常是 relpath，实际 ID 是 relpath_N）
//   - 否则回退关键词子串匹配：所有关键词都在某个 doc.Content 中出现才算命中
func sampleHit(docs []*schema.Document, sample goldenSample) bool {
	if len(sample.RelevantDocIDs) > 0 {
		for _, d := range docs {
			for _, rid := range sample.RelevantDocIDs {
				if docIDMatches(d.ID, rid) {
					return true
				}
			}
		}
		return false
	}
	if len(sample.RelevantKeywords) == 0 {
		return false
	}
	for _, kw := range sample.RelevantKeywords {
		kwL := strings.ToLower(kw)
		found := false
		for _, d := range docs {
			if strings.Contains(strings.ToLower(d.Content), kwL) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// docIDMatches 用前缀语义对比两个 doc_id，相等或 "relpath" 作为 "relpath_N" 的前缀都算 match。
func docIDMatches(actual, want string) bool {
	if actual == want {
		return true
	}
	return strings.HasPrefix(actual, want+"_")
}

type evalScore struct {
	Label     string  `json:"label"`
	Samples   int     `json:"samples"`
	Hits      int     `json:"hits"`
	RecallAt5 float64 `json:"recall_at_5"`
	MRR       float64 `json:"mrr"`
}

func runEval(t *testing.T, label string, ret Retriever, samples []goldenSample, topK int) evalScore {
	hit := 0
	mrrSum := 0.0
	for _, s := range samples {
		docs, err := ret.Retrieve(context.Background(), s.Query)
		if err != nil {
			t.Logf("[%s] query=%q error=%v", label, s.Query, err)
			continue
		}
		k := len(docs)
		if k > topK {
			k = topK
		}
		top := docs[:k]
		if sampleHit(top, s) {
			hit++
		}
		// MRR: 取满足判据的单 doc 最小 rank
		for i := 0; i < k; i++ {
			if sampleHit([]*schema.Document{top[i]}, s) {
				mrrSum += 1.0 / float64(i+1)
				break
			}
		}
	}
	recall := float64(hit) / float64(len(samples))
	mrr := mrrSum / float64(len(samples))
	t.Logf("=== [%s] Recall@%d = %.3f (%d/%d)  MRR = %.3f ===", label, topK, recall, hit, len(samples), mrr)
	return evalScore{Label: label, Samples: len(samples), Hits: hit, RecallAt5: recall, MRR: mrr}
}

// TestRagAB runs A/B: legacy vs hybrid retriever against golden seed.
// Run: DASHSCOPE_API_KEY=xxx go test -tags=eval ./internal/rag/ -run TestRagAB -v
// Requires local Redis Stack with indexed ./docs content.
func TestRagAB(t *testing.T) {
	_ = godotenv.Load("../../.env")
	apiKey := firstNonEmpty(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"))
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY / QWEN_API_KEY not set")
	}
	cfg := &config.Config{}
	cfg.DashScope.APIKey = apiKey
	cfg.DashScope.EmbeddingModel = firstNonEmpty(os.Getenv("QWEN_EMBEDDING_MODEL"), "text-embedding-v3")
	cfg.DashScope.EmbeddingDimensions = 1024
	cfg.DashScope.RerankModel = firstNonEmpty(os.Getenv("QWEN_RERANK_MODEL"), "qwen3-rerank")

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
	cfg.RAG.BaseRetriever.MaxResults = 30
	cfg.RAG.BaseRetriever.MinScore = 0.0
	cfg.RAG.Rerank.FinalTopN = 5
	cfg.RAG.ChannelTimeoutMs = 2000
	cfg.RAG.RRF.K = 60
	cfg.RAG.RRF.ConsistencyBonus = 1.3
	cfg.RAG.Diversity.PerFileCap = 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	store, err := NewStore(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	rerankClient := NewQwenRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)
	pre := NewQueryPreprocessor()
	legacy := NewReRankingRetriever(store, rerankClient, cfg, pre)

	tok, tokErr := tokenizer.Default()
	if tokErr != nil {
		t.Logf("tokenizer init failed: %v — hybrid will fall back", tokErr)
	}

	// Re-ingest if requested (RAG_RELOAD_DOCS=true) to populate content_tokenized field.
	if os.Getenv("RAG_RELOAD_DOCS") == "true" {
		// 测试 CWD 是 internal/rag，固定到 repo 根目录的 docs
		cfg.RAG.DocsPath = "../../docs"
		if _, statErr := os.Stat(cfg.RAG.DocsPath); statErr != nil {
			t.Fatalf("docs path %s not found: %v", cfg.RAG.DocsPath, statErr)
		}
		idx := NewIndexer(store, cfg).WithTokenizer(tok)
		t.Logf("reloading docs from %s ...", cfg.RAG.DocsPath)
		NewDataLoader(cfg.RAG.DocsPath, idx).Load(ctx)
	}

	metrics := monitor.DefaultRegistry.AiMetrics
	bm25 := channel.NewBM25Channel(store.RedisClient, redisIndexName, 20)
	if tok != nil {
		bm25 = bm25.WithTokenizedField(tok.Tokenize)
	}
	channels := []channel.Channel{
		channel.NewVectorChannel(store.Retriever, cfg.RAG.BaseRetriever.MinScore),
		bm25,
	}
	procs := []postprocessor.Processor{
		postprocessor.NewDedup(),
		postprocessor.NewRRF(cfg.RAG.RRF.K, cfg.RAG.RRF.ConsistencyBonus),
		postprocessor.NewRerank(rerankClient, cfg.RAG.Rerank.FinalTopN, metrics.RecordRAGRerankFallback),
		postprocessor.NewDiversity(cfg.RAG.Diversity.PerFileCap),
	}
	hybrid := NewPipeline(pre, channels, procs,
		time.Duration(cfg.RAG.ChannelTimeoutMs)*time.Millisecond,
		legacy, cfg.RAG.Rerank.FinalTopN,
		PipelineHooks{OnChannelFailed: metrics.RecordRAGChannelFailed, OnZeroHit: metrics.RecordRAGZeroHit}).
		WithPhraseFallback(channel.NewPhraseChannel(store.RedisClient, redisIndexName, 10))

	samples := loadGoldenSet(t, "../../docs/eval/rag/golden_set_seed.jsonl")
	t.Logf("loaded %d golden samples", len(samples))

	scores := []evalScore{
		runEval(t, "legacy", legacy, samples, 5),
		runEval(t, "hybrid", hybrid, samples, 5),
	}
	writeScorecard(t, samples, scores)
}

func writeScorecard(t *testing.T, samples []goldenSample, scores []evalScore) {
	dir := "../../docs/eval/reports"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("mkdir reports failed: %v", err)
		return
	}
	docIDSamples, kwSamples := 0, 0
	for _, s := range samples {
		if len(s.RelevantDocIDs) > 0 {
			docIDSamples++
		} else if len(s.RelevantKeywords) > 0 {
			kwSamples++
		}
	}
	report := map[string]any{
		"timestamp":     time.Now().Format("2006-01-02T15:04:05Z07:00"),
		"golden_count":  len(samples),
		"golden_source": "docs/eval/rag/golden_set_seed.jsonl",
		"judgment": map[string]int{
			"by_doc_id":   docIDSamples,
			"by_keyword":  kwSamples,
			"unlabelled":  len(samples) - docIDSamples - kwSamples,
		},
		"scores": scores,
	}
	body, _ := json.MarshalIndent(report, "", "  ")
	stamp := time.Now().Format("2006-01-02-1504")
	path := dir + "/" + stamp + ".json"
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Logf("write scorecard failed: %v", err)
		return
	}
	// also overwrite latest.json for easy diff
	_ = os.WriteFile(dir+"/latest.json", body, 0o644)
	t.Logf("scorecard written: %s (by_doc_id=%d, by_keyword=%d)", path, docIDSamples, kwSamples)
}

func getEnvOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
