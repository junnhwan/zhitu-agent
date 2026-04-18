# paismart-go 调研笔记

> 调研时间：2026-04-17
> 仓库路径：`D:/dev/learn_proj/go_proj/paismart-go`
> 语言栈：Go 1.23 + Gin + GORM + Viper + Zap

## 1. 项目定位

**派聪明 Go 版**是企业级 RAG 知识库系统，功能完整度远超 ZhituAgent —— 多租户（orgTag 层级权限）、Kafka 异步 pipeline、MinIO 分片/秒传上传、Tika 多格式解析、Elasticsearch 混合检索、WebSocket 流式对话。

是**"能写进简历的 AI 知识库项目"参考范本**（作者就是用 Java 版进院大厂的）。

## 2. 架构概览

```
cmd/server/main.go              # 依赖注入入口
internal/
  ├── config/                   # Viper
  ├── handler/                  # Gin controllers（含 WebSocket）
  ├── middleware/               # auth / admin_auth / logging / request_id
  ├── model/                    # domain + DTO + ES document
  ├── pipeline/processor.go     # 文件处理流水线（消费 Kafka）
  ├── repository/               # GORM + Redis
  └── service/                  # 业务逻辑
pkg/
  ├── database/                 # MySQL / Redis 初始化
  ├── embedding/                # DashScope OpenAI 兼容
  ├── es/                       # Elasticsearch 客户端（KNN + BM25）
  ├── kafka/                    # 生产者 / 消费者 + TaskProcessor 接口
  ├── llm/                      # DeepSeek / Ollama 流式客户端
  ├── storage/                  # MinIO（含预签名 URL）
  ├── tika/                     # Apache Tika HTTP 客户端
  └── token/                    # JWT
```

**基础设施**：MySQL 8 + Redis 7 + Elasticsearch 8 + Kafka + MinIO + Tika（Docker Compose 一键拉起）。

## 3. 亮点实现

### 3.1 Kafka 异步 pipeline + TaskProcessor 接口
- `pkg/kafka/client.go:17` 定义 `TaskProcessor interface{ Process(ctx, task) error }`，**把消费者和业务 Processor 解耦**。
- `pkg/kafka/client.go:50` 消费者：`FetchMessage` + goroutine 并发处理 + **成功才 CommitMessages**（手动 offset，失败消息不丢）。
- `pkg/kafka/client.go:86-90` 对无法解析的格式错误消息：**直接 Commit 防止队列阻塞**（实用的死信处理）。
- `internal/service/upload_service.go:262` 合并后通过 `kafka.ProduceFileTask` 发布任务，上传响应立即返回，处理异步。

**价值**：上传 → 解析 → 向量化 → 索引链路彻底解耦，消费者可水平扩展。

### 3.2 分片上传 + 秒传 + 断点续传
- `internal/service/upload_service.go:54` `CheckFile`：MD5 查库，命中 `Status==1` 直接秒传；否则返回已上传分片列表。
- `internal/service/upload_service.go:83` `UploadChunk`：MinIO 存分片（`chunks/{md5}/{index}`），数据库记分片元数据，Redis 用 bitmap/set 标记分片已传。
- `internal/service/upload_service.go:215` `MergeChunks`：**单分片用 `CopyObject`、多分片用 `ComposeObject`**，合并后 `merged/{filename}`。
- `internal/service/upload_service.go:278` 合并成功后 `go func` **后台清理**分片和 Redis 标记。

**价值**：可恢复上传、大文件友好、MD5 秒传，是大文件场景的教科书实现。

### 3.3 ES 混合检索：KNN + BM25 rescore + 短语兜底
核心文件：`internal/service/search_service.go:44` `HybridSearch`

```json
{
  "knn": {"field": "vector", "k": topK*30, "num_candidates": topK*30},
  "query": {
    "bool": {
      "must": {"match": {"text_content": normalized}},
      "filter": {"bool": {"should": [user_id, is_public, org_tag], "minimum_should_match": 1}},
      "should": [{"match_phrase": {"text_content": {"query": phrase, "boost": 3.0}}}]
    }
  },
  "rescore": {
    "window_size": topK*30,
    "query": {"rescore_query": {"match": {"operator": "and"}}, "query_weight": 0.2, "rescore_query_weight": 1.0}
  }
}
```

