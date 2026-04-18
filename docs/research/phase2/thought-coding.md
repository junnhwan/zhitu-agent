# ThoughtCoding 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/ThoughtCoding`
> 语言栈：Java 17 + LangChain4j + JLine + picocli（58 Java 源文件，小而精）

## 1. 项目定位

**Claude Code 风格的 Java 版 CLI 编程助手**——支持流式输出、工具调用、MCP 集成，运行在终端。定位是"本地 IDE 替代品的 coding agent"，不是服务端 RAG 系统。

**和 ZhituAgent 定位差异大**，但**工具调用人机交互**、**多策略记忆压缩**等细节可以借鉴。

## 2. 架构概览

```
cli/                # picocli 命令入口（Session/Config/MCP 子命令）
core/
  ├── AgentLoop                       # 主循环（Agent 思考→工具调用→返回）
  ├── ThoughtCodingContext            # 应用上下文
  ├── MessageHandler                  # 消息处理
  ├── StreamingOutput                 # 流式输出
  ├── ProjectContext                  # 自动检测 Maven/Gradle/NPM
  ├── OptionManager                   # 🌟 AI 提供多选项，用户数字选择
  ├── ToolExecutionConfirmation       # 🌟 工具执行前用户确认（3 种智能选项）
  └── DirectCommandExecutor
service/
  ├── LangChainService                # AI 服务核心
  ├── SessionService                  # 会话保存/加载/继续
  ├── ContextManager                  # 🌟 上下文管理（滑动窗口/Token/混合三策略）
  └── PerformanceMonitor
tools/{exec,file,search}/*            # 工具实现
mcp/                                  # MCP 客户端（动态发现 + 适配）
ui/                                   # JLine + ANSI 终端 UI
```

## 3. 亮点实现

### 3.1 工具执行确认（Claude Code 风格）★★★★★
`core/ToolExecutionConfirmation.java`

```java
public enum ActionType {
    CREATE_ONLY,           // 仅创建
    CREATE_AND_RUN,        // 创建并运行
    DISCARD                // 丢弃
}

public ActionType askConfirmationWithOptions(ToolExecution execution) {
    if (autoApproveMode) return ActionType.CREATE_ONLY;
    displaySmartOptions(execution);  // 🌟 根据工具类型显示不同选项
    // 用户输入 1/2/3 → 对应 Action
}
```

**智能选项生成**：
- `write_file` 且是 `.java`：选项 2 变成"创建并立即编译运行（javac + java）"
- `write_file` 且是 `.py`：选项 2 变成"创建并立即运行（python3）"
- `command_executor` 且命令含 `rm -rf / sudo / git push --force`：**红色警告 + 选项 1 改为"是的，我确认要执行"**

**自动批准模式**：用户输入 `auto` 切换到免确认模式。

**价值**：把"工具调用的副作用风险"显式交还给用户，是高风险工具（send_email、insert_knowledge、rm）的最佳实践。

### 3.2 上下文管理三策略 + Micro Compact ★★★★★
`service/ContextManager.java`

**四层压缩**（递进式）：

1. **Micro Compact**（先做）：
   ```java
   // 超过 3 条的旧工具结果消息压缩为 "[Previous: used read_file]"
   for (ChatMessage msg : toCompact) {
       if (content.length() > 100) {
           msg.setContent("[Previous: used " + extractToolName(content) + "]");
       }
   }
   ```
   针对性压缩**工具调用结果**这类体积大但信息冗余的消息。

2. **Strategy.SLIDING_WINDOW**：保留最近 N 轮
3. **Strategy.TOKEN_BASED**（默认）：估算 Token（**中文 /2、英文 /4**），超过阈值 → **LLM 生成摘要** + 保留最后 2 条
4. **Strategy.HYBRID**：先滑动窗口再 Token 控制

**Token 估算公式**：
```java
int chineseChars = 0, otherChars = 0;
for (char c : text.toCharArray()) {
    if (c >= 0x4E00 && c <= 0x9FA5) chineseChars++;
    else otherChars++;
}
return (chineseChars / 2) + (otherChars / 4);
```

比 ZhituAgent 的 `len(text) / 4` 准确，针对中文做了单独估算。

**摘要提示词**（生产可用）：
```
Summarize this conversation for continuity. Include:
1) What was accomplished,
2) Current state,
3) Key decisions made.
Be concise but preserve critical details.
```

### 3.3 AI 多选项交互（OptionManager）★★★★
`core/OptionManager.java`

**流程**：
1. AI 回复包含编号选项列表（`1. xxx / 2. xxx / 3. xxx`）
2. `extractOptionsFromResponse` 用正则解析出选项 + **推断 action 类型**（overwrite/edit/create_new/run/view）
3. 用户输入 `1` 或 `"选择 1"`
4. `processOptionSelection` 根据 action 自动生成下一条命令（`"覆盖 HelloWorld.java，创建新的内容"`）

**价值**：把"模棱两可的输入"从模型侧抛给用户。对 ZhituAgent 的 `multiAgentChat`，**多意图场景可以列出候选让用户选**，比"强行猜测意图"体验好。

### 3.4 项目上下文自动检测（ProjectContext）★★★
启动时扫描 `pom.xml / build.gradle / package.json` 自动识别项目类型，注入到 system prompt。

### 3.5 会话保存/加载/继续 ★★★
`SessionService`：JSON 序列化会话到 `sessions/{uuid}.json`，支持 `session load` / `session list` / `session continue` 命令。

### 3.6 MCP 客户端（动态发现 + 预定义快捷）★★★
`MCPService` + `MCPToolManager`：
- 配置 `mcp.servers` 列表
- 启动时连接所有 server，用 `tools/list` 拉所有工具
- 工具自动注册到 `ToolRegistry`，无需重启即可挂载新 MCP server

