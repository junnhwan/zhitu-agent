# ai-mcp-gateway 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/xfg/ai-mcp-gateway`
> 语言栈：Java 17 + Spring Boot 3 + WebFlux + MyBatis + MySQL + Retrofit + Guava（DDD 6 层架构）

## 1. 项目定位

**MCP 网关**——把**现有的 HTTP/OpenAPI 接口**转换成**MCP Server**供 AI Agent 调用。作者是 "小傅哥"（`bugstack.cn`），这是个系统化的 DDD 脚手架示范项目。

**一句话**：OpsPilot 是 MCP **客户端**（消费工具），ai-mcp-gateway 是 MCP **服务端**（暴露工具）——合起来就是完整的 MCP 协议两端。

## 2. 架构概览（教科书级 DDD）

```
ai-mcp-gateway-api/             # 对外接口 + DTO + 统一 Response
ai-mcp-gateway-app/             # Spring Boot 启动 + 配置
ai-mcp-gateway-trigger/         # 触发层（HTTP Controller / 事件）
ai-mcp-gateway-case/            # 用例层（tree strategy 编排）
  └── mcp/
      ├── session/              # 建立 SSE 会话：VerifyNode → SessionNode → EndNode
      └── message/              # 处理消息：RootNode → SessionNode → MessageHandlerNode
ai-mcp-gateway-domain/          # 领域层
  ├── auth/                     # apiKey 鉴权 + 限流 + 注册
  ├── gateway/                  # 网关配置（网关 ID / tool / 协议映射）
  ├── protocol/                 # Swagger/OpenAPI → MCP 协议解析
  └── session/                  # MCP 消息处理（tools/list, tools/call）
ai-mcp-gateway-infrastructure/  # 基础设施（DAO / Retrofit / Redis）
ai-mcp-gateway-types/           # 公共枚举/异常/常量
```

**特点**：
- 严格 DDD：domain 是核心，case 做编排，infrastructure 做技术适配
- 大量使用 `cn.bugstack.wrench`（作者自己的设计模式工具库）里的 `StrategyHandler<P, C, R>` 泛型接口

## 3. 亮点实现

### 3.1 Tree Strategy 责任链处理 MCP 请求 ★★★★★
核心文件：`ai-mcp-gateway-case/src/main/java/cn/bugstack/ai/cases/mcp/session/node/*.java`

建立 SSE 会话是一条 **Node 链**：
```
RootNode → VerifyNode（鉴权）→ SessionNode（建会话）→ EndNode
```

每个 Node 继承 `AbstractMcpSessionSupport`，实现两个方法：
- `doApply(req, ctx)`：本节点的业务逻辑
- `get(req, ctx)`：返回下一个 Node（**路由决策**）

```java
@Service("mcpSessionVerifyNode")
public class VerifyNode extends AbstractMcpSessionSupport {
    @Override
    protected Flux<SSE> doApply(String req, DynamicContext ctx) {
        boolean ok = authLicenseService.checkLicense(...);
        if (!ok) throw new AppException(INSUFFICIENT_PERMISSIONS);
        return router(req, ctx);  // 自动路由到 get() 返回的下一节点
    }

    @Override
    public StrategyHandler<...> get(String req, DynamicContext ctx) {
        return sessionNode;  // 下一个节点
    }
}
```

**价值**：比 if-else 或单一 Service 方法灵活，每个节点单一职责，**可插入可替换**（如加日志节点、埋点节点、降级节点），**类似 Eino Graph 的节点概念**，但更轻量。

### 3.2 OpenAPI/Swagger → MCP Tool 协议自动转换 ★★★★★
核心文件：
- `ai-mcp-gateway-domain/.../protocol/service/analysis/ProtocolAnalysis.java`
- `AbstractProtocolAnalysisStrategy.java`

思路：
1. 传入 **OpenAPI JSON 文档 + endpoint 列表**（如 `/api/order/create`）
2. 解析 `paths.{endpoint}.{method}.requestBody.content.application/json.schema`
3. 递归展开 `$ref` 到 `components.schemas`
4. 输出 `HTTPProtocolVO.ProtocolMapping` 列表：`{parentPath, fieldName, mcpPath: "a.b.c", mcpType, mcpDesc, isRequired, sortOrder}`
5. 用枚举 `AnalysisTypeEnum.SwaggerAnalysisAction` 选择策略（object/array/primitive 各有不同解析逻辑）

