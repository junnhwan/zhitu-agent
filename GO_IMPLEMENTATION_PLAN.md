# Go + Eino 复刻 QianyanAgent 完整实现方案

> 本文档旨在作为独立参考，使 AI 能在另一个会话中仅凭此文档完成 Go 版项目的完整编码复刻。
> 所有实现细节均来源于对原 Java 项目的逐文件精读，确保无遗漏。

---

## 一、原项目完整文件清单与功能映射

原项目共 43 个 Java 源文件，以下是每个文件的职责及其在 Go 版中的对应：

### 1.1 启动与配置层（8 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 1 | `InfiniteChatAgentApplication.java` | Spring Boot 入口，排除 RedisEmbeddingStore 自动配置，启用 Scheduling | `cmd/server/main.go` |
| 2 | `config/DashScopeModelConfig.java` | 创建 QwenChatModel + QwenStreamingChatModel，注册 AiModelMonitorListener | `internal/chat/service.go` 中初始化模型 |
| 3 | `config/EmbeddingStoreConfig.java` | 创建 PgVectorEmbeddingStore（dimension=1024，dropTableFirst=true） | `internal/rag/store.go` 改用 Redis Indexer |
| 4 | `config/RagConfig.java` | 创建 EmbeddingStoreIngestor（RecursiveDocumentSplitter 800/200）+ ContentRetriever（粗排 maxResults=30 minScore=0.55 → 精排 finalTopN=5）| `internal/rag/indexer.go` + `internal/rag/retriever.go` |
| 5 | `config/RedisChatMemoryStoreConfig.java` | 创建 RedisChatMemoryStore（host/port/password/ttl/user=default）| `internal/memory/redis_store.go` |
| 6 | `config/ChatMemoryConfig.java` | 加载 chat.memory.* 配置，创建 TokenCountChatMemoryCompressor Bean | `internal/config/config.go` + `internal/memory/compressor.go` |
| 7 | `config/McpToolConfig.java` | 创建两个 MCP Client：BigModelSearchMcpClient（HTTP SSE）、timeClient（Stdio uvx mcp-server-time）| `internal/chat/service.go` 中初始化 MCP |
| 8 | `config/RestTemplateConfig.java` | 创建 RestTemplate Bean（用于 Rerank HTTP 调用）| 不需要，Go 用 net/http |

### 1.2 核心业务层（7 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 9 | `ai/AiChat.java` | **核心接口**：声明 @SystemMessage + @MemoryId + @UserMessage + @InputGuardrails | `internal/chat/service.go` — 用 Eino ChatModelAgent 替代 |
| 10 | `ai/AiChatService.java` | **核心装配**：用 AiServices.builder 构建 AiChat，绑定 chatModel/streamingChatModel/contentRetriever/chatMemoryProvider/tools/timeTool/ragTool/emailTool/mcpToolProvider | `internal/chat/service.go` — 用 Eino ADK 组装 |
| 11 | `agent/Agent.java` | Agent 接口：execute(sessionId, input) + getAgentName() | `internal/agent/agent.go` |
| 12 | `agent/KnowledgeAgent.java` | 调用 ragTool.retrieve(input) 做知识检索 | `internal/agent/knowledge_agent.go` |
| 13 | `agent/ReasoningAgent.java` | 调用 aiChat.chat(sessionId, input) 做推理对话 | `internal/agent/reasoning_agent.go` |
| 14 | `orchestrator/SimpleOrchestrator.java` | 关键词匹配分流：KNOWLEDGE_KEYWORDS=("查询","了解","什么是","介绍","解释","说明")，命中则先 KnowledgeAgent 再 ReasoningAgent，否则直接 ReasoningAgent | `internal/agent/orchestrator.go` |
| 15 | `controller/AiChatController.java` | 4 个接口：/chat, /streamChat, /multiAgentChat, /insert（详见 1.8 节）| `internal/handler/chat_handler.go` |

### 1.3 RAG 层（6 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 16 | `rag/RecursiveDocumentSplitter.java` | 递归文档切片：separators=["。","！","？","\n\n","\n"," ",""]，maxChunkSize=800，chunkOverlap=200 | `internal/rag/document_splitter.go` |
| 17 | `rag/QueryPreprocessor.java` | 查询预处理：移除标点+停用词（30个中文停用词）| `internal/rag/query_preprocessor.go` |
| 18 | `rag/QwenRerankClient.java` | 调用 DashScope Rerank API（POST text-rerank），解析 results 按 relevance_score 降序返回索引 | `internal/rag/rerank_client.go` |
| 19 | `rag/ReRankingContentRetriever.java` | 两阶段检索：粗排(baseRetriever)→精排(rerankClient)，含降级逻辑 | `internal/rag/reranking_retriever.go` |
| 20 | `rag/RerankVerifier.java` | 启动时验证 Rerank 功能（CommandLineRunner，条件 rerank.test.enabled=true）| `internal/rag/rerank_verifier.go` |
| 21 | `job/RagDataLoader.java` | 启动时加载 docsPath 下全部文档（CommandLineRunner）| `internal/rag/data_loader.go` |
| 22 | `job/RagAutoReloadJob.java` | 定时扫描文档目录（@Scheduled 300000ms），检测新/更新文件并索引 | `internal/rag/auto_reload.go` |

### 1.4 记忆层（2 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 23 | `memory/CompressibleChatMemory.java` | 可压缩的 ChatMemory：分布式锁(Lua脚本) + 压缩触发(消息数/Token阈值) + 原子更新(Redis事务) + 降级保留 | `internal/memory/compressible_memory.go` |
| 24 | `memory/TokenCountChatMemoryCompressor.java` | Token 计数估算(text.length/4) + 摘要生成(保留最近N轮，历史生成SystemMessage摘要) | `internal/memory/compressor.go` |

### 1.5 工具层（3 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 25 | `tool/TimeTool.java` | @Tool("getCurrentTime")，返回上海时间格式 "yyyy-MM-dd HH:mm:ss EEEE (中国标准时间)" | `internal/tool/time_tool.go` |
| 26 | `tool/EmailTool.java` | @Tool("向特定用户发送电子邮件")，参数：targetEmail/subject/content，用 JavaMailSender | `internal/tool/email_tool.go` |
| 27 | `tool/RagTool.java` | 两个方法：retrieve(query) 用 EmbeddingSearchRequest 检索；@Tool addKnowledgeToRag(question, answer, fileName) 动态添加知识 | `internal/tool/rag_tool.go` |

### 1.6 监控层（6 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 28 | `Monitor/MonitorContext.java` | 监控上下文：requestId/sessionId/userId/startTime | `internal/monitor/context.go` |
| 29 | `Monitor/MonitorContextHolder.java` | ThreadLocal 存储监控上下文 | Go 用 context.Context 传递 |
| 30 | `Monitor/AiModelMonitorListener.java` | 实现 ChatModelListener：onRequest/onResponse/onError，记录请求耗时+Token用量 | `internal/monitor/model_listener.go` |
| 31 | `Monitor/AiModelMetricsCollector.java` | Micrometer 指标：ai_model_requests_total / ai_model_errors_total / ai_model_tokens_total / ai_model_response_duration_seconds | `internal/monitor/ai_metrics.go` |
| 32 | `Monitor/RagMetricsCollector.java` | Micrometer 指标：rag_retrieval_hit_total / rag_retrieval_miss_total / rag_retrieval_duration_seconds | `internal/monitor/rag_metrics.go` |
| 33 | `Monitor/ObservabilityLogger.java` | 结构化日志：logRequest/logSuccess/logError，用 MDC 传递 requestId/sessionId/userId | `internal/monitor/logger.go` |
| 34 | `interceptor/ObservabilityInterceptor.java` | Spring HandlerInterceptor：preHandle 生成 requestId + 设置响应头 X-Request-ID；afterCompletion 清除上下文 | `internal/middleware/observability.go` |

