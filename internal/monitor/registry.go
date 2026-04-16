package monitor

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Registry holds the shared Prometheus registry and all metrics collectors.
type Registry struct {
	Prometheus *prometheus.Registry
	AiMetrics  *AiMetrics
	RagMetrics *RagMetrics
	Logger     *Logger
}

// NewRegistry creates a new monitor registry with all metrics registered.
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()

	return &Registry{
		Prometheus: reg,
		AiMetrics:  NewAiMetrics(reg),
		RagMetrics: NewRagMetrics(reg),
		Logger:     NewLogger(),
	}
}

// DefaultRegistry is the global monitor registry instance.
var DefaultRegistry = NewRegistry()
