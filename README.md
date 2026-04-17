# ZhituAgent (知途)

基于 **Go + Eino** 框架的 AI 智能体项目。

## 功能

- **对话接口** — SSE 流式聊天
- **RAG 检索增强** — 文档加载 → 切分 → Embedding → 向量检索 → Rerank → 知识写回
- **会话记忆** — Redis 存储多轮对话，支持 Token 压缩与降级
- **工具调用** — 时间查询、邮件发送、知识动态写入
- **多 Agent 编排** — 关键词路由 + 知识检索增强 + 推理主链路叠加
- **输入安全** — 敏感词 Guardrail 拦截
- **可观测** — Prometheus AI/RAG 指标 + Grafana 面板 + Request ID 透传
- **Docker 一键部署** — Redis Stack + App + Prometheus + Grafana

## 技术栈

| 组件 | 选型 |
|---|---|
| 语言 | Go 1.26 |
| AI 框架 | [Eino](https://github.com/cloudwego/eino) v0.8.9 |
| 对话模型 | Qwen (qwen-max) via DashScope |
| Embedding | text-embedding-v3 (1024 维) |
| Rerank | qwen3-rerank |
| 向量存储 | Redis Stack (RediSearch + HNSW) |
| 会话存储 | Redis |
| Web 框架 | Gin |
| 监控 | Prometheus + Grafana |
| 配置 | Viper (config.yaml + 环境变量) |

## 目录结构

```
├── cmd/server/           # 入口
├── internal/
│   ├── agent/            # KnowledgeAgent + ReasoningAgent + Orchestrator
│   ├── chat/             # ChatService (对话 + 流式 + 记忆 + 工具)
│   ├── config/           # 配置加载
│   ├── common/           # 错误码 + 响应封装
│   ├── handler/          # HTTP handler
│   ├── memory/           # Redis 会话记忆 + 压缩 + 分布式锁
│   ├── middleware/       # CORS / Guardrail / Observability / ErrorHandler
│   ├── model/            # DTO
│   ├── monitor/          # Prometheus 指标 + 日志
│   ├── rag/              # 文档加载/切分/Embedding/检索/Rerank/写回
│   └── tool/             # TimeTool / EmailTool / RagTool
├── system-prompt/        # 知途人设
├── static/               # 前端演示页
├── deploy/               # Prometheus + Grafana 配置
├── docs/                 # 知识文档目录
├── config.yaml           # 默认配置
├── .env.example          # 环境变量模板
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── test_api.sh           # API 全链路测试脚本
```

## API

| 方法 | 路径 | 说明 | 成功响应 |
|---|---|---|---|
| POST | `/api/chat` | 普通对话 | 纯文本 |
| POST | `/api/streamChat` | 流式对话 | SSE 流式文本 |
| POST | `/api/multiAgentChat` | 多 Agent 对话 | 纯文本 |
| POST | `/api/insert` | 插入知识 | 纯文本 |
| GET | `/healthz` | 健康检查 | JSON |
| GET | `/metrics` | Prometheus 指标 | Prometheus 格式 |

异常响应统一为 JSON：`{"code": xxxxx, "data": null, "message": "错误信息"}`

## 快速开始

### 1. 本地运行

```bash
# 安装依赖
go mod download

# 复制环境变量
cp .env.example .env
# 编辑 .env，填入 DASHSCOPE_API_KEY 等

# 启动
go run cmd/server/main.go
```

### 2. Docker 部署

```bash
# 创建共享网络
docker network create zhitu-net

# 启动 Redis Stack
docker run -d --name redis-stack \
  --network zhitu-net \
  -p 6379:6379 \
  -v redis-data:/data \
  -e REDIS_ARGS="--requirepass your_password --appendonly yes" \
  redis/redis-stack-server:latest

# 配置 .env
cp .env.example .env
# 编辑 .env：
#   REDIS_ADDR=redis-stack:6379
#   REDIS_PASSWORD=your_password
#   QWEN_API_KEY=sk-xxx

# 启动全栈
docker compose up -d --build
```

### 3. 验证

```bash
# 健康检查
curl http://localhost:10010/healthz

# 对话
curl -X POST http://localhost:10010/api/chat \
  -H "Content-Type: application/json" \
  -d '{"sessionId":1,"userId":1,"prompt":"你好"}'

# 全链路测试
./test_api.sh http://localhost:10010
```

## 监控

- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)

关键指标：

| 指标 | 说明 |
|---|---|
| `ai_model_requests_total` | 模型请求总数 |
| `ai_model_errors_total` | 模型错误次数 |
| `ai_model_response_duration_seconds` | 模型响应耗时 |
| `ai_model_tokens_total` | Token 消耗 |
| `rag_retrieval_hit_total` | RAG 命中次数 |
| `rag_retrieval_miss_total` | RAG 未命中次数 |
| `rag_retrieval_duration_seconds` | RAG 检索耗时 |

## 核心参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| 服务端口 | 10010 | |
| 文档切片 | 800 字 / 200 重叠 | |
| 粗排 | 30 条 / 0.55 最低分 | |
| 精排 | Top 5 | |
| 会话上限 | 20 条消息 | |
| Token 阈值 | 6000 | 超过触发压缩 |
| Redis TTL | 3600 秒 | |
| 自动重载 | 5 分钟 | 扫描文档目录变更 |

## 测试

```bash
# 单元测试
go test ./internal/...

# 覆盖率
go test -cover ./internal/...

# API 全链路测试
./test_api.sh
```