### 1.7 安全与异常层（5 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 35 | `guardrail/SafeInputGuardrail.java` | 实现 InputGuardrail：敏感词检测（"死","杀"），命中返回 fatal | `internal/middleware/guardrail.go` |
| 36 | `common/ErrorCode.java` | 错误码枚举（40xxx通用/50xxx系统/70xxx用户/80xxx AI Agent/90xxx WebSocket）| `internal/common/errno.go` |
| 37 | `common/BaseResponse.java` | 统一响应：code + data + message | `internal/common/response.go` |
| 38 | `common/ResultUtils.java` | success(data) / error(errorCode) 工具方法 | `internal/common/response.go` |
| 39 | `Exception/BusinessException.java` | 业务异常：code + message | `internal/common/errno.go` |
| 40 | `Exception/GlobalExceptionHandler.java` | 全局异常处理：BusinessException / RuntimeException / MethodArgumentNotValidException / InputGuardrailException | `internal/middleware/error_handler.go` |
| 41 | `Exception/ThrowUtils.java` | throwIf(condition, errorCode) 工具 | Go 不需要，直接 return error |

### 1.8 其他（3 文件）

| # | Java 文件 | 职责 | Go 对应 |
|---|---|---|---|
| 42 | `model/dto/ChatRequest.java` | 请求 DTO：sessionId(Long) + userId(Long) + prompt(String) | `internal/model/dto.go` |
| 43 | `model/dto/KnowledgeRequest.java` | 请求 DTO：question(String) + answer(String) + sourceName(String) | `internal/model/dto.go` |
| 44 | `config/CorsConfig.java` | 全局 CORS：allowedOriginPatterns=*, allowCredentials=true | Gin CORS 中间件 |
| 45 | `config/WebMvcConfig.java` | 注册 ObservabilityInterceptor | Gin 中间件链 |

---

## 二、API 接口完整规格

### 2.1 POST /api/chat

**请求体：**
```json
{
  "sessionId": 123456,
  "userId": 654321,
  "prompt": "你好"
}
```
**响应：** 纯文本字符串（AI 回复）

**逻辑：**
1. MonitorContextHolder 设置上下文(userId, sessionId)
2. 调用 `aiChat.chat(sessionId, prompt)` — Eino ChatModelAgent 处理（自动带 RAG + Tools + Memory）
3. MonitorContextHolder 清除上下文
4. 返回结果

### 2.2 POST /api/streamChat

**请求体：** 同 /chat

**响应：** SSE 流式文本（`data: xxx\n\n` 格式）

**逻辑：**
1. 构建 MonitorContext
2. 用 Flux.defer 延迟执行，确保订阅时设置上下文
3. 调用 `aiChat.streamChat(sessionId, prompt)`
4. doFinally 清除上下文
5. 前端解析 SSE data: 前缀拼接完整回复

### 2.3 POST /api/multiAgentChat

**请求体：** 同 /chat

**响应：** 纯文本字符串

**逻辑：**
1. 设置 MonitorContext
2. 调用 `simpleOrchestrator.process(sessionId, prompt)`
3. Orchestrator 内部：
   - 检查 prompt 是否包含 KNOWLEDGE_KEYWORDS（"查询","了解","什么是","介绍","解释","说明"）
   - 命中：先 KnowledgeAgent.retrieve(input) → 将知识注入 enhancedInput = "参考知识：\n" + result + "\n\n用户问题：" + input → ReasoningAgent.chat(enhancedInput)
   - 未命中：直接 ReasoningAgent.chat(input)
4. 清除 MonitorContext

### 2.4 POST /api/insert

**请求体：**
```json
{
  "question": "这个软件叫什么名字？",
  "answer": "本软件名为知途",
  "sourceName": "InfiniteChat.md"
}
```
**响应：** 纯文本字符串（"插入成功：已同步至 xxx 及向量数据库" / 失败提示）

**逻辑：**
1. 格式化内容：`### Q：{question}\n\nA：{answer}`
2. 写入物理文件 docsPath/sourceName（synchronized 追加写入，不存在则创建）
3. 存入向量数据库：Document.from(formattedContent, Metadata.from("file_name", sourceName)) → embeddingStoreIngestor.ingest(document)
4. 返回结果

---

## 三、核心配置完整规格（application.yml）

Go 版 `config.yaml` 应完整复刻以下配置：

```yaml
server:
  port: 10010
  context_path: /api    # Gin 路由组前缀

spring_data_redis:       # → redis 配置
  host: "127.0.0.1"
  port: 6379
  password: ""
  ttl: 3600

dashscope:
  api_key: "sk-xxx"
  chat_model: "qwen-max"
  embedding_model: "text-embedding-v3"   # 注意：Java版用v4，Eino dashscope只支持v1/v2/v3
  embedding_dimensions: 1024
  rerank_model: "qwen3-rerank"

pgvector:               # → 改为 Redis 向量存储配置
  # 不再需要，但保留 docs_path
  docs_path: "./docs"

mail:
  host: "smtp.qq.com"
  port: 587
  username: ""
  password: ""          # SMTP 授权码

bigmodel:
  api_key: ""           # MCP BigModel Search API Key

chat_memory:
  max_messages: 20
  compression:
    token_threshold: 6000
    recent_rounds: 5
    recent_token_limit: 2000
    summary_token_limit: 500
    summary_prompt: "请将以下对话历史压缩为简洁摘要，严格控制在{tokenLimit} tokens以内，必须保留用户偏好、重要决策、核心诉求等关键信息，去除无意义寒暄：\n{messages}"
    fallback_recent_rounds: 10
  redis:
    ttl_seconds: 3600
    lock:
      expire_seconds: 5
      retry_times: 3
      retry_interval_ms: 100

rag:
  docs_path: "./docs"
  retrieve_top_k: 3
  base_retriever:
    max_results: 30
    min_score: 0.55
  rerank:
    final_top_n: 5
  test:
    enabled: false

monitoring:
  prometheus:
    enabled: true
  grafana:
    enabled: true
```

---

## 四、系统 Prompt 完整内容