三个精华点：
- **KNN 语义召回** + **BM25 rescore 权重 1.0 / KNN 权重 0.2**（召回全用语义、精排主靠 BM25）
- **短语 boost=3.0 should 兜底**：长尾查询里精准短语能拉分
- **零命中时用归一化短语重试**（`search_service.go:168-202`）

### 3.4 Query 归一化与停用词清洗
- `internal/service/search_service.go:265` `normalizeQuery`：
  - 去口语停用词（"是什么"、"请问"、"？"…）
  - 正则只保留中文/英文/数字/空白
  - 返回规范化查询 + 核心短语双输出
- 双用途：BM25 用规范化，短语兜底用核心短语。

### 3.5 多租户层级权限 + 检索期过滤
- `internal/service/user_service.go` `GetUserEffectiveOrgTags`：根据用户组织标签展开所有有效标签。
- ES 过滤用 **`should + minimum_should_match: 1`** 表示"满足任一条件即可"：user_id 匹配 OR is_public OR org_tag 命中。
- 索引字段里 `user_id` / `org_tag` / `is_public` 直接作为 filter，不走评分。

**价值**：权限过滤做在检索层不是应用层，性能更好且不会出现"召回了看不到的文档"。

### 3.6 WebSocket 流式输出 + 停止指令
- `internal/service/chat_service.go:199` `wsWriterInterceptor`：
  - 包装 `websocket.Conn` 作为 `llm.MessageWriter`
  - 每个 chunk 被 `strings.Builder` 累积 + `{"chunk": "..."}` JSON 包装后推送
  - `shouldStop()` 回调在每次写入前检查，前端可随时打断
- `internal/service/chat_service.go:220` 完成时推送 `{"type":"completion","status":"finished","timestamp":...}` 结构化消息。
- 流式完成后 `context.Background()` 保存历史（即使 ws 断开也要落盘）。

### 3.7 两阶段幂等入库
`internal/pipeline/processor.go:107` 处理前先 `DeleteByFileMD5` 清理旧分块 → MySQL 批量入库 → 逐块向量化 + ES 索引。**任何阶段失败重跑都幂等**。

### 3.8 Tika HTTP 客户端
`pkg/tika/client.go` + Docker 里起独立 Tika 服务（port 9998），通过 HTTP 传文件流解析。解耦 Go 进程、支持 PDF/DOCX/PPTX/XLS/TXT 全家桶。

## 4. 对 ZhituAgent 的启示

ZhituAgent 当前只有：本地文档 → 切分 → Embedding → Redis Stack HNSW → Rerank → 简单写回。对比 paismart-go 缺：异步管道、大文件上传、多格式解析、混合检索、多租户权限。

### 候选 A：RAG 检索改造为"向量召回 + BM25 rescore + 短语兜底"★★★★★
- **描述**：把 ZhituAgent 的 `rag.Retrieve`（现在是纯向量 Top30 → 0.55 阈值 → qwen3-rerank Top5）升级为 **Redis Stack 内联的 hybrid search**（RediSearch 同时支持 KNN 和 BM25 TEXT 字段）或**引入 ES** 复用 paismart-go 的查询。
- **借鉴位置**：`internal/service/search_service.go:74-119` 的 JSON 查询结构 + `search_service.go:265` 的 `normalizeQuery`。
- **Go+Eino 可行性**：
  - 方案 A（低成本）：在 Redis Stack 里加 `FT.CREATE` 的 `TEXT` 字段，用 `FT.SEARCH` 同时带 `@vec:[VECTOR_RANGE ...]` 和 BM25，客户端做 score 融合。
  - 方案 B（中成本）：新增 `pkg/es` 替换 Redis Stack，部署 ES + ik 分词。
