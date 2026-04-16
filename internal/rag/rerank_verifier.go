package rag

import (
	"context"
	"fmt"
	"log"
)

// RerankVerifier runs a smoke test of the rerank API at startup.
// Mirrors Java RerankVerifier (@ConditionalOnProperty rerank.test.enabled=true).
type RerankVerifier struct {
	rerankClient *QwenRerankClient
	enabled      bool
}

// NewRerankVerifier creates a verifier that conditionally tests rerank at startup.
func NewRerankVerifier(rerankClient *QwenRerankClient, enabled bool) *RerankVerifier {
	return &RerankVerifier{
		rerankClient: rerankClient,
		enabled:      enabled,
	}
}

// Verify runs the rerank smoke test if enabled.
func (v *RerankVerifier) Verify(ctx context.Context) {
	if !v.enabled {
		log.Println("[RerankVerifier] skipped (rerank.test.enabled=false)")
		return
	}

	log.Println("=== Rerank verification start ===")

	query := "Java多线程实现方式"
	docs := []string{
		"Java多线程可以通过继承Thread类、实现Runnable接口、实现Callable接口来实现",
		"Python的多线程使用threading模块",
		"线程池是管理线程的一种方式",
		"Java的synchronized关键字用于线程同步",
	}

	result := v.rerankClient.Rerank(query, docs, 2)

	if result == nil || len(result) == 0 {
		log.Println("[RerankVerifier] rerank failed (will fall back to vector retrieval)")
	} else {
		log.Println("[RerankVerifier] rerank succeeded")
		fmt.Println("Query:", query)
		fmt.Println("Top 2 results:")
		for i, idx := range result {
			if idx >= 0 && idx < len(docs) {
				fmt.Printf("  %d. %s\n", i+1, docs[idx])
			} else {
				fmt.Printf("  %d. invalid index: %d\n", i+1, idx)
			}
		}
	}

	log.Println("=== Rerank verification end ===")
}
