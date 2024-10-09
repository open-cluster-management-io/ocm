package generic

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Subsystem used to define the metrics:
const metricsSubsystem = "resources"

// Names of the labels added to metrics:
const (
	metricsSourceLabel   = "source"
	metricsClusterLabel  = "cluster"
	metrucsDataTypeLabel = "type"
)

// metricsLabels - Array of labels added to metrics:
var metricsLabels = []string{
	metricsSourceLabel,   // source
	metricsClusterLabel,  // cluster
	metrucsDataTypeLabel, // resource type
}

// Names of the metrics:
const (
	specResyncDurationMetric   = "spec_resync_duration_seconds"
	statusResyncDurationMetric = "status_resync_duration_seconds"
)

// The resource spec resync duration metric is a histogram with a base metric name of 'resource_spec_resync_duration_second'
// exposes multiple time series during a scrape:
// 1. cumulative counters for the observation buckets, exposed as 'resource_spec_resync_duration_seconds_bucket{le="<upper inclusive bound>"}'
// 2. the total sum of all observed values, exposed as 'resource_spec_resync_duration_seconds_sum'
// 3. the count of events that have been observed, exposed as 'resource_spec_resync_duration_seconds_count' (identical to 'resource_spec_resync_duration_seconds_bucket{le="+Inf"}' above)
// For example, 2 resource spec resync for manifests type that have been observed, one taking 0.5s and the other taking 0.7s, would result in the following metrics:
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="0.1"} 0
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="0.2"} 0
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="0.5"} 1
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="1.0"} 2
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="2.0"} 2
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="10.0"} 2
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="30.0"} 2
// resource_spec_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",le="+Inf"} 2
// resource_spec_resync_duration_seconds_sum{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests"} 1.2
// resource_spec_resync_duration_seconds_count{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests"} 2
var resourceSpecResyncDurationMetric = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: metricsSubsystem,
		Name:      specResyncDurationMetric,
		Help:      "The duration of the resource spec resync in seconds.",
		Buckets: []float64{
			0.1,
			0.2,
			0.5,
			1.0,
			2.0,
			10.0,
			30.0,
		},
	},
	metricsLabels,
)

// The resource status resync duration metric is a histogram with a base metric name of 'resource_status_resync_duration_second'
// exposes multiple time series during a scrape:
// 1. cumulative counters for the observation buckets, exposed as 'resource_status_resync_duration_seconds_bucket{le="<upper inclusive bound>"}'
// 2. the total sum of all observed values, exposed as 'resource_status_resync_duration_seconds_sum'
// 3. the count of events that have been observed, exposed as 'resource_status_resync_duration_seconds_count' (identical to 'resource_status_resync_duration_seconds_bucket{le="+Inf"}' above)
// For example, 2 resource status resync for manifestbundles type that have been observed, one taking 0.5s and the other taking 1.1s, would result in the following metrics:
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="0.1"} 0
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="0.2"} 0
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="0.5"} 1
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="1.0"} 1
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="2.0"} 2
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="10.0"} 2
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="30.0"} 2
// resource_status_resync_duration_seconds_bucket{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",le="+Inf"} 2
// resource_status_resync_duration_seconds_sum{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles"} 1.6
// resource_status_resync_duration_seconds_count{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles"} 2
var resourceStatusResyncDurationMetric = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: metricsSubsystem,
		Name:      statusResyncDurationMetric,
		Help:      "The duration of the resource status resync in seconds.",
		Buckets: []float64{
			0.1,
			0.2,
			0.5,
			1.0,
			2.0,
			10.0,
			30.0,
		},
	},
	metricsLabels,
)

// Register the metrics:
func RegisterResourceResyncMetrics() {
	prometheus.MustRegister(resourceSpecResyncDurationMetric)
	prometheus.MustRegister(resourceStatusResyncDurationMetric)
}

// Unregister the metrics:
func UnregisterResourceResyncMetrics() {
	prometheus.Unregister(resourceStatusResyncDurationMetric)
	prometheus.Unregister(resourceStatusResyncDurationMetric)
}

// ResetResourceResyncMetricsCollectors resets all collectors
func ResetResourceResyncMetricsCollectors() {
	resourceSpecResyncDurationMetric.Reset()
	resourceStatusResyncDurationMetric.Reset()
}

// updateResourceSpecResyncDurationMetric updates the resource spec resync duration metric:
func updateResourceSpecResyncDurationMetric(source, cluster, dataType string, startTime time.Time) {
	labels := prometheus.Labels{
		metricsSourceLabel:   source,
		metricsClusterLabel:  cluster,
		metrucsDataTypeLabel: dataType,
	}
	duration := time.Since(startTime)
	resourceSpecResyncDurationMetric.With(labels).Observe(duration.Seconds())
}

// updateResourceStatusResyncDurationMetric updates the resource status resync duration metric:
func updateResourceStatusResyncDurationMetric(source, cluster, dataType string, startTime time.Time) {
	labels := prometheus.Labels{
		metricsSourceLabel:   source,
		metricsClusterLabel:  cluster,
		metrucsDataTypeLabel: dataType,
	}
	duration := time.Since(startTime)
	resourceStatusResyncDurationMetric.With(labels).Observe(duration.Seconds())
}
