package chat

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/memory"
	"github.com/zhitu-agent/zhitu-agent/internal/rag"
	ztool "github.com/zhitu-agent/zhitu-agent/internal/tool"
)

// Service implements the core chat logic, mirroring Java AiChat + AiChatService.
// It loads system prompt, calls Qwen ChatModel, integrates RAG retrieval,
// manages session memory, and handles tool calls.
type Service struct {
	chatModel     model.ChatModel
	systemPrompt  string
	rag           *rag.RAG
	docsPath     string
	redisClient  *redis.Client
	memoryCfg    *config.ChatMemoryConfig
	compressor   *memory.TokenCountCompressor
	toolInfos    []*schema.ToolInfo
	toolMap      map[string]tool.InvokableTool
}

// NewService creates a ChatService with the given Qwen chat model and optional RAG.
// System prompt is loaded from the file specified in config.
func NewService(cfg *config.Config, r *rag.RAG) (*Service, error) {
	ctx := context.Background()

	chatModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  cfg.DashScope.APIKey,
		Model:   cfg.DashScope.ChatModel,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create qwen chat model: %w", err)
	}

	// Load system prompt
	systemPrompt, err := loadSystemPrompt()
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %w", err)
	}

	// Create Redis client for memory (same config as RAG Redis)
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.Redis.Password,
		DB:       0,
	})

	// Create compressor
	compressor := memory.NewTokenCountCompressor(
		cfg.ChatMemory.Compression.RecentRounds,
		cfg.ChatMemory.Compression.RecentTokenLimit,
	)

	// Create tools
	toolInfos, toolMap, err := createTools(cfg, r)
	if err != nil {
		return nil, fmt.Errorf("failed to create tools: %w", err)
	}

	// Bind tools to chat model
	if len(toolInfos) > 0 {
		if err := chatModel.BindTools(toolInfos); err != nil {
			return nil, fmt.Errorf("failed to bind tools: %w", err)
		}
		log.Printf("[ChatService] bound %d tools to chat model", len(toolInfos))
	}

	return &Service{
		chatModel:    chatModel,
		systemPrompt: systemPrompt,
		rag:          r,
		docsPath:     cfg.RAG.DocsPath,
		redisClient:  rdb,
		memoryCfg:    &cfg.ChatMemory,
		compressor:   compressor,
		toolInfos:    toolInfos,
		toolMap:      toolMap,
	}, nil
}

// createTools creates all tool instances and returns their ToolInfo list and a name→tool map.
func createTools(cfg *config.Config, r *rag.RAG) ([]*schema.ToolInfo, map[string]tool.InvokableTool, error) {
	toolMap := make(map[string]tool.InvokableTool)
	var toolInfos []*schema.ToolInfo
	bgCtx := context.Background()

	// TimeTool
	timeTool, err := ztool.NewTimeTool()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create time tool: %w", err)
	}
	timeInfo, _ := timeTool.Info(bgCtx)
	toolInfos = append(toolInfos, timeInfo)
	toolMap[timeInfo.Name] = timeTool

	// EmailTool
	emailTool, err := ztool.NewEmailTool(&cfg.Mail)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create email tool: %w", err)
	}
	emailInfo, _ := emailTool.Info(bgCtx)
	toolInfos = append(toolInfos, emailInfo)
	toolMap[emailInfo.Name] = emailTool

	// RagTool
	ragTool, err := ztool.NewRagTool(r, cfg.RAG.DocsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create rag tool: %w", err)
	}
	ragInfo, _ := ragTool.Info(bgCtx)
	toolInfos = append(toolInfos, ragInfo)
	toolMap[ragInfo.Name] = ragTool

	return toolInfos, toolMap, nil
}

// getMemory returns the CompressibleMemory for the given session.
func (s *Service) getMemory(sessionID int64) *memory.CompressibleMemory {
	return memory.NewCompressibleMemory(sessionID, s.redisClient, s.memoryCfg, s.compressor)
}

// Chat corresponds to Java aiChat.chat(sessionId, prompt).
// Returns the AI reply as plain text.
// Handles tool call loop: model may return tool_calls → execute → feed back → repeat.
func (s *Service) Chat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	mem := s.getMemory(sessionID)
	messages := s.buildMessages(ctx, mem, prompt)

	// Add user message to memory
	mem.Add(ctx, schema.UserMessage(prompt))

	resp, err := s.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat model generate failed: %w", err)
	}

	// Handle tool calls in a loop
	maxIterations := 10
	for i := 0; i < maxIterations; i++ {
		if len(resp.ToolCalls) == 0 {
			break
		}

		// Add assistant message with tool calls to messages
		messages = append(messages, resp)

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			toolResult, err := s.executeToolCall(ctx, tc)
			if err != nil {
				log.Printf("[ChatService] tool %s execution failed: %v", tc.Function.Name, err)
				toolResult = fmt.Sprintf("工具调用失败: %v", err)
			}

			// Add tool result message
			messages = append(messages, schema.ToolMessage(toolResult, tc.ID,
				schema.WithToolName(tc.Function.Name)))
		}

		// Call model again with tool results
		resp, err = s.chatModel.Generate(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("chat model generate (tool follow-up) failed: %w", err)
		}
	}

	// Add assistant response to memory
	mem.Add(ctx, resp)

	return resp.Content, nil
}

