package metrics

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"

	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	RegistrationSubsystem = "registration"

	ManagedClusterStatusConditionKey = "managedcluster_status_condition"
	ManagedClusterFleetCountKey      = "managedcluster_fleet_count"
)

var (
	// MonitoredConditions are the core lifecycle and availability conditions tracked for managed clusters.
	MonitoredConditions = []string{
		clusterv1.ManagedClusterConditionAvailable,
		clusterv1.ManagedClusterConditionJoined,
		clusterv1.ManagedClusterConditionHubAccepted,
	}

	// MonitoredStatuses are the standard condition statuses.
	MonitoredStatuses = []string{
		string(metav1.ConditionTrue),
		string(metav1.ConditionFalse),
		string(metav1.ConditionUnknown),
	}

	ClusterStatusConditionDesc = k8smetrics.NewDesc(
		k8smetrics.BuildFQName("", RegistrationSubsystem, ManagedClusterStatusConditionKey),
		"Current status condition of managed clusters (1 if active, 0 otherwise).",
		[]string{"name", "condition", "status"},
		nil,
		k8smetrics.ALPHA,
		"",
	)

	ClusterFleetCountDesc = k8smetrics.NewDesc(
		k8smetrics.BuildFQName("", RegistrationSubsystem, ManagedClusterFleetCountKey),
		"Total number of managed clusters in the fleet partitioned by condition and status.",
		[]string{"condition", "status"},
		nil,
		k8smetrics.ALPHA,
		"",
	)
)

// ManagedClusterCollector collects Prometheus metrics about managed cluster conditions and fleet availability.
type ManagedClusterCollector struct {
	k8smetrics.BaseStableCollector
	clusterLister listerv1.ManagedClusterLister
}

var _ k8smetrics.StableCollector = &ManagedClusterCollector{}

// NewManagedClusterCollector creates a new ManagedClusterCollector backed by the given cluster lister.
func NewManagedClusterCollector(clusterLister listerv1.ManagedClusterLister) *ManagedClusterCollector {
	return &ManagedClusterCollector{
		clusterLister: clusterLister,
	}
}

// DescribeWithStability sends all metric descriptors to the provided channel.
func (c *ManagedClusterCollector) DescribeWithStability(ch chan<- *k8smetrics.Desc) {
	ch <- ClusterStatusConditionDesc
	ch <- ClusterFleetCountDesc
}

// CollectWithStability collects metrics from the informer cache and sends them to the channel.
func (c *ManagedClusterCollector) CollectWithStability(ch chan<- k8smetrics.Metric) {
	// Initialize fleet counts for all monitored conditions and statuses to 0
	fleetCounts := make(map[string]map[string]int)
	for _, condType := range MonitoredConditions {
		fleetCounts[condType] = make(map[string]int)
		for _, status := range MonitoredStatuses {
			fleetCounts[condType][status] = 0
		}
	}

	if c.clusterLister != nil {
		clusters, err := c.clusterLister.List(labels.Everything())
		if err == nil {
			for _, cluster := range clusters {
				for _, condType := range MonitoredConditions {
					activeStatus := string(metav1.ConditionUnknown)
					if cond := meta.FindStatusCondition(cluster.Status.Conditions, condType); cond != nil {
						activeStatus = string(cond.Status)
					}
					// Increment fleet count
					fleetCounts[condType][activeStatus]++

					// Emit per-cluster condition status (1 for active, 0 for inactive)
					for _, s := range MonitoredStatuses {
						val := 0.0
						if s == activeStatus {
							val = 1.0
						}
						ch <- k8smetrics.NewLazyConstMetric(
							ClusterStatusConditionDesc,
							k8smetrics.GaugeValue,
							val,
							cluster.Name,
							condType,
							s,
						)
					}
				}
			}
		}
	}

	// Emit fleet counts for all monitored conditions and statuses
	for _, condType := range MonitoredConditions {
		for _, status := range MonitoredStatuses {
			ch <- k8smetrics.NewLazyConstMetric(
				ClusterFleetCountDesc,
				k8smetrics.GaugeValue,
				float64(fleetCounts[condType][status]),
				condType,
				status,
			)
		}
	}
}

var registerMetricsOnce sync.Once

// RegisterClusterMetrics registers the managed cluster metrics collector with legacyregistry.
func RegisterClusterMetrics(clusterLister listerv1.ManagedClusterLister) {
	registerMetricsOnce.Do(func() {
		collector := NewManagedClusterCollector(clusterLister)
		legacyregistry.CustomMustRegister(collector)
	})
}