```
你是"知途"技术顾问，由先进大模型驱动，致力于为开发者提供精准、高效、专业的技术支持！你由独立研发团队打造，擅长编程问题解答、架构分析、故障排查与最佳实践建议。回答务必简明扼要、逻辑清晰，直击问题核心！绝不冗长、不总结、不提供链接！

请严格遵守以下规则：
1. 知识边界：优先根据检索到的知识回答问题，知识不足时可结合自身技术判断补充，但必须明确标注哪些来自知识库、哪些是补充建议。
2. 日常交互：若为日常闲聊或非技术问题，可简短自然地回应，保持专业但不刻板。
3. 安全合规（重要）：
   - 严禁生成任何政治敏感、暴力色情、辱骂攻击或不文明的内容。
   - 严格保护隐私：若涉及敏感信息（如密码、密钥、身份证号、银行卡等），必须忽略或模糊处理；仅在执行邮件发送任务时，允许使用必要的邮箱地址。
4. 格式要求：输出必须为纯文本格式，换行请严格使用 \n 表示。代码片段使用 ``` 包裹。
5. 语气约束：保持专业务实，避免过度热情或使用感叹号堆砌。
```

---

## 五、目录结构设计（最终版）

```
QianyanAgent-go/
├── cmd/
│   └── server/
│       └── main.go                       # 入口：加载配置 → 初始化组件 → 启动 Gin
├── internal/
│   ├── config/
│   │   └── config.go                     # 配置结构体 + Viper 加载
│   ├── handler/
│   │   └── chat_handler.go              # 4 个 API handler
│   ├── middleware/
│   │   ├── cors.go                       # CORS 中间件
│   │   ├── guardrail.go                  # 敏感词拦截中间件
│   │   ├── observability.go              # requestId 生成 + 上下文注入
│   │   └── error_handler.go             # 全局错误恢复
│   ├── agent/
│   │   ├── agent.go                      # Agent 接口
│   │   ├── knowledge_agent.go            # 知识检索 Agent
│   │   ├── reasoning_agent.go            # 推理对话 Agent
│   │   └── orchestrator.go               # 关键词匹配分流
│   ├── chat/
│   │   └── service.go                    # Eino ChatModelAgent 组装 + 流式
│   ├── memory/
│   │   ├── redis_store.go                # Redis 会话记忆 CRUD
│   │   ├── compressor.go                # Token 计数 + 摘要生成
│   │   └── compressible_memory.go        # 分布式锁 + 压缩触发 + 降级
│   ├── rag/
│   │   ├── store.go                      # Redis 向量存储初始化
│   │   ├── indexer.go                    # 文档索引（EmbeddingStoreIngestor 对应）
│   │   ├── retriever.go                  # 两阶段检索（粗排→精排）
│   │   ├── rerank_client.go             # DashScope Rerank HTTP 客户端
│   │   ├── query_preprocessor.go         # 停用词 + 标点移除
│   │   ├── document_splitter.go          # 递归文档切片
│   │   ├── data_loader.go               # 启动时文档加载
│   │   ├── auto_reload.go               # 定时扫描文档变更
│   │   └── rerank_verifier.go           # 启动时 Rerank 验证
│   ├── tool/
│   │   ├── time_tool.go                  # 获取上海时间
│   │   ├── email_tool.go                # SMTP 邮件发送
│   │   └── rag_tool.go                   # 知识检索 + 动态添加知识
│   ├── monitor/
│   │   ├── context.go                    # MonitorContext 定义 + context key
│   │   ├── ai_metrics.go                # AI 模型 Prometheus 指标
│   │   ├── rag_metrics.go               # RAG 检索 Prometheus 指标
│   │   ├── model_listener.go            # Eino Callback 替代 ChatModelListener
│   │   └── logger.go                    # 结构化日志
│   ├── common/
│   │   ├── errno.go                      # 错误码枚举 + BusinessException
│   │   └── response.go                  # BaseResponse + ResultUtils
│   └── model/
│       └── dto.go                        # ChatRequest + KnowledgeRequest
├── system-prompt/
│   └── chat-bot.txt                      # 系统提示词
├── static/
│   ├── gpt.html                           # 前端页面
│   ├── ai.png
│   └── user.png
├── docs/                                  # RAG 知识文档目录
├── config.yaml                            # 配置文件
├── docker-compose.yml                     # Redis + Prometheus + Grafana
├── Dockerfile
├── go.mod
└── go.sum
```

---

## 六、核心模块详细实现方案

### 6.1 入口 main.go

```go
func main() {
    // 1. 加载配置
    cfg := config.Load("config.yaml")

    // 2. 初始化 Redis 客户端
    redisClient := redis.NewClient(&redis.Options{...})

    // 3. 初始化 Eino 组件
    ctx := context.Background()
    qwenModel := initQwenModel(cfg)        // eino-ext/model/qwen
    embedder := initDashScopeEmbedder(cfg)  // eino-ext/embedding/dashscope
    redisIndexer := initRedisIndexer(cfg, redisClient, embedder)
    redisRetriever := initRedisRetriever(cfg, redisClient, embedder)

    // 4. 初始化 RAG 组件
    rerankClient := rag.NewRerankClient(cfg.DashScope.APIKey, cfg.DashScope.RerankModel)
    rerankingRetriever := rag.NewRerankingRetriever(redisRetriever, rerankClient, cfg)
    queryPreprocessor := rag.NewQueryPreprocessor()
    documentSplitter := rag.NewRecursiveDocumentSplitter(800, 200)
    ingestor := rag.NewIngestor(redisIndexer, embedder, documentSplitter)

    // 5. 初始化会话记忆
    memoryStore := memory.NewRedisStore(redisClient, cfg.ChatMemory.Redis.TTLSeconds)

    // 6. 初始化 MCP 工具
    mcpToolProvider := initMCPToolProvider(cfg)

    // 7. 初始化自定义工具
    timeTool := tool.NewTimeTool()
    emailTool := tool.NewEmailTool(cfg.Mail)
    ragTool := tool.NewRagTool(rerankingRetriever, ingestor, cfg.RAG.DocsPath)

    // 8. 组装 ChatService（核心 Agent）
    chatService := chat.NewService(cfg, qwenModel, rerankingRetriever, memoryStore,
        timeTool, emailTool, ragTool, mcpToolProvider)

    // 9. 初始化 Agent 编排
    knowledgeAgent := agent.NewKnowledgeAgent(rerankingRetriever)
    reasoningAgent := agent.NewReasoningAgent(chatService)
    orchestrator := agent.NewOrchestrator(knowledgeAgent, reasoningAgent)

    // 10. 启动时加载 RAG 文档
    dataLoader := rag.NewDataLoader(ingestor, cfg.RAG.DocsPath)
    dataLoader.Load(ctx)

    // 11. 启动定时扫描
    autoReload := rag.NewAutoReload(ingestor, cfg.RAG.DocsPath)
    go autoReload.Start(ctx, 5*time.Minute)

    // 12. Rerank 验证（可选）
    if cfg.RAG.Rerank.TestEnabled {
        rag.NewRerankVerifier(rerankClient).Verify(ctx)
    }

    // 13. 初始化 Gin + 中间件 + 路由
    r := gin.Default()
    r.Use(middleware.CORS())
    r.Use(middleware.Observability())
    r.Use(middleware.Guardrail())

    api := r.Group(cfg.Server.ContextPath)
    handler.RegisterRoutes(api, chatService, orchestrator, ingestor, cfg)

    // 14. Prometheus 指标端点
    prometheus.MustRegister(...)
    api.GET("/metrics", gin.WrapH(promhttp.Handler()))

    // 15. 静态文件
    r.StaticFile("/chat", "./static/gpt.html")

    r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}
```

### 6.2 Eino ChatModelAgent 组装 (chat/service.go)

Java 版用 `AiServices.builder(AiChat.class)` 声明式装配，Go 版用 Eino ADK 手动组装：

```go
type Service struct {
    agent       *adk.ChatModelAgent
    runner      *adk.Runner
    memoryStore *memory.RedisStore
    model       model.ChatModel
    streamingModel model.ChatModel  // Eino 用同一个 ChatModel 支持流式
}

func NewService(cfg *config.Config, chatModel model.ChatModel,
    retriever retriever.Retriever, memStore *memory.RedisStore,
    timeTool, emailTool, ragTool tool.BaseTool,
    mcpProvider *mcp.ToolProvider) *Service {

    // 组装所有工具
    allTools := []tool.BaseTool{timeTool, emailTool, ragTool}
    if mcpProvider != nil {
        allTools = append(allTools, mcpProvider.GetTools()...)
    }

    // 创建 ChatModelAgent
    agent := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
        Model: chatModel,
        ToolsConfig: compose.ToolsNodeConfig{
            Tools: allTools,
        },
    })

    runner := adk.NewRunner(context.Background(), &adk.RunnerConfig{}, agent)

    return &Service{
        agent:       agent,
        runner:      runner,
        memoryStore: memStore,
        model:       chatModel,
    }
}

