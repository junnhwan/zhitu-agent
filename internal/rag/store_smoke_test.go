//go:build eval

package rag

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// TestScoreSmokeReal 证明 NewRetriever 配置后 doc.Score() 不再恒为 0。
// 依赖 .env 的 REDIS_ADDR / DASHSCOPE_API_KEY。跑 1 条 query，打印前 3 条
// 候选的 score 分布，全为 0 即失败。
func TestScoreSmokeReal(t *testing.T) {
	_ = godotenv.Load("../../.env")
	apiKey := firstNonEmpty(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"))
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY / QWEN_API_KEY not set")
	}

	cfg := &config.Config{}
	cfg.DashScope.APIKey = apiKey
	cfg.DashScope.EmbeddingModel = firstNonEmpty(os.Getenv("QWEN_EMBEDDING_MODEL"), "text-embedding-v3")
	cfg.DashScope.EmbeddingDimensions = 1024

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
	cfg.RAG.BaseRetriever.MaxResults = 5
	cfg.RAG.BaseRetriever.MinScore = 0.0

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewStore(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}

	docs, err := store.Retriever.Retrieve(ctx, "什么是 RAG")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Skip("no docs indexed — run make eval-workflow with RAG_RELOAD=true first")
	}

	nonZero := 0
	for i, d := range docs {
		t.Logf("[%d] id=%s score=%.4f dist=%v preview=%.60s",
			i, d.ID, d.Score(), d.MetaData["distance"], d.Content)
		if d.Score() != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatalf("all %d docs have score=0 — fix did not land", len(docs))
	}
}
