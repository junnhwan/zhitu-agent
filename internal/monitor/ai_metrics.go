package monitor

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// AiMetrics mirrors Java AiModelMetricsCollector — Prometheus metrics for AI model calls.
type AiMetrics struct {
	registry *prometheus.Registry

	requestCounter *prometheus.CounterVec
	errorCounter   *prometheus.CounterVec
	tokenCounter   *prometheus.CounterVec
	responseTimer  *prometheus.HistogramVec
	workflowModeCounter *prometheus.CounterVec
	ragChannelFailed    *prometheus.CounterVec
	ragZeroHit          *prometheus.CounterVec
	ragRerankFallback   prometheus.Counter
	ragRetrieveDuration *prometheus.HistogramVec
	mcpToolsGauge       *prometheus.GaugeVec
	mcpCalls            *prometheus.CounterVec
	mcpCallDuration     *prometheus.HistogramVec

	counterCache map[string]prometheus.Counter
	timerCache   map[string]prometheus.Observer
	mu           sync.Mutex
}

// NewAiMetrics creates and registers AI model metrics with the given registry.
func NewAiMetrics(registry *prometheus.Registry) *AiMetrics {
	m := &AiMetrics{
		registry:     registry,
		counterCache: make(map[string]prometheus.Counter),
		timerCache:   make(map[string]prometheus.Observer),
	}

	m.requestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_model_requests_total",
		Help: "AI模型请求总数",
	}, []string{"user_id", "session_id", "model_name", "status"})

	m.errorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_model_errors_total",
		Help: "AI模型错误次数",
	}, []string{"user_id", "session_id", "model_name", "error_message"})

	m.tokenCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_model_tokens_total",
		Help: "AI模型Token消耗总数",
	}, []string{"user_id", "session_id", "model_name", "token_type"})

	m.responseTimer = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_model_response_duration_seconds",
		Help:    "AI模型响应时间",
		Buckets: prometheus.DefBuckets,
	}, []string{"user_id", "session_id", "model_name"})

	m.workflowModeCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_workflow_requests_total",
		Help: "对话链路选择分布（legacy vs graph）",
	}, []string{"mode", "entry"})

	registry.MustRegister(m.requestCounter, m.errorCounter, m.tokenCounter, m.responseTimer, m.workflowModeCounter)

	m.ragChannelFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rag_channel_failed_total",
		Help: "RAG 单通道检索失败/超时次数",
	}, []string{"channel"})
	m.ragZeroHit = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rag_zero_hit_total",
		Help: "RAG 所有通道零命中的兜底路径分布",
	}, []string{"fallback"})
	m.ragRerankFallback = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rag_rerank_fallback_total",
		Help: "rerank 失败/空结果降级次数",
	})
	m.ragRetrieveDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rag_retrieve_duration_seconds",
		Help:    "RAG 检索端到端耗时",
		Buckets: prometheus.DefBuckets,
	}, []string{"mode"})
	registry.MustRegister(m.ragChannelFailed, m.ragZeroHit, m.ragRerankFallback, m.ragRetrieveDuration)

	m.mcpToolsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_client_tools_total",
		Help: "MCP 客户端注册的工具数（按 server 分组）",
	}, []string{"server"})
	m.mcpCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_client_calls_total",
		Help: "MCP 工具调用次数（status=success|error）",
	}, []string{"server", "tool", "status"})
	m.mcpCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_client_call_duration_seconds",
		Help:    "MCP 工具调用端到端耗时",
		Buckets: prometheus.DefBuckets,
	}, []string{"server", "tool"})
	registry.MustRegister(m.mcpToolsGauge, m.mcpCalls, m.mcpCallDuration)

	return m
}

func (m *AiMetrics) SetMCPToolsCount(server string, n int) {
	m.mcpToolsGauge.WithLabelValues(server).Set(float64(n))
}

func (m *AiMetrics) RecordMCPCall(server, toolName, status string, d time.Duration) {
	m.mcpCalls.WithLabelValues(server, toolName, status).Inc()
	m.mcpCallDuration.WithLabelValues(server, toolName).Observe(d.Seconds())
}

func (m *AiMetrics) RecordRAGChannelFailed(name string) {
	m.ragChannelFailed.WithLabelValues(name).Inc()
}

func (m *AiMetrics) RecordRAGZeroHit(fallback string) {
	m.ragZeroHit.WithLabelValues(fallback).Inc()
}

func (m *AiMetrics) RecordRAGRerankFallback() {
	m.ragRerankFallback.Inc()
}

func (m *AiMetrics) RecordRAGRetrieveDuration(mode string, d time.Duration) {
	m.ragRetrieveDuration.WithLabelValues(mode).Observe(d.Seconds())
}

// RecordRequest increments the request counter with the given labels.
func (m *AiMetrics) RecordRequest(userID, sessionID, modelName, status string) {
	safeLabel := func(s string) string {
		if s == "" {
			return "unknown"
		}
		return s
	}
	m.requestCounter.WithLabelValues(safeLabel(userID), safeLabel(sessionID), safeLabel(modelName), safeLabel(status)).Inc()
}

// RecordError increments the error counter.
func (m *AiMetrics) RecordError(userID, sessionID, modelName, errorMessage string) {
	m.errorCounter.WithLabelValues(userID, sessionID, modelName, errorMessage).Inc()
}

// RecordTokenUsage increments the token counter.
func (m *AiMetrics) RecordTokenUsage(userID, sessionID, modelName, tokenType string, count float64) {
	m.tokenCounter.WithLabelValues(userID, sessionID, modelName, tokenType).Add(count)
}

// RecordResponseTime records the response duration.
func (m *AiMetrics) RecordResponseTime(userID, sessionID, modelName string, duration time.Duration) {
	m.responseTimer.WithLabelValues(userID, sessionID, modelName).Observe(duration.Seconds())
}

// RecordWorkflowMode increments the counter tracking legacy vs graph chat chain usage.
// entry is the caller — "chat" or "stream_chat".
func (m *AiMetrics) RecordWorkflowMode(mode, entry string) {
	if mode == "" {
		mode = "legacy"
	}
	m.workflowModeCounter.WithLabelValues(mode, entry).Inc()
}
