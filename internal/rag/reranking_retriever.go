package rag

import (
	"context"
	"log"

	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// ReRankingRetriever implements two-stage retrieval: coarse (vector) → fine (rerank).
// Mirrors Java ReRankingContentRetriever with fallback on rerank failure.
type ReRankingRetriever struct {
	baseRetriever     *baseRetriever
	rerankClient      *QwenRerankClient
	queryPreprocessor *QueryPreprocessor
	finalTopN         int
}

// baseRetriever wraps the Redis vector retriever with minScore filtering.
type baseRetriever struct {
	store    *Store
	minScore float64
}

// NewReRankingRetriever creates a two-stage retriever.
func NewReRankingRetriever(store *Store, rerankClient *QwenRerankClient, cfg *config.Config, queryPreprocessor *QueryPreprocessor) *ReRankingRetriever {
	finalTopN := cfg.RAG.Rerank.FinalTopN
	if finalTopN <= 0 {
		finalTopN = 5
	}

	return &ReRankingRetriever{
		baseRetriever: &baseRetriever{
			store:    store,
			minScore: cfg.RAG.BaseRetriever.MinScore,
		},
		rerankClient:      rerankClient,
		queryPreprocessor: queryPreprocessor,
		finalTopN:         finalTopN,
	}
}

// Retrieve performs two-stage RAG retrieval:
// 1. Coarse: vector search returns top-K candidates (default 30)
// 2. Fine: rerank selects finalTopN (default 5) most relevant results
// On rerank failure, falls back to first finalTopN vector results.
func (r *ReRankingRetriever) Retrieve(ctx context.Context, query string) ([]*schema.Document, error) {
	if query == "" {
		log.Println("[RAG] empty query, returning empty results")
		return nil, nil
	}

	// Step 1: Query preprocessing
	processedQuery := query
	if r.queryPreprocessor != nil {
		processedQuery = r.queryPreprocessor.Preprocess(query)
	}

	// Step 2: Coarse retrieval — vector search
	candidates, err := r.baseRetriever.retrieve(ctx, processedQuery)
	if err != nil {
		log.Printf("[RAG] coarse retrieval failed: %v", err)
		return nil, err
	}

	if len(candidates) == 0 {
		log.Println("[RAG] coarse retrieval returned 0 results")
		return nil, nil
	}

	log.Printf("[RAG] stage1 coarse: %d candidates", len(candidates))

	// If candidates are few enough, skip rerank
	if len(candidates) <= r.finalTopN {
		return candidates, nil
	}

	// Step 3: Fine retrieval — rerank
	log.Printf("[RAG] stage2 rerank: %d candidates -> top %d", len(candidates), r.finalTopN)

	documents := make([]string, len(candidates))
	for i, doc := range candidates {
		documents[i] = doc.Content
	}

	rerankIndices := r.rerankClient.Rerank(processedQuery, documents, r.finalTopN)

	if rerankIndices == nil || len(rerankIndices) == 0 {
		log.Printf("[RAG] rerank failed, falling back to top %d vector results", r.finalTopN)
		return candidates[:min(r.finalTopN, len(candidates))], nil
	}

	// Map rerank indices back to documents
	var results []*schema.Document
	for _, idx := range rerankIndices {
		if idx >= 0 && idx < len(candidates) {
			results = append(results, candidates[idx])
		}
	}

	if len(results) == 0 {
		log.Println("[RAG] rerank returned no valid results, falling back to top vector results")
		return candidates[:min(r.finalTopN, len(candidates))], nil
	}

	log.Printf("[RAG] stage2 rerank complete: %d results", len(results))
	return results, nil
}

// retrieve performs vector search via the Redis retriever and filters by minScore.
func (b *baseRetriever) retrieve(ctx context.Context, query string) ([]*schema.Document, error) {
	docs, err := b.store.Retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, err
	}

	// Filter by minScore
	var filtered []*schema.Document
	for _, doc := range docs {
		score := doc.Score()
		if score >= b.minScore {
			filtered = append(filtered, doc)
		}
	}

	return filtered, nil
}
