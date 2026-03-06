package patcher

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/errors"
)

// Subsystem used to define the patcher metrics
const patcherMetricsSubsystem = "patcher"

// Names of the labels added to metrics
const (
	metricsOperationLabel = "operation"
	metricsStatusLabel    = "status"
	metricsResourceLabel  = "resource"
)

// Status values
const (
	StatusSuccess  = "success"
	StatusError    = "error"
	StatusConflict = "conflict"
)

// Operation types
const (
	OperationAddFinalizer         = "add_finalizer"
	OperationRemoveFinalizer      = "remove_finalizer"
	OperationPatchStatus          = "patch_status"
	OperationPatchSpec            = "patch_spec"
	OperationPatchLabelAnnotation = "patch_label_annotation"
)

// patcherOperationsMetricsLabels - Array of labels added to patcher operations metrics
var patcherOperationsMetricsLabels = []string{
	metricsOperationLabel, // operation type
	metricsResourceLabel,  // resource GVK (e.g., "ManagedCluster.cluster.open-cluster-management.io")
	metricsStatusLabel,    // status (success/error/conflict)
}

// patcherDurationMetricsLabels - Array of labels added to patcher duration metrics
var patcherDurationMetricsLabels = []string{
	metricsOperationLabel, // operation type
	metricsResourceLabel,  // resource GVK (e.g., "ManagedCluster.cluster.open-cluster-management.io")
}

// Names of the metrics
const (
	operationsCountMetric = "operations_total"
	durationMetric        = "operation_duration_seconds"
)

// PatcherOperationsCounterMetric is a counter metric that tracks the total number of patcher operations
// with their status (success/error/conflict) and resource GVK.
// For example, 2 successful AddFinalizer operations on ManagedClusters and 1 conflict on PatchStatus would result in:
// patcher_operations_total{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io",status="success"} 2
// patcher_operations_total{operation="patch_status",resource="ManagedCluster.cluster.open-cluster-management.io",status="conflict"} 1
var PatcherOperationsCounterMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: patcherMetricsSubsystem,
		Name:      operationsCountMetric,
		Help:      "The total number of patcher operations.",
	},
	patcherOperationsMetricsLabels,
)

// PatcherOperationDurationMetric is a histogram metric that tracks the duration of patcher operations by resource GVK.
// It exposes multiple time series during a scrape:
// 1. cumulative counters for the observation buckets, exposed as 'patcher_operation_duration_seconds_bucket{le="<upper inclusive bound>"}'
// 2. the total sum of all observed values, exposed as 'patcher_operation_duration_seconds_sum'
// 3. the count of events that have been observed, exposed as 'patcher_operation_duration_seconds_count'
// For example, 2 AddFinalizer operations on ManagedClusters that took 0.05s and 0.15s would result in:
// patcher_operation_duration_seconds_bucket{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io",le="0.01"} 0
// patcher_operation_duration_seconds_bucket{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io",le="0.05"} 1
// patcher_operation_duration_seconds_bucket{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io",le="0.1"} 1
// patcher_operation_duration_seconds_bucket{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io",le="0.5"} 2
// patcher_operation_duration_seconds_sum{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io"} 0.2
// patcher_operation_duration_seconds_count{operation="add_finalizer",resource="ManagedCluster.cluster.open-cluster-management.io"} 2
var PatcherOperationDurationMetric = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: patcherMetricsSubsystem,
		Name:      durationMetric,
		Help:      "The duration of patcher operations in seconds.",
		Buckets: []float64{
			0.001, // 1ms
			0.005, // 5ms
			0.01,  // 10ms
			0.05,  // 50ms
			0.1,   // 100ms
			0.5,   // 500ms
			1.0,   // 1s
			5.0,   // 5s
		},
	},
	patcherDurationMetricsLabels,
)

// RegisterPatcherMetrics registers all patcher metrics
func RegisterPatcherMetrics(register prometheus.Registerer) {
	register.MustRegister(PatcherOperationsCounterMetric)
	register.MustRegister(PatcherOperationDurationMetric)
}

// ResetPatcherMetrics resets all patcher metrics
func ResetPatcherMetrics() {
	PatcherOperationsCounterMetric.Reset()
	PatcherOperationDurationMetric.Reset()
}

// RecordPatcherOperation records a patcher operation with its resource, error, and duration
// The status label is automatically determined from the error:
// - nil error -> "success"
// - conflict error -> "conflict"
// - other error -> "error"
func RecordPatcherOperation(operation, resource string, err error, duration time.Duration) {
	// Determine status from error
	status := StatusSuccess
	if err != nil {
		if errors.IsConflict(err) {
			status = StatusConflict
		} else {
			status = StatusError
		}
	}

	// Record the counter
	PatcherOperationsCounterMetric.With(prometheus.Labels{
		metricsOperationLabel: operation,
		metricsResourceLabel:  resource,
		metricsStatusLabel:    status,
	}).Inc()

	// Record the duration
	PatcherOperationDurationMetric.With(prometheus.Labels{
		metricsOperationLabel: operation,
		metricsResourceLabel:  resource,
	}).Observe(duration.Seconds())
}