// Chat 对应 Java 版 aiChat.chat(sessionId, prompt)
func (s *Service) Chat(ctx context.Context, sessionId int64, prompt string) (string, error) {
    // 1. 从 Redis 加载历史记忆
    history, _ := s.memoryStore.GetMessages(ctx, sessionId)

    // 2. 构建消息列表
    messages := append(history, schema.UserMessage(prompt))

    // 3. 调用模型（含 RAG + Tools）
    resp, err := s.model.Generate(ctx, messages,
        model.WithTools(s.tools),  // 如果 ChatModelAgent 不自动处理工具
    )

    // 4. 保存 AI 回复到记忆
    s.memoryStore.AddMessage(ctx, sessionId, schema.AssistantMessage(resp.Content))

    return resp.Content, nil
}

// StreamChat 对应 Java 版 aiChat.streamChat(sessionId, prompt)
func (s *Service) StreamChat(ctx context.Context, sessionId int64, prompt string) (<-chan string, error) {
    // 使用 Eino 流式 API
    // 注意：Eino 的 Stream 返回 *schema.StreamReader[*Message]
    // 需要适配为 SSE 格式
}
```

**重要说明：** Java 版的 AiChat 接口通过 `@SystemMessage(fromResource = "system-prompt/chat-bot.txt")` 和 `@InputGuardrails({SafeInputGuardrail.class})` 自动注入系统提示词和安全防护。Go 版需要：
- 系统提示词：在构建消息时手动插入 `schema.SystemMessage(systemPrompt)`
- 安全防护：在 Gin 中间件层拦截（见 6.9 节）

### 6.3 RAG 两阶段检索 (rag/retriever.go)

完整复刻 `ReRankingContentRetriever.java` 的逻辑：

```go
type RerankingRetriever struct {
    baseRetriever    retriever.Retriever   // Eino Redis Retriever
    rerankClient     *RerankClient
    queryPreprocessor *QueryPreprocessor
    ragMetrics       *monitor.RagMetrics
    obsLogger        *monitor.ObservabilityLogger
    finalTopN        int
}

func (r *RerankingRetriever) Retrieve(ctx context.Context, query string) ([]*schema.Document, error) {
    startTime := time.Now()
    userId := monitor.GetUserId(ctx)
    sessionId := monitor.GetSessionId(ctx)

    // 1. 查询预处理
    processedQuery := r.queryPreprocessor.Preprocess(query)

    // 2. 阶段1：粗排（Redis 向量检索）
    candidates, err := r.baseRetriever.Retrieve(ctx, processedQuery)
    if err != nil || len(candidates) == 0 {
        r.ragMetrics.RecordMiss(userId, sessionId)
        return []*schema.Document{}, nil
    }

    if len(candidates) <= r.finalTopN {
        r.ragMetrics.RecordHit(userId, sessionId)
        return candidates, nil
    }

    // 3. 阶段2：精排（DashScope Rerank API）
    rerankedResults, err := r.rerankWithFallback(ctx, query, candidates)
    if err != nil {
        // 降级：返回原始 TopN
        return candidates[:min(r.finalTopN, len(candidates))], nil
    }

    r.ragMetrics.RecordHit(userId, sessionId)
    r.ragMetrics.RecordRetrievalTime(userId, sessionId, time.Since(startTime))
    return rerankedResults, nil
}

func (r *RerankingRetriever) rerankWithFallback(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error) {
    // 提取文档文本
    texts := make([]string, len(docs))
    for i, doc := range docs {
        texts[i] = doc.Content
    }

    // 调用 Rerank API
    rankedIndices, err := r.rerankClient.Rerank(ctx, query, texts, r.finalTopN)
    if err != nil {
        return nil, err
    }

    // 按索引重排文档
    results := make([]*schema.Document, 0, r.finalTopN)
    for _, idx := range rankedIndices {
        if idx >= 0 && idx < len(docs) {
            results = append(results, docs[idx])
        }
    }

    if len(results) == 0 {
        return nil, fmt.Errorf("rerank returned no valid results")
    }

    return results, nil
}
```

### 6.4 DashScope Rerank 客户端 (rag/rerank_client.go)

完整复刻 `QwenRerankClient.java`：

```go
type RerankClient struct {
    apiKey  string
    model   string  // "qwen3-rerank"
    baseURL string  // "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
    client  *http.Client
}

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

func (c *RerankClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]int, error) {
    if query == "" || len(documents) == 0 {
        return nil, fmt.Errorf("query or documents is empty")
    }
    if topN <= 0 || topN > len(documents) {
        topN = len(documents)
    }

    reqBody := rerankRequest{
        Model: c.model,
        Input: rerankInput{Query: query, Documents: documents},
        Parameters: rerankParams{TopN: topN},
    }

    bodyBytes, _ := json.Marshal(reqBody)
    req, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(bodyBytes))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+c.apiKey)

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("rerank API returned %d", resp.StatusCode)
    }

    var result rerankResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    // 按 relevance_score 降序排序
    sort.Slice(result.Output.Results, func(i, j int) bool {
        return result.Output.Results[i].RelevanceScore > result.Output.Results[j].RelevanceScore
    })

    indices := make([]int, 0, topN)
    for _, r := range result.Output.Results {
        indices = append(indices, r.Index)
    }
    return indices, nil
}
```

### 6.5 分布式会话记忆 (memory/)

完整复刻 `CompressibleChatMemory.java` + `TokenCountChatMemoryCompressor.java`：

**redis_store.go：**
```go
type RedisStore struct {
    client *redis.Client
    ttl    time.Duration
}

func (s *RedisStore) GetMessages(ctx context.Context, sessionId int64) ([]*schema.Message, error) {
    key := fmt.Sprintf("chat:memory:%d", sessionId)
    data, err := s.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return []*schema.Message{}, nil
    }
    // 反序列化 JSON → []*schema.Message
}

func (s *RedisStore) UpdateMessages(ctx context.Context, sessionId int64, messages []*schema.Message) error {
    key := fmt.Sprintf("chat:memory:%d", sessionId)
    data, _ := json.Marshal(messages)
    return s.client.Set(ctx, key, data, s.ttl).Err()
}

func (s *RedisStore) DeleteMessages(ctx context.Context, sessionId int64) error {
    key := fmt.Sprintf("chat:memory:%d", sessionId)
    return s.client.Del(ctx, key).Err()
}
```

**compressor.go：**
```go
type Compressor struct {
    recentRounds     int
    recentTokenLimit int
}

func (c *Compressor) Compress(messages []*schema.Message) []*schema.Message {
    if len(messages) <= c.recentRounds*2 {
        return messages  // 无需压缩
    }

    splitIndex := len(messages) - c.recentRounds*2
    oldMessages := messages[:splitIndex]
    recentMessages := messages[splitIndex:]

    // 生成历史摘要
    summary := c.generateSummary(oldMessages)

    // 组装压缩后消息
    compressed := make([]*schema.Message, 0, len(recentMessages)+1)
    compressed = append(compressed, schema.SystemMessage("历史对话摘要: " + summary))
    compressed = append(compressed, recentMessages...)

    return compressed
}

func (c *Compressor) EstimateTokens(messages []*schema.Message) int {
    total := 0
    for _, msg := range messages {
        total += len(msg.Content) / 4  // Java 版: text.length / 4
    }
    return total
}

func (c *Compressor) generateSummary(messages []*schema.Message) string {
    // Java 版逻辑：取前3条消息，每条截取前50字符
    summary := fmt.Sprintf("共%d轮对话。", len(messages)/2)
    for i := 0; i < min(3, len(messages)); i++ {
        text := messages[i].Content
        if len(text) > 50 {
            text = text[:50]
        }
        summary += " " + text
    }
    return summary
}
```

**compressible_memory.go：**
```go
type CompressibleMemory struct {
    store              *RedisStore
    compressor         *Compressor
    maxMessages        int
    tokenThreshold     int
    fallbackRecentRounds int
    redisClient        *redis.Client
    lockExpireSeconds  int
    lockRetryTimes     int
    lockRetryIntervalMs int
}

