package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Error types for metrics classification
const (
	ErrorTypeAPIServer  = "api_server"
	ErrorTypeValidation = "validation"
	ErrorTypeInternal   = "internal"
	ErrorTypeNotFound   = "not_found"
	ErrorTypeConflict   = "conflict"
	ErrorTypeUnknown    = "unknown"
)

// Result labels for RED metrics
const (
	ResultSuccess = "success"
	ResultError   = "error"
)

// Metrics holds all the Prometheus metrics for the VPA operator
// Following RED principle: Rate, Errors, Duration
type Metrics struct {
	// ReconcileTotal is the total number of reconciliations (RED: Rate + Errors via result label)
	ReconcileTotal *prometheus.CounterVec

	// ReconcileDuration is the duration of reconciliation in seconds (RED: Duration)
	ReconcileDuration *prometheus.HistogramVec

	// ManagedVPAs is the number of VPAs managed by the operator (operator state gauge)
	ManagedVPAs *prometheus.GaugeVec

	// WatchedDeployments is the number of deployments watched by the operator (operator state gauge)
	WatchedDeployments *prometheus.GaugeVec

	// WebhookRequestsTotal is the total number of webhook requests (RED: Rate + Errors via result label)
	WebhookRequestsTotal *prometheus.CounterVec

	// WebhookDuration is the duration of webhook operations in seconds (RED: Duration)
	WebhookDuration *prometheus.HistogramVec

	// VPAOperationsTotal is the total number of VPA lifecycle operations
	VPAOperationsTotal *prometheus.CounterVec
}

// NewMetrics creates and registers all metrics with the given registry
// Metrics follow the RED principle:
// - Rate: request/operation counts with result labels
// - Errors: captured via result="error" label with error_type classification
// - Duration: histogram of operation latencies
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		// RED: Rate + Errors (combined via result label)
		ReconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vpa_operator_reconcile_total",
			Help: "Total number of reconciliations by result and error type",
		}, []string{"vpamanager", "result", "error_type"}),

		// RED: Duration
		ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vpa_operator_reconcile_duration_seconds",
			Help:    "Duration of reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"vpamanager", "result"}),

		// Operator state gauges (not RED, but useful for capacity planning)
		ManagedVPAs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vpa_operator_managed_vpas",
			Help: "Number of VPAs managed by the operator per VpaManager",
		}, []string{"vpamanager"}),

		WatchedDeployments: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vpa_operator_watched_deployments",
			Help: "Number of deployments watched by the operator per VpaManager",
		}, []string{"vpamanager"}),

		// RED: Rate + Errors (combined via result label)
		WebhookRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vpa_operator_webhook_requests_total",
			Help: "Total number of webhook requests by operation, result, and error type",
		}, []string{"operation", "result", "error_type"}),

		// RED: Duration
		WebhookDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vpa_operator_webhook_duration_seconds",
			Help:    "Duration of webhook operations in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation", "result"}),

		// VPA lifecycle operations
		VPAOperationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vpa_operator_vpa_operations_total",
			Help: "Total number of VPA lifecycle operations (create, delete, update)",
		}, []string{"operation", "vpamanager"}),
	}

	reg.MustRegister(
		m.ReconcileTotal,
		m.ReconcileDuration,
		m.ManagedVPAs,
		m.WatchedDeployments,
		m.WebhookRequestsTotal,
		m.WebhookDuration,
		m.VPAOperationsTotal,
	)

	return m
}

// RecordReconcile records a reconciliation attempt following RED principle
func (m *Metrics) RecordReconcile(vpaManagerName string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	result, errorType := classifyResult(err)

	m.ReconcileTotal.WithLabelValues(vpaManagerName, result, errorType).Inc()
	m.ReconcileDuration.WithLabelValues(vpaManagerName, result).Observe(duration)
}

// RecordWebhookRequest records a webhook request following RED principle
func (m *Metrics) RecordWebhookRequest(operation string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	result, errorType := classifyResult(err)

	m.WebhookRequestsTotal.WithLabelValues(operation, result, errorType).Inc()
	m.WebhookDuration.WithLabelValues(operation, result).Observe(duration)
}

// UpdateManagedResources updates the managed VPAs and watched deployments gauges
func (m *Metrics) UpdateManagedResources(vpaManagerName string, vpas, deployments int) {
	m.ManagedVPAs.WithLabelValues(vpaManagerName).Set(float64(vpas))
	m.WatchedDeployments.WithLabelValues(vpaManagerName).Set(float64(deployments))
}

// RecordVPAOperation records a VPA lifecycle operation (create, delete, update)
func (m *Metrics) RecordVPAOperation(operation, vpaManagerName string) {
	m.VPAOperationsTotal.WithLabelValues(operation, vpaManagerName).Inc()
}

// classifyResult returns the result label and error type for a given error
func classifyResult(err error) (result, errorType string) {
	if err == nil {
		return ResultSuccess, ""
	}
	return ResultError, ClassifyError(err)
}

// ClassifyError categorizes an error for metrics
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// Check for common Kubernetes API error patterns
	switch {
	case containsAny(errStr, "not found", "NotFound"):
		return ErrorTypeNotFound
	case containsAny(errStr, "conflict", "Conflict", "already exists"):
		return ErrorTypeConflict
	case containsAny(errStr, "validation", "invalid", "Invalid"):
		return ErrorTypeValidation
	case containsAny(errStr, "connection refused", "timeout", "context deadline"):
		return ErrorTypeAPIServer
	default:
		return ErrorTypeUnknown
	}
}

// containsAny checks if s contains any of the substrings
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
