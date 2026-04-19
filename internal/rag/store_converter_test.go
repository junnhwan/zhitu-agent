package rag

import (
	"context"
	"strconv"
	"testing"

	redisretriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/redis/go-redis/v9"
)

func TestVectorScoreConverterDistanceToScore(t *testing.T) {
	cases := []struct {
		dist     float64
		wantScore float64
	}{
		{0.0, 1.0},
		{0.3, 0.7},
		{0.45, 0.55},
		{1.0, 0.0},
		{2.0, -1.0},
	}
	for _, c := range cases {
		raw := redis.Document{
			ID: "doc1",
			Fields: map[string]string{
				"content":                                       "hello",
				redisretriever.SortByDistanceAttributeName:      strconv.FormatFloat(c.dist, 'f', -1, 64),
			},
		}
		out, err := vectorScoreConverter(context.Background(), raw)
		if err != nil {
			t.Fatalf("dist=%v: %v", c.dist, err)
		}
		if got := out.Score(); got != c.wantScore {
			t.Errorf("dist=%v: score=%v, want=%v", c.dist, got, c.wantScore)
		}
		if d, _ := out.MetaData["distance"].(float64); d != c.dist {
			t.Errorf("dist=%v: metadata distance=%v, want=%v", c.dist, d, c.dist)
		}
		if out.Content != "hello" {
			t.Errorf("dist=%v: content lost: %q", c.dist, out.Content)
		}
	}
}

func TestVectorScoreConverterMissingDistance(t *testing.T) {
	raw := redis.Document{
		ID:     "doc1",
		Fields: map[string]string{"content": "hello"},
	}
	out, err := vectorScoreConverter(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Score() != 0 {
		t.Errorf("missing distance should leave score zero, got %v", out.Score())
	}
}

func TestVectorScoreConverterBadDistance(t *testing.T) {
	raw := redis.Document{
		ID: "doc1",
		Fields: map[string]string{
			redisretriever.SortByDistanceAttributeName: "not-a-number",
		},
	}
	if _, err := vectorScoreConverter(context.Background(), raw); err == nil {
		t.Error("expected error for malformed distance")
	}
}

func TestVectorScoreConverterUnknownFieldsToMetadata(t *testing.T) {
	raw := redis.Document{
		ID: "doc1",
		Fields: map[string]string{
			"file_name":                                  "a.md",
			redisretriever.SortByDistanceAttributeName:   "0.2",
		},
	}
	out, err := vectorScoreConverter(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.MetaData["file_name"] != "a.md" {
		t.Errorf("unknown field not preserved: %v", out.MetaData)
	}
}