func (m *CompressibleMemory) Add(ctx context.Context, sessionId int64, msg *schema.Message) error {
    lockKey := fmt.Sprintf("chat:memory:lock:%d", sessionId)
    lockValue := uuid.New().String()

    // 1. 获取分布式锁（带重试）
    acquired := m.acquireLockWithRetry(ctx, lockKey, lockValue)
    if !acquired {
        // 降级：直接写入
        messages, _ := m.store.GetMessages(ctx, sessionId)
        messages = append(messages, msg)
        return m.store.UpdateMessages(ctx, sessionId, messages)
    }
    defer m.releaseLock(ctx, lockKey, lockValue)

    // 2. 加载 + 追加消息
    messages, _ := m.store.GetMessages(ctx, sessionId)
    messages = append(messages, msg)

    // 3. 判断是否需要压缩
    needCompress := false
    tokenCount := m.compressor.EstimateTokens(messages)
    if len(messages) > m.maxMessages || tokenCount > m.tokenThreshold {
        needCompress = true
    }

    if needCompress {
        compressed, err := m.compressor.Compress(messages)
        if err != nil {
            // 压缩失败降级：保留最近 fallbackRecentRounds 轮
            if len(messages) > m.fallbackRecentRounds*2 {
                compressed = messages[len(messages)-m.fallbackRecentRounds*2:]
            }
        }
        // 原子更新（Redis 事务）
        return m.atomicUpdateMessages(ctx, sessionId, compressed)
    }

    return m.store.UpdateMessages(ctx, sessionId, messages)
}

// acquireLockWithRetry — Redis SETNX + TTL
func (m *CompressibleMemory) acquireLockWithRetry(ctx context.Context, key, value string) bool {
    for i := 0; i < m.lockRetryTimes; i++ {
        ok, _ := m.redisClient.SetNX(ctx, key, value,
            time.Duration(m.lockExpireSeconds)*time.Second).Result()
        if ok {
            return true
        }
        time.Sleep(time.Duration(m.lockRetryIntervalMs) * time.Millisecond)
    }
    return false
}

// releaseLock — Lua 脚本保证原子性（与 Java 版完全一致）
func (m *CompressibleMemory) releaseLock(ctx context.Context, key, value string) {
    script := `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) else return 0 end`
    m.redisClient.Eval(ctx, script, []string{key}, value)
}

// atomicUpdateMessages — Redis 事务删除+写入
func (m *CompressibleMemory) atomicUpdateMessages(ctx context.Context, sessionId int64, messages []*schema.Message) error {
    key := fmt.Sprintf("chat:memory:%d", sessionId)
    pipe := m.redisClient.TxPipeline()
    pipe.Del(ctx, key)
    data, _ := json.Marshal(messages)
    pipe.Set(ctx, key, data, m.store.ttl)
    _, err := pipe.Exec(ctx)
    return err
}
```

### 6.6 工具实现

**time_tool.go：**
```go
type TimeTool struct{}

func (t *TimeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return &schema.ToolInfo{
        Name: "getCurrentTime",
        Desc: "获取当前时间（上海时区）",
    }, nil
}

// InvokableRun — 实现 tool.BaseTool 接口
func (t *TimeTool) InvokableRun(ctx context.Context, args string) (string, error) {
    loc, _ := time.LoadLocation("Asia/Shanghai")
    now := time.Now().In(loc)
    weekday := map[time.Weekday]string{
        time.Sunday: "星期日", time.Monday: "星期一", time.Tuesday: "星期二",
        time.Wednesday: "星期三", time.Thursday: "星期四",
        time.Friday: "星期五", time.Saturday: "星期六",
    }
    return fmt.Sprintf("%s %s (中国标准时间)",
        now.Format("2006-01-02 15:04:05"), weekday[now.Weekday()]), nil
}
```

**email_tool.go：**
```go
type EmailTool struct {
    host     string
    port     int
    username string
    password string
}

func (e *EmailTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return &schema.ToolInfo{
        Name: "send_email",
        Desc: "向特定用户发送电子邮件。参数：targetEmail(收件人邮箱), subject(邮件标题), content(邮件正文)",
    }, nil
}

func (e *EmailTool) InvokableRun(ctx context.Context, args string) (string, error) {
    // 解析 args JSON → targetEmail, subject, content
    // 用 net/smtp 发送邮件
    // Java 版用 JavaMailSender，Go 用 net/smtp 标准库
}
```

**rag_tool.go：**
```go
type RagTool struct {
    retriever *rag.RerankingRetriever
    ingestor  *rag.Ingestor
    docsPath  string
}

func (r *RagTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return &schema.ToolInfo{
        Name: "rag_tool",
        Desc: "知识检索和知识库管理工具。retrieve(query)检索知识；addKnowledge(question,answer,fileName)添加知识",
    }, nil
}

func (r *RagTool) InvokableRun(ctx context.Context, args string) (string, error) {
    // 解析 args，根据 action 分发：
    //   retrieve: 调用 r.retriever.Retrieve()
    //   addKnowledge: 写入文件 + 索引到向量库
}
```

**RagTool.retrieve 方法 — 对应 Java 版：**
```go
func (r *RagTool) Retrieve(query string) string {
    if query == "" {
        return ""
    }
    docs, err := r.retriever.Retrieve(context.Background(), query)
    if err != nil || len(docs) == 0 {
        return ""
    }

    // 拼接结果，格式："【来源：xxx | 相似度：0.xx】\n内容"
    var builder strings.Builder
    for i, doc := range docs {
        fileName := "未知文件"
        if v, ok := doc.MetaData["file_name"]; ok {
            fileName = fmt.Sprint(v)
        }
        score := 0.0
        if v, ok := doc.MetaData["score"]; ok {
            score = v.(float64)
        }
        if i > 0 {
            builder.WriteString("\n\n---\n\n")
        }
        builder.WriteString(fmt.Sprintf("【来源：%s | 相似度：%.2f】\n%s", fileName, score, doc.Content))
    }
    return builder.String()
}
```

**RagTool.addKnowledge 方法 — 对应 Java 版 @Tool 注解方法：**
```go
func (r *RagTool) AddKnowledge(question, answer, fileName string) string {
    if fileName == "" {
        fileName = "InfiniteChat.md"
    }
    if !strings.HasSuffix(fileName, ".md") {
        fileName += ".md"
    }

    // 格式化内容
    content := fmt.Sprintf("### Q：%s\n\nA：%s", question, answer)

    // 写入文件
    if !r.appendToFile(fileName, content) {
        return "保存失败：无法写入本地文件系统"
    }

    // 存入向量库
    doc := &schema.Document{
        ID:      uuid.New().String(),
        Content: content,
        MetaData: map[string]any{"file_name": fileName},
    }
    if err := r.ingestor.Ingest(context.Background(), []*schema.Document{doc}); err != nil {
        return "文件写入成功，但向量数据库更新失败：" + err.Error()
    }

    return fmt.Sprintf("成功！已将该知识点保存到文档 [%s] 并同步至向量数据库。", fileName)
}
```

### 6.7 Agent 编排 (agent/)

**agent.go：**
```go
type Agent interface {
    Execute(ctx context.Context, sessionId int64, input string) (string, error)
    GetAgentName() string
}
```

**knowledge_agent.go：**
```go
type KnowledgeAgent struct {
    ragTool *tool.RagTool
}

func (a *KnowledgeAgent) Execute(ctx context.Context, sessionId int64, input string) (string, error) {
    result := a.ragTool.Retrieve(input)
    if result == "" {
        return "", nil
    }
    return result, nil
}
```

**reasoning_agent.go：**
```go
type ReasoningAgent struct {
    chatService *chat.Service
}