- **升级 or 新增**：升级现有 RAG。
- **简历**：`设计 RAG 两阶段混合检索——向量 KNN 召回 30 候选 + BM25 按 rescore_query_weight=1.0 精排 + match_phrase boost=3.0 兜底，零召回自动用归一化短语重试，问答 HitRate 在内部知识库基准上提升 X%`。
- **面试深入**：讲得清 HNSW vs IVF、KNN recall 和 rescore 精度的权衡、rescore 的 query_weight 意义、为什么要做零命中重试（稀疏场景用户 query 里含大量停用词）。
- **深度评分**：5/5。

### 候选 B：Kafka 异步文档处理 pipeline ★★★★★
- **描述**：把 ZhituAgent 的"文档自动重载"（现在是每 5 分钟扫目录）改为**事件驱动 + Kafka 异步解析链路**：上传/新增/变更 → Kafka → 消费者处理。
- **借鉴位置**：`pkg/kafka/client.go` 全文 + `internal/pipeline/processor.go`。
- **Go+Eino 可行性**：中成本。新增 `pkg/kafka`（用 `segmentio/kafka-go`）+ `internal/pipeline/processor.go` + 上传接口。docker-compose 加 `kafka` + `zookeeper`（或用 Redis Stream 作为简化替代，零新组件）。
- **升级 or 新增**：升级现有文档自动重载。
- **简历**：`基于 Kafka 构建文档解析异步流水线，通过 TaskProcessor 接口解耦消费者与业务层，失败消息手动重试、格式错误直接提交防止队列阻塞，支持消费者水平扩容`。
- **面试深入**：能讲消费组、手动 offset 提交 vs 自动提交、至少一次 vs 精确一次、死信队列、消费者 rebalance、Kafka 和 Redis Stream 选型差异。
- **深度评分**：5/5。

### 候选 C：分片上传 + 秒传 + 断点续传 ★★★★
- **描述**：ZhituAgent 目前只有 `/api/insert` 插单条知识，没有文件上传能力。加一套完整的文件上传 API（大文件分片 + MD5 秒传 + 断点续传）作为 RAG 的入口。
- **借鉴位置**：`internal/service/upload_service.go` 全文。
- **Go+Eino 可行性**：中成本。需要引入对象存储（MinIO 或 MinIO-compatible S3），新增 handler/service/repository。
- **升级 or 新增**：新增（跟候选 B 联动）。
- **简历**：`实现企业级大文件分片上传模块：前端切 5MB 块、MD5 秒传、Redis bitmap 记录分片进度、MinIO CopyObject/ComposeObject 按分片数选择合并策略、合并成功异步清理`。
- **面试深入**：讲 HTTP multipart、分片合并的 S3 multipart upload 协议、幂等设计（Redis 原子标记）、后台清理失败的容错、大文件 OOM 防护。
- **深度评分**：4/5。

### 候选 D：多格式文档解析（Apache Tika）★★★
- **描述**：ZhituAgent 现在只支持 txt/md；引入 Tika 后支持 PDF/DOCX/PPTX/XLS 全家桶。
- **借鉴位置**：`pkg/tika/client.go`（HTTP 客户端调用独立 Tika 容器）。
- **Go+Eino 可行性**：低成本。Tika 有官方 docker 镜像，只需加 `pkg/tika` HTTP 封装 + docker-compose 加 tika 服务。
- **升级 or 新增**：升级 `rag.Loader`。
- **简历**：`接入 Apache Tika 服务支持 PDF/DOCX/PPT/XLS 等格式的文本抽取，Tika 独立进程部署解耦 Go 主服务`。
- **面试深入**：讲 Tika 架构（为什么是独立 JVM 服务而不是 cgo 嵌入）、文档解析的性能瓶颈、为什么选择 HTTP 解耦。
- **深度评分**：3/5。

