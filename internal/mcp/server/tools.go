package server

import (
	"fmt"

	"github.com/cloudwego/eino/components/tool"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/rag"
	ztool "github.com/zhitu-agent/zhitu-agent/internal/tool"
)

// DefaultTools 构造 MCP Server 对外暴露的默认工具集：
//   - getCurrentTime
//   - addKnowledgeToRag
//   - retrieveKnowledge（新）
// sendEmail 不暴露（副作用 + 没有远端调用方审计）。
func DefaultTools(r *rag.RAG, cfg *config.Config) ([]tool.InvokableTool, error) {
	timeTool, err := ztool.NewTimeTool()
	if err != nil {
		return nil, fmt.Errorf("new time tool: %w", err)
	}
	ragAdd, err := ztool.NewRagTool(r, cfg.RAG.DocsPath)
	if err != nil {
		return nil, fmt.Errorf("new rag add tool: %w", err)
	}
	ragRetrieve, err := ztool.NewRetrieveKnowledgeTool(r)
	if err != nil {
		return nil, fmt.Errorf("new retrieve tool: %w", err)
	}
	return []tool.InvokableTool{timeTool, ragAdd, ragRetrieve}, nil
}