```java
protected void parseProperties(String parentMcpPath, JSONObject properties, ...) {
    for (String propName : properties.keySet()) {
        JSONObject prop = properties.getJSONObject(propName);
        String currentMcpPath = parentMcpPath + "." + propName;
        if (prop.containsKey("$ref")) {
            String refName = ref.substring(ref.lastIndexOf('/') + 1);
            effectiveSchema = definitions.getJSONObject(refName);
        }
        // 产出 ProtocolMapping
        if (effectiveSchema.containsKey("properties")) {
            parseProperties(currentMcpPath, ...);  // 递归
        }
    }
}
```

**价值**：把一个 Spring 项目的 Swagger 喂进来，**自动变成 MCP Server 暴露的工具列表**，零手写 Schema。

### 3.3 Generic HTTP Gateway（动态调后端）★★★★
`ai-mcp-gateway-infrastructure/.../GenericHttpGateway.java`

```java
public interface GenericHttpGateway {
    @POST
    Call<ResponseBody> post(@Url String url, @HeaderMap Map<String,Object> headers, @Body RequestBody body);

    @GET
    Call<ResponseBody> get(@Url String url, @HeaderMap Map<String,Object> headers, @QueryMap Map<String,Object> queryParams);
}
```

用 **Retrofit `@Url` 动态 URL**，一个接口处理**所有后端 HTTP 调用**。MCP 网关收到 `tools/call` 请求 → 查 DB 找到映射 → 组装 URL/Header/Body → 调后端。

### 3.4 Guava Cache + RateLimiter 限流 ★★★★
`ai-mcp-gateway-domain/.../auth/service/ratelimit/AuthRateLimitService.java`

```java
private final Cache<String, RateLimiter> rateLimiterCache = CacheBuilder.newBuilder()
        .expireAfterAccess(1, TimeUnit.HOURS)
        .build();

public boolean rateLimit(RateLimitCommandEntity cmd) {
    RateLimiter limiter = rateLimiterCache.get(gatewayId + "_" + apiKey, () -> {
        var auth = repository.queryEffectiveGatewayAuthInfo(...);
        double permitsPerSecond = (double) auth.getRateLimit() / 3600;  // 每小时次数 → 每秒
        return RateLimiter.create(permitsPerSecond);
    });
    return !limiter.tryAcquire();  // 返回 true 表示"触发限流"
}
```

**亮点**：
- **双层键**（gatewayId + apiKey）：每个 API key 单独限流
- **LoadingCache**：RateLimiter 懒加载并缓存 1 小时，避免每次查 DB
- **小时→秒单位转换**：业务层用"次/小时"更符合用户认知，底层用 Guava 每秒令牌桶
- 异常分类处理（IllegalState = 无配置放行 / IllegalArgument = 配置错误拒绝）

### 3.5 Spring WebFlux SSE + 反应式接口 ★★★
`ai-mcp-gateway-api/.../IMcpGatewayService.java`

```java
Flux<ServerSentEvent<String>> handleSseConnection(String gatewayId, String apiKey);
Mono<ResponseEntity<Void>> handleMessage(String gatewayId, String apiKey, String sessionId, String messageBody);
```

用 `Flux`（多个事件）+ `Mono`（单个事件）组合 WebFlux 原生 SSE，**非阻塞**，一个线程能扛大量长连接。

### 3.6 API Key 注册 + License 许可双层鉴权 ★★★
- `AuthRegisterService`：用户注册，生成 apiKey
- `AuthLicenseService`：每次调用前校验 apiKey 对 gatewayId 的权限
- `AuthRateLimitService`：校验通过后做限流

三个服务独立，单一职责。

### 3.7 DDD 6 层架构模板 ★★★
作者的 `xfg-frame-archetype v2.2` 是个可复用模板。每层 pom 独立、接口与实现分离（`IXxxService` + `XxxService`），`types` 放纯粹的 enum/exception，`api` 放对外 DTO。

