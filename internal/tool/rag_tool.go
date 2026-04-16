package tool

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/zhitu-agent/zhitu-agent/internal/rag"
)

// RagToolInput is the input for addKnowledgeToRag.
type RagToolInput struct {
	Question string `json:"question" jsonschema:"description=问题内容"`
	Answer   string `json:"answer"   jsonschema:"description=答案内容"`
	FileName string `json:"fileName" jsonschema:"description=目标文件名,默认InfiniteChat.md"`
}

// NewRagTool creates a tool that adds knowledge to the RAG vector store.
// Mirrors Java RagTool.addKnowledgeToRag — @Tool annotated, NOT retrieve (retrieve is internal).
func NewRagTool(r *rag.RAG, docsPath string) (tool.InvokableTool, error) {
	return utils.InferTool[RagToolInput, string](
		"addKnowledgeToRag",
		"当用户想要保存问答对、知识点或者向知识库添加新信息时调用此工具。将问题、答案和目标文件名作为参数。",
		func(ctx context.Context, input RagToolInput) (string, error) {
			return addKnowledgeToRag(ctx, r, docsPath, input.Question, input.Answer, input.FileName), nil
		},
	)
}

// addKnowledgeToRag adds a Q&A pair to a markdown file and ingests it into the vector store.
// Mirrors Java RagTool.addKnowledgeToRag exactly.
func addKnowledgeToRag(ctx context.Context, r *rag.RAG, docsPath, question, answer, fileName string) string {
	log.Printf("[RagTool] saving knowledge - Q: %s, file: %s", question, fileName)

	// 1. Format content
	formattedContent := fmt.Sprintf("### Q：%s\n\nA：%s", question, answer)

	// 2. Handle file name (prevent missing suffix)
	if fileName == "" || strings.TrimSpace(fileName) == "" {
		fileName = "InfiniteChat.md"
	}
	if !strings.HasSuffix(fileName, ".md") {
		fileName = fileName + ".md"
	}

	// 3. Write to physical file
	if !appendToFile(docsPath, formattedContent, fileName) {
		return "保存失败：无法写入本地文件系统，请检查日志。"
	}

	// 4. Ingest into vector store
	if r == nil || r.Indexer == nil {
		return "文件写入成功，但向量数据库未初始化。"
	}

	doc := &schema.Document{
		ID:      fileName + "_" + question,
		Content: formattedContent,
		MetaData: map[string]any{
			"file_name": fileName,
		},
	}

	if err := r.Indexer.Ingest(ctx, []*schema.Document{doc}); err != nil {
		log.Printf("[RagTool] vectorization failed: %v", err)
		return "文件写入成功，但向量数据库更新失败：" + err.Error()
	}

	log.Printf("[RagTool] knowledge synced to RAG")
	return fmt.Sprintf("成功！已将该知识点保存到文档 [%s] 并同步至向量数据库。", fileName)
}

// appendToFile appends content to a file under docsPath.
// Mirrors Java RagTool.appendToFile — synchronized, creates file if not exists.
func appendToFile(docsPath, content, fileName string) bool {
	filePath := filepath.Join(docsPath, fileName)

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[RagTool] create directory failed: %v", err)
		return false
	}

	// Create file if not exists, then append
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[RagTool] open file failed: %v", err)
		return false
	}
	defer f.Close()

	textToAppend := "\n\n" + content
	if _, err := f.WriteString(textToAppend); err != nil {
		log.Printf("[RagTool] write file failed: %v", err)
		return false
	}

	return true
}