# Wave 4B 面试向实施文档：分布式排队限流 + 多模型路由熔断

> 目标：把 ZhituAgent 从"demo 级"升级到"**生产级高并发**"，具备面试爆点级别的流控能力。
> 覆盖优化点：**P7（Redis ZSET + Pub/Sub 分布式排队限流）**、**P6（多模型路由 + 三态熔断 + 首包探测）**。
> 迭代时长：2-3 周。
> **战略地位**：这是两个**面试爆点**，单独拎出来每个都能讲 15 分钟。借鉴 **ragent** 的设计（Java 生产项目验证过）。

---

## P7 · Redis ZSET + Pub/Sub 分布式排队限流

### 1. 业务背景 / 痛点

**当前现状**：无限流。请求直接打到 `chat.Service.Chat`，随 QPS 涨会：
- 上游 DashScope 返回 429（rate limit），本地没有重试逻辑，用户直接看到 error
- 多实例部署时，每个实例各自打满 QPS，合计爆破上游配额
- 突发流量（爆款场景 / 爬虫）时整个服务被拖垮

**常规限流不够**：
- 本地令牌桶（`golang.org/x/time/rate`）：单实例内有效，多实例不互通 → 合计超配额
- Redis INCR 滑动窗口：能做分布式计数，**但请求超限就直接 429**——用户体验差
- **真正的生产需求**：超限的请求**排队等待**而不是被拒绝，并能看到"第 N 位"进度

**业务驱动**：企业应用场景用户对"排队"容忍度远高于"直接拒绝"（电商秒杀、AI 流量高峰都验证过）。ragent 的设计是业界公认的成熟方案，可以直接借鉴。

### 2. 技术选型 & 对比

**选型 1：限流算法**

| 算法 | 分布式 | 排队 | 平滑度 |
|---|---|---|---|
| 令牌桶 | ❌ 单机 | ❌ | ✅ |
| 漏桶 | ❌ 单机 | ✅ | ✅ |
| Redis INCR 滑动窗口 | ✅ | ❌ | 🔸 |
| **Redis ZSET 排队 + Semaphore 并发** | ✅ | ✅ | ✅ |

选最后一种——**ZSET 维护排队顺序 + Semaphore 控并发上限 + Pub/Sub 跨实例唤醒**。

**选型 2：为什么是 ZSET 而不是 List**

| 数据结构 | 插入 | 查位置 | 跨实例 |
|---|---|---|---|
| List | O(1) | O(N) 不支持位置查询 | ❌ FIFO 严格但查不到位置 |
| **ZSET** | O(logN) | **O(logN)**（ZRANK）| ✅ |

ZSET score 用时间戳（毫秒级），天然按时间排序；ZRANK 查位置 O(logN)，能告诉用户"你排第 N 位"——这是 List 做不到的。

**选型 3：跨实例唤醒**

当实例 A 释放一个 semaphore 许可，其他实例的 waiter 怎么知道？
- **轮询**：每 100ms 查一次 → 延迟抖动 + 无效查询
- **Redis Pub/Sub**：A 释放后 PUBLISH，所有实例 SUBSCRIBE 收到消息后唤醒自己的 waiter ✅

### 3. 核心实现方案

**新目录结构**：

```
internal/ratelimit/
├── queue.go              # ZSET 排队核心
├── semaphore.go          # 分布式 Semaphore
├── permit.go             # 许可 lease（带 TTL 防死锁）
├── notifier.go           # Pub/Sub 跨实例唤醒
├── sse.go                # SSE 推排队状态给前端
├── scripts/              # Lua 脚本（保证原子）
│   ├── enqueue.lua
│   ├── try_acquire.lua
│   └── release.lua
└── middleware.go         # Gin middleware
```

**核心流程**：

