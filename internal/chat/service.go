package chat

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/agent"
	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/memory"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag"
	ztool "github.com/zhitu-agent/zhitu-agent/internal/tool"
	"github.com/zhitu-agent/zhitu-agent/internal/understand"
	"github.com/zhitu-agent/zhitu-agent/internal/chat/workflow"
)

// Service implements the core chat logic, mirroring Java AiChat + AiChatService.
// It loads system prompt, calls Qwen ChatModel, integrates RAG retrieval,
// manages session memory, and handles tool calls.
type Service struct {
	chatModel      model.ChatModel
	systemPrompt   string
	rag            *rag.RAG
	docsPath       string
	redisClient    *redis.Client
	memoryCfg      *config.ChatMemoryConfig
	compressor     memory.Compressor
	microCompactor *memory.MicroCompactor
	toolInfos      []*schema.ToolInfo
	toolMap        map[string]tool.InvokableTool
	orchestrator   *agent.SimpleOrchestrator
	intentRouter   *understand.Service
	workflow       *workflow.ChatWorkflow
	workflowMode   string
	modelName      string
	monitor        *monitor.Registry
}

// NewService creates a ChatService with the given Qwen chat model and optional RAG.
// System prompt is loaded from the file specified in config.
func NewService(cfg *config.Config, r *rag.RAG) (*Service, error) {	ctx := context.Background()

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

	// Create compressor + optional micro compactor based on strategy
	compressorCfg := memory.Config{
		Strategy:           cfg.ChatMemory.Compression.Strategy,
		RecentRounds:       cfg.ChatMemory.Compression.RecentRounds,
		RecentTokenLimit:   cfg.ChatMemory.Compression.RecentTokenLimit,
		LLMModel:           cfg.ChatMemory.Compression.LLMModel,
		APIKey:             cfg.DashScope.APIKey,
		BaseURL:            "https://dashscope.aliyuncs.com/compatible-mode/v1",
		SummaryPrompt:      cfg.ChatMemory.Compression.SummaryPrompt,
		MicroCompactMinLen: cfg.ChatMemory.Compression.MicroCompactThreshold,
	}
	compressor, err := memory.NewCompressor(compressorCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build compressor: %w", err)
	}
	microCompactor, err := memory.NewMicroCompactor(compressorCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build micro compactor: %w", err)
	}

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

	// Optional understand.Service for intent-driven routing
	var intentRouter *understand.Service
	if cfg.Understand.Enabled {
		tree, err := understand.LoadTree(cfg.Understand.TreePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load intent tree: %w", err)
		}
		understandModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKey:  cfg.DashScope.APIKey,
			Model:   cfg.Understand.LLMModel,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create understand chat model: %w", err)
		}
		intentRouter = understand.NewService(
			understand.NewRewriter(understandModel),
			understand.NewClassifier(understandModel, tree),
			understand.NewGuardian(cfg.Understand.ConfidenceThreshold, cfg.Understand.MaxClarifyAttempts),
			nil,
		)
		log.Printf("[ChatService] understand service enabled (model=%s)", cfg.Understand.LLMModel)
	}

	// Optional graph-based workflow (灰度开关)
	var chatWorkflow *workflow.ChatWorkflow
	workflowMode := cfg.Chat.WorkflowMode
	if workflowMode == "graph" {
		baseTools := make([]tool.BaseTool, 0, len(toolMap))
		for _, t := range toolMap {
			baseTools = append(baseTools, t)
		}
		chatWorkflow, err = workflow.NewChatWorkflow(ctx, &workflow.Deps{
			ChatModel:     chatModel,
			Tools:         baseTools,
			IntentRouter:  intentRouter,
			RAG:           r,
			SystemPrompt:  systemPrompt,
			MaxReActSteps: cfg.Chat.MaxReActSteps,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build chat workflow: %w", err)
		}
		log.Printf("[ChatService] graph workflow enabled")
	}

	return &Service{
		chatModel:      chatModel,
		systemPrompt:   systemPrompt,
		rag:            r,
		docsPath:       cfg.RAG.DocsPath,
		redisClient:    rdb,
		memoryCfg:      &cfg.ChatMemory,
		compressor:     compressor,
		microCompactor: microCompactor,
		toolInfos:      toolInfos,
		toolMap:        toolMap,
		modelName:      cfg.DashScope.ChatModel,
		monitor:        monitor.DefaultRegistry,
		intentRouter:   intentRouter,
		workflow:       chatWorkflow,
		workflowMode:   workflowMode,
	}, nil
}

