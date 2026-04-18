# AgentX 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/AgentX`
> 语言栈：Java 17 + Spring Boot + MyBatis + LangGraph4j（用于顶层编排）+ 251 Java 源文件

## 1. 项目定位

**面向代码交付场景的 Agent 平台内核**——用 martin 的话说是"做 Coding Agent 的可持续工程化底座"。**和 ZhituAgent 定位差异大**（对话 + RAG vs 代码交付 + 工作流）。

核心理念："不是'怎么让模型偶尔写出一段代码'，而是'怎么把 Code Agent 做成能持续扩展、持续评测、持续优化的工程系统'"。**主链固定**：`requirement → architect → ticket-gate → task-graph → worker-manager → coding → merge-gate → verify`。

**借鉴价值**：通用架构方法论 + 评测体系 + 上下文编译，**不是具体功能**。

## 2. 架构概览

**三层架构**（不是 DDD 标准三层，而是 AgentX 自定义的）：

```
controlplane/   # 命令入口、目录管理、人工命令（Web 层 + 固定工作流编排）
domain/         # 聚合、值对象、状态机、不变量（纯领域逻辑）
runtime/        # LangGraph 编排 + Agent Pool + Task Run + Worktree + Run Supervisor
└── context/         # Context Compilation Center（上下文编译）
└── evaluation/      # WorkflowEvalCenter（评测中心）
└── retrieval/       # FactRetriever + RepoIndexService + LexicalChunkRetriever + Planner
└── orchestration/   # LangGraph 节点派发
└── workspace/       # Git worktree 隔离
└── application/     # workflow profile（Stack Profile）
└── agentkernel/     # architect / worker 等 agent 逻辑
└── persistence/
└── tooling/
└── support/
db/
├── schema/      # L1-L5 五层数据真相
├── seeds/profiles/  # Stack Profile 种子数据
└── demo/
```

底层：**MySQL 为"真相层"**（L1-L5）承载持久化，Redis 非核心，未引入向量库（代码检索走本地 lexical index）。

## 3. 亮点实现

### 3.1 Context Compilation Center（上下文编译中心）★★★★★
`runtime/context/DefaultContextCompilationCenter.java`

```java
public CompiledContextPack compile(ContextCompilationRequest request) {
    FactBundle factBundle = factRetriever.retrieve(request);        // 结构化事实
    RetrievalBundle retrievalBundle = retrievalBundle(request, ...); // 召回片段
    String fingerprint = fingerprint(request, factBundle, retrievalBundle);  // 🌟 内容哈希
    String contentJson = serializedPayload(...);
    String trimmedContentJson = trimIfNeeded(...);                   // 🌟 长度裁剪
    Path artifactPath = writeArtifact(request, trimmedContentJson); // 🌟 落盘
    workflowEvalTraceCollector.recordContextPack(contextPack);      // 🌟 评测追踪
    return new CompiledContextPack(...);
}
```

**精华**：
- **"上下文"不再是散落在各节点的 prompt 拼接**，而是统一编译产物
- **支持 PackType**：REQUIREMENT / ARCHITECT / CODING / VERIFY 各有不同的上下文组合
- **Fingerprint**：同样请求生成相同哈希，可缓存
- **Artifact 落盘**：可以事后检查"当时喂给模型的到底是啥"
- **WorkflowEvalTraceCollector**：每次编译都被评测中心记录，方便回答"失败是上下文不对还是模型不对"

### 3.2 Eval Center（evidence-first 评测中心）★★★★★
`runtime/evaluation/WorkflowEvalCenter.java`

输出固定三件套：
- **`raw-evidence.json`**：原始证据（所有节点输入/输出/状态）
- **`scorecard.json`**：结构化评分（按维度 + 严重等级）
- **`workflow-eval-report.md`**：人类可读的 markdown 报告

**核心领域模型**：
- `EvalScenario`：测试场景定义
- `EvalEvidenceBundle`：一次运行的所有证据（workflow snapshot、artifact refs）
- `EvalDimensionResult`：单个维度评分（schema / catalog / tool protocol / RAG / runtime robustness）
- `EvalFinding` + `EvalFindingSeverity`：具体问题 + 严重等级
- `EvalScorecard`：综合评分卡

**精华**：
- **不是一个 AI 裁判给个总分**，而是分维度说清楚"哪里出了问题"
- **评测结果本身是可版本化的 artifact**，便于同场景多轮迭代对比

### 3.3 Stack Profile（技术栈扩展不污染主链）★★★★
- `db/seeds/profiles/` 存 profile manifest
- 一个 profile 包含：planner 允许的 task template + prompt 补充规则 + capability runtime + verify 命令 + eval 分类规则
- 业务上用 `profileId` 路由到对应 profile 配置

**价值**：同一套工作流内核，通过 profile 支持 Java / TypeScript / Python 等不同技术栈，**不需要复制主链代码**。类似 Spring Profile 但针对 Agent 配置。

### 3.4 三层严格边界 ★★★★
`controlplane` ↔ `domain` ↔ `runtime` 通过**显式命令/端口/事件**交互，**不允许直接调用**：
- `controlplane → domain` 只通过聚合根方法
- `runtime → domain` 只通过明确接口
- `controlplane ↔ runtime` 零直接耦合

**价值**：Go 标准的 `cmd/internal/pkg` 已经隐含这种分层，但 AgentX 强制得更彻底。

### 3.5 LangGraph 顶层编排 ★★★
`runtime/orchestration/` 用 LangGraph4j（LangChain Graph Java 版）编排 8 个主节点。**和 Eino Graph 是同概念**，都是 DAG/状态机式编排。