## 4. 对 ZhituAgent 的启示

### 候选 A：把 ZhituAgent 暴露为 MCP Server ★★★★★
- **描述**：这是**最顶级的借鉴价值**。当前 ZhituAgent 是"端到端对话系统"，如果暴露 MCP Server（比如 `/mcp/sse` + `/mcp/message`）让外部 AI（Claude Desktop / Cursor / OpsPilot）能**像调工具一样调 ZhituAgent 的 `rag.Retrieve` 和 `insertKnowledge`**，项目定位立刻从"应用"升级为"基础设施"。
- **借鉴位置**：`cases/mcp/session/*` + `cases/mcp/message/*` 的节点链 + `IMcpGatewayService` 接口。
- **Go+Eino 可行性**：中成本。**`mark3labs/mcp-go` 提供 Go 版 MCP Server SDK**（OpsPilot 用它做 client，你用它做 server），再加 gin SSE handler 即可。
- **升级 or 新增**：新增（独立 `cmd/mcp-server` 或在现有 server 加 `/mcp/*` 路由）。
- **简历**：`基于 mark3labs/mcp-go 实现 MCP Server，将 ZhituAgent 的 RAG 检索能力暴露为 MCP 标准工具（tools/list + tools/call），支持 SSE 长连接与 api_key 鉴权，可接入 Claude Desktop / Cursor 等 MCP 客户端`。
- **面试深入**：能讲 MCP 协议规范（Initialize 握手 → Capabilities 协商 → tools/list → tools/call 循环）、SSE 在 MCP 中的角色、session 管理、MCP Server vs Tool schema 的对应关系。
- **深度评分**：5/5（**和 OpsPilot 的 MCP Client 双向完整，简历能形成完整故事**）。

### 候选 B：Guava 本地限流 + API Key 鉴权中间件 ★★★★
- **描述**：ZhituAgent 当前只有 Guardrail 敏感词过滤，没有流量/权限管控。加一层 API Key + 限流中间件。
- **借鉴位置**：`AuthRateLimitService.java` 的 **LoadingCache<key, RateLimiter>** 模式，小时→秒的转换。
- **Go+Eino 可行性**：低成本。Go 有 `golang.org/x/time/rate` 做令牌桶 + `groupcache/lru` 或 `hashicorp/golang-lru` 做缓存。实现逻辑一致。
- **升级 or 新增**：新增 `middleware/ratelimit.go`。
- **简历**：`基于令牌桶（x/time/rate）和 LRU 缓存实现多租户限流中间件：按 (userID, apiKey) 二元组缓存 RateLimiter 实例，1 小时空闲淘汰，支持业务层按"次/小时"配置、底层以"次/秒"执行`。
- **面试深入**：能讲令牌桶 vs 漏桶、单机限流 vs 分布式限流（Redis + Lua）、为什么缓存 RateLimiter 实例（避免每次重建开销）、`permitsPerSecond` 计算的精度。
- **深度评分**：4/5。

### 候选 C：Tree Strategy 责任链重构多 Agent 路由 ★★★★
- **描述**：ZhituAgent 的 `multiAgentChat` 现在是**关键词 switch**路由。换成**Node 链**：`InputNode → GuardrailNode → RoutingNode → KnowledgeEnhanceNode → ReasoningNode`，每个 Node 负责单一职责，可扩展（加日志、埋点、降级）。
- **借鉴位置**：`AbstractMcpSessionSupport` 的 `doApply + get` 模式。
- **Go+Eino 可行性**：低成本（Go 用接口 + 委托直接写）。但其实 **Eino 的 Graph 已经是这个模式**——所以这条**和 OpsPilot 的 Eino Graph 重叠**。
- **深度评分**：3/5（和 OpsPilot 候选 B 重复，**优先走 Eino Graph 原生方案**）。

### 候选 D：OpenAPI 文档 → MCP Tool 自动转换 ★★
- **描述**：`ProtocolAnalysis` 把 Swagger JSON 解析成 ToolSchema。**对 ZhituAgent 而言偏工具侧**，除非你想做"把任意 HTTP API 变成 MCP 工具"，否则用不上。
- **深度评分**：2/5（创意加分但跟 ZhituAgent 定位不匹配）。