// InitOrchestrator initializes the multi-agent orchestrator.
// Called after Service creation to avoid circular dependency.
func (s *Service) InitOrchestrator() {
	knowledgeAgent := agent.NewKnowledgeAgent(s.rag)
	reasoningAgent := agent.NewReasoningAgent(s.Chat)
	s.orchestrator = agent.NewSimpleOrchestrator(knowledgeAgent, reasoningAgent)
	if s.intentRouter != nil {
		s.orchestrator.WithIntentRouter(s.intentRouter)
		log.Printf("[ChatService] orchestrator initialized with intent router")
	} else {
		log.Printf("[ChatService] orchestrator initialized")
	}
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

func (s *Service) getMemory(sessionID int64) *memory.CompressibleMemory {
	return memory.NewCompressibleMemory(sessionID, s.redisClient, s.memoryCfg, s.compressor, s.microCompactor)
}

// Chat corresponds to Java aiChat.chat(sessionId, prompt).
// Returns the AI reply as plain text.
// Handles tool call loop: model may return tool_calls → execute → feed back → repeat.
func (s *Service) Chat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	mc := monitor.FromContext(ctx)
	userIDStr := "unknown"
	sessionIDStr := "unknown"
	if mc != nil {
		userIDStr = fmt.Sprintf("%d", mc.UserID)
		sessionIDStr = fmt.Sprintf("%d", mc.SessionID)
	}

	// Record request start
	s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "started")
	s.monitor.Logger.LogRequest("AI_CHAT", "model", s.modelName, "session", sessionIDStr)

	if s.workflow != nil && s.workflowMode == "graph" {
		return s.chatViaWorkflow(ctx, sessionID, prompt)
	}

	mem := s.getMemory(sessionID)
	messages := s.buildMessages(ctx, mem, prompt)

	// Add user message to memory
	mem.Add(ctx, schema.UserMessage(prompt))

	resp, err := s.chatModel.Generate(ctx, messages)
	if err != nil {
		s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "error")
		s.monitor.AiMetrics.RecordError(userIDStr, sessionIDStr, s.modelName, err.Error())
		s.monitor.Logger.LogError("AI_CHAT", 0, err.Error())
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
		mem.Add(ctx, resp)

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			toolResult, err := s.executeToolCall(ctx, tc)
			if err != nil {
				log.Printf("[ChatService] tool %s execution failed: %v", tc.Function.Name, err)
				toolResult = fmt.Sprintf("工具调用失败: %v", err)
			}

			toolMsg := schema.ToolMessage(toolResult, tc.ID, schema.WithToolName(tc.Function.Name))
			messages = append(messages, toolMsg)
			mem.Add(ctx, toolMsg)
		}

		// Call model again with tool results
		resp, err = s.chatModel.Generate(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("chat model generate (tool follow-up) failed: %w", err)
		}
	}

	// Add assistant response to memory
	mem.Add(ctx, resp)

	// Record success metrics
	s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "success")
	if mc != nil {
		s.monitor.AiMetrics.RecordResponseTime(userIDStr, sessionIDStr, s.modelName, time.Duration(mc.DurationMs())*time.Millisecond)
		s.monitor.Logger.LogSuccess("AI_CHAT", mc.DurationMs(), "model", s.modelName)
	}

	return resp.Content, nil
}