### 候选 E：WebSocket 流式对话 + 停止指令 ★★★
- **描述**：ZhituAgent 现在是 SSE，只能单向下行。WebSocket 允许前端随时发送"停止生成"指令，用户体验更好。
- **借鉴位置**：`internal/service/chat_service.go:199` `wsWriterInterceptor` + `chat_handler.go`（token 换 ws 连接两步鉴权）。
- **Go+Eino 可行性**：低成本。Eino 的 Stream 模式已经支持 chunk 回调，用 gorilla/websocket 包装即可。
- **升级 or 新增**：升级 `/api/streamChat`（或新增 `/api/wsChat`）。
- **简历**：`在 Eino Stream 上包装 WebSocket 双向通信层，支持前端中途停止生成（shouldStop 回调写入前检查），已推送内容保留到会话记忆`。
- **面试深入**：讲 SSE vs WebSocket 的权衡、生成中途打断的语义（已生成内容保不保）、ws 连接鉴权的 token 交换模式。
- **深度评分**：3/5（偏业务侧，技术深度中等）。

### 候选 F：多租户 orgTag 层级权限 + 检索期过滤 ★★★★
- **描述**：为 ZhituAgent 引入用户/组织模型，每条知识带 `userId / orgTag / isPublic`，检索时走 ES/Redis filter。
- **借鉴位置**：`internal/service/search_service.go:88-98` 的 `filter.bool.should + minimum_should_match: 1`。
- **Go+Eino 可行性**：中成本。涉及 JWT 鉴权 + 用户表 + 知识写入带元信息 + 检索过滤改造。
- **升级 or 新增**：新增。
- **简历**：`设计三级可见性（私有/组织/公开）的 RAG 权限模型，检索期通过 ES filter 下推过滤（should + minimum_should_match），避免"召回后裁剪"带来的 Top-K 失真`。
- **面试深入**：讲为什么过滤要做在检索层（避免召回 Top-K 后被过滤干净）、JWT 设计（access + refresh）、权限模型里 public/org/private 的 SQL/ES 表达。
- **深度评分**：4/5。

### 候选 G：两阶段幂等入库（先 MySQL 再 ES）★★★
- **描述**：ZhituAgent 现在是"直接算向量 → 写 Redis Stack"，失败重试容易重复写入或半残留。改成**先落 MySQL（或 Redis Hash）作为事实表**，再异步向量化+索引，且入库前先清理同 md5 旧记录。
- **借鉴位置**：`internal/pipeline/processor.go:107-134`。
- **Go+Eino 可行性**：低-中成本，但 ZhituAgent 没 MySQL，可用 Redis Hash 作为事实表简化。
- **升级 or 新增**：升级 RAG 写回流程。
- **简历**：`RAG 索引链路重构为两阶段幂等写入：分块先入事实表（重跑前 DeleteByFileMD5 清理），再异步向量化/索引，任何阶段失败可无副作用重跑`。
- **面试深入**：讲为什么不能直接写 ES（部分失败导致索引污染）、事实表与索引的一致性保证、幂等设计的 delete-first 手法。
- **深度评分**：3/5。

## 5. 推荐优先级（性价比 = 简历价值 / 实现成本）

| 排名 | 候选 | 价值 | 成本 | 说明 |
|---|---|---|---|---|
| 🥇 | **A. 混合检索改造** | ★★★★★ | 低（走 Redis Stack BM25）/中（引入 ES） | RAG 项目的命脉，面试必问，Redis Stack 本身就能做 |
| 🥈 | **B. Kafka 异步 pipeline** | ★★★★★ | 中 | 把"5分钟扫目录"换成事件驱动，硬核中间件能力体现 |
| 🥉 | **F. 多租户权限** | ★★★★ | 中 | 配合 C 成为完整企业级方案；单独做也能讲清权限模型 |
| 4 | **C. 分片上传** | ★★★★ | 中 | 工程能力体现，但跟 AI 关系较弱 |
| 5 | **D. Tika 多格式** | ★★★ | 低 | 性价比高，但不够亮眼 |
| 6 | **E. WebSocket** | ★★★ | 低 | 技术深度一般 |
| 7 | **G. 幂等两阶段写入** | ★★★ | 低 | 可以包装进 B 一起讲 |

**建议组合路径**：Phase 2 做 **A（RAG 混合检索）+ B（Kafka pipeline）+ G（两阶段入库）** 一包装，能在简历上写出"企业级 RAG 异步索引 + 混合检索系统"。**D（Tika）**作为 B 的自然延伸。**F（多租户）+ C（分片上传）**作为后期扩展。
