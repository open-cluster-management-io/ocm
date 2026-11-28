package metrics

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8smetrics "k8s.io/component-base/metrics"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestManagedClusterCollector_EmptyLister(t *testing.T) {
	clusterClient := clusterfake.NewSimpleClientset()
	informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	clusterInformer := informerFactory.Cluster().V1().ManagedClusters()
	collector := NewManagedClusterCollector(clusterInformer.Lister())

	registry := k8smetrics.NewKubeRegistry()
	registry.CustomMustRegister(collector)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	fleetCountFound := 0
	for _, mf := range mfs {
		if mf.GetName() == "registration_managedcluster_status_condition" {
			if len(mf.GetMetric()) != 0 {
				t.Errorf("expected 0 status condition metrics, got %d", len(mf.GetMetric()))
			}
		}
		if mf.GetName() == "registration_managedcluster_fleet_count" {
			for _, m := range mf.GetMetric() {
				fleetCountFound++
				if m.GetGauge().GetValue() != 0 {
					t.Errorf("expected fleet count to be 0, got %v", m.GetGauge().GetValue())
				}
			}
		}
	}

	expectedFleetMetrics := len(MonitoredConditions) * len(MonitoredStatuses)
	if fleetCountFound != expectedFleetMetrics {
		t.Errorf("expected %d fleet count metrics, got %d", expectedFleetMetrics, fleetCountFound)
	}
}

func TestManagedClusterCollector_WithClusters(t *testing.T) {
	clusterClient := clusterfake.NewSimpleClientset()
	informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	clusterInformer := informerFactory.Cluster().V1().ManagedClusters()
	store := clusterInformer.Informer().GetStore()

	c1 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: []metav1.Condition{
				{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionTrue},
				{Type: clusterv1.ManagedClusterConditionJoined, Status: metav1.ConditionTrue},
				{Type: clusterv1.ManagedClusterConditionHubAccepted, Status: metav1.ConditionTrue},
			},
		},
	}
	c2 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: []metav1.Condition{
				{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionFalse},
				{Type: clusterv1.ManagedClusterConditionJoined, Status: metav1.ConditionTrue},
				{Type: clusterv1.ManagedClusterConditionHubAccepted, Status: metav1.ConditionTrue},
			},
		},
	}
	c3 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-3"},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: []metav1.Condition{
				{Type: clusterv1.ManagedClusterConditionAvailable, Status: metav1.ConditionUnknown},
				{Type: clusterv1.ManagedClusterConditionJoined, Status: metav1.ConditionFalse},
				{Type: clusterv1.ManagedClusterConditionHubAccepted, Status: metav1.ConditionTrue},
			},
		},
	}
	c4 := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-4"},
		// No conditions; all should fall back to Unknown
	}

	if err := store.Add(c1); err != nil {
		t.Fatalf("failed to add c1: %v", err)
	}
	if err := store.Add(c2); err != nil {
		t.Fatalf("failed to add c2: %v", err)
	}
	if err := store.Add(c3); err != nil {
		t.Fatalf("failed to add c3: %v", err)
	}
	if err := store.Add(c4); err != nil {
		t.Fatalf("failed to add c4: %v", err)
	}

	collector := NewManagedClusterCollector(clusterInformer.Lister())
	registry := k8smetrics.NewKubeRegistry()
	registry.CustomMustRegister(collector)

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Expected fleet counts:
	// Available: True=1, False=1, Unknown=2
	// Joined: True=2, False=1, Unknown=1
	// HubAccepted: True=3, False=0, Unknown=1
	expectedFleet := map[string]map[string]float64{
		clusterv1.ManagedClusterConditionAvailable: {
			"True":    1,
			"False":   1,
			"Unknown": 2,
		},
		clusterv1.ManagedClusterConditionJoined: {
			"True":    2,
			"False":   1,
			"Unknown": 1,
		},
		clusterv1.ManagedClusterConditionHubAccepted: {
			"True":    3,
			"False":   0,
			"Unknown": 1,
		},
	}

	for _, mf := range mfs {
		if mf.GetName() == "registration_managedcluster_fleet_count" {
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, l := range m.GetLabel() {
					labels[l.GetName()] = l.GetValue()
				}
				cond := labels["condition"]
				status := labels["status"]
				val := m.GetGauge().GetValue()
				exp, ok := expectedFleet[cond][status]
				if !ok {
					t.Errorf("unexpected fleet metric condition=%s status=%s", cond, status)
					continue
				}
				if val != exp {
					t.Errorf("fleet count condition=%s status=%s: expected %v, got %v", cond, status, exp, val)
				}
			}
		}

		if mf.GetName() == "registration_managedcluster_status_condition" {
			// Total per-cluster metrics = 4 clusters * 3 conditions * 3 statuses = 36
			if len(mf.GetMetric()) != 36 {
				t.Errorf("expected 36 condition metrics, got %d", len(mf.GetMetric()))
			}

			// Validate c1 Available True == 1, False == 0, Unknown == 0
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, l := range m.GetLabel() {
					labels[l.GetName()] = l.GetValue()
				}
				if labels["name"] == "cluster-1" && labels["condition"] == clusterv1.ManagedClusterConditionAvailable {
					if labels["status"] == "True" && m.GetGauge().GetValue() != 1.0 {
						t.Errorf("expected cluster-1 Available True=1, got %v", m.GetGauge().GetValue())
					}
					if (labels["status"] == "False" || labels["status"] == "Unknown") && m.GetGauge().GetValue() != 0.0 {
						t.Errorf("expected cluster-1 Available %s=0, got %v", labels["status"], m.GetGauge().GetValue())
					}
				}
			}
		}
	}
}

func TestRegisterClusterMetrics(t *testing.T) {
	clusterClient := clusterfake.NewSimpleClientset()
	informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	clusterInformer := informerFactory.Cluster().V1().ManagedClusters()

	// Should not panic when called multiple times
	RegisterClusterMetrics(clusterInformer.Lister())
	RegisterClusterMetrics(clusterInformer.Lister())
}
