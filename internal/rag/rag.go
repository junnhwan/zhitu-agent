package rag

import (
	"context"
	"time"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// RAG is the top-level RAG system that holds all components.
type RAG struct {
	Store              *Store
	Indexer            *Indexer
	Retriever          *ReRankingRetriever
	RerankClient       *QwenRerankClient
	QueryPreprocessor  *QueryPreprocessor
	DataLoader         *DataLoader
	AutoReloader       *AutoReloader
	RerankVerifier     *RerankVerifier
}

// NewRAG initializes all RAG components and returns a fully wired RAG system.
func NewRAG(ctx context.Context, cfg *config.Config) (*RAG, error) {
	// 1. Redis store (client + embedder + indexer + retriever)
	store, err := NewStore(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 2. Document indexer (splitter + store)
	indexer := NewIndexer(store, cfg)

	// 3. Rerank client
	rerankClient := NewQwenRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)

	// 4. Query preprocessor
	queryPreprocessor := NewQueryPreprocessor()

	// 5. Two-stage retriever
	retriever := NewReRankingRetriever(store, rerankClient, cfg, queryPreprocessor)

	// 6. Data loader (startup)
	dataLoader := NewDataLoader(cfg.RAG.DocsPath, indexer)

	// 7. Auto reloader (periodic scan)
	autoReloader := NewAutoReloader(cfg.RAG.DocsPath, indexer, 5*time.Minute)

	// 8. Rerank verifier (conditional startup test)
	rerankVerifier := NewRerankVerifier(rerankClient, cfg.Rerank.Test.Enabled)

	return &RAG{
		Store:             store,
		Indexer:           indexer,
		Retriever:         retriever,
		RerankClient:      rerankClient,
		QueryPreprocessor: queryPreprocessor,
		DataLoader:        dataLoader,
		AutoReloader:      autoReloader,
		RerankVerifier:    rerankVerifier,
	}, nil
}

// Startup performs all startup tasks: load docs, verify rerank, start auto-reload.
func (r *RAG) Startup(ctx context.Context) {
	// Load existing documents
	r.DataLoader.Load(ctx)

	// Verify rerank if enabled
	r.RerankVerifier.Verify(ctx)

	// Start auto-reload
	r.AutoReloader.Start(ctx)
}

// Shutdown stops background tasks.
func (r *RAG) Shutdown() {
	r.AutoReloader.Stop()
}
