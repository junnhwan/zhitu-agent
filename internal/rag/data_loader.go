package rag

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// DataLoader loads documents from the docs directory at startup.
// Mirrors Java RagDataLoader (CommandLineRunner).
type DataLoader struct {
	docsPath string
	indexer  *Indexer
}

// NewDataLoader creates a data loader for the given docs path.
func NewDataLoader(docsPath string, indexer *Indexer) *DataLoader {
	return &DataLoader{
		docsPath: docsPath,
		indexer:  indexer,
	}
}

// Load scans the docs directory for .md and .txt files and ingests them.
// Should be called once at startup.
func (dl *DataLoader) Load(ctx context.Context) {
	log.Printf("[DataLoader] === RAG document loading start ===")
	log.Printf("[DataLoader] docs path: %s", dl.docsPath)

	absDocs, err := filepath.Abs(dl.docsPath)
	if err != nil {
		absDocs = dl.docsPath
	}
	dirInfo, err := os.Stat(absDocs)
	if err != nil || !dirInfo.IsDir() {
		log.Printf("[DataLoader] docs directory does not exist: %s", absDocs)
		return
	}

	var docs []*schema.Document
	if err := dl.scanDirectory(absDocs, absDocs, &docs); err != nil {
		log.Printf("[DataLoader] scan failed: %v", err)
		return
	}

	if len(docs) == 0 {
		log.Println("[DataLoader] no documents found")
		return
	}

	log.Printf("[DataLoader] found %d documents, ingesting...", len(docs))
	if err := dl.indexer.Ingest(ctx, docs); err != nil {
		log.Printf("[DataLoader] ingest failed: %v", err)
		return
	}

	log.Printf("[DataLoader] === RAG document loading complete — %d documents processed ===", len(docs))
}

func (dl *DataLoader) scanDirectory(root, dir string, docs *[]*schema.Document) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			if err := dl.scanDirectory(root, fullPath, docs); err != nil {
				log.Printf("[DataLoader] scan subdir %s failed: %v", fullPath, err)
			}
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("[DataLoader] read file %s failed: %v", fullPath, err)
			continue
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			continue
		}

		// Doc ID 用相对 docs 根的 forward-slash 路径，保证不同 CWD 下 ID 一致。
		rel, err := filepath.Rel(root, fullPath)
		if err != nil {
			rel = fullPath
		}
		rel = filepath.ToSlash(rel)

		doc := &schema.Document{
			ID:      rel,
			Content: content,
			MetaData: map[string]any{
				"file_name": name,
				"file_path": rel,
			},
		}
		*docs = append(*docs, doc)
		log.Printf("[DataLoader] loaded: %s (%d chars)", rel, len(content))
	}

	return nil
}
