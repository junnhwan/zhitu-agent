package rag

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino-ext/components/embedding/dashscope"
	redisindexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisretriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

const (
	redisIndexName = "zhitu_docs_idx"
	redisKeyPrefix = "zhitu:doc:"
)

// Store holds the Redis-based indexer and retriever.
type Store struct {
	RedisClient *redis.Client
	Indexer     *redisindexer.Indexer
	Retriever   *redisretriever.Retriever
	Embedder    *dashscope.Embedder
}

// NewStore initializes the Redis client, embedding model, indexer, and retriever.
// It also creates the RediSearch vector index if it does not exist.
func NewStore(ctx context.Context, cfg *config.Config) (*Store, error) {
	// 1. Create Redis client with Protocol:2 + UnstableResp3:true (required for FT.SEARCH)
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:          redisAddr,
		Password:      cfg.Redis.Password,
		DB:            0,
		Protocol:      2,
		UnstableResp3: true,
	})

	// Verify Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	log.Printf("[Store] Redis connected at %s", redisAddr)

	// 2. Create DashScope embedding model
	dims := cfg.DashScope.EmbeddingDimensions
	embedder, err := dashscope.NewEmbedder(ctx, &dashscope.EmbeddingConfig{
		APIKey:     cfg.DashScope.APIKey,
		Model:      cfg.DashScope.EmbeddingModel,
		Dimensions: &dims,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dashscope embedder: %w", err)
	}
	log.Printf("[Store] DashScope embedder created (model=%s, dims=%d)", cfg.DashScope.EmbeddingModel, dims)

	// 3. Create RediSearch vector index (idempotent — skip if exists)
	if err := createIndexIfNotExists(ctx, rdb, redisIndexName, redisKeyPrefix, dims); err != nil {
		// Log warning but don't fail — index may already exist from a different client
		log.Printf("[Store] Warning: FT.CREATE failed (may already exist): %v", err)
	}
	log.Printf("[Store] RediSearch index '%s' ready", redisIndexName)

	// 4. Create Redis indexer
	indexer, err := redisindexer.NewIndexer(ctx, &redisindexer.IndexerConfig{
		Client:     rdb,
		KeyPrefix:  redisKeyPrefix,
		BatchSize:  10,
		Embedding:  embedder,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create redis indexer: %w", err)
	}

	// 5. Create Redis retriever
	topK := cfg.RAG.BaseRetriever.MaxResults
	retriever, err := redisretriever.NewRetriever(ctx, &redisretriever.RetrieverConfig{
		Client:   rdb,
		Index:    redisIndexName,
		TopK:     topK,
		Embedding: embedder,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create redis retriever: %w", err)
	}

	log.Printf("[Store] Redis indexer + retriever ready (TopK=%d)", topK)

	return &Store{
		RedisClient: rdb,
		Indexer:     indexer,
		Retriever:   retriever,
		Embedder:    embedder,
	}, nil
}

// createIndexIfNotExists creates a RediSearch vector index using FT.CREATE.
// This is required before the retriever can perform FT.SEARCH.
func createIndexIfNotExists(ctx context.Context, rdb *redis.Client, indexName, keyPrefix string, dimensions int) error {
	// Check if index already exists
	err := rdb.FTInfo(ctx, indexName).Err()
	if err == nil {
		// Index exists
		return nil
	}
	// If error is "unknown index name", create it; otherwise propagate
	log.Printf("[Store] Index '%s' does not exist, creating...", indexName)

	// FT.CREATE zhitu_docs_idx
	//   ON HASH PREFIX 1 zhitu:doc:
	//   SCHEMA content TEXT vector_content VECTOR FLAT 6 TYPE FLOAT32 DIM 1024 DISTANCE_METRIC COSINE
	createCmd := rdb.FTCreate(ctx, indexName,
		&redis.FTCreateOptions{
			OnHash: true,
			Prefix: []interface{}{keyPrefix},
		},
		&redis.FieldSchema{
			FieldName: "content",
			FieldType: redis.SearchFieldTypeText,
		},
		&redis.FieldSchema{
			FieldName: "file_name",
			FieldType: redis.SearchFieldTypeText,
		},
		&redis.FieldSchema{
			FieldName: "vector_content",
			FieldType: redis.SearchFieldTypeVector,
			VectorArgs: &redis.FTVectorArgs{
				FlatOptions: &redis.FTFlatOptions{
					Type:           "FLOAT32",
					Dim:            dimensions,
					DistanceMetric: "COSINE",
				},
			},
		},
	)

	return createCmd.Err()
}