### 3.7 危险命令检测 ★★★
在工具确认层检查命令字符串是否包含 `rm -rf / sudo / git push --force / docker rm / kill -9`，如果是则**红色警告 + 换一套更严肃的选项**。

## 4. 对 ZhituAgent 的启示

### 候选 A：会话记忆三策略 + Micro Compact ★★★★★
- **描述**：把 ZhituAgent 的"取前 3 条各截 50 字"升级为 ThoughtCoding 的三策略栈：
  1. **Micro Compact**：老的工具调用结果（RagTool 返回的检索 chunk）压成短标签
  2. **Token 控制**（中文 /2、英文 /4 的精准估算）超阈值时 **LLM 摘要旧对话** + 保留最近 2 条
  3. **Hybrid**（滑动窗口 + Token）
- **借鉴位置**：`ContextManager.java:102-130` 的 `getContextForAI` + `micro_compact` + `applyTokenLimit`。
- **Go+Eino 可行性**：低成本。Go 实现 Token 估算函数 + 摘要调用 + `micro_compact` 是纯函数逻辑。
- **升级 or 新增**：升级 `memory.Compress`（**和 OpsPilot 候选 D 合并**，形成完整方案）。
- **简历**：`会话记忆多策略压缩：Micro Compact 老工具结果 → 中英文分词 Token 估算（中文/2 + 英文/4）→ 超阈值 LLM 摘要压缩 + 保留最近 2 条明细，防止上下文膨胀同时保留关键信息`。
- **面试深入**：为什么工具结果消息需要特殊对待（体积大、时效性低）、Token 估算方法的准确性、摘要 vs 截取的权衡。
- **深度评分**：5/5（**比 OpsPilot 的纯摘要方案更细腻**）。

### 候选 B：工具执行确认（高风险工具交互）★★★★
- **描述**：ZhituAgent 有 `EmailTool`（发邮件）和 `RagTool`（写知识库）这些**有副作用的工具**。现在直接调用没有用户确认。加一个"确认中间件"：LLM 决定调用工具后，**流式先输出"即将调用 X 工具，参数 Y，确认执行吗？"**，等用户确认再真执行。
- **借鉴位置**：`ToolExecutionConfirmation.java` 的智能选项设计。
- **Go+Eino 可行性**：中成本。需要改 tool-call loop：模型产出 tool_call → 前端展示确认 UI → 收到用户确认才真执行 → 结果回流。SSE 双向通信。
- **升级 or 新增**：升级 tool 调用链路。
- **简历**：`高风险工具交互确认机制：Eino ReAct Agent 产出 tool_call 后暂停流式输出，前端展示工具名/参数/风险等级（危险命令如 rm -rf 红色警告），用户确认后才真正执行，支持 auto 全局免确认模式`。
- **面试深入**：流式 AI 中途暂停的实现、工具副作用的分级（read-only vs mutating vs destructive）、auto 模式的安全边界。
- **深度评分**：4/5。

### 候选 C：AI 多选项结构化输出 + 用户数字选择 ★★★
- **描述**：ZhituAgent 的 `multiAgentChat` 遇到意图不明时可以让 AI 主动列 3-4 个解读选项，用户输入数字选。
- **借鉴位置**：`OptionManager.java` 的正则解析 + action 推断。
- **Go+Eino 可行性**：低成本。但前端要配合。
- **升级 or 新增**：升级 `multiAgentChat`（结合 ragent 的 `IntentGuidanceService`）。
- **深度评分**：3/5（前端成本大于后端）。

### 候选 D：Token 估算中英文分开 ★★★
- **描述**：**这就是一行代码的改动**。把 ZhituAgent 的 `len(text) / 4` 改为"中文 / 2 + 英文 / 4"更准确。
- **借鉴位置**：`ContextManager.estimateTokens()`。
- **升级 or 新增**：升级 `memory.EstimateTokens`。
- **注意**：CLAUDE.md 明确说 "Token 估算：len(text) / 4，不要换成 tiktoken 之类" —— 这是被冻结的契约。如果要改，**先和契约协商或标注为有意的 Phase 2 升级**。
- **深度评分**：3/5（实现低，但需协商契约）。

### 候选 E：危险命令检测 ★★
- **描述**：如果将来接 shell 工具，借鉴危险命令模式匹配。当前 ZhituAgent 不需要。
- **深度评分**：2/5。

### 候选 F：MCP 动态工具发现 ★★★
- **描述**：和前面所有项目的 MCP 候选重合。
- **深度评分**：3/5（和 OpsPilot/ai-mcp-gateway 候选合并）。

## 5. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **A. 三策略记忆压缩 + Micro Compact** | ★★★★★ | 低 | 比 OpsPilot 方案更细腻，**合并成最终版** |
| 🥈 | **B. 工具执行确认** | ★★★★ | 中 | 对 EmailTool 等副作用工具价值高 |
| 🥉 | D. Token 估算中英文分开 | ★★★ | 极低 | 一行代码，但需协商契约 |
| 4 | C. 多选项交互 | ★★★ | 中 | 前端工作量大 |
| 5 | E. 危险命令检测 | ★★ | 低 | 当前用不上 |

## 6. 总结

ThoughtCoding 作为 CLI 工具，和 ZhituAgent 服务端定位错位，但有两个可直接借鉴的工程细节：

1. **Micro Compact + Token 估算更精准的记忆压缩**（比 OpsPilot 的单摘要方案更细腻）
2. **工具执行人机确认**（让 `EmailTool` 这种副作用工具更安全）

其他亮点（OptionManager、MCP、危险命令检测）已被其他项目覆盖或不适用。**核心价值：记忆压缩的细节升级**。
