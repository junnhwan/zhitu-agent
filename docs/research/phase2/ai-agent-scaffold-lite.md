# ai-agent-scaffold-lite 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/xfg/ai-agent-scaffold-lite`
> 语言栈：Java 17 + Spring Boot 3 + **Spring AI + Google ADK for Java** + MyBatis（DDD 6 层）

## 1. 项目定位

**AI Agent 脚手架（Lite 版）**——同作者（小傅哥）作品。核心设计点是**从数据库配置装配 Agent**：管理员在 DB 里配置 Agent 名称、描述、模型、工具、Workflow 类型（Loop/Parallel/Sequential），运行时一键"装配"成可执行的 Agent。

用 Google ADK（`com.google.adk.agents.{LlmAgent, LoopAgent, ParallelAgent, SequentialAgent}`）作为 Agent 抽象，Spring AI 作为 LLM 层适配器。

## 2. 架构概览

```
ai-agent-scaffold-lite-api/             # DTO + Response + IAgentService 接口
ai-agent-scaffold-lite-app/             # SpringBoot 启动 + AiAgentAutoConfig
ai-agent-scaffold-lite-trigger/         # HTTP Controller
ai-agent-scaffold-lite-domain/
  └── agent/service/
      ├── armory/                       # 装配厂：把 DB 配置变成运行时 Agent
      │   ├── factory/DefaultArmoryFactory.java      # Node 链入口
      │   ├── node/                                  # 装配链节点
      │   │   ├── RootNode → AiApiNode → ChatModelNode → AgentNode → AgentWorkflowNode → RunnerNode
      │   │   └── workflow/{LoopAgentNode, ParallelAgentNode, SequentialAgentNode}
      │   └── matter/                                # 装配所需物料
      │       ├── mcp/client/{Local,SSE,Stdio}ToolMcpCreateService.java  # 🌟 3 种 MCP 传输
      │       ├── mcp/server/MyTestMcpService.java
      │       ├── skills/                           # 本地工具技能
      │       ├── plugin/{MyLogPlugin, MyTestPlugin}  # 插件系统
      │       └── patch/{MyMessageConverter, MySpringAI}  # 自定义 Spring AI 扩展
      └── chat/ChatService.java
ai-agent-scaffold-lite-infrastructure/  # DAO + Redis + MCP Gateway adapter
ai-agent-scaffold-lite-types/           # 常量 + 枚举 + 异常
```

## 3. 亮点实现

### 3.1 Agent 装配厂（Armory Pattern）★★★★★
核心：`domain/agent/service/armory/` 一整套 Node 链

**流程**：管理员在 DB 里定义 Agent 配置（AI API、ChatModel、Agent 列表、Workflow 编排）→ 调 `/armory?agentId=xxx` 触发装配 → Node 链依次执行 → 最终得到一个可运行的 `Runner`。

**Node 链职责分解**：
```
RootNode              → 查 DB 取配置
AiApiNode             → 设置 OpenAI 兼容 API（baseUrl/apiKey）
ChatModelNode         → 构建 ChatModel，绑定 MCP 工具
AgentNode             → 把配置里的每个 agent 变成 LlmAgent 放进 agentGroup
AgentWorkflowNode     → 根据 workflow type 路由到下方某个节点
  ├─ LoopAgentNode        → LoopAgent.builder().subAgents(...).maxIterations(N)
  ├─ ParallelAgentNode    → ParallelAgent.builder().subAgents(...)
  └─ SequentialAgentNode  → SequentialAgent.builder().subAgents(...)
RunnerNode            → 包装成可执行 Runner
```

**关键代码**（`LoopAgentNode.java:30-36`）：
```java
LoopAgent loopAgent = LoopAgent.builder()
    .name(currentAgentWorkflow.getName())
    .description(currentAgentWorkflow.getDescription())
    .subAgents(subAgents)                      // DB 里配的子 agent 名
    .maxIterations(currentAgentWorkflow.getMaxIterations())
    .build();
```

