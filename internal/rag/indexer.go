package rag

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// Indexer orchestrates document ingestion: split → embed → store.
// Mirrors Java EmbeddingStoreIngestor.
type Indexer struct {
	store   *Store
	splitter *RecursiveDocumentSplitter
}

// NewIndexer creates a document indexer with the given store and config.
func NewIndexer(store *Store, cfg *config.Config) *Indexer {
	return &Indexer{
		store:   store,
		splitter: NewRecursiveDocumentSplitter(800, 200),
	}
}

// Ingest splits documents, transforms segments (prepend file_name), and stores them.
// Mirrors Java embeddingStoreIngestor.ingest(document).
func (idx *Indexer) Ingest(ctx context.Context, docs []*schema.Document) error {
	if len(docs) == 0 {
		return nil
	}

	var allSegments []*schema.Document
	for _, doc := range docs {
		segments := idx.splitter.SplitDocument(doc)
		allSegments = append(allSegments, segments...)
		log.Printf("[Indexer] document '%s' -> %d segments", doc.ID, len(segments))
	}

	if len(allSegments) == 0 {
		log.Println("[Indexer] no segments to ingest")
		return nil
	}

	log.Printf("[Indexer] ingesting %d segments...", len(allSegments))

	ids, err := idx.store.Indexer.Store(ctx, allSegments)
	if err != nil {
		return fmt.Errorf("indexer store failed: %w", err)
	}

	log.Printf("[Indexer] ingested %d segments (ids: %v)", len(ids), ids)
	return nil
}