```
用户请求
  ↓
Middleware 入口
  ↓
① ENQUEUE: ZADD queue:ai_chat {score=now_ms} {member=request_id}
  ↓
② 循环尝试 TRY_ACQUIRE:
   - 取 ZSET 前 N 位（N = 当前 semaphore 空闲数）
   - 如果我在这 N 位里 → 获得许可 permit（Redis SET {permit_id} NX EX 30s）
   - 否则返回当前 ZRANK，SSE 推给前端"第 X 位"
  ↓
③ 等待 Pub/Sub 通知 → 重试 ②
  ↓
④ 获得许可 → 执行业务
  ↓
⑤ RELEASE: 删除 permit + ZREM queue + PUBLISH wake
```

**Lua 脚本保证原子性**（`try_acquire.lua`）：

```lua
-- KEYS[1] = queue_key, KEYS[2] = semaphore_key, KEYS[3] = permit_prefix
-- ARGV[1] = max_concurrent, ARGV[2] = request_id, ARGV[3] = permit_ttl_sec, ARGV[4] = permit_id

local current = redis.call('SCARD', KEYS[2])  -- 当前活跃 permit 数
if current >= tonumber(ARGV[1]) then
    return redis.call('ZRANK', KEYS[1], ARGV[2])  -- 返回排队位置
end

-- 检查我是否在队列前 max_concurrent 位
local rank = redis.call('ZRANK', KEYS[1], ARGV[2])
if rank == false or rank >= tonumber(ARGV[1]) then
    return rank or -1
end

-- 获取许可：加入 semaphore set + 写 permit key（带 TTL 防死锁）
redis.call('SADD', KEYS[2], ARGV[4])
redis.call('SET', KEYS[3] .. ARGV[4], ARGV[2], 'EX', tonumber(ARGV[3]))
redis.call('ZREM', KEYS[1], ARGV[2])  -- 出队
return -2  -- -2 表示成功获取
```

**释放脚本**（保证 permit 过期/主动释放都能清理）：

```lua
-- KEYS[1] = semaphore_key, KEYS[2] = permit_key, KEYS[3] = channel_key
-- ARGV[1] = permit_id

redis.call('SREM', KEYS[1], ARGV[1])
redis.call('DEL', KEYS[2])
redis.call('PUBLISH', KEYS[3], 'wake')
return 1
```

**Go 侧 Queue 封装**（骨架）：

```go
type Queue struct {
    rdb         *redis.Client
    keyPrefix   string
    maxConcurrent int
    permitTTL   time.Duration
    pollTimeout time.Duration  // 最长排队时间
    notifier    *Notifier
}

func (q *Queue) Acquire(ctx context.Context, scope string, onProgress func(rank int)) (*Permit, error) {
    requestID := uuid.New().String()
    permitID := uuid.New().String()
    // 1. 入队
    q.rdb.ZAdd(ctx, q.queueKey(scope), redis.Z{Score: float64(time.Now().UnixMilli()), Member: requestID})

    // 2. 订阅唤醒
    sub := q.notifier.Subscribe(scope)
    defer sub.Close()

    // 3. 循环 try_acquire
    timeout := time.After(q.pollTimeout)
    for {
        rank, err := q.tryAcquireLua(ctx, scope, requestID, permitID)
        if rank == -2 {
            return &Permit{ID: permitID, scope: scope, q: q}, nil
        }
        if err != nil { return nil, err }
        onProgress(int(rank))

        select {
        case <-sub.Ch:
            continue  // 被唤醒，重试
        case <-time.After(200 * time.Millisecond):
            continue  // 降级定时轮询，防 Pub/Sub 丢消息
        case <-timeout:
            q.cleanupQueue(ctx, scope, requestID)
            return nil, ErrQueueTimeout
        case <-ctx.Done():
            q.cleanupQueue(ctx, scope, requestID)
            return nil, ctx.Err()
        }
    }
}
```

**SSE 推排队进度**：

