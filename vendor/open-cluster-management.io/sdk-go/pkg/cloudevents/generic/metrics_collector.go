package generic

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Subsystem used to define the metrics:
const (
	cloudeventsMetricsSubsystem  = "cloudevents"
	resourcesMetricsSubsystem    = "resources"
	manifestworkMetricsSubsystem = "manifestworks"
)

// Names of the labels added to metrics:
const (
	metricsSourceLabel         = "source"
	metricsOriginalSourceLabel = "original_source"
	metricsClusterLabel        = "cluster"
	metricsDataTypeLabel       = "type"
	metricsSubResourceLabel    = "subresource"
	metricsActionLabel         = "action"
	metricsClientIDLabel       = "client_id"
	metricsWorkActionLabel     = "action"
	metricsWorkCodeLabel       = "code"
)

const noneOriginalSource = "none"

// cloudeventsReceivedMetricsLabels - Array of labels added to cloudevents received metrics:
var cloudeventsReceivedMetricsLabels = []string{
	metricsSourceLabel,      // source
	metricsClusterLabel,     // cluster
	metricsDataTypeLabel,    // data type, e.g. manifests, manifestbundles
	metricsSubResourceLabel, // subresource, eg, spec or status
	metricsActionLabel,      // action, eg, create, update, delete, resync_request, resync_response
}

// cloudeventsSentMetricsLabels - Array of labels added to cloudevents sent metrics:
var cloudeventsSentMetricsLabels = []string{
	metricsSourceLabel,         // source
	metricsOriginalSourceLabel, // original source, if no, set to "none"
	metricsClusterLabel,        // cluster
	metricsDataTypeLabel,       // data type, e.g. manifests, manifestbundles
	metricsSubResourceLabel,    // subresource, eg, spec or status
	metricsActionLabel,         // action, eg, create, update, delete, resync_request, resync_response
}

// cloudeventsResyncMetricsLabels - Array of labels added to cloudevents resync metrics:
var cloudeventsResyncMetricsLabels = []string{
	metricsSourceLabel,   // source
	metricsClusterLabel,  // cluster
	metricsDataTypeLabel, // data type, e.g. manifests, manifestbundles
}

// cloudeventsClientMetricsLabels - Array of labels added to cloudevents client metrics:
var cloudeventsClientMetricsLabels = []string{
	metricsClientIDLabel, // client_id
}

// workMetricsLabels - Array of labels added to manifestwork metrics:
var workMetricsLabels = []string{
	metricsWorkActionLabel, // action
	metricsWorkCodeLabel,   // code
}

// Names of the metrics:
const (
	receivedCounterMetric      = "received_total"
	sentCounterMetric          = "sent_total"
	specResyncDurationMetric   = "spec_resync_duration_seconds"
	statusResyncDurationMetric = "status_resync_duration_seconds"
	clientReconnectedCounter   = "client_reconnected_total"
	workProcessedCounter       = "processed_total"
)

// The cloudevents received counter metric is a counter with a base metric name of 'received_total'
// and a help string of 'The total number of received CloudEvents.'
// For example, 2 CloudEvents received from source1 to agent on cluster1 with data type manifests, one for resource create,
// another for resource updatewould result in the following metrics:
// cloudevents_received_total{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",subresource="spec",action="create"} 1
// cloudevents_received_total{source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifests",subresource="spec",action="update"} 1
var cloudeventsReceivedCounterMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: cloudeventsMetricsSubsystem,
		Name:      receivedCounterMetric,
		Help:      "The total number of received CloudEvents.",
	},
	cloudeventsReceivedMetricsLabels,
)

// The cloudevents sent counter metric is a counter with a base metric name of 'sent_total'
// and a help string of 'The total number of sent CloudEvents.'
// For example, 1 cloudevent sent from source1 with data type manifestbundles for resource spec create (original source is empty),
// and 2 CloudEvents sent from agent on cluster1 back to source1 for resource status update would result in the following metrics:
// cloudevents_sent_total{source="source1",original_source="none",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",subresource="spec",action="create"} 1
// cloudevents_sent_total{source="cluster1-work-agent",original_source="source1",cluster="cluster1",type="io.open-cluster-management.works.v1alpha1.manifestbundles",subresource="status",action="update"} 2
var cloudeventsSentCounterMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: cloudeventsMetricsSubsystem,
		Name:      sentCounterMetric,
		Help:      "The total number of sent CloudEvents.",
	},
	cloudeventsSentMetricsLabels,
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
		Subsystem: resourcesMetricsSubsystem,
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
	cloudeventsResyncMetricsLabels,
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
		Subsystem: resourcesMetricsSubsystem,
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
	cloudeventsResyncMetricsLabels,
)