func (a *ReasoningAgent) Execute(ctx context.Context, sessionId int64, input string) (string, error) {
    return a.chatService.Chat(ctx, sessionId, input)
}
```

**orchestrator.go：**
```go
var KnowledgeKeywords = []string{"查询", "了解", "什么是", "介绍", "解释", "说明"}

type Orchestrator struct {
    knowledgeAgent *KnowledgeAgent
    reasoningAgent *ReasoningAgent
}

func (o *Orchestrator) Process(ctx context.Context, sessionId int64, input string) (string, error) {
    enhancedInput := input

    if o.needKnowledgeRetrieval(input) {
        knowledgeResult, _ := o.knowledgeAgent.Execute(ctx, sessionId, input)
        if knowledgeResult != "" {
            enhancedInput = fmt.Sprintf("参考知识：\n%s\n\n用户问题：%s", knowledgeResult, input)
        }
    }

    return o.reasoningAgent.Execute(ctx, sessionId, enhancedInput)
}

func (o *Orchestrator) needKnowledgeRetrieval(input string) bool {
    for _, keyword := range KnowledgeKeywords {
        if strings.Contains(input, keyword) {
            return true
        }
    }
    return false
}
```

### 6.8 Handler (handler/chat_handler.go)

```go
type ChatHandler struct {
    chatService *chat.Service
    orchestrator *agent.Orchestrator
    ingestor    *rag.Ingestor
    docsPath    string
}

func RegisterRoutes(r *gin.RouterGroup, svc *chat.Service, orch *agent.Orchestrator,
    ing *rag.Ingestor, cfg *config.Config) {

    h := &ChatHandler{svc, orch, ing, cfg.RAG.DocsPath}
    r.POST("/chat", h.Chat)
    r.POST("/streamChat", h.StreamChat)
    r.POST("/multiAgentChat", h.MultiAgentChat)
    r.POST("/insert", h.InsertKnowledge)
}

// POST /api/chat
func (h *ChatHandler) Chat(c *gin.Context) {
    var req model.ChatRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, common.Error(common.ParamsError, err.Error()))
        return
    }
    ctx := monitor.WithContext(c.Request.Context(), req.UserId, req.SessionId)
    result, err := h.chatService.Chat(ctx, req.SessionId, req.Prompt)
    if err != nil {
        c.JSON(500, common.Error(common.AIModelError, err.Error()))
        return
    }
    c.String(200, result)
}

// POST /api/streamChat
func (h *ChatHandler) StreamChat(c *gin.Context) {
    var req model.ChatRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, common.Error(common.ParamsError, err.Error()))
        return
    }

    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    ctx := monitor.WithContext(c.Request.Context(), req.UserId, req.SessionId)
    stream, err := h.chatService.StreamChat(ctx, req.SessionId, req.Prompt)
    if err != nil {
        c.SSEvent("error", err.Error())
        return
    }

    c.Stream(func(w io.Writer) bool {
        select {
        case chunk, ok := <-stream:
            if !ok {
                return false
            }
            c.SSEvent("", chunk)  // data: chunk\n\n
            return true
        case <-c.Request.Context().Done():
            return false
        }
    })
}

// POST /api/multiAgentChat
func (h *ChatHandler) MultiAgentChat(c *gin.Context) {
    var req model.ChatRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, common.Error(common.ParamsError, err.Error()))
        return
    }
    ctx := monitor.WithContext(c.Request.Context(), req.UserId, req.SessionId)
    result, err := h.orchestrator.Process(ctx, req.SessionId, req.Prompt)
    if err != nil {
        c.JSON(500, common.Error(common.AIModelError, err.Error()))
        return
    }
    c.String(200, result)
}

// POST /api/insert
func (h *ChatHandler) InsertKnowledge(c *gin.Context) {
    var req model.KnowledgeRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, common.Error(common.ParamsError, err.Error()))
        return
    }

    // 1. 格式化内容
    content := fmt.Sprintf("### Q：%s\n\nA：%s", req.Question, req.Answer)

    // 2. 写入文件
    fileName := req.SourceName
    if fileName == "" {
        fileName = "InfiniteChat.md"
    }
    filePath := filepath.Join(h.docsPath, fileName)
    if err := h.appendToFile(filePath, content); err != nil {
        c.String(200, "插入失败：无法写入本地文件")
        return
    }

    // 3. 存入向量数据库
    doc := &schema.Document{
        ID:      uuid.New().String(),
        Content: content,
        MetaData: map[string]any{"file_name": fileName},
    }
    if err := h.ingestor.Ingest(c.Request.Context(), []*schema.Document{doc}); err != nil {
        c.String(200, "插入部分成功：文件已写入，但向量库更新失败")
        return
    }

    c.String(200, fmt.Sprintf("插入成功：已同步至 %s 及向量数据库", fileName))
}
```

### 6.9 中间件

**guardrail.go：**
```go
var sensitiveWords = []string{"死", "杀"}

func Guardrail() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 仅对 POST 请求检查
        if c.Request.Method == "POST" {
            var body map[string]any
            data, _ := io.ReadAll(c.Request.Body)
            json.Unmarshal(data, &body)
            // 恢复 body 供后续 handler 读取
            c.Request.Body = io.NopCloser(bytes.NewReader(data))

            if prompt, ok := body["prompt"].(string); ok {
                for _, word := range sensitiveWords {
                    if strings.Contains(prompt, word) {
                        c.JSON(400, common.Error(common.SensitiveWordError, "提问不能包含敏感词"))
                        c.Abort()
                        return
                    }
                }
            }
        }
        c.Next()
    }
}
```

**observability.go：**
```go
func Observability() gin.HandlerFunc {
    return func(c *gin.Context) {
        requestId := strings.ReplaceAll(uuid.New().String(), "-", "")
        ctx := context.WithValue(c.Request.Context(), monitor.RequestIDKey, requestId)
        c.Request = c.Request.WithContext(ctx)
        c.Header("X-Request-ID", requestId)
        c.Next()
    }
}
```

### 6.10 监控指标

**ai_metrics.go — 对应 AiModelMetricsCollector.java：**
```go
var (
    aiRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "ai_model_requests_total",
    }, []string{"user_id", "session_id", "model_name", "status"})

    aiErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "ai_model_errors_total",
        Help: "AI模型错误次数",
    }, []string{"user_id", "session_id", "model_name", "error_message"})

    aiTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "ai_model_tokens_total",
        Help: "AI模型Token消耗总数",
    }, []string{"user_id", "session_id", "model_name", "token_type"})

    aiResponseDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name: "ai_model_response_duration_seconds",
        Help: "AI模型响应时间",
    }, []string{"user_id", "session_id", "model_name"})
)
```

**rag_metrics.go — 对应 RagMetricsCollector.java：**
```go
var (
    ragHitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "rag_retrieval_hit_total",
    }, []string{"user_id", "session_id"})

    ragMissTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "rag_retrieval_miss_total",
    }, []string{"user_id", "session_id"})

    ragRetrievalDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name: "rag_retrieval_duration_seconds",
    }, []string{"user_id", "session_id"})
)
```

### 6.11 递归文档切片 (rag/document_splitter.go)

完整复刻 `RecursiveDocumentSplitter.java`：

```go
type RecursiveDocumentSplitter struct {
    maxChunkSize int
    chunkOverlap int
    separators   []string  // ["。","！","？","\n\n","\n"," ",""]
}

func NewRecursiveDocumentSplitter(maxChunkSize, chunkOverlap int) *RecursiveDocumentSplitter {
    return &RecursiveDocumentSplitter{
        maxChunkSize: maxChunkSize,
        chunkOverlap: chunkOverlap,
        separators:   []string{"。", "！", "？", "\n\n", "\n", " ", ""},
    }
}