```go
// middleware.go
func QueueMiddleware(q *Queue) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 只在某些 endpoint 启用
        if c.Request.URL.Path == "/api/chat/stream" {
            flusher, _ := c.Writer.(http.Flusher)
            c.Header("Content-Type", "text/event-stream")
            permit, err := q.Acquire(c.Request.Context(), "chat", func(rank int) {
                fmt.Fprintf(c.Writer, "event: queue\ndata: {\"rank\": %d}\n\n", rank)
                flusher.Flush()
            })
            if err != nil { c.AbortWithStatus(503); return }
            defer permit.Release()
        }
        c.Next()
    }
}
```

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| Permit 过期但业务还在执行（LLM 慢）| Lua watchdog 续期：业务侧每 10s SETEX 延长 TTL，超时上限 120s 强制释放 |
| Pub/Sub 消息丢失（网络抖动）| 200ms 定时轮询兜底（上面 select case），不完全依赖 Pub/Sub |
| 进程 crash，permit 没释放 | permit 自带 TTL 自动过期 + 启动时扫描孤儿 permit 清理 |
| Redis 宕机 | 本地 fallback 令牌桶（degrade mode），记指标告警，允许更低的 QPS |
| 恶意请求刷队列 | 按 user_id 维度加入队配额（每用户最多 5 个 in-flight request）|
| 队列堆积过长（> 1000）| 新进请求直接返回 "服务繁忙"，保护系统不雪崩 |
| 多队列 scope（chat / stream / embed 各自）| Queue 支持 scope 参数，每个 scope 独立 ZSET + semaphore |
| 超长对话独占 permit | permit 有 max duration（60s），超时业务侧主动 release 切段（流式场景不适用则放弃）|

### 5. 兜底策略

**三级降级**：

```
分布式 ZSET 限流
  ↓ Redis 主宕机
单机令牌桶 (x/time/rate)
  ↓ 本地也饱和
直接 429 + Retry-After header
```

**回滚**：配置开关 `ratelimit.mode: distributed | local | off`。

### 6. 量化指标 & 评估方案

| 指标 | 目标 |
|---|---|
| 限流准确率（实际并发不超过配置上限）| > 99% |
| 排队公平性（FIFO 偏差）| ZRANK 顺序完整保持 |
| 排队等待 P50 / P95 | P50 < 500ms, P95 < 3s |
| Pub/Sub 唤醒延迟 | < 50ms |
| Permit 泄漏率（过期后 orphan）| < 0.1% |
| 死锁次数 | 0（TTL 保护）|
| 突发 3 倍流量时系统可用性 | 正常用户仍能在 10s 内获得响应 |

**压测方案**：

```bash
# 单实例基线
vegeta attack -rate=50 -duration=60s -targets=chat_stream.txt | vegeta report

# 多实例协同（3 个实例 + Redis）
for i in 1 2 3; do (vegeta ... &); done
# 观察：合计 QPS 不超过 max_concurrent 配置；ZRANK 单调递增无乱序
```

**混沌测试**：
- Redis 主从切换期间（5s）限流仍基本可用（走本地 fallback）
- 单实例 kill -9：其他实例能接管队列（permit TTL 过期自动回收）

### 7. 面试 Q&A 预演（硬核）

**Q1：为什么不用 Redis-cell / Redis rate-limit 模块？**

A：Redis-cell 只做**限流**不做**排队**——超限就拒绝，没有"第 N 位"的反馈。我们的业务需要排队（用户愿意等），所以必须自己基于 ZSET 实现。另外 Redis-cell 需要额外模块，Redis Stack 里也有但非默认启用，工程侧要多一步部署。

**HQ1：两个实例同时 ZRANGE 拿到前 N 位，都想 acquire 怎么办？不会重复获取许可吗？**

A：**不会**，因为 `try_acquire` 是**一个 Lua 脚本原子执行**的，不是两步。Lua 内部：读 SCARD 判断 → SADD（检查不超限）→ SET permit → ZREM。Redis 单线程执行 Lua，两个实例的请求必然**顺序串行**，后到的那个会看到 SCARD 已经 +1，如果超限就返回 rank。

