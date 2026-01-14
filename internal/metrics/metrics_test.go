package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: vpa_operator_reconcile_total metric (RED: Rate + Errors)
func TestMetrics_ReconcileTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Initially should be 0
	assert.Equal(t, float64(0), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "")))

	// Increment reconcile count with success
	m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "").Inc()
	assert.Equal(t, float64(1), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "")))

	// Track errors separately
	m.ReconcileTotal.WithLabelValues("test-manager", ResultError, ErrorTypeAPIServer).Inc()
	assert.Equal(t, float64(1), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultError, ErrorTypeAPIServer)))
}

// Test: vpa_operator_reconcile_duration_seconds metric (RED: Duration)
func TestMetrics_ReconcileDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Observe some durations with labels
	m.ReconcileDuration.WithLabelValues("test-manager", ResultSuccess).Observe(0.1)
	m.ReconcileDuration.WithLabelValues("test-manager", ResultSuccess).Observe(0.2)
	m.ReconcileDuration.WithLabelValues("test-manager", ResultError).Observe(0.3)

	// Verify histogram has observations
	count := testutil.CollectAndCount(m.ReconcileDuration)
	assert.Equal(t, 2, count, "should have histogram metrics for each result type")
}

// Test: vpa_operator_managed_vpas metric (with vpamanager label)
func TestMetrics_ManagedVPAs(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Set managed VPAs count per vpamanager
	m.ManagedVPAs.WithLabelValues("manager-1").Set(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(m.ManagedVPAs.WithLabelValues("manager-1")))

	m.ManagedVPAs.WithLabelValues("manager-1").Set(10)
	assert.Equal(t, float64(10), testutil.ToFloat64(m.ManagedVPAs.WithLabelValues("manager-1")))

	// Different managers tracked separately
	m.ManagedVPAs.WithLabelValues("manager-2").Set(3)
	assert.Equal(t, float64(10), testutil.ToFloat64(m.ManagedVPAs.WithLabelValues("manager-1")))
	assert.Equal(t, float64(3), testutil.ToFloat64(m.ManagedVPAs.WithLabelValues("manager-2")))
}

// Test: vpa_operator_watched_deployments metric (with vpamanager label)
func TestMetrics_WatchedDeployments(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.WatchedDeployments.WithLabelValues("manager-1").Set(15)
	assert.Equal(t, float64(15), testutil.ToFloat64(m.WatchedDeployments.WithLabelValues("manager-1")))
}

// Test: vpa_operator_webhook_requests_total metric (RED: Rate + Errors via result label)
func TestMetrics_WebhookRequestsTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Track requests by operation type and result
	m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "").Inc()
	m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "").Inc()
	m.WebhookRequestsTotal.WithLabelValues("DELETE", ResultSuccess, "").Inc()
	m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultError, ErrorTypeNotFound).Inc()

	createSuccessCount := testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, ""))
	assert.Equal(t, float64(2), createSuccessCount)

	deleteSuccessCount := testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("DELETE", ResultSuccess, ""))
	assert.Equal(t, float64(1), deleteSuccessCount)

	createErrorCount := testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultError, ErrorTypeNotFound))
	assert.Equal(t, float64(1), createErrorCount)
}

// Test: vpa_operator_webhook_duration_seconds metric (RED: Duration)
func TestMetrics_WebhookDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.WebhookDuration.WithLabelValues("CREATE", ResultSuccess).Observe(0.05)
	m.WebhookDuration.WithLabelValues("CREATE", ResultSuccess).Observe(0.02)
	m.WebhookDuration.WithLabelValues("DELETE", ResultError).Observe(0.01)

	count := testutil.CollectAndCount(m.WebhookDuration)
	assert.Equal(t, 2, count, "should have histogram metrics for each operation/result combination")
}

// Test: vpa_operator_vpa_operations_total metric
func TestMetrics_VPAOperationsTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.VPAOperationsTotal.WithLabelValues("create", "manager-1").Inc()
	m.VPAOperationsTotal.WithLabelValues("create", "manager-1").Inc()
	m.VPAOperationsTotal.WithLabelValues("delete", "manager-1").Inc()
	m.VPAOperationsTotal.WithLabelValues("create", "manager-2").Inc()

	assert.Equal(t, float64(2), testutil.ToFloat64(m.VPAOperationsTotal.WithLabelValues("create", "manager-1")))
	assert.Equal(t, float64(1), testutil.ToFloat64(m.VPAOperationsTotal.WithLabelValues("delete", "manager-1")))
	assert.Equal(t, float64(1), testutil.ToFloat64(m.VPAOperationsTotal.WithLabelValues("create", "manager-2")))
}

// Test: All metrics are registered correctly
func TestMetrics_AllMetricsRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Verify all metrics can be collected
	metrics, err := reg.Gather()
	require.NoError(t, err)

	expectedMetrics := []string{
		"vpa_operator_reconcile_total",
		"vpa_operator_reconcile_duration_seconds",
		"vpa_operator_managed_vpas",
		"vpa_operator_watched_deployments",
		"vpa_operator_webhook_requests_total",
		"vpa_operator_webhook_duration_seconds",
		"vpa_operator_vpa_operations_total",
	}

	// Initialize all label combinations to ensure they appear
	m.ReconcileTotal.WithLabelValues("test", ResultSuccess, "")
	m.ReconcileDuration.WithLabelValues("test", ResultSuccess)
	m.ManagedVPAs.WithLabelValues("test")
	m.WatchedDeployments.WithLabelValues("test")
	m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "")
	m.WebhookDuration.WithLabelValues("CREATE", ResultSuccess)
	m.VPAOperationsTotal.WithLabelValues("create", "test")

	metrics, err = reg.Gather()
	require.NoError(t, err)

	registeredNames := make([]string, 0, len(metrics))
	for _, mf := range metrics {
		registeredNames = append(registeredNames, *mf.Name)
	}

	for _, expected := range expectedMetrics {
		assert.Contains(t, registeredNames, expected, "metric %s should be registered", expected)
	}
}

// Test: Metrics helper functions
func TestMetrics_RecordReconcile(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record successful reconcile
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	m.RecordReconcile("test-manager", start, nil)

	assert.Equal(t, float64(1), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "")))
	assert.Equal(t, float64(0), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultError, ErrorTypeUnknown)))

	// Record failed reconcile
	start = time.Now()
	m.RecordReconcile("test-manager", start, assert.AnError)

	assert.Equal(t, float64(1), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "")))
	assert.Equal(t, float64(1), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultError, ErrorTypeUnknown)))
}

func TestMetrics_RecordWebhookRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	start := time.Now()
	time.Sleep(5 * time.Millisecond)
	m.RecordWebhookRequest("CREATE", start, nil)

	assert.Equal(t, float64(1), testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "")))

	// Record with error
	start = time.Now()
	m.RecordWebhookRequest("DELETE", start, assert.AnError)

	assert.Equal(t, float64(1), testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("DELETE", ResultError, ErrorTypeUnknown)))
}

func TestMetrics_UpdateManagedResources(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.UpdateManagedResources("test-manager", 10, 25)

	assert.Equal(t, float64(10), testutil.ToFloat64(m.ManagedVPAs.WithLabelValues("test-manager")))
	assert.Equal(t, float64(25), testutil.ToFloat64(m.WatchedDeployments.WithLabelValues("test-manager")))
}

func TestMetrics_RecordVPAOperation(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordVPAOperation("create", "manager-1")
	m.RecordVPAOperation("create", "manager-1")
	m.RecordVPAOperation("delete", "manager-1")

	assert.Equal(t, float64(2), testutil.ToFloat64(m.VPAOperationsTotal.WithLabelValues("create", "manager-1")))
	assert.Equal(t, float64(1), testutil.ToFloat64(m.VPAOperationsTotal.WithLabelValues("delete", "manager-1")))
}

// Test: Metrics descriptions match README documentation
func TestMetrics_DescriptionsMatchDocumentation(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Initialize metrics to ensure they appear
	m.ReconcileTotal.WithLabelValues("test", ResultSuccess, "")
	m.ReconcileDuration.WithLabelValues("test", ResultSuccess)
	m.ManagedVPAs.WithLabelValues("test")
	m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "")
	m.WebhookDuration.WithLabelValues("CREATE", ResultSuccess)

	metrics, err := reg.Gather()
	require.NoError(t, err)

	descriptions := make(map[string]string)
	for _, mf := range metrics {
		descriptions[*mf.Name] = *mf.Help
	}

	// Verify key descriptions
	assert.Contains(t, descriptions["vpa_operator_reconcile_total"], "reconciliation", "reconcile_total should describe reconciliations")
	assert.Contains(t, descriptions["vpa_operator_managed_vpas"], "VPA", "managed_vpas should mention VPAs")
	assert.Contains(t, descriptions["vpa_operator_webhook_requests_total"], "webhook", "webhook_requests should describe webhook")
}

// Test: Concurrent access to metrics is safe
func TestMetrics_ConcurrentAccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	done := make(chan bool)

	// Concurrent reconcile count updates
	go func() {
		for i := 0; i < 100; i++ {
			m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "").Inc()
		}
		done <- true
	}()

	// Concurrent managed VPAs updates
	go func() {
		for i := 0; i < 100; i++ {
			m.ManagedVPAs.WithLabelValues("test-manager").Set(float64(i))
		}
		done <- true
	}()

	// Concurrent webhook metrics updates
	go func() {
		for i := 0; i < 100; i++ {
			m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "").Inc()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify reconcile count
	assert.Equal(t, float64(100), testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("test-manager", ResultSuccess, "")))
	assert.Equal(t, float64(100), testutil.ToFloat64(m.WebhookRequestsTotal.WithLabelValues("CREATE", ResultSuccess, "")))
}

// Test: Error classification
func TestMetrics_ClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"nil error", nil, ""},
		{"not found error", assert.AnError, ErrorTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test: containsAny helper function
func TestMetrics_ContainsAny(t *testing.T) {
	assert.True(t, containsAny("connection refused", "connection refused", "timeout"))
	assert.True(t, containsAny("not found in cluster", "not found", "NotFound"))
	assert.False(t, containsAny("success", "error", "failed"))
	assert.False(t, containsAny("", "error"))
}