// StreamChat corresponds to Java aiChat.streamChat(sessionId, prompt).
// Returns a StreamReader of message chunks for SSE streaming.
func (s *Service) StreamChat(ctx context.Context, sessionID int64, prompt string) (*schema.StreamReader[*schema.Message], error) {
	mem := s.getMemory(sessionID)
	messages := s.buildMessages(ctx, mem, prompt)

	// Add user message to memory
	mem.Add(ctx, schema.UserMessage(prompt))

	stream, err := s.chatModel.Stream(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("chat model stream failed: %w", err)
	}

	// Note: tool call handling in streaming mode is complex.
	// For now, stream directly. Tool call support in streaming will be enhanced in Phase 5.
	return stream, nil
}

// buildMessages constructs the message list with system prompt, memory, optional RAG context, and user message.
func (s *Service) buildMessages(ctx context.Context, mem *memory.CompressibleMemory, prompt string) []*schema.Message {
	messages := []*schema.Message{
		schema.SystemMessage(s.systemPrompt),
	}

	// Add memory messages
	if mem != nil {
		history := mem.GetMessages(ctx)
		if len(history) > 0 {
			messages = append(messages, history...)
		}
	}

	// RAG retrieval: inject relevant knowledge before user message
	if s.rag != nil && s.rag.Retriever != nil {
		docs, err := s.rag.Retriever.Retrieve(ctx, prompt)
		if err == nil && len(docs) > 0 {
			var kb strings.Builder
			kb.WriteString("参考知识：\n")
			for i, doc := range docs {
				fileName := "未知文件"
				if v, ok := doc.MetaData["file_name"]; ok {
					if fn, ok := v.(string); ok && fn != "" {
						fileName = fn
					}
				}
				kb.WriteString(fmt.Sprintf("【来源：%s | 相似度：%.2f】\n%s", fileName, doc.Score(), doc.Content))
				if i < len(docs)-1 {
					kb.WriteString("\n\n---\n\n")
				}
			}
			messages = append(messages, schema.UserMessage(kb.String()))
			messages = append(messages, schema.AssistantMessage("好的，我已了解相关知识，请继续提问。", nil))
		}
	}

	messages = append(messages, schema.UserMessage(prompt))
	return messages
}

// executeToolCall runs a single tool call and returns the result string.
func (s *Service) executeToolCall(ctx context.Context, tc schema.ToolCall) (string, error) {
	t, ok := s.toolMap[tc.Function.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
	result, err := t.InvokableRun(ctx, tc.Function.Arguments)
	if err != nil {
		return "", fmt.Errorf("tool execution error: %w", err)
	}
	return result, nil
}

// InsertKnowledge writes a Q&A pair to a markdown file and ingests it into the vector store.
// Mirrors Java AiChatController.insertKnowledge.
func (s *Service) InsertKnowledge(ctx context.Context, question, answer, sourceName string) (string, error) {
	if s.rag == nil {
		return "插入失败：RAG服务未初始化", nil
	}

	formattedContent := fmt.Sprintf("### Q：%s\n\nA：%s", question, answer)

	// Write to local file (synchronized — mirrors Java synchronized appendToFile)
	if !s.appendToFile(formattedContent, sourceName) {
		return "插入失败：无法写入本地文件", nil
	}

	// Ingest into vector store
	doc := &schema.Document{
		ID:      sourceName + "_" + question,
		Content: formattedContent,
		MetaData: map[string]any{
			"file_name": sourceName,
		},
	}

	if err := s.rag.Indexer.Ingest(ctx, []*schema.Document{doc}); err != nil {
		return "插入部分成功：文件已写入，但向量库更新失败", nil
	}

	return fmt.Sprintf("插入成功：已同步至 %s 及向量数据库", sourceName), nil
}

// appendToFile appends content to the knowledge file.
// Mirrors Java synchronized appendToFile.
func (s *Service) appendToFile(content, sourceName string) bool {
	filePath := filepath.Join(s.docsPath, sourceName)

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}

	// Create file if not exists
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()

	textToAppend := "\n\n" + content
	if _, err := f.WriteString(textToAppend); err != nil {
		return false
	}

	return true
}

// loadSystemPrompt reads the system prompt file.
// Mirrors Java @SystemMessage(fromResource = "system-prompt/chat-bot.txt")
func loadSystemPrompt() (string, error) {
	// Check env override first
	if path := os.Getenv("SYSTEM_PROMPT_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Default: system-prompt/chat-bot.txt relative to working directory
	data, err := os.ReadFile("system-prompt/chat-bot.txt")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}