//go:build eval

package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/joho/godotenv"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// TestDumpCandidates 跑 legacy retriever 对每个 golden query 产出 top-N 候选，
// 写到 docs/eval/rag/candidates-{timestamp}.jsonl 供人工挑 relevant_doc_ids。
// 每行结构：
//
//	{
//	  "query": "...",
//	  "relevant_keywords": [...],       // 从 golden 拷过来帮 annotator 回忆
//	  "relevant_doc_ids": [],           // 留空，annotator 从 _candidates 里挑填进来
//	  "_candidates": [                  // 下划线前缀＝元数据，加载时忽略
//	    {"doc_id": "relpath_0", "preview": "首200字..."},
//	    ...
//	  ]
//	}
//
// Run:
//
//	DASHSCOPE_API_KEY=xxx go test -tags=eval ./internal/rag/ -run TestDumpCandidates -v
//
// 可选 env：
//
//	CANDIDATE_TOP_N=10     // 每 query 保留多少候选（默认 10）
//	CANDIDATE_OUT=path.jsonl // 覆盖默认输出路径
func TestDumpCandidates(t *testing.T) {
	_ = godotenv.Load("../../.env")
	apiKey := firstNonEmpty(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"))
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY / QWEN_API_KEY not set")
	}

	topN := 10
	if v := os.Getenv("CANDIDATE_TOP_N"); v != "" {
		fmt.Sscanf(v, "%d", &topN)
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
	cfg.RAG.Rerank.FinalTopN = topN // 调大让 legacy 返 top-N 而不是默认 5
	cfg.RAG.ChannelTimeoutMs = 2000

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	store, err := NewStore(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	rerankClient := NewQwenRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)
	pre := NewQueryPreprocessor()
	legacy := NewReRankingRetriever(store, rerankClient, cfg, pre)

	samples := loadGoldenSet(t, "../../docs/eval/rag/golden_set_seed.jsonl")
	t.Logf("loaded %d golden samples", len(samples))

	outPath := os.Getenv("CANDIDATE_OUT")
	if outPath == "" {
		stamp := time.Now().Format("2006-01-02-1504")
		outPath = "../../docs/eval/rag/candidates-" + stamp + ".jsonl"
	}
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create candidates file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	failed := 0
	for i, s := range samples {
		docs, err := legacy.Retrieve(ctx, s.Query)
		if err != nil {
			t.Logf("[%d] query=%q retrieve error: %v", i, s.Query, err)
			failed++
			continue
		}
		if len(docs) > topN {
			docs = docs[:topN]
		}
		cands := make([]map[string]string, 0, len(docs))
		for _, d := range docs {
			cands = append(cands, map[string]string{
				"doc_id":  d.ID,
				"preview": preview(d.Content, 200),
			})
		}
		record := map[string]any{
			"query":             s.Query,
			"relevant_keywords": s.RelevantKeywords,
			"relevant_doc_ids":  s.RelevantDocIDs,
			"_candidates":       cands,
		}
		if err := enc.Encode(record); err != nil {
			t.Fatalf("encode line %d: %v", i, err)
		}
	}
	t.Logf("wrote %s (top-%d candidates × %d queries, %d failed)", outPath, topN, len(samples)-failed, failed)
}

// preview 返回 s 的前 maxRunes 个 rune，换行/多空白折叠为单空格。
func preview(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