### 3.6 Heartbeat + Lease + Recovery（Agent 可恢复运行）★★★★
`RunSupervisor` 负责：
- 每个 Task Run 有 `heartbeat` 定期上报
- 每个 worker 持有 `lease`，失联超时自动释放
- 失联 worker 任务自动 recovery（重分配或升级）

**价值**：真正的"长时运行 Agent"需要这些生产特性。ZhituAgent 是短对话，用不上；但如果要做"后台离线处理 Agent"（如每小时自动抓取新闻入库）则需要。

### 3.7 Repo Local Skills（项目内 Claude Code Skill）★★★
`.codex/skills/` 存项目专属的 Claude Code Skill：
- `agentx-capability-profile-author`
- `agentx-eval-report-reader`
- `agentx-eval-scenario-pack-author`
- `agentx-interview-bank-curator`

**这是一个有意思的元趋势**：项目自带"怎么和 AI 协作"的 skill 清单。

## 4. 对 ZhituAgent 的启示

### 候选 A：RAG 评测中心（evidence-first）★★★★★
- **描述**：ZhituAgent 目前无评测体系——改了 prompt 或换了模型，效果好坏全靠感觉。借鉴 AgentX 的 Eval Center 做一个**RAG 评测中心**：
  - 定义 `EvalScenario`（标准问答集 + 期望答案）
  - 每次跑测试输出 `raw-evidence.json`（每个问题的意图识别、检索结果、最终答案）+ `scorecard.json`（命中率、答案 BLEU/ROUGE、citation 准确性、响应延迟）+ `report.md`
  - 支持**多轮对比**：同场景跑两个版本看哪里变好哪里变差
- **借鉴位置**：`runtime/evaluation/WorkflowEvalCenter.java` 的整体设计 + 三件套 artifact 约定。
- **Go+Eino 可行性**：中成本。新增 `eval/` 模块 + 测试集 JSON + 运行器 + 报告生成器。可以先做简化版（一种 scorecard 维度）。
- **升级 or 新增**：新增。
- **简历**：`为 RAG 系统设计 evidence-first 评测中心：场景包（questions.yaml）→ 运行流水线 → 输出 raw-evidence.json + scorecard.json + workflow-eval-report.md 三件套，支持多版本回归对比，驱动 prompt/检索策略持续优化`。
- **面试深入**：能讲 RAG 评测的难点（ground truth 获取、LLM 评分的偏差、幻觉检测）、evidence vs score 分离的意义、场景化回归 vs 单元测试的差异。
- **深度评分**：5/5（**这是最易被忽略但最能体现工程素养的点**）。

### 候选 B：Context Compilation Center（上下文编译）★★★★
- **描述**：把 ZhituAgent 现在分散在各 service 里的"拼 system prompt + 拼检索结果 + 拼历史"逻辑**统一抽出一个 `ContextCompiler`**，输入 `(packType, scope)`，输出 `CompiledContextPack{fingerprint, content, facts, retrieval}`。
- **借鉴位置**：`DefaultContextCompilationCenter.java` 的整体设计。
- **Go+Eino 可行性**：低-中成本。Eino 本来就有 `compose.ChatTemplate` 可以做类似事情，但封装成显式的"编译中心"更清晰。
- **升级 or 新增**：升级现有 `chat.ChatService.composeMessages`。
- **简历**：`将对话上下文组装抽象为 Context Compiler：输入（session / intent / retrieval） → 输出 fingerprinted ContextPack（JSON 持久化），支持缓存、审计、评测追踪`。
- **面试深入**：fingerprint 的哈希算法选型、大上下文 trim 策略、落盘 artifact 的存储位置、ContextPack 和评测系统的联动。
- **深度评分**：4/5。

### 候选 C：Stack Profile（配置化多场景）★★★
- **描述**：ZhituAgent 若想支持多场景（客服 / 技术问答 / 代码助手），可以引入 `profileId` 配置，每个 profile 定义 system prompt / 可用工具集 / 检索范围。
- **Go+Eino 可行性**：中成本。
- **升级 or 新增**：新增，或作为 MultiAgent 的升级。
- **深度评分**：3/5（偏业务扩展，ZhituAgent 当前单场景用不上）。

### 候选 D：三层架构（controlplane/domain/runtime）★★
- **Go+Eino 可行性**：高成本（重构）。**Go 标准 project layout 已经覆盖**，不必强求。
- **深度评分**：2/5。

### 候选 E：Repo Local Skills（项目内 AI skill）★★
- **描述**：给 ZhituAgent 配 Claude Code skill 方便后续协作。但这是"工程习惯"不是"代码能力"，不是简历点。
- **深度评分**：2/5。

## 5. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **A. RAG 评测中心** | ★★★★★ | 中 | **这批项目里独特的差异化点，强烈推荐** |
| 🥈 | **B. Context Compilation** | ★★★★ | 低-中 | 搭配 A 做，评测需要 fingerprinted context |
| 🥉 | C. Stack Profile | ★★★ | 中 | Phase 3 扩展 |
| 4 | D. 三层架构重构 | ★★ | 高 | 不推荐 |

## 6. 总结

AgentX 和 ZhituAgent 定位错位，不适合整包借鉴。但它有**一个被其他所有项目忽略的独特能力——系统化评测**：

> "如果没有评测，所谓'优化'都是感觉驱动。"

这一句话值得 ZhituAgent 吸收：**做个能回归对比的 RAG Eval Center**，哪怕最简版（10 道标准问题 + 自动跑 + 输出命中率报告），也比没有任何评测强得多。

搭配 **Context Compilation**（候选 B）作为评测的"输入快照"，组合起来能写进简历：`基于 evidence-first 方法论为 RAG 系统建立评测中心 + 上下文编译层，每次运行生成可追溯的 ContextPack 和三件套 scorecard，驱动多轮优化并量化改进收益`。
