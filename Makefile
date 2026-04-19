.PHONY: build run test lint docker-up docker-down clean eval eval-rag eval-dump-candidates eval-memory eval-workflow eval-mcp

APP_NAME    := zhitu-agent
BUILD_DIR   := ./bin
DOCKER_COMPOSE := docker compose

build:
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run: build
	$(BUILD_DIR)/$(APP_NAME)

test:
	go test -v -race ./...

test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

docker-up:
	$(DOCKER_COMPOSE) up -d --build

docker-down:
	$(DOCKER_COMPOSE) down

docker-logs:
	$(DOCKER_COMPOSE) logs -f zhitu-agent

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

# ---- Eval Center (Wave 4 P9) ----
# 所有 eval 走 -tags=eval，与日常 go test ./... 解耦。需要 DASHSCOPE_API_KEY + Redis Stack。
# 首次跑建议 RAG_RELOAD_DOCS=true 让 tokenized 字段回写。

eval: eval-rag eval-memory eval-workflow ## 跑全部 eval（RAG + Memory + Workflow）

eval-rag: ## RAG A/B — legacy vs hybrid，写 docs/eval/reports/latest.json
	go test -tags=eval ./internal/rag/ -run TestRagAB -v

eval-dump-candidates: ## 产出人工标注文件 docs/eval/rag/candidates-<ts>.jsonl
	go test -tags=eval ./internal/rag/ -run TestDumpCandidates -v

eval-memory: ## Memory 三策略对比，写 docs/eval/reports/memory-latest.json
	go test -tags=eval ./internal/memory/ -run TestMemoryEval -v

eval-workflow: ## Workflow legacy vs graph，写 docs/eval/reports/workflow-latest.json
	go test -tags=eval -timeout=60m ./internal/chat/ -run TestWorkflowBenchmark -v

eval-mcp: ## MCP client + server 集成冒烟（需要 npx for server-everything）
	go test -tags=mcp ./internal/mcp/... -v
