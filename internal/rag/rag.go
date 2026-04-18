package rag

import (
	"context"
	"log"
	"time"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/channel"
	"github.com/zhitu-agent/zhitu-agent/internal/rag/postprocessor"
)

// RAG is the top-level RAG system that holds all components.
type RAG struct {
	Store              *Store
	Indexer            *Indexer
	Retriever          Retriever
	RerankClient       *QwenRerankClient
	QueryPreprocessor  *QueryPreprocessor
	DataLoader         *DataLoader
	AutoReloader       *AutoReloader
	RerankVerifier     *RerankVerifier
}

// NewRAG initializes all RAG components and returns a fully wired RAG system.
func NewRAG(ctx context.Context, cfg *config.Config, metrics *monitor.AiMetrics) (*RAG, error) {
	store, err := NewStore(ctx, cfg)
	if err != nil {
		return nil, err
	}

	indexer := NewIndexer(store, cfg)
	rerankClient := NewQwenRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)
	queryPreprocessor := NewQueryPreprocessor()

	legacy := NewReRankingRetriever(store, rerankClient, cfg, queryPreprocessor)

	var retriever Retriever = legacy
	if cfg.RAG.PipelineMode == "hybrid" {
		retriever = buildHybridPipeline(cfg, store, rerankClient, queryPreprocessor, legacy, metrics)
		log.Printf("[RAG] pipeline_mode=hybrid — multi-channel enabled")
	} else {
		log.Printf("[RAG] pipeline_mode=legacy — single-channel vector+rerank")
	}

	dataLoader := NewDataLoader(cfg.RAG.DocsPath, indexer)
	autoReloader := NewAutoReloader(cfg.RAG.DocsPath, indexer, 5*time.Minute)
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

func buildHybridPipeline(
	cfg *config.Config,
	store *Store,
	rerankClient *QwenRerankClient,
	pre *QueryPreprocessor,
	legacy Retriever,
	metrics *monitor.AiMetrics,
) *Pipeline {
	channels := []channel.Channel{
		channel.NewVectorChannel(store.Retriever, cfg.RAG.BaseRetriever.MinScore),
		channel.NewBM25Channel(store.RedisClient, redisIndexName, 20),
	}

	hooks := PipelineHooks{}
	if metrics != nil {
		hooks.OnChannelFailed = metrics.RecordRAGChannelFailed
		hooks.OnZeroHit = metrics.RecordRAGZeroHit
	}
	var rerankFallback func()
	if metrics != nil {
		rerankFallback = metrics.RecordRAGRerankFallback
	}
	procs := []postprocessor.Processor{
		postprocessor.NewDedup(),
		postprocessor.NewRRF(cfg.RAG.RRF.K, cfg.RAG.RRF.ConsistencyBonus),
		postprocessor.NewRerank(rerankClient, cfg.RAG.Rerank.FinalTopN, rerankFallback),
	}

	timeout := time.Duration(cfg.RAG.ChannelTimeoutMs) * time.Millisecond
	return NewPipeline(pre, channels, procs, timeout, legacy, cfg.RAG.Rerank.FinalTopN, hooks)
}

// Startup performs all startup tasks: load docs, verify rerank, start auto-reload.
func (r *RAG) Startup(ctx context.Context) {
	r.DataLoader.Load(ctx)
	r.RerankVerifier.Verify(ctx)
	r.AutoReloader.Start(ctx)
}

// Shutdown stops background tasks.
func (r *RAG) Shutdown() {
	r.AutoReloader.Stop()
}
