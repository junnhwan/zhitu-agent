# Go + Eino Full Replica Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go + Eino implementation that first fully replicates the current Java agent project, including backend APIs, RAG, memory, tool calling, demo page, Docker Compose, and Prometheus/Grafana support.

**Architecture:** Keep the existing Java project untouched as the reference implementation and add a new Go project under `go-port/`. Use Go services for application logic, RAG, memory, and observability; use Eino mainly for chat model invocation, streaming, and tool integration.

**Tech Stack:** Go 1.24+, Eino, Gin, go-redis, pgx/pgvector, Prometheus client, Docker Compose, Prometheus, Grafana, static HTML/CSS/JS.

---

## File Structure

The implementation should create and fill these top-level areas:

- `go-port/go.mod`
- `go-port/.env.example`
- `go-port/Makefile`
- `go-port/cmd/server/main.go`
- `go-port/internal/config/*`
- `go-port/internal/transport/http/*`
- `go-port/internal/app/chat/*`
- `go-port/internal/agent/*`
- `go-port/internal/guardrail/*`
- `go-port/internal/rag/*`
- `go-port/internal/memory/*`
- `go-port/internal/tools/*`
- `go-port/internal/observability/*`
- `go-port/internal/store/postgres/*`
- `go-port/internal/store/redis/*`
- `go-port/pkg/llm/*`
- `go-port/resources/system-prompt/*`
- `go-port/web/*`
- `go-port/deployments/docker/*`
- `go-port/monitoring/prometheus/*`
- `go-port/monitoring/grafana/dashboards/*`

The Java code remains unchanged during implementation.

### Task 1: Scaffold the Go Project

**Files:**
- Create: `go-port/go.mod`
- Create: `go-port/.env.example`
- Create: `go-port/Makefile`
- Create: `go-port/cmd/server/main.go`
- Create: `go-port/internal/config/config.go`
- Test: `go-port/internal/config/config_test.go`

- [ ] **Step 1: Write the failing config test**

```go
func TestLoadConfigFromEnv(t *testing.T) {
    t.Setenv("APP_PORT", "10010")
    t.Setenv("QWEN_CHAT_MODEL", "qwen-max")

    cfg, err := Load()
    require.NoError(t, err)
    require.Equal(t, "10010", cfg.HTTP.Port)
    require.Equal(t, "qwen-max", cfg.Model.ChatModel)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config -run TestLoadConfigFromEnv -v`
Expected: FAIL with `undefined: Load`

- [ ] **Step 3: Implement minimal config loading**

```go
type Config struct {
    HTTP  HTTPConfig
    Model ModelConfig
}

func Load() (Config, error) {
    return Config{
        HTTP:  HTTPConfig{Port: getenv("APP_PORT", "10010")},
        Model: ModelConfig{ChatModel: getenv("QWEN_CHAT_MODEL", "qwen-max")},
    }, nil
}
```

- [ ] **Step 4: Expand the config structure**

Add nested config for:

- HTTP
- model
- Redis
- Postgres
- RAG
- mail
- metrics
- memory thresholds
- guardrail words
- system prompt path
- rerank startup verification flag
- pgvector `dropTableFirst` flag

- [ ] **Step 5: Run the test suite**

Run: `go test ./internal/config -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-port/go.mod go-port/.env.example go-port/Makefile go-port/cmd/server/main.go go-port/internal/config
git commit -m "feat: scaffold go port and config loading"
```

### Task 2: Build the Server Bootstrap and Routing Shell

**Files:**
- Modify: `go-port/cmd/server/main.go`
- Create: `go-port/internal/transport/http/router.go`
- Create: `go-port/internal/transport/http/handlers.go`
- Create: `go-port/internal/transport/http/dto.go`
- Create: `go-port/internal/observability/middleware.go`
- Test: `go-port/internal/transport/http/router_test.go`

- [ ] **Step 1: Write the failing router test**

