package monitor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RagMetrics mirrors Java RagMetricsCollector — Prometheus metrics for RAG retrieval.
type RagMetrics struct {
	registry *prometheus.Registry

	hitCounter      *prometheus.CounterVec
	missCounter     *prometheus.CounterVec
	retrievalTimer  *prometheus.HistogramVec
}

// NewRagMetrics creates and registers RAG metrics with the given registry.
func NewRagMetrics(registry *prometheus.Registry) *RagMetrics {
	m := &RagMetrics{
		registry: registry,
	}

	m.hitCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rag_retrieval_hit_total",
		Help: "RAG知识检索命中次数",
	}, []string{"user_id", "session_id"})

	m.missCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rag_retrieval_miss_total",
		Help: "RAG知识检索未命中次数",
	}, []string{"user_id", "session_id"})

	m.retrievalTimer = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rag_retrieval_duration_seconds",
		Help:    "RAG知识检索耗时",
		Buckets: prometheus.DefBuckets,
	}, []string{"user_id", "session_id"})

	registry.MustRegister(m.hitCounter, m.missCounter, m.retrievalTimer)
	return m
}

// RecordHit increments the RAG hit counter.
func (m *RagMetrics) RecordHit(userID, sessionID string) {
	m.hitCounter.WithLabelValues(userID, sessionID).Inc()
}

// RecordMiss increments the RAG miss counter.
func (m *RagMetrics) RecordMiss(userID, sessionID string) {
	m.missCounter.WithLabelValues(userID, sessionID).Inc()
}

// RecordRetrievalTime records the RAG retrieval duration.
func (m *RagMetrics) RecordRetrievalTime(userID, sessionID string, duration time.Duration) {
	m.retrievalTimer.WithLabelValues(userID, sessionID).Observe(duration.Seconds())
}