**踩坑版本**：早期用两个 Redis 命令（SCARD + SADD 分开）就会出现 race condition——所以**必须整个判断 + 修改放一个 Lua 里**。

**HQ2：permit TTL 30s 是怎么定的？太短业务没跑完就过期，太长 crash 后许可长期占用。**

A：三点：
1. **业务 P99 延迟决定下限**：统计过去 1 周 P99 = 15s，TTL 最少 2 倍 = 30s。
2. **业务侧续期**：长任务每 10s 主动 `EXPIRE` 延长，直到业务结束主动 `release`——像分布式锁的 watchdog。
3. **上限**：最长续期到 2min 强制释放（超过这个时间基本是 bug，宁可杀任务保系统）。

**可观测性**：监控 `permit_ttl_extend_total` + `permit_orphan_cleanup_total`，前者异常高说明业务慢了，后者异常高说明 crash 多——两者都要告警。

**HQ3：Pub/Sub 丢消息怎么办？你说 200ms 定时轮询兜底——但如果队列里有 100 个 waiter，每 200ms 都去 Redis 查一次，Redis 会不会被打爆？**

A：是真实问题。优化：
1. **Pub/Sub 不丢消息的场景**：单实例 + 单 Redis 99.9% 可靠；跨地域多实例 Redis sentinel 切换时会丢一次——低频
2. **轮询策略**：不是固定 200ms，而是**随机 jitter**（100-300ms），避免 100 个 waiter 同时打 Redis
3. **waiter 数量上限**：每实例最多 100 个在队列（超过就拒绝进队），Redis 压力可控
4. **更激进的优化**：用 **Redis Streams** 替代 Pub/Sub，消息持久化不丢——但这是架构升级，不在 MVP 范围

**HQ4：请求方 context 被取消（用户关页面），排队和 permit 怎么清理？**

A：
1. **队列清理**：Acquire 里 `case <-ctx.Done()` 分支里 `ZREM` 自己的 requestID——防止占位
2. **permit 清理**：如果已获取 permit 但 ctx cancel，业务层要 `defer permit.Release()`
3. **最差情况**：两步都漏了，permit TTL 过期自动 GC，ZSET 里的无效 requestID 被下个 waiter 跳过（Lua try_acquire 发现 rank 不匹配会返回 -1）

**本质**：**资源获取与释放必须配对 defer**——这是 Go 的基本功，但分布式场景下 TTL 兜底是"最后防线"。

**HQ5：假设队列 scope=chat 上限 10 并发，突然涌入 1000 请求，你说前端看"第 X 位"——但 10 个释放一个来一个，最后排到 500 位的用户等多久？P95 SLA 怎么定？**

A：数学算：
- 每 permit 平均持续 **T = 5s**（chat P50 延迟）
- 每 T 服务 10 个 → QPS = 2
- 第 500 位等待 = 500 / 2 = **250s ≈ 4 min**

显然不可接受。所以：
1. **队列长度上限**：超过 `max_queue = 100` 直接拒绝，告诉用户"稍后再试"
2. **自适应上限**：max_queue = `max_concurrent × target_p95_wait_sec / avg_T`
3. **流控前置**：Nginx 层先做粗限流（每 IP QPS），防止恶意洪水进队列
4. **P95 目标**：正常流量下 P95 等待 < 3s；突发时走拒绝策略而不是无限排队

**面试加分**：我会展示 `queue_wait_duration_bucket` histogram，当 P95 > SLA 时自动告警 + 触发 scale out。

**HQ6：多 region 部署时，每个 region 自己一套 Redis，limit 怎么算？全局 10 还是各 10？**

