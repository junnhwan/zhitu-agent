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

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/postprocessor"
)

type goldenSample struct {
	Query            string   `json:"query"`
	RelevantKeywords []string `json:"relevant_keywords"`
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

// sampleHit returns true if every relevant keyword appears (case-insensitive substring)
// in at least one of the top-k retrieved doc contents.
func sampleHit(docs []string, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	for _, kw := range keywords {
		kwL := strings.ToLower(kw)
		found := false
		for _, c := range docs {
			if strings.Contains(strings.ToLower(c), kwL) {
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

func runEval(t *testing.T, label string, ret Retriever, samples []goldenSample, topK int) {
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
		contents := make([]string, k)
		for i := 0; i < k; i++ {
			contents[i] = docs[i].Content
		}
		if sampleHit(contents, s.RelevantKeywords) {
			hit++
		}
		// MRR: 取包含全部关键词的最小 rank
		for i := 0; i < k; i++ {
			if sampleHit([]string{contents[i]}, s.RelevantKeywords) {
				mrrSum += 1.0 / float64(i+1)
				break
			}
		}
	}
	recall := float64(hit) / float64(len(samples))
	mrr := mrrSum / float64(len(samples))
	t.Logf("=== [%s] Recall@%d = %.3f (%d/%d)  MRR = %.3f ===", label, topK, recall, hit, len(samples), mrr)
}

// TestRagAB runs A/B: legacy vs hybrid retriever against golden seed.
// Run: DASHSCOPE_API_KEY=xxx go test -tags=eval ./internal/rag/ -run TestRagAB -v
// Requires local Redis Stack with indexed ./docs content.
func TestRagAB(t *testing.T) {
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}
	cfg := &config.Config{}
	cfg.DashScope.APIKey = os.Getenv("DASHSCOPE_API_KEY")
	cfg.DashScope.EmbeddingModel = "text-embedding-v3"
	cfg.DashScope.EmbeddingDimensions = 1024
	cfg.DashScope.RerankModel = "qwen3-rerank"
	cfg.Redis.Host = getEnvOr("REDIS_HOST", "127.0.0.1")
	fmt.Sscanf(getEnvOr("REDIS_PORT", "6379"), "%d", &cfg.Redis.Port)
	cfg.RAG.BaseRetriever.MaxResults = 30
	cfg.RAG.BaseRetriever.MinScore = 0.0
	cfg.RAG.Rerank.FinalTopN = 5
	cfg.RAG.ChannelTimeoutMs = 2000
	cfg.RAG.RRF.K = 60
	cfg.RAG.RRF.ConsistencyBonus = 1.3

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := NewStore(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rerankClient := NewQwenRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)
	pre := NewQueryPreprocessor()
	legacy := NewReRankingRetriever(store, rerankClient, cfg, pre)

	metrics := monitor.DefaultRegistry.AiMetrics
	channels := []channel.Channel{
		channel.NewVectorChannel(store.Retriever, cfg.RAG.BaseRetriever.MinScore),
		channel.NewBM25Channel(store.RedisClient, redisIndexName, 20),
	}
	procs := []postprocessor.Processor{
		postprocessor.NewDedup(),
		postprocessor.NewRRF(cfg.RAG.RRF.K, cfg.RAG.RRF.ConsistencyBonus),
		postprocessor.NewRerank(rerankClient, cfg.RAG.Rerank.FinalTopN, metrics.RecordRAGRerankFallback),
	}
	hybrid := NewPipeline(pre, channels, procs,
		time.Duration(cfg.RAG.ChannelTimeoutMs)*time.Millisecond,
		legacy, cfg.RAG.Rerank.FinalTopN,
		PipelineHooks{OnChannelFailed: metrics.RecordRAGChannelFailed, OnZeroHit: metrics.RecordRAGZeroHit})

	samples := loadGoldenSet(t, "../../docs/eval/rag/golden_set_seed.jsonl")
	t.Logf("loaded %d golden samples", len(samples))

	runEval(t, "legacy", legacy, samples, 5)
	runEval(t, "hybrid", hybrid, samples, 5)
}

func getEnvOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
