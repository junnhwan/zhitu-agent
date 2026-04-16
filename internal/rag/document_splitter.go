package rag

import (
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// RecursiveDocumentSplitter splits text recursively using Chinese-prioritized separators.
// Mirrors Java RecursiveDocumentSplitter — maxChunkSize=800, chunkOverlap=200.
type RecursiveDocumentSplitter struct {
	maxChunkSize int
	chunkOverlap int
	separators   []string
}

// NewRecursiveDocumentSplitter creates a splitter with default Chinese separators.
func NewRecursiveDocumentSplitter(maxChunkSize, chunkOverlap int) *RecursiveDocumentSplitter {
	return &RecursiveDocumentSplitter{
		maxChunkSize: maxChunkSize,
		chunkOverlap: chunkOverlap,
		separators:   []string{"。", "！", "？", "\n\n", "\n", " ", ""},
	}
}

// SplitDocument splits a schema.Document into multiple segments, preserving metadata.
// Prepends file_name metadata to each segment content (mirrors Java TextSegmentTransformer).
func (s *RecursiveDocumentSplitter) SplitDocument(doc *schema.Document) []*schema.Document {
	text := doc.Content
	meta := doc.MetaData
	if meta == nil {
		meta = map[string]any{}
	}

	log.Printf("[Splitter] start — text length: %d chars", len(text))

	chunks := s.splitText(text, 0)

	// Prepend file_name to content (mirrors Java textSegmentTransformer)
	fileName := ""
	if v, ok := meta["file_name"]; ok {
		if fn, ok := v.(string); ok {
			fileName = fn
		}
	}

	segments := make([]*schema.Document, 0, len(chunks))
	for i, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		content := chunk
		if fileName != "" {
			content = fileName + "\n" + content
		}

		// Copy metadata for each segment
		newMeta := make(map[string]any, len(meta))
		for k, v := range meta {
			newMeta[k] = v
		}

		segments = append(segments, &schema.Document{
			ID:       fmtDocID(doc.ID, i),
			Content:  content,
			MetaData: newMeta,
		})
	}

	log.Printf("[Splitter] done — %d chars -> %d segments (avg %d chars/segment)",
		len(text), len(segments), len(text)/max(1, len(segments)))

	return segments
}

// SplitText splits plain text into chunks (no metadata handling).
func (s *RecursiveDocumentSplitter) SplitText(text string) []string {
	return s.splitText(text, 0)
}

func (s *RecursiveDocumentSplitter) splitText(text string, separatorIndex int) []string {
	if len(text) <= s.maxChunkSize {
		return []string{text}
	}

	if separatorIndex >= len(s.separators) {
		return s.splitByCharacter(text)
	}

	separator := s.separators[separatorIndex]
	if separator == "" {
		return s.splitByCharacter(text)
	}

	parts := strings.Split(text, separator)
	var result []string
	var currentChunk strings.Builder

	for i, part := range parts {
		if len(part) > s.maxChunkSize {
			// Part itself is too large — flush current and recurse
			if currentChunk.Len() > 0 {
				result = append(result, currentChunk.String())
				currentChunk.Reset()
			}
			result = append(result, s.splitText(part, separatorIndex+1)...)
		} else {
			testChunk := part
			if currentChunk.Len() > 0 {
				testChunk = currentChunk.String() + separator + part
			}

			if len(testChunk) <= s.maxChunkSize {
				currentChunk.Reset()
				currentChunk.WriteString(testChunk)
			} else {
				if currentChunk.Len() > 0 {
					result = append(result, currentChunk.String())
				}
				currentChunk.Reset()
				currentChunk.WriteString(part)
			}
		}

		// Preserve separator between parts (except after last part)
		// This is handled by the concatenation above
		_ = i
	}

	if currentChunk.Len() > 0 {
		result = append(result, currentChunk.String())
	}

	return s.addOverlap(result)
}

func (s *RecursiveDocumentSplitter) splitByCharacter(text string) []string {
	var result []string
	for i := 0; i < len(text); i += s.maxChunkSize {
		end := i + s.maxChunkSize
		if end > len(text) {
			end = len(text)
		}
		result = append(result, text[i:end])
	}
	return result
}

func (s *RecursiveDocumentSplitter) addOverlap(chunks []string) []string {
	if s.chunkOverlap == 0 || len(chunks) <= 1 {
		return chunks
	}

	result := make([]string, len(chunks))
	for i, chunk := range chunks {
		result[i] = chunk
		if i > 0 && s.chunkOverlap > 0 {
			prev := chunks[i-1]
			overlapStart := len(prev) - s.chunkOverlap
			if overlapStart < 0 {
				overlapStart = 0
			}
			result[i] = prev[overlapStart:] + chunk
		}
	}
	return result
}

func fmtDocID(baseID string, index int) string {
	if baseID == "" {
		return ""
	}
	return fmt.Sprintf("%s_%d", baseID, index)
}