### 候选 E：DDD 6 层架构重构 ★★
- **描述**：把 ZhituAgent 的 `internal/{agent,chat,rag,tool,memory}` 重构为 `interfaces/application/domain/infrastructure/types`。
- **Go+Eino 可行性**：高成本（大重构）。
- **利弊**：**Go 社区并不普遍吃 DDD**，标准 Go Project Layout 已经够用。**不推荐重构**，但可以考虑**只对 MCP Server 模块（候选 A）用 DDD 结构**作为一个"架构展示"切面。
- **深度评分**：2/5。

## 5. MCP 特别章节（重点）

### 5.1 MCP 协议流程（从这个项目里学到的）

```
Client                                 Server
  |                                       |
  |   GET /sse?gatewayId=x&apiKey=y       |
  |-------------------------------------->|
  |                                       |  创建 session，返回 endpoint URL
  |  <event: endpoint>                    |
  |<--------------------------------------|
  |                                       |
  |   POST /message?sessionId=s           |
  |   {"method":"initialize", ...}        |
  |-------------------------------------->|
  |                                       |
  |  <event: message>                     |
  |  {"result":{capabilities, serverInfo}}|
  |<--------------------------------------|
  |                                       |
  |   POST /message {"method":"tools/list"}
  |-------------------------------------->|
  |  <event: message>                     |
  |  {"result":{"tools":[...]}}           |
  |<--------------------------------------|
  |                                       |
  |   POST /message {"method":"tools/call","params":{"name":"x","arguments":{}}}
  |-------------------------------------->|
  |  <event: message>                     |
  |  {"result":{"content":[...]}}         |
  |<--------------------------------------|
```

**关键**：
- **SSE 单向接收事件**（server → client）
- **POST `/message` 发送请求**（client → server）
- **`sessionId` 绑定 SSE 连接和 POST 请求**
- **`tools/list` 列工具、`tools/call` 调用工具**

### 5.2 Go + Eino 生态下接入 MCP 的路径

| 场景 | 推荐库 | 说明 |
|---|---|---|
| ZhituAgent 作为 **MCP 客户端**（消费外部工具） | `github.com/cloudwego/eino-ext/components/tool/mcp` + `mark3labs/mcp-go/client` | OpsPilot 已用（`infra/mcp/client.go`） |
| ZhituAgent 作为 **MCP 服务端**（暴露自己的能力） | `mark3labs/mcp-go/server` + gin SSE handler | **ai-mcp-gateway 用 Spring WebFlux，你用 mcp-go/server + gin** |
| MCP 工具路由/编排层 | 自研（参考 ai-mcp-gateway 的 Node 链） | 非必须，Eino ReAct Agent 自动处理 |

### 5.3 简历写法组合

> **同时实现 MCP Client + MCP Server，面试能讲 "我既会用 MCP 生态，也懂 MCP 协议内部"。**

- "接入 MCP 客户端从远端动态加载工具"（借 OpsPilot）
- "暴露 RAG 为 MCP Server，可供 Claude Desktop / Cursor 接入"（借 ai-mcp-gateway）

## 6. 推荐优先级

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **A. ZhituAgent 作为 MCP Server** | ★★★★★ | 中 | **和 OpsPilot 的 MCP Client 构成对称 + 热点协议** |
| 🥈 | **B. Guava-style 限流中间件** | ★★★★ | 低 | 独立小功能，加分点明确 |
| 🥉 | C. Tree Strategy 路由 | ★★★ | 低 | 被 Eino Graph 覆盖，不优先 |
| 4 | D. OpenAPI → MCP 转换 | ★★ | 中 | 跟 ZhituAgent 定位不匹配 |
| 5 | E. DDD 重构 | ★★ | 高 | 不推荐 |

**核心结论**：**MCP Server 是这个项目最值钱的借鉴点**。配合 OpsPilot 的 MCP Client，能在 ZhituAgent 里**形成完整的 MCP 协议闭环**，简历上写 "基于 MCP 协议构建可互操作的 AI Agent 生态"。