A：取决于上游**限流的是谁**：
- **上游 DashScope 按 API key 限**：global 10，必须跨 region 协同——用 **全局 Redis cluster** 或 **中心化 rate limit service**
- **上游按 IP 限**：每 region 各 10（各 region IP 不同），本地 Redis 即可
- **实际**：通常是 global 配额，方案 1 成本高（跨 region 延迟 ×2），折中方案是 **按 region 分配配额**（region A 获 60%, region B 获 40%）+ 每 region 独立限流

**谁做决策**：这是 SRE + 业务方讨论的配额分配问题，不是纯技术决策。

---

## P6 · 多模型路由 + 三态熔断 + 首包探测

### 1. 业务背景 / 痛点

**当前现状**：硬编码 `qwen-turbo`，没有备用路由，没有熔断。

典型故障：
- DashScope qwen-turbo 短时故障（上个月 3 次）→ 用户直接看到 error
- 可以降级到 qwen-plus 或 deepseek 但没做
- 流式场景尤其要命：**切模型时如果已经推了"正在思考..."，切到新模型又推"你好"**——用户端看到脏数据

**业务驱动**：ragent 的"三态熔断 + 首包探测"解决的就是**流式 AI 的模型切换脏数据问题**，这是生产级必备。

### 2. 技术选型 & 对比

**选型：熔断库**

| 方案 | 特点 |
|---|---|
| `sony/gobreaker` | 成熟、接口简洁 | ✅ |
| 自写 | 可控但造轮子 | ❌ |
| hystrix-go | 老旧，已不维护 | ❌ |

**首包探测**借鉴 ragent 的 **ProbeBufferingCallback**：

- 流式调用开始后，**先缓冲前 N 个 token 不推给客户端**
- 缓冲期内检测到 error → 切下一个模型，客户端完全无感
- 缓冲期过了没 error → 推缓冲 + 后续流

### 3. 核心实现方案

```
internal/router/
├── model_router.go       # 模型路由 + 优先级链
├── breaker.go            # 三态熔断器封装
├── probe_buffer.go       # 首包探测缓冲
└── config.go
```

**配置**：

```yaml
model_router:
  chains:
    - name: main
      models:
        - model: qwen-turbo
          priority: 1
          breaker: {failure_threshold: 5, timeout: 30s, half_open_requests: 3}
        - model: qwen-plus
          priority: 2
          breaker: {...}
        - model: deepseek-chat
          priority: 3
  probe_buffer:
    first_n_tokens: 10       # 缓冲前 10 个 token
    probe_timeout: 2s        # 2s 内没 error 就推给用户
```

**路由核心**：

```go
func (r *Router) Stream(ctx context.Context, messages []*schema.Message) (*StreamReader, error) {
    for _, m := range r.chain {
        if !m.breaker.AllowRequest() { continue }  // OPEN 跳过
        stream, err := m.model.Stream(ctx, messages)
        if err != nil { m.breaker.RecordFailure(); continue }

        // 首包探测
        pb := NewProbeBuffer(r.probeCfg.FirstNTokens, r.probeCfg.ProbeTimeout)
        buffered, probeErr := pb.Probe(stream)
        if probeErr != nil {
            m.breaker.RecordFailure()
            continue  // 切下一个模型，用户无感
        }
        m.breaker.RecordSuccess()
        return buffered.Combine(stream), nil  // 返回缓冲 + 剩余流
    }
    return nil, ErrAllModelsFailed
}
```

### 4. 边界 / 异常场景

| 场景 | 处理 |
|---|---|
| 所有模型都熔断 | 返 503 + Retry-After; metric 重点告警 |
| 首包后才 error | 已推给用户，无法切换；只能返错 |
| 熔断器误熔断（抖动）| HALF_OPEN 放 3 个请求探测恢复 |
| 不同模型 system prompt 不兼容 | router 层做 prompt 适配（不同模型不同模板）|
| 流式缓冲 OOM | 缓冲有 max_size（10KB）超了直接放行 |

### 5. 量化指标