```go
func TestRouterRegistersCoreEndpoints(t *testing.T) {
    r := NewRouter(Dependencies{})
    routes := r.Routes()

    assertRouteExists(t, routes, "POST", "/api/chat")
    assertRouteExists(t, routes, "POST", "/api/streamChat")
    assertRouteExists(t, routes, "POST", "/api/multiAgentChat")
    assertRouteExists(t, routes, "POST", "/api/insert")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/transport/http -run TestRouterRegistersCoreEndpoints -v`
Expected: FAIL with `undefined: NewRouter`

- [ ] **Step 3: Register placeholder routes**

```go
func NewRouter(dep Dependencies) *gin.Engine {
    r := gin.New()
    api := r.Group("/api")
    api.POST("/chat", dep.Handler.Chat)
    api.POST("/streamChat", dep.Handler.StreamChat)
    api.POST("/multiAgentChat", dep.Handler.MultiAgentChat)
    api.POST("/insert", dep.Handler.InsertKnowledge)
    return r
}
```

- [ ] **Step 4: Add request ID and recovery middleware**

Expose `X-Request-ID` and attach request metadata to context.

- [ ] **Step 5: Add placeholder health and metrics routes**

Expose:

- `GET /healthz`
- `GET /metrics`
- `GET /actuator/prometheus`

- [ ] **Step 5A: Add a route contract test for mixed response types**

Verify that the HTTP layer supports:

- plain text success responses for `/api/chat`, `/api/multiAgentChat`, `/api/insert`
- streaming text responses for `/api/streamChat`
- JSON error responses for validation or guardrail failures

- [ ] **Step 6: Run the test suite**

Run: `go test ./internal/transport/http -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go-port/cmd/server/main.go go-port/internal/transport/http go-port/internal/observability/middleware.go
git commit -m "feat: add server bootstrap and route shell"
```

### Task 3: Add Model Client Wrappers, Prompt Loading, and Guardrail Handling

**Files:**
- Create: `go-port/pkg/llm/qwen_client.go`
- Create: `go-port/pkg/llm/streaming.go`
- Create: `go-port/internal/guardrail/validator.go`
- Create: `go-port/internal/guardrail/validator_test.go`
- Create: `go-port/resources/system-prompt/chat-bot.txt`
- Create: `go-port/internal/agent/reasoning/service.go`
- Create: `go-port/internal/agent/reasoning/service_test.go`
- Modify: `go-port/internal/transport/http/handlers.go`

- [ ] **Step 1: Write the failing reasoning test**

```go
func TestReasoningAgentCallsChatModel(t *testing.T) {
    model := &fakeModel{reply: "pong"}
    agent := New(model)

    got, err := agent.Execute(context.Background(), 1, "ping")
    require.NoError(t, err)
    require.Equal(t, "pong", got)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/reasoning -run TestReasoningAgentCallsChatModel -v`
Expected: FAIL with `undefined: New`

- [ ] **Step 3: Implement a small model interface and Qwen adapter**

```go
type ChatModel interface {
    Chat(ctx context.Context, req ChatRequest) (string, error)
    Stream(ctx context.Context, req ChatRequest, onChunk func(string) error) error
}
```

- [ ] **Step 4: Add system prompt loading and request guardrail checks**

Replicate the current Java behavior:

- load `system-prompt/chat-bot.txt` for both sync and stream chat
- reject inputs containing `死` or `杀`
- return JSON error payloads on guardrail failure

- [ ] **Step 5: Wire `/api/chat` and `/api/streamChat` to the reasoning layer**

Make `/api/streamChat` send SSE frames using `text/event-stream`.

- [ ] **Step 6: Add tests for the prompt and guardrail contract**

Cover:

