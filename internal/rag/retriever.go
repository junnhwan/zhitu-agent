package rag

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

type Retriever interface {
	Retrieve(ctx context.Context, query string) ([]*schema.Document, error)
}

var _ Retriever = (*ReRankingRetriever)(nil)
