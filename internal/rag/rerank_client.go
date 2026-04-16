package rag

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

// QwenRerankClient calls the DashScope rerank API.
// Mirrors Java QwenRerankClient — pure HTTP, no eino dependency.
type QwenRerankClient struct {
	apiKey    string
	modelName string
	client    *http.Client
}

// NewQwenRerankClient creates a rerank client with the given API key and model name.
func NewQwenRerankClient(apiKey, modelName string) *QwenRerankClient {
	return &QwenRerankClient{
		apiKey:    apiKey,
		modelName: modelName,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const rerankAPIURL = "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"

type rerankRequest struct {
	Model     string        `json:"model"`
	Input     rerankInput   `json:"input"`
	Parameters rerankParams `json:"parameters"`
}

type rerankInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankParams struct {
	TopN int `json:"top_n"`
}

type rerankResponse struct {
	Output rerankOutput `json:"output"`
}

type rerankOutput struct {
	Results []rerankResult `json:"results"`
}

type rerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank sends a rerank request and returns the indices of documents sorted by
// descending relevance score. Returns nil on error (caller should fallback).
func (c *QwenRerankClient) Rerank(query string, documents []string, topN int) []int {
	if query == "" {
		log.Println("[Rerank] query is empty")
		return nil
	}
	if len(documents) == 0 {
		log.Println("[Rerank] documents list is empty")
		return nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}

	reqBody := rerankRequest{
		Model: c.modelName,
		Input: rerankInput{
			Query:     query,
			Documents: documents,
		},
		Parameters: rerankParams{
			TopN: topN,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("[Rerank] marshal request failed: %v", err)
		return nil
	}

	req, err := http.NewRequest(http.MethodPost, rerankAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("[Rerank] create request failed: %v", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("[Rerank] HTTP request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Rerank] read response failed: %v", err)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Rerank] API returned status %d: %s", resp.StatusCode, string(respBody))
		return nil
	}

	var rerankResp rerankResponse
	if err := json.Unmarshal(respBody, &rerankResp); err != nil {
		log.Printf("[Rerank] unmarshal response failed: %v", err)
		return nil
	}

	// Sort results by relevance_score descending
	results := rerankResp.Output.Results
	sortRerankResults(results)

	indices := make([]int, 0, len(results))
	for _, r := range results {
		indices = append(indices, r.Index)
	}

	log.Printf("[Rerank] success — %d docs -> indices: %v", len(documents), indices)
	return indices
}

func sortRerankResults(results []rerankResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].RelevanceScore > results[j-1].RelevanceScore; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