// StreamChat corresponds to Java aiChat.streamChat(sessionId, prompt).
// Uses a callback to stream text content to the client while handling tool calls internally.
// When tool calls are detected in the stream, they are accumulated, executed, and the model
// is called again — the final text response is then streamed to the client.
func (s *Service) StreamChat(ctx context.Context, sessionID int64, prompt string, onChunk func(content string)) error {
	mc := monitor.FromContext(ctx)
	userIDStr := "unknown"
	sessionIDStr := "unknown"
	if mc != nil {
		userIDStr = fmt.Sprintf("%d", mc.UserID)
		sessionIDStr = fmt.Sprintf("%d", mc.SessionID)
	}

	// Record request start
	s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "started")
	s.monitor.Logger.LogRequest("AI_STREAM_CHAT", "model", s.modelName, "session", sessionIDStr)

	mem := s.getMemory(sessionID)
	messages := s.buildMessages(ctx, mem, prompt)

	// Add user message to memory
	mem.Add(ctx, schema.UserMessage(prompt))

	// Tool call loop — same as Chat but with streaming
	maxIterations := 10
	for i := 0; i < maxIterations; i++ {
		stream, err := s.chatModel.Stream(ctx, messages)
		if err != nil {
			s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "error")
			s.monitor.AiMetrics.RecordError(userIDStr, sessionIDStr, s.modelName, err.Error())
			s.monitor.Logger.LogError("AI_STREAM_CHAT", 0, err.Error())
			return fmt.Errorf("chat model stream failed: %w", err)
		}

		// Read stream chunks: forward text to client, detect tool calls
		var accumulated []*schema.Message
		hasToolCalls := false

		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("stream read error: %w", err)
			}

			accumulated = append(accumulated, chunk)

			if len(chunk.ToolCalls) > 0 {
				hasToolCalls = true
			} else if chunk.Content != "" && !hasToolCalls {
				// Stream text content directly to client
				onChunk(chunk.Content)
			}
		}

		if !hasToolCalls {
			// No tool calls — streaming complete
			// Add the final assistant response to memory
			if len(accumulated) > 0 {
				finalMsg, _ := schema.ConcatMessages(accumulated)
				if finalMsg != nil {
					mem.Add(ctx, finalMsg)
				}
			}
			break
		}

		// Tool calls detected — merge accumulated chunks and execute tools
		resp, err := schema.ConcatMessages(accumulated)
		if err != nil {
			return fmt.Errorf("failed to merge stream chunks: %w", err)
		}

		messages = append(messages, resp)
		mem.Add(ctx, resp)

		for _, tc := range resp.ToolCalls {
			toolResult, err := s.executeToolCall(ctx, tc)
			if err != nil {
				log.Printf("[ChatService] tool %s execution failed: %v", tc.Function.Name, err)
				toolResult = fmt.Sprintf("工具调用失败: %v", err)
			}
			toolMsg := schema.ToolMessage(toolResult, tc.ID, schema.WithToolName(tc.Function.Name))
			messages = append(messages, toolMsg)
			mem.Add(ctx, toolMsg)
		}

		// Loop continues — next iteration will call Stream again with tool results
		// The final text response from this follow-up call will be streamed to the client
	}

	// Record success metrics
	s.monitor.AiMetrics.RecordRequest(userIDStr, sessionIDStr, s.modelName, "success")
	if mc != nil {
		s.monitor.AiMetrics.RecordResponseTime(userIDStr, sessionIDStr, s.modelName, time.Duration(mc.DurationMs())*time.Millisecond)
		s.monitor.Logger.LogSuccess("AI_STREAM_CHAT", mc.DurationMs(), "model", s.modelName)
	}

	return nil
}

// MultiAgentChat corresponds to Java simpleOrchestrator.process(sessionId, prompt).
// Uses the orchestrator to coordinate KnowledgeAgent and ReasoningAgent.
func (s *Service) MultiAgentChat(ctx context.Context, sessionID int64, prompt string) (string, error) {
	if s.orchestrator == nil {
		// Fallback to normal chat if orchestrator not initialized
		return s.Chat(ctx, sessionID, prompt)
	}

	result := s.orchestrator.Process(ctx, sessionID, prompt)
	return result, nil
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
		start := time.Now()
		docs, err := s.rag.Retriever.Retrieve(ctx, prompt)
		ragDuration := time.Since(start)

		userIDStr := "unknown"
		sessionIDStr := "unknown"
		if mc := monitor.FromContext(ctx); mc != nil {
			userIDStr = fmt.Sprintf("%d", mc.UserID)
			sessionIDStr = fmt.Sprintf("%d", mc.SessionID)
		}
		s.monitor.RagMetrics.RecordRetrievalTime(userIDStr, sessionIDStr, ragDuration)

		if err == nil && len(docs) > 0 {
			s.monitor.RagMetrics.RecordHit(userIDStr, sessionIDStr)

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
		} else if err == nil {
			s.monitor.RagMetrics.RecordMiss(userIDStr, sessionIDStr)
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

// chatViaWorkflow routes a chat request through the Eino graph workflow.
// Keeps memory I/O outside the graph — we pre-fetch history and post-write
// the exchange, which simplifies node types and keeps the legacy memory
// compression / micro-compact path in the same place as the legacy chain.
func (s *Service) chatViaWorkflow(ctx context.Context, sessionID int64, prompt string) (string, error) {
	mem := s.getMemory(sessionID)
	var history []*schema.Message
	if mem != nil {
		history = mem.GetMessages(ctx)
	}

	resp, err := s.workflow.Invoke(ctx, &workflow.Request{
		SessionID: sessionID,
		Prompt:    prompt,
		History:   history,
	})
	if err != nil {
		return "", fmt.Errorf("workflow invoke: %w", err)
	}
	if resp == nil || resp.Message == nil {
		return "", fmt.Errorf("workflow returned empty response")
	}

	if mem != nil {
		mem.Add(ctx, schema.UserMessage(prompt))
		mem.Add(ctx, resp.Message)
	}
	return resp.Message.Content, nil
}