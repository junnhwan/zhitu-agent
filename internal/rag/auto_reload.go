package rag

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// AutoReloader periodically scans the docs directory and re-ingests new or modified files.
// Mirrors Java RagAutoReloadJob (@Scheduled fixedRate=300000).
type AutoReloader struct {
	docsPath    string
	indexer     *Indexer
	ticker      *time.Ticker
	fileTimes   map[string]int64
	mu          sync.Mutex
	stopCh      chan struct{}
	running     bool
}

// NewAutoReloader creates an auto-reloader that scans docsPath every interval.
func NewAutoReloader(docsPath string, indexer *Indexer, interval time.Duration) *AutoReloader {
	return &AutoReloader{
		docsPath: docsPath,
		indexer:  indexer,
		ticker:   time.NewTicker(interval),
		fileTimes: make(map[string]int64),
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic document scanning.
func (ar *AutoReloader) Start(ctx context.Context) {
	ar.mu.Lock()
	if ar.running {
		ar.mu.Unlock()
		return
	}
	ar.running = true
	ar.mu.Unlock()

	log.Println("[AutoReloader] started — scanning every 5m0s")

	go func() {
		for {
			select {
			case <-ar.ticker.C:
				ar.scan(ctx)
			case <-ctx.Done():
				ar.ticker.Stop()
				ar.mu.Lock()
				ar.running = false
				ar.mu.Unlock()
				log.Println("[AutoReloader] stopped")
				return
			}
		}
	}()
}

// Stop terminates periodic scanning.
func (ar *AutoReloader) Stop() {
	ar.ticker.Stop()
	ar.stopCh <- struct{}{}
	ar.mu.Lock()
	ar.running = false
	ar.mu.Unlock()
	log.Println("[AutoReloader] stopped")
}

func (ar *AutoReloader) scan(ctx context.Context) {
	log.Println("[AutoReloader] scanning docs directory...")

	dirInfo, err := os.Stat(ar.docsPath)
	if err != nil || !dirInfo.IsDir() {
		log.Printf("[AutoReloader] docs directory does not exist: %s", ar.docsPath)
		return
	}

	var changed bool
	var docs []*schema.Document
	if err := ar.scanDirectory(ar.docsPath, &docs, &changed); err != nil {
		log.Printf("[AutoReloader] scan failed: %v", err)
		return
	}

	if !changed || len(docs) == 0 {
		log.Println("[AutoReloader] no new or modified documents")
		return
	}

	log.Printf("[AutoReloader] found %d changed documents, ingesting...", len(docs))
	if err := ar.indexer.Ingest(ctx, docs); err != nil {
		log.Printf("[AutoReloader] ingest failed: %v", err)
		return
	}
	log.Printf("[AutoReloader] ingested %d documents", len(docs))
}

func (ar *AutoReloader) scanDirectory(dir string, docs *[]*schema.Document, changed *bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			if err := ar.scanDirectory(fullPath, docs, changed); err != nil {
				log.Printf("[AutoReloader] scan subdir %s failed: %v", fullPath, err)
			}
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			log.Printf("[AutoReloader] stat file %s failed: %v", fullPath, err)
			continue
		}

		lastModified := info.ModTime().Unix()

		ar.mu.Lock()
		cached, exists := ar.fileTimes[fullPath]
		ar.mu.Unlock()

		if exists && lastModified <= cached {
			continue
		}

		// New or modified file
		data, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("[AutoReloader] read file %s failed: %v", fullPath, err)
			continue
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			continue
		}

		doc := &schema.Document{
			ID:      fullPath,
			Content: content,
			MetaData: map[string]any{
				"file_name": name,
				"file_path": fullPath,
			},
		}
		*docs = append(*docs, doc)

		ar.mu.Lock()
		ar.fileTimes[fullPath] = lastModified
		ar.mu.Unlock()

		*changed = true
		log.Printf("[AutoReloader] detected: %s", name)
	}

	return nil
}
