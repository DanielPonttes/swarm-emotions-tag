package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Reporter interface {
	ObserveStepDuration(step string, duration time.Duration)
	IncDependencyError(dependency, operation string)
}

type noopReporter struct{}

func (noopReporter) ObserveStepDuration(string, time.Duration) {}

func (noopReporter) IncDependencyError(string, string) {}

func NewNoopReporter() Reporter {
	return noopReporter{}
}

type PrometheusReporter struct {
	stepDurationMs   *prometheus.HistogramVec
	dependencyErrors *prometheus.CounterVec
}

func NewPrometheusReporter(registerer prometheus.Registerer) *PrometheusReporter {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	stepDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "orchestrator_step_duration_ms",
			Help:    "Pipeline step duration in milliseconds.",
			Buckets: []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
		},
		[]string{"step"},
	)
	dependencyErrors := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orchestrator_dependency_errors_total",
			Help: "Total dependency errors by dependency and operation.",
		},
		[]string{"dependency", "operation"},
	)

	registerer.MustRegister(stepDuration, dependencyErrors)
	return &PrometheusReporter{
		stepDurationMs:   stepDuration,
		dependencyErrors: dependencyErrors,
	}
}

func (r *PrometheusReporter) ObserveStepDuration(step string, duration time.Duration) {
	if r == nil {
		return
	}
	r.stepDurationMs.WithLabelValues(step).Observe(float64(duration) / float64(time.Millisecond))
}

func (r *PrometheusReporter) IncDependencyError(dependency, operation string) {
	if r == nil {
		return
	}
	r.dependencyErrors.WithLabelValues(dependency, operation).Inc()
}