// The cloudevents client reconnected counter metric is a counter with a base metric name of 'client_reconnected_total'
// and a help string of 'The total number of reconnects for the CloudEvents client.'
// For example, 2 reconnects for the CloudEvents client with client_id=client1 would result in the following metrics:
// client_reconnected_total{client_id="client1"} 2
var clientReconnectedCounterMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: cloudeventsMetricsSubsystem,
		Name:      clientReconnectedCounter,
		Help:      "The total number of reconnects for the CloudEvents client.",
	},
	cloudeventsClientMetricsLabels,
)

var workProcessedCounterMetric = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: manifestworkMetricsSubsystem,
		Name:      workProcessedCounter,
		Help:      "The total number of processed manifestworks.",
	},
	workMetricsLabels,
)

// Register the metrics:
func RegisterCloudEventsMetrics(register prometheus.Registerer) {
	register.MustRegister(cloudeventsReceivedCounterMetric)
	register.MustRegister(cloudeventsSentCounterMetric)
	register.MustRegister(resourceSpecResyncDurationMetric)
	register.MustRegister(resourceStatusResyncDurationMetric)
	register.MustRegister(clientReconnectedCounterMetric)
	register.MustRegister(workProcessedCounterMetric)
}

// Unregister the metrics:
func UnregisterCloudEventsMetrics(register prometheus.Registerer) {
	register.Unregister(cloudeventsReceivedCounterMetric)
	register.Unregister(cloudeventsSentCounterMetric)
	register.Unregister(resourceStatusResyncDurationMetric)
	register.Unregister(resourceStatusResyncDurationMetric)
	register.Unregister(clientReconnectedCounterMetric)
	register.Unregister(workProcessedCounterMetric)
}

// ResetCloudEventsMetrics resets all collectors
func ResetCloudEventsMetrics() {
	cloudeventsReceivedCounterMetric.Reset()
	cloudeventsSentCounterMetric.Reset()
	resourceSpecResyncDurationMetric.Reset()
	resourceStatusResyncDurationMetric.Reset()
	clientReconnectedCounterMetric.Reset()
	workProcessedCounterMetric.Reset()
}

// increaseCloudEventsReceivedCounter increases the cloudevents sent counter metric:
func increaseCloudEventsReceivedCounter(source, cluster, dataType, subresource, action string) {
	labels := prometheus.Labels{
		metricsSourceLabel:      source,
		metricsClusterLabel:     cluster,
		metricsDataTypeLabel:    dataType,
		metricsSubResourceLabel: subresource,
		metricsActionLabel:      action,
	}
	cloudeventsReceivedCounterMetric.With(labels).Inc()
}

// increaseCloudEventsSentCounter increases the cloudevents sent counter metric:
func increaseCloudEventsSentCounter(source, originalSource, cluster, dataType, subresource, action string) {
	if originalSource == "" {
		originalSource = noneOriginalSource
	}
	labels := prometheus.Labels{
		metricsSourceLabel:         source,
		metricsOriginalSourceLabel: originalSource,
		metricsClusterLabel:        cluster,
		metricsDataTypeLabel:       dataType,
		metricsSubResourceLabel:    subresource,
		metricsActionLabel:         action,
	}
	cloudeventsSentCounterMetric.With(labels).Inc()
}

// updateResourceSpecResyncDurationMetric updates the resource spec resync duration metric:
func updateResourceSpecResyncDurationMetric(source, cluster, dataType string, startTime time.Time) {
	labels := prometheus.Labels{
		metricsSourceLabel:   source,
		metricsClusterLabel:  cluster,
		metricsDataTypeLabel: dataType,
	}
	duration := time.Since(startTime)
	resourceSpecResyncDurationMetric.With(labels).Observe(duration.Seconds())
}

// updateResourceStatusResyncDurationMetric updates the resource status resync duration metric:
func updateResourceStatusResyncDurationMetric(source, cluster, dataType string, startTime time.Time) {
	labels := prometheus.Labels{
		metricsSourceLabel:   source,
		metricsClusterLabel:  cluster,
		metricsDataTypeLabel: dataType,
	}
	duration := time.Since(startTime)
	resourceStatusResyncDurationMetric.With(labels).Observe(duration.Seconds())
}

// increaseClientReconnectedCounter increases the client reconnected counter metric:
func increaseClientReconnectedCounter(clientID string) {
	labels := prometheus.Labels{
		metricsClientIDLabel: clientID,
	}
	clientReconnectedCounterMetric.With(labels).Inc()
}

// IncreaseWorkProcessedCounter increases the work processed counter metric:
func IncreaseWorkProcessedCounter(action, code string) {
	labels := prometheus.Labels{
		metricsWorkActionLabel: action,
		metricsWorkCodeLabel:   code,
	}
	workProcessedCounterMetric.With(labels).Inc()
}