func (s *RecursiveDocumentSplitter) Split(text string, metaData map[string]any) []*schema.Document {
    chunks := s.splitText(text, 0)
    var docs []*schema.Document
    for _, chunk := range chunks {
        if strings.TrimSpace(chunk) != "" {
            docs = append(docs, &schema.Document{
                ID:       uuid.New().String(),
                Content:  chunk,
                MetaData: metaData,
            })
        }
    }
    return docs
}

func (s *RecursiveDocumentSplitter) splitText(text string, sepIdx int) []string {
    if len(text) <= s.maxChunkSize {
        return []string{text}
    }
    if sepIdx >= len(s.separators) {
        return s.splitByCharacter(text)
    }

    sep := s.separators[sepIdx]
    if sep == "" {
        return s.splitByCharacter(text)
    }

    parts := strings.Split(text, sep)
    var result []string
    var currentChunk string

    for _, part := range parts {
        if len(part) > s.maxChunkSize {
            if currentChunk != "" {
                result = append(result, currentChunk)
                currentChunk = ""
            }
            result = append(result, s.splitText(part, sepIdx+1)...)
        } else {
            testChunk := part
            if currentChunk != "" {
                testChunk = currentChunk + sep + part
            }
            if len(testChunk) <= s.maxChunkSize {
                currentChunk = testChunk
            } else {
                if currentChunk != "" {
                    result = append(result, currentChunk)
                }
                currentChunk = part
            }
        }
    }
    if currentChunk != "" {
        result = append(result, currentChunk)
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
        if i > 0 {
            prev := chunks[i-1]
            overlapStart := len(prev) - s.chunkOverlap
            if overlapStart < 0 {
                overlapStart = 0
            }
            result[i] = prev[overlapStart:] + chunk
        } else {
            result[i] = chunk
        }
    }
    return result
}
```

### 6.12 查询预处理 (rag/query_preprocessor.go)

完整复刻 `QueryPreprocessor.java`：

```go
var stopWords = []string{
    "的", "了", "是", "在", "我", "有", "和", "就", "不", "人", "都", "一", "一个",
    "上", "也", "很", "到", "说", "要", "去", "你", "会", "着", "没有", "看", "好",
    "吗", "呢", "吧", "啊", "哦", "嗯",
}

type QueryPreprocessor struct{}

func (p *QueryPreprocessor) Preprocess(query string) string {
    if len(query) < 3 {
        return query
    }

    processed := strings.TrimSpace(query)
    // 移除标点符号
    re := regexp.MustCompile(`[\p{Punct}\s]+`)
    processed = re.ReplaceAllString(processed, " ")

    // 移除停用词
    for _, word := range stopWords {
        processed = strings.ReplaceAll(processed, word, " ")
    }

    // 合并多余空格
    processed = regexp.MustCompile(`\s+`).ReplaceAllString(processed, " ")
    processed = strings.TrimSpace(processed)

    if processed == "" {
        return query
    }
    return processed
}
```

### 6.13 文档自动加载与定时扫描

**data_loader.go — 对应 RagDataLoader.java：**
```go
type DataLoader struct {
    ingestor *Ingestor
    docsPath string
}

func (d *DataLoader) Load(ctx context.Context) {
    // 遍历 docsPath 下所有 .md/.txt 文件
    // 用 RecursiveDocumentSplitter 切片
    // 调用 ingestor.Ingest() 索引
}
```

**auto_reload.go — 对应 RagAutoReloadJob.java：**
```go
type AutoReload struct {
    ingestor       *Ingestor
    docsPath       string
    fileTimestamps map[string]int64  // 文件路径 → 最后修改时间
    mu             sync.Mutex
}

func (a *AutoReload) Start(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            a.scanAndReload(ctx)
        }
    }
}

func (a *AutoReload) scanAndReload(ctx context.Context) {
    a.mu.Lock()
    defer a.mu.Unlock()
    // 遍历目录，检查 lastModified 变化
    // 新/更新文件 → 加载并索引
}
```

### 6.14 错误码 (common/errno.go)

```go
type ErrorCode struct {
    Code    int
    Message string
}

var (
    ParamsError        = ErrorCode{40000, "请求参数错误"}
    SensitiveWordError = ErrorCode{40003, "包含敏感词，请求被拒绝"}
    SystemError        = ErrorCode{50000, "系统内部异常"}
    AIModelError       = ErrorCode{80000, "AI模型调用失败"}
    AIModelTimeout     = ErrorCode{80001, "AI模型响应超时"}
    MemoryCompressError = ErrorCode{80010, "记忆压缩失败"}
    MemoryStoreError   = ErrorCode{80011, "记忆存储失败"}
    RAGEmbeddingError  = ErrorCode{80020, "文档向量化失败"}
    RAGRetrievalError  = ErrorCode{80021, "知识检索失败"}
    RAGRerankError     = ErrorCode{80022, "重排序失败"}
    ToolExecutionError = ErrorCode{80030, "工具执行失败"}
    MCPConnectionError = ErrorCode{80040, "MCP服务连接失败"}
    GuardrailBlocked   = ErrorCode{80050, "内容安全检查未通过"}
)

type BusinessException struct {
    Code    int
    Message string
}

func (e *BusinessException) Error() string {
    return e.Message
}

func NewBusinessError(code ErrorCode, msg string) *BusinessException {
    if msg == "" {
        msg = code.Message
    }
    return &BusinessException{Code: code.Code, Message: msg}
}
```

### 6.15 统一响应 (common/response.go)

```go
type BaseResponse struct {
    Code    int         `json:"code"`
    Data    interface{} `json:"data"`
    Message string      `json:"message"`
}

func Success(data interface{}) *BaseResponse {
    return &BaseResponse{Code: 200, Data: data, Message: "ok"}
}

func Error(errCode ErrorCode, msg string) *BaseResponse {
    if msg == "" {
        msg = errCode.Message
    }
    return &BaseResponse{Code: errCode.Code, Data: nil, Message: msg}
}
```

### 6.16 MCP 工具集成 (chat/service.go 中初始化)

```go
// 对应 Java 版 McpToolConfig.java
func initMCPToolProvider(cfg *config.Config) {
    // BigModel Search MCP Client (HTTP SSE)
    // searchClient, _ := mcp.NewClient(ctx, &mcp.ClientConfig{
    //     Key: "BigModelSearchMcpClient",
    //     Transport: mcp.NewHTTPTransport(fmt.Sprintf(
    //         "https://open.bigmodel.cn/api/mcp/web_search/sse?Authorization=%s",
    //         cfg.BigModel.APIKey)),
    // })

    // Time MCP Client (Stdio)
    // timeClient, _ := mcp.NewClient(ctx, &mcp.ClientConfig{
    //     Key: "timeClient",
    //     Transport: mcp.NewStdioTransport("uvx", []string{"mcp-server-time", "--local-timezone=Asia/Shanghai"}),
    // })

    // provider := mcp.NewToolProvider(searchClient, timeClient)
}
```

---

## 七、技术栈映射与依赖清单

### Go 依赖 (go.mod)

```
github.com/gin-gonic/gin
github.com/redis/go-redis/v9
github.com/cloudwego/eino
github.com/cloudwego/eino-ext/components/model/qwen
github.com/cloudwego/eino-ext/components/embedding/dashscope
github.com/cloudwego/eino-ext/components/indexer/redis
github.com/cloudwego/eino-ext/components/retriever/redis
github.com/cloudwego/eino-ext/components/tool/mcp
github.com/cloudwego/eino-ext/components/document/transformer/splitter
github.com/cloudwego/eino/adk
github.com/cloudwego/eino/compose
github.com/prometheus/client_golang
github.com/spf13/viper
github.com/google/uuid
```

### 关键版本锁定建议

| 依赖 | 建议版本 | 原因 |
|---|---|---|
| eino | v0.8.x | 最新稳定版 |
| eino-ext | 对应 eino 版本 | 子模块版本需与 core 对齐 |
| gin | v1.10.x | 稳定版 |
| go-redis | v9.x | 最新大版本 |

---

## 八、架构差异与设计决策

### 8.1 PgVector → Redis 向量存储

**原因：** Eino 没有 PgVector 适配器，但 Redis 同时支持向量检索（FT.SEARCH）和会话记忆存储，架构更简洁。

**影响：**
- 向量存储用 Redis Hash + FT.SEARCH 替代 PgVector
- 向量维度不变（1024）
- 检索能力等价（KNN + Range Search）
- 部署简化（少一个 PostgreSQL 依赖）

### 8.2 ThreadLocal → context.Context

**原因：** Go 没有 ThreadLocal，用 `context.Context` 传递 MonitorContext。

**实现：**
```go
type contextKey string
const (
    RequestIDKey contextKey = "request_id"
    UserIDKey    contextKey = "user_id"
    SessionIDKey contextKey = "session_id"
)