**价值**：**把 Agent 从硬编码变成数据驱动**。你可以在 DB 里改 Agent 配置、加 workflow，不用改代码。面试能讲"配置即代码"、"装配模式"、"责任链解耦"。

### 3.2 MCP Client 三种传输方式 ★★★★★
`domain/agent/service/armory/matter/mcp/client/impl/`：
- **LocalToolMcpCreateService** - 本地进程
- **SSEToolMcpCreateService** - HTTP SSE（远端）
- **StdioToolMcpCreateService** - 标准输入输出（子进程）

SSE 实现片段（`SSEToolMcpCreateService.java:51-66`）：
```java
HttpClientSseClientTransport sseClientTransport = HttpClientSseClientTransport
    .builder(baseUri)
    .sseEndpoint(sseEndpoint)
    .build();

McpSyncClient mcpSyncClient = McpClient.sync(sseClientTransport)
    .requestTimeout(Duration.ofMillis(sseConfig.getRequestTimeout())).build();
mcpSyncClient.initialize();

return SyncMcpToolCallbackProvider.builder()
    .mcpClients(mcpSyncClient).build()
    .getToolCallbacks();
```

**价值**：MCP 协议规范里 **Stdio 和 SSE 是两个主流传输**，你以后接 Claude Desktop（Stdio 通信）或 web 部署的 MCP Server（SSE）都能复用。

### 3.3 Google ADK Workflow Agent（Loop/Parallel/Sequential）★★★★
Google 官方 ADK 定义了三种编排原语：
- **LoopAgent**：循环执行子 agent 直到终止（带 `maxIterations`）—— 适合 Plan-Execute-Replan 这种迭代场景
- **ParallelAgent**：多个子 agent 并发执行 —— 适合并行调研（像这次你派的 7 个子 agent）
- **SequentialAgent**：串行执行 —— 适合 pipeline

**OpsPilot 的 Plan-Execute-Replan 本质上是 LoopAgent 的一个实例**。这三种原语是更底层的抽象。

### 3.4 Spring AI AutoConfig + 配置属性类 ★★★
`config/AiAgentAutoConfig.java` + `properties/AiAgentAutoConfigProperties.java`：把 Agent 装配的参数（默认模型、默认超时、MCP 默认端点等）全部放进 `@ConfigurationProperties`，Spring Boot 启动时自动注入。**"库化"能力**。

### 3.5 Plugin 扩展点（自定义日志/埋点）★★★
`matter/plugin/{MyLogPlugin, MyTestPlugin}`：实现 ADK 的 Plugin 接口，可以 hook 到 Agent 执行的各个阶段（before/after agent run）。**和 Eino Callbacks 是同一思路**。

### 3.6 自定义 Spring AI Patch ★★
`matter/patch/MySpringAI.java` + `MyMessageConverter.java`：当 Spring AI 原生不支持某些 ChatModel 特性时，用 patch 类扩展。**工程"遇坑就补"的实用主义**。

## 4. 对 ZhituAgent 的启示

### 候选 A：从 DB 配置装配 Agent（Armory Pattern）★★★★
- **描述**：ZhituAgent 的 multiAgent 是硬编码的 KnowledgeAgent + ReasoningAgent。加一个 `aiAgentConfig` 表（agentName, description, systemPrompt, model, tools, workflowType, subAgents），运行时通过 Eino Graph 装配。这样**新增 Agent 不用改代码**，只要改 DB。
- **借鉴位置**：`armory/node/*` 的 Node 链模式 + `AiAgentConfigTableVO` 的配置结构。
- **Go+Eino 可行性**：中成本。需要加 DB 表（SQLite 或 Redis Hash）+ Go 版 Node 链 + Eino Graph 动态构建。
- **升级 or 新增**：升级 `multiAgent`。
- **简历**：`基于装配厂模式（Armory）实现 Agent 配置化：从数据库读取 Agent 定义（name/model/tools/workflow）通过 Node 链装配为可执行 Eino Graph，支持运行时新增/修改 Agent 无需重启`。
- **面试深入**：能讲"配置即代码"、责任链模式、工厂模式、运行时 vs 启动时装配的权衡。
- **深度评分**：4/5（但和 ZhituAgent 当前简单架构有匹配度问题——可能过度工程）。