- prompt resource exists and is loaded
- blocked words are rejected
- successful chat path still returns plain text

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/agent/reasoning ./internal/transport/http -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add go-port/pkg/llm go-port/internal/guardrail go-port/resources/system-prompt go-port/internal/agent/reasoning go-port/internal/transport/http/handlers.go
git commit -m "feat: add qwen chat wrapper prompt loading and guardrail handling"
```

### Task 4: Add Redis and PostgreSQL Store Adapters

**Files:**
- Create: `go-port/internal/store/redis/client.go`
- Create: `go-port/internal/store/postgres/client.go`
- Create: `go-port/internal/store/postgres/pgvector.go`
- Create: `go-port/internal/store/redis/client_test.go`
- Create: `go-port/internal/store/postgres/pgvector_test.go`

- [ ] **Step 1: Write failing connection smoke tests**

```go
func TestRedisClientPing(t *testing.T) {
    c := NewTestRedisClient(t)
    require.NoError(t, c.Ping(context.Background()).Err())
}
```

```go
func TestPGVectorStoreCanInitializeSchema(t *testing.T) {
    db := NewTestPGVectorStore(t)
    require.NoError(t, db.InitSchema(context.Background()))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/... -v`
Expected: FAIL because clients are not implemented

- [ ] **Step 3: Implement Redis and Postgres client factories**

Initialize:

- Redis client
- pgx pool
- pgvector extension bootstrap
- embedding table creation
- vector dimension `1024`
- support for `dropTableFirst` parity flag

- [ ] **Step 4: Add readiness helpers used by the app bootstrap**

Expose simple `Ping` or `Health` functions.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/store/... -v`
Expected: PASS when the local test dependencies are available; if not, mark them as integration tests and document the required services

- [ ] **Step 6: Commit**

```bash
git add go-port/internal/store
git commit -m "feat: add redis and pgvector store adapters"
```

### Task 5: Implement the RAG Ingestion, Retrieval, and Knowledge Insert Flow

**Files:**
- Create: `go-port/internal/rag/loader.go`
- Create: `go-port/internal/rag/splitter.go`
- Create: `go-port/internal/rag/indexer.go`
- Create: `go-port/internal/rag/retriever.go`
- Create: `go-port/internal/rag/reranker.go`
- Create: `go-port/internal/rag/writer.go`
- Create: `go-port/internal/rag/service.go`
- Create: `go-port/internal/rag/service_test.go`
- Modify: `go-port/internal/transport/http/handlers.go`

- [ ] **Step 1: Write the failing retrieval test**

```go
func TestServiceRetrieveReturnsFormattedMatches(t *testing.T) {
    svc := NewService(fakeEmbedder{}, fakeStore{}, fakeReranker{})
    got, err := svc.Retrieve(context.Background(), "什么是 Redis")
    require.NoError(t, err)
    require.Contains(t, got, "来源")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/rag -run TestServiceRetrieveReturnsFormattedMatches -v`
Expected: FAIL with `undefined: NewService`

- [ ] **Step 3: Implement the RAG service API**

Support:

- `LoadDocuments(ctx)`
- `Retrieve(ctx, query)`
- `InsertKnowledge(ctx, question, answer, fileName)`
- `ReloadChangedDocuments(ctx)`

Preserve the current Java defaults and behavior:

- recursive load of `.md` and `.txt`
- splitter `800/200`
- retrieve `30` candidates with `0.55` min score
- rerank to Top `5`
- prepend `file_name + "\n"` before embedding
- keep a startup rerank verifier flag
- return the same formatted retrieval text as `RagTool.retrieve`

- [ ] **Step 4: Implement `/api/insert`**

Use the same Q/A Markdown format as the Java version:

```md
### Q：问题

A：答案
```

- [ ] **Step 5: Add startup loading and a periodic reload job**

Use a ticker-backed job that scans the docs directory and reloads changed `.md` or `.txt` files.

Match the current default interval: every 5 minutes.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/rag -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go-port/internal/rag go-port/internal/transport/http/handlers.go
git commit -m "feat: add rag ingestion retrieval and knowledge insert flow"
```

### Task 6: Implement Redis Session Memory and Compression

**Files:**
- Create: `go-port/internal/memory/service.go`
- Create: `go-port/internal/memory/compressor.go`
- Create: `go-port/internal/memory/locker.go`
- Create: `go-port/internal/memory/service_test.go`
- Modify: `go-port/internal/agent/reasoning/service.go`

- [ ] **Step 1: Write the failing memory compression test**

```go
func TestMemoryCompressesWhenThresholdExceeded(t *testing.T) {
    mem := NewService(fakeStore{}, fakeCompressor{}, Config{
        MaxMessages: 2,
        TokenThreshold: 100,
        FallbackRecentRounds: 2,
    })

    require.NoError(t, mem.Append(context.Background(), "1", userMessage("a")))
    require.NoError(t, mem.Append(context.Background(), "1", assistantMessage("b")))
    require.NoError(t, mem.Append(context.Background(), "1", userMessage("c")))

    require.True(t, mem.LastAppendCompressed())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/memory -run TestMemoryCompressesWhenThresholdExceeded -v`
Expected: FAIL because the memory service is missing

- [ ] **Step 3: Implement Redis-backed message storage**

Support:

- session-scoped message history
- TTL
- distributed lock per session
- fallback recent-round retention
- current simplified summary behavior, not LLM summarization
- approximate token counting using character length divided by four

- [ ] **Step 4: Integrate memory reads/writes into chat execution**

Before model call:

- load memory history

After model call:

- append user and assistant messages

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/memory ./internal/agent/reasoning -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-port/internal/memory go-port/internal/agent/reasoning/service.go
git commit -m "feat: add redis session memory and compression"
```

### Task 7: Implement Tools and the Multi-Agent Orchestrator

**Files:**
- Create: `go-port/internal/tools/time_tool.go`
- Create: `go-port/internal/tools/email_tool.go`
- Create: `go-port/internal/tools/rag_tool.go`
- Create: `go-port/internal/tools/web_search_tool.go`
- Create: `go-port/internal/tools/registry.go`
- Create: `go-port/internal/agent/knowledge/service.go`
- Create: `go-port/internal/agent/orchestrator/service.go`
- Create: `go-port/internal/agent/orchestrator/service_test.go`
- Modify: `go-port/internal/transport/http/handlers.go`

- [ ] **Step 1: Write the failing orchestrator routing test**

```go
func TestProcessUsesKnowledgeAgentForKnowledgeQueries(t *testing.T) {
    o := New(fakeKnowledgeAgent{reply: "知识"}, fakeReasoningAgent{reply: "答案"})
    got, err := o.Process(context.Background(), 1, "什么是 RAG")
    require.NoError(t, err)
    require.Equal(t, "答案", got)
    require.True(t, o.LastCallUsedKnowledge())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/orchestrator -run TestProcessUsesKnowledgeAgentForKnowledgeQueries -v`
Expected: FAIL because the orchestrator is missing

- [ ] **Step 3: Implement the keyword-based orchestrator**

Preserve the Java behavior:

- match keywords such as `查询 / 了解 / 什么是 / 介绍 / 解释 / 说明`
- call knowledge agent when matched
- prepend retrieved knowledge to the reasoning input
- still route the final prompt through the full reasoning chain with prompt, guardrail, memory, content retriever, tools, and MCP tools

- [ ] **Step 4: Register tools with the reasoning layer**

Support:

- current time
- send email
- add knowledge to RAG
- web search

Replicate the current tool response style:

- Shanghai time string formatting
- success/failure text for email
- retrieval text with `来源` and `相似度`

- [ ] **Step 5: Wire `/api/multiAgentChat`**

Use the orchestrator instead of direct reasoning.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/agent/... ./internal/tools -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go-port/internal/tools go-port/internal/agent go-port/internal/transport/http/handlers.go
git commit -m "feat: add tools and multi-agent orchestration"
```

### Task 8: Add Metrics, Logs, and Monitoring Context

**Files:**
- Create: `go-port/internal/observability/context.go`
- Create: `go-port/internal/observability/metrics.go`
- Create: `go-port/internal/observability/logger.go`
- Create: `go-port/internal/observability/metrics_test.go`
- Modify: `go-port/internal/transport/http/handlers.go`
- Modify: `go-port/internal/agent/reasoning/service.go`
- Modify: `go-port/internal/rag/service.go`

- [ ] **Step 1: Write the failing metrics test**

```go
func TestMetricsRecorderCountsRequests(t *testing.T) {
    reg := prometheus.NewRegistry()
    m := NewMetrics(reg)

    m.RecordRequest("u1", "s1", "qwen-max", "success")

    got := gatherMetricValue(t, reg, "ai_model_requests_total")
    require.Equal(t, 1.0, got)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/observability -run TestMetricsRecorderCountsRequests -v`
Expected: FAIL because the recorder is missing

- [ ] **Step 3: Implement observability primitives**

Include:

- request counters
- error counters
- token counters
- response duration histograms
- RAG query counters
- tool invocation counters

Prefer the current Java metric names:

- `ai_model_requests_total`
- `ai_model_errors_total`
- `ai_model_tokens_total`
- `ai_model_response_duration_seconds`
- `rag_retrieval_hit_total`
- `rag_retrieval_miss_total`
- `rag_retrieval_duration_seconds`

- [ ] **Step 4: Propagate monitoring context**

Carry:

- request ID
- user ID
- session ID
- start time

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/observability ./internal/rag ./internal/agent/reasoning -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-port/internal/observability go-port/internal/transport/http/handlers.go go-port/internal/agent/reasoning/service.go go-port/internal/rag/service.go
git commit -m "feat: add observability metrics and monitoring context"
```

### Task 9: Build the Demo Web UI

**Files:**
- Create: `go-port/web/index.html`
- Create: `go-port/web/qwen.html`
- Create: `go-port/web/gpt.html`
- Create: `go-port/web/gemini.html`
- Create: `go-port/web/app.js`
- Create: `go-port/web/styles.css`
- Modify: `go-port/internal/transport/http/router.go`

- [ ] **Step 1: Write a manual acceptance checklist**

Create a short checklist in comments or a local note:

- page loads
- can submit `/api/chat`
- can submit `/api/streamChat`
- can set `userId` and `sessionId`
- can see response and error state
- localStorage persists sessions
- `qwen.html` and `gemini.html` use `/api/chat`
- `gpt.html` uses `/api/streamChat`

- [ ] **Step 2: Serve static files from the Go app**

Mount `/` or `/web` to the `go-port/web` directory.

- [ ] **Step 3: Implement the demo UI**

Support:

- message list
- prompt input
- model page variants
- normal and streaming modes

- [ ] **Step 4: Manually verify the checklist**

Run:

```bash
go run ./cmd/server
```

Expected:

- the page opens in a browser
- both chat modes work against the local backend

- [ ] **Step 5: Commit**

```bash
git add go-port/web go-port/internal/transport/http/router.go
git commit -m "feat: add demo web interface"
```

### Task 10: Add Docker Compose and Monitoring Stack

**Files:**
- Create: `go-port/deployments/docker/docker-compose.yml`
- Create: `go-port/deployments/docker/go.Dockerfile`
- Create: `go-port/deployments/docker/prometheus.yml`
- Create: `go-port/monitoring/grafana/provisioning/datasources/datasource.yml`
- Create: `go-port/monitoring/grafana/provisioning/dashboards/dashboard.yml`
- Create: `go-port/monitoring/grafana/dashboards/agent-overview.json`
- Test: `go-port/deployments/docker/docker-compose.config.snapshot.txt`

- [ ] **Step 1: Write the failing compose config check**

Run:

```bash
docker compose -f go-port/deployments/docker/docker-compose.yml config
```

Expected initially: FAIL because the compose file does not exist

- [ ] **Step 2: Add the base service graph**

Include:

- `app`
- `redis`
- `postgres`
- `prometheus`
- `grafana`

- [ ] **Step 3: Add Prometheus and Grafana configuration**

Prometheus must scrape:

- `app:10010/metrics`

Grafana must load a dashboard from disk.

- [ ] **Step 4: Re-run the compose config check**

Run:

```bash
docker compose -f go-port/deployments/docker/docker-compose.yml config > go-port/deployments/docker/docker-compose.config.snapshot.txt
```

Expected: PASS and the snapshot file is written

- [ ] **Step 5: Bring the stack up locally**

Run:

```bash
docker compose -f go-port/deployments/docker/docker-compose.yml up -d --build
```

Expected:

- all services become healthy or running
- Prometheus can scrape the app
- Grafana opens successfully

- [ ] **Step 6: Commit**

```bash
git add go-port/deployments/docker go-port/monitoring
git commit -m "feat: add docker compose and monitoring stack"
```

### Task 11: Add End-to-End Parity Checks

**Files:**
- Create: `go-port/tests/e2e/chat_flow_test.go`
- Create: `go-port/tests/e2e/insert_flow_test.go`
- Create: `go-port/tests/e2e/multi_agent_flow_test.go`
- Create: `go-port/tests/e2e/README.md`
- Modify: `go-port/Makefile`

- [ ] **Step 1: Write the failing E2E test skeleton**

```go
func TestChatEndpointReturns200(t *testing.T) {
    resp := postJSON(t, baseURL+"/api/chat", map[string]any{
        "userId": 1,
        "sessionId": 1,
        "prompt": "你好",
    })
    require.Equal(t, http.StatusOK, resp.StatusCode)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./tests/e2e -run TestChatEndpointReturns200 -v`
Expected: FAIL until the app and dependencies are running

- [ ] **Step 3: Add E2E helpers and Makefile targets**

Add:

- `make test-unit`
- `make test-integration`
- `make test-e2e`
- `make up`
- `make down`

- [ ] **Step 4: Run the full validation sequence**

Run:

```bash
make test-unit
make up
make test-e2e
make down
```

Expected:

- unit tests pass
- stack starts
- E2E tests pass
- stack shuts down cleanly

- [ ] **Step 5: Commit**

```bash
git add go-port/tests/e2e go-port/Makefile
git commit -m "test: add end-to-end parity checks"
```

### Task 12: Final Acceptance and Documentation Polish

**Files:**
- Create: `go-port/README.md`
- Modify: `go-port/.env.example`
- Modify: `docs/superpowers/specs/2026-04-16-go-eino-full-replica-design.md`
- Modify: `docs/superpowers/plans/2026-04-16-go-eino-full-replica.md`

- [ ] **Step 1: Write the Go-port README**

Include:

- project overview
- quick start
- env variables
- supported endpoints
- demo instructions
- monitoring access URLs

- [ ] **Step 2: Add the parity checklist**

Document whether each Java feature has been:

- replicated
- partially replicated
- intentionally deferred

- [ ] **Step 3: Run the final verification pass**

Run:

```bash
go test ./...
docker compose -f go-port/deployments/docker/docker-compose.yml config
```

Expected:

- Go tests pass
- compose config validates

- [ ] **Step 4: Commit**

```bash
git add go-port/README.md go-port/.env.example docs/superpowers/specs/2026-04-16-go-eino-full-replica-design.md docs/superpowers/plans/2026-04-16-go-eino-full-replica.md
git commit -m "docs: finalize go full replica plan and docs"
```

## Acceptance Checklist

- [ ] The Go service exposes all core Java endpoints
- [ ] Streaming chat works through SSE
- [ ] Multi-agent route uses keyword-based knowledge routing
- [ ] RAG ingestion and retrieval work with pgvector
- [ ] Session memory is isolated by session ID and supports compression
- [ ] Time, email, knowledge insert, and web search tools are available
- [ ] System prompt and guardrail behavior match the Java project
- [ ] Success responses are plain text while error responses use JSON wrappers
- [ ] Metrics are visible from Prometheus and Grafana
- [ ] Docker Compose starts the full stack
- [ ] The demo web page can be used in a browser for recording and interviews

## Notes for Execution

- Keep the Java project as a live reference and compare behavior module by module.
- Do not optimize architecture early; match behavior first.
- Do not hardcode secrets from `application.yml`; move everything to env-based config.
- If a feature cannot be protocol-equivalent in Eino on day one, preserve user-visible behavior and document the gap in the parity checklist.