| 指标 | 目标 |
|---|---|
| 单模型故障时用户端可用性 | > 99.5%（靠 fallback）|
| 首包探测切换延迟 | < 2s |
| 脏数据率（切换时用户看到半截内容）| 0 |
| 熔断误触率 | < 1% |

### 6. 硬核 Q&A

**HQ1：ProbeBufferingCallback 缓冲 10 个 token，但模型慢（首 token 3s 才出），用户会感觉延迟，体验差吗？**

A：折中：
- 缓冲上限 = `min(10 tokens, 2s timeout)`——**两个条件先到先走**
- 如果模型 1s 吐 5 token，2s 超时触发 → 推 5 token 给用户，风险：如果第 6 个 token 才 error，此时已推了 5 个，切换会脏
- **二阶段优化**：只在第一次生产环境观察到脏数据 > 0.1% 时启用"严格模式"（缓冲到 token 15 才推）；默认用宽松模式（2s timeout）

**HQ2：熔断 HALF_OPEN 放 3 个探测请求，如果这 3 个里 2 个还是失败怎么办？**

A：`gobreaker` 默认策略：HALF_OPEN 期间只要**一个失败**就 back to OPEN，多一轮 timeout。可配置：
- `MaxRequests`：HALF_OPEN 允许的探测请求数
- `OnStateChange`：状态变化 callback，打点到 Prometheus

**我们的策略**：HALF_OPEN 最多放 3 个，3 个里 success > 2 才转 CLOSED，否则 OPEN 再等 30s。**防抖动**。

**HQ3：Router 按 priority 串行 fallback，但第一个模型慢（20s 后 error）才切换，用户等太久怎么办？**

A：**并行探测 + race**：
- 一开始给 priority=1 模型发请求，2s 内无首 token → 并行给 priority=2 发
- 谁先吐首 token 用谁，另一个 cancel
- 成本翻倍但延迟稳定——在**高价值流量**上启用（VIP 用户 / SLA 承诺场景）

**但默认不并行**：串行简单 + 成本可控；除非业务明确要求，不加并行。

---

## Wave 4B 落地节奏（2-3 周）

| 周 | 任务 | 产出 |
|---|---|---|
| Week 1 | P7 ZSET 排队 + Semaphore + Lua 原子脚本 + 单测 | 单实例排队跑通 |
| Week 2 | P7 Pub/Sub + SSE 进度推送 + 压测 + 混沌 | 多实例协同 + 量化报告 |
| Week 3 | P6 模型路由 + 三态熔断 + 首包探测 | 上游故障演练 |

---

## 面试总纲：三分钟讲完 Wave 4B

> **P7 分布式排队限流**：
> 借鉴 ragent，基于 Redis ZSET 实现分布式排队（score = 毫秒时间戳，ZRANK 查位置告诉用户"第 N 位"）+ Semaphore 控并发上限 + **Lua 脚本保证原子**（SCARD 判断 + SADD 获取 + SET permit + ZREM 一次做完，避免 race）+ permit TTL 防死锁 + **Pub/Sub 跨实例唤醒** + SSE 推排队进度。三级降级：分布式 → 单机令牌桶 → 429。压测验证 3 倍突发流量下系统可用性 > 99.5%。
>
> **P6 模型路由 + 熔断**：
> 多模型优先级链（qwen-turbo → qwen-plus → deepseek），每条链路独立三态熔断器（sony/gobreaker，CLOSED/OPEN/HALF_OPEN，HALF_OPEN 3 探测恢复防抖动）。**流式场景的核心是首包探测**：缓冲前 10 个 token 或 2s，期内检测 error 切下一个模型，用户端无脏数据——这是流式 AI 生产环境的必备能力。单模型故障时用户可用性 > 99.5%。
>
> **面试爆点**：两个单独都能讲 15 分钟，结合起来是"流量侧（排队 + 限流）+ 后端侧（熔断 + 路由）"的完整生产级高可用故事。