### 候选 B：MCP Client 多传输支持（SSE + Stdio + Local）★★★★
- **描述**：OpsPilot 只支持 SSE，`ai-agent-scaffold-lite` 支持 3 种。ZhituAgent 接入 MCP 时同时支持 SSE 和 Stdio 可以**覆盖所有主流 MCP Server**（Claude Desktop 走 Stdio、web 服务走 SSE）。
- **借鉴位置**：`matter/mcp/client/impl/{Local,SSE,Stdio}ToolMcpCreateService.java`。
- **Go+Eino 可行性**：低成本。`mark3labs/mcp-go` 同时支持 SSE 和 Stdio transport。
- **升级 or 新增**：新增（结合前面 OpsPilot 的候选 E 一起做）。
- **简历**：`实现 MCP 多传输适配层：SSE（HTTP 长连接）+ Stdio（子进程管道）双模式，支持接入 Claude Desktop / Cursor / 自建 MCP Server`。
- **面试深入**：Stdio vs SSE 的适用场景（本地工具 vs 远程服务）、子进程管理、传输层抽象。
- **深度评分**：4/5。

### 候选 C：Workflow Agent 三原语（Loop/Parallel/Sequential）★★★★
- **描述**：Eino 也有类似概念（Plan-Execute 是 Loop 的特例），但没有 Parallel 这么明确。可以在 ZhituAgent 里建一个 **Workflow 抽象层**，把"多 Agent 并行调研 + 结果合并" 做成一等公民。
- **借鉴位置**：`node/workflow/{Loop,Parallel,Sequential}AgentNode.java`。
- **Go+Eino 可行性**：中成本。Eino Graph 本身能实现，但需要封装成三种明确的 workflow 原语。
- **升级 or 新增**：升级 `multiAgent`（和候选 A 互补）。
- **深度评分**：3/5（和 OpsPilot 的 Plan-Execute 有重合）。

### 候选 D：Plugin 扩展点 ★★★
- **描述**：在关键路径插入 hook，类似 Spring 的 AOP。**Eino Callbacks 已经提供相同能力**，不必新造。
- **深度评分**：3/5（已被 OpsPilot 候选 F 覆盖）。

### 候选 E：AutoConfig 库化 ★★
- **描述**：把 ZhituAgent 做成可嵌入的 Go Module/package，别人 `go get` 即可集成。工程精度高但和当前定位"独立 AI 应用"不匹配。
- **深度评分**：2/5。

## 5. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **B. MCP 多传输（SSE + Stdio）** | ★★★★ | 低 | 和 OpsPilot 候选 E 合并成"MCP 客户端双传输" |
| 🥈 | A. Armory（DB 配置装配） | ★★★★ | 中 | 亮点大但工程量中等，放 Phase 3 考虑 |
| 🥉 | C. Workflow 三原语 | ★★★ | 中 | 被 OpsPilot 覆盖 |
| 4 | D. Plugin 扩展点 | ★★★ | 低 | 被 Eino Callbacks 覆盖 |
| 5 | E. AutoConfig 库化 | ★★ | 高 | 不推荐 |

## 6. 结论

这个项目**单独看借鉴价值不如 OpsPilot + ai-mcp-gateway 组合**：
- Agent 编排：OpsPilot 的 Eino Plan-Execute 更贴合你的技术栈
- MCP Client：OpsPilot 已经覆盖基本用法，本项目多一个**Stdio 传输**值得补上
- DDD 架构：和 ai-mcp-gateway 重复
- Google ADK：Java 生态独有，Go 不直接适用

**唯一独特且高价值的借鉴点：MCP Stdio 传输**，让 ZhituAgent 能被 Claude Desktop 等桌面 MCP 客户端作为工具接入。其他点被其他项目覆盖。
