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
	return m
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
