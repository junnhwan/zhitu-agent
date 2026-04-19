package rag

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/cloudwego/eino-ext/components/embedding/dashscope"
	redisindexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisretriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

const (
	redisIndexName = "zhitu_docs_idx"
	redisKeyPrefix = "zhitu:doc:"
)

// Store holds the Redis-based indexer and retriever.
type Store struct {
	RedisClient    *redis.Client
	Indexer        *redisindexer.Indexer
	Retriever      *redisretriever.Retriever
	Embedder       *dashscope.Embedder
	HasTokenizedIx bool
}

// NewStore initializes the Redis client, embedding model, indexer, and retriever.
// It also creates the RediSearch vector index if it does not exist.
// needsTokenized=true 会确保索引含 content_tokenized TEXT 字段（hybrid 模式需要，
// 缺字段时 drop+重建，docs 不丢，DataLoader 下一步会 HSET 回写所有字段）。
func NewStore(ctx context.Context, cfg *config.Config, needsTokenized bool) (*Store, error) {
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
	if err := ensureIndex(ctx, rdb, redisIndexName, redisKeyPrefix, dims, needsTokenized); err != nil {
		// Log warning but don't fail — index may already exist from a different client
		log.Printf("[Store] Warning: FT.CREATE failed (may already exist): %v", err)
	}
	log.Printf("[Store] RediSearch index '%s' ready (tokenized=%v)", redisIndexName, needsTokenized)

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

	// 5. Create Redis retriever.
	// 注：eino redisretriever 内部 FT.SEARCH 用 WithScores:false，要拿相关性
	// 必须把 KNN 的 distance 字段加进 ReturnFields，再在 DocumentConverter 里
	// 转成 score = 1 - distance（cosine 距离 → 相似度），否则 doc.Score() 永远 0，
	// 下游 base_retriever.min_score / vector_channel.min_score 全部失效（0 命中）。
	topK := cfg.RAG.BaseRetriever.MaxResults
	retriever, err := redisretriever.NewRetriever(ctx, &redisretriever.RetrieverConfig{
		Client: rdb,
		Index:  redisIndexName,
		TopK:   topK,
		ReturnFields: []string{
			"content",
			"vector_content",
			"file_name",
			redisretriever.SortByDistanceAttributeName,
		},
		DocumentConverter: vectorScoreConverter,
		Embedding:         embedder,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create redis retriever: %w", err)
	}

	log.Printf("[Store] Redis indexer + retriever ready (TopK=%d)", topK)

	return &Store{
		RedisClient:    rdb,
		Indexer:        indexer,
		Retriever:      retriever,
		Embedder:       embedder,
		HasTokenizedIx: needsTokenized,
	}, nil
}

// ensureIndex creates the index if missing, or recreates it when needsTokenized is requested
// but the existing index lacks the content_tokenized attribute. FT.DROPINDEX without DD
// keeps the underlying hashes — re-ingest on startup rewrites them with the new field.
func ensureIndex(ctx context.Context, rdb *redis.Client, indexName, keyPrefix string, dimensions int, needsTokenized bool) error {
	info, err := rdb.FTInfo(ctx, indexName).Result()
	if err == nil {
		if !needsTokenized || indexHasAttribute(info, "content_tokenized") {
			return nil
		}
		log.Printf("[Store] existing index lacks content_tokenized, dropping for rebuild")
		_ = rdb.FTDropIndex(ctx, indexName).Err()
	} else {
		log.Printf("[Store] Index '%s' does not exist, creating...", indexName)
	}
	return createIndex(ctx, rdb, indexName, keyPrefix, dimensions, needsTokenized)
}

func indexHasAttribute(info redis.FTInfoResult, name string) bool {
	for _, a := range info.Attributes {
		if a.Attribute == name || a.Identifier == name {
			return true
		}
	}
	return false
}

// createIndex builds the RediSearch vector index. When tokenized is true, an extra
// content_tokenized TEXT field (weight 1.5) is added for BM25 over gse-tokenized text.
func createIndex(ctx context.Context, rdb *redis.Client, indexName, keyPrefix string, dimensions int, tokenized bool) error {
	schemas := []*redis.FieldSchema{
		{FieldName: "content", FieldType: redis.SearchFieldTypeText},
		{FieldName: "file_name", FieldType: redis.SearchFieldTypeText},
	}
	if tokenized {
		schemas = append(schemas, &redis.FieldSchema{
			FieldName: "content_tokenized",
			FieldType: redis.SearchFieldTypeText,
			Weight:    1.5,
		})
	}
	schemas = append(schemas, &redis.FieldSchema{
		FieldName: "vector_content",
		FieldType: redis.SearchFieldTypeVector,
		VectorArgs: &redis.FTVectorArgs{
			FlatOptions: &redis.FTFlatOptions{
				Type:           "FLOAT32",
				Dim:            dimensions,
				DistanceMetric: "COSINE",
			},
		},
	})

	args := make([]*redis.FieldSchema, len(schemas))
	copy(args, schemas)
	return rdb.FTCreate(ctx, indexName,
		&redis.FTCreateOptions{OnHash: true, Prefix: []interface{}{keyPrefix}},
		args...,
	).Err()
}

// vectorScoreConverter parses the FT.SEARCH KNN result, capturing the
// distance-as-attribute and converting cosine distance ∈ [0,2] into a
// similarity score ∈ [-1,1] via score = 1 - distance, written via WithScore
// so downstream min_score filters work.
func vectorScoreConverter(_ context.Context, doc redis.Document) (*schema.Document, error) {
	resp := &schema.Document{
		ID:       doc.ID,
		MetaData: map[string]any{},
	}
	for k, v := range doc.Fields {
		switch k {
		case "content":
			resp.Content = v
		case "vector_content":
			resp.WithDenseVector(redisretriever.Bytes2Vector([]byte(v)))
		case redisretriever.SortByDistanceAttributeName:
			d, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("parse distance %q: %w", v, err)
			}
			resp.WithScore(1 - d)
			resp.MetaData["distance"] = d
		default:
			resp.MetaData[k] = v
		}
	}
	return resp, nil
}