func WithContext(ctx context.Context, userId, sessionId int64) context.Context {
    ctx = context.WithValue(ctx, RequestIDKey, uuid.New().String())
    ctx = context.WithValue(ctx, UserIDKey, userId)
    ctx = context.WithValue(ctx, SessionIDKey, sessionId)
    return ctx
}
```

### 8.3 Spring DI → 手动依赖管理

**原因：** Go 没有 Spring 的依赖注入容器，采用构造函数注入。

**模式：** main.go 负责所有组件的创建和连接，通过参数传递依赖。

### 8.4 @Scheduled → goroutine + time.Ticker

**原因：** Go 没有 Spring Scheduling，用 goroutine 实现定时任务。

### 8.5 Embedding Model 版本

**注意：** Java 版用 `text-embedding-v4`，但 Eino 的 dashscope embedding 组件只支持 v1/v2/v3。**使用 v3 即可**，输出维度支持 512/768/1024，与 Java 版的 1024 维度对齐。

### 8.6 Rerank API URL 差异

Java 版 URL: `https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank`

Go 版保持一致，因为都是调用同一 DashScope HTTP API。

---

## 九、分阶段实现计划（最终版）

### 第一阶段：项目骨架 + 基本对话（3-4 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. go mod init + 目录结构 | 项目骨架 | 编译通过 |
| 2. config.go + config.yaml | Viper 配置加载 | 配置正确读取 |
| 3. Gin 路由 + CORS + 基础中间件 | HTTP 框架 | curl /api/chat 返回 |
| 4. Qwen ChatModel 初始化 | chat/service.go | 模型连通 |
| 5. /chat 接口 | handler/chat_handler.go | 与 qwen-max 正常对话 |
| 6. 统一响应 + 错误码 | common/ | JSON 错误响应正确 |

### 第二阶段：RAG 全链路（4-5 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. DashScope Embedding 初始化 | chat/service.go | embed 调用成功 |
| 2. Redis Indexer + Retriever 初始化 | rag/store.go | 向量存取成功 |
| 3. RecursiveDocumentSplitter | rag/document_splitter.go | 切片结果正确 |
| 4. QueryPreprocessor | rag/query_preprocessor.go | 停用词过滤生效 |
| 5. QwenRerankClient | rag/rerank_client.go | Rerank API 调用成功 |
| 6. RerankingRetriever | rag/retriever.go | 两阶段检索正常 |
| 7. Ingestor | rag/indexer.go | 文档入库成功 |
| 8. DataLoader + AutoReload | rag/data_loader.go | 启动加载+定时扫描 |
| 9. RerankVerifier | rag/rerank_verifier.go | 启动验证通过 |
| 10. /insert 接口 | handler | 知识插入成功 |
| 11. RAG 注入 ChatService | chat/service.go | 对话自动检索知识 |

### 第三阶段：会话记忆 + 工具调用（3-4 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. RedisStore | memory/redis_store.go | 消息存取成功 |
| 2. Compressor | memory/compressor.go | Token 估算+摘要正确 |
| 3. CompressibleMemory | memory/compressible_memory.go | 分布式锁+压缩触发+降级 |
| 4. TimeTool | tool/time_tool.go | 返回正确时间 |
| 5. EmailTool | tool/email_tool.go | 邮件发送成功 |
| 6. RagTool | tool/rag_tool.go | 检索+添加知识 |
| 7. MCP 工具集成 | chat/service.go | MCP 连接成功 |
| 8. 工具注册到 Agent | chat/service.go | 对话中调用工具 |

### 第四阶段：多 Agent 编排（2-3 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. Agent 接口 | agent/agent.go | 编译通过 |
| 2. KnowledgeAgent | agent/knowledge_agent.go | 知识检索返回 |
| 3. ReasoningAgent | agent/reasoning_agent.go | 推理对话正常 |
| 4. Orchestrator | agent/orchestrator.go | 关键词分流正确 |
| 5. /multiAgentChat 接口 | handler | 接口返回正确 |

### 第五阶段：安全 + 可观测 + 流式（3-4 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. Guardrail 中间件 | middleware/guardrail.go | 敏感词被拦截 |
| 2. Observability 中间件 | middleware/observability.go | X-Request-ID 响应头 |
| 3. AI 指标 | monitor/ai_metrics.go | Prometheus 采集 |
| 4. RAG 指标 | monitor/rag_metrics.go | Prometheus 采集 |
| 5. /metrics 端点 | Gin 路由 | curl /metrics 返回 |
| 6. 流式输出 | chat/service.go | /streamChat SSE 正常 |
| 7. 前端页面 | static/gpt.html | 流式显示正常 |
| 8. docker-compose | docker-compose.yml | Redis+Prometheus+Grafana 启动 |

### 第六阶段：测试 + 收尾（2-3 天）

| 步骤 | 产出 | 验收 |
|---|---|---|
| 1. 单元测试 | _test.go | go test 通过 |
| 2. 集成测试 | 测试脚本 | 全链路验证 |
| 3. Dockerfile | Dockerfile | docker build 成功 |
| 4. README | README.md | 文档完整 |

**总计预估：17-23 天**

---

## 十、关键风险与应对

| 风险 | 影响 | 应对 |
|---|---|---|
| Eino ADK ChatModelAgent API 不稳定 | Agent 组装方式可能变化 | 参考 eino-examples 最新示例，锁定版本 |
| Eino Redis Indexer/Retriever 配置复杂 | Redis FT.SEARCH 需要预先创建索引 | 仔细阅读 eino-ext Redis 组件 README |
| Eino 流式输出适配 SSE | 需将 StreamReader 适配为 SSE 格式 | 参考前端 gpt.html 的 SSE 解析逻辑 |
| MCP Stdio Transport 在 Windows 兼容 | uvx mcp-server-time 可能需要调整 | 考虑改用 HTTP MCP 或内置时间工具 |
| DashScope Embedding v3 vs v4 | 向量维度可能不一致 | v3 支持 1024 维度，与 Java 版对齐 |
| 并发安全 | CompressibleMemory 分布式锁 | 完整复刻 Java 版 Lua 脚本锁逻辑 |
| 上下文传递 | Go 无 ThreadLocal | 用 context.Context + gin 中间件 |

---

## 十一、前端页面

原项目有 3 个 HTML 前端页面（gpt.html, qwen.html, gemini.html），功能基本相同：
- gpt.html 调用 `/api/streamChat`
- 左侧会话列表 + 右侧对话区
- 支持暗色/亮色主题切换
- userId 随机生成 + localStorage 持久化
- 流式输出（解析 SSE data: 前缀）
- 暗色渐变背景 + 圆角毛玻璃卡片风格

Go 版只需复制 `gpt.html` 到 `static/` 目录，确保 API_URL 指向正确的后端地址即可。前端无需任何修改。
