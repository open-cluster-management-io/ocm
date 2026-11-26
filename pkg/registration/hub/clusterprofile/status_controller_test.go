package clusterprofile

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestStatusControllerSync(t *testing.T) {
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.28.0",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "platform", Value: "AWS"},
				{Name: "region", Value: "us-west-2"},
			},
			Conditions: []metav1.Condition{
				{
					Type:    v1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterAvailable",
					Message: "Cluster is available",
				},
				{
					Type:    v1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "ManagedClusterJoined",
					Message: "Cluster has joined",
				},
			},
		},
	}

	profile1Ns1 := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profile1Ns2 := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns2",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileNotManaged := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns3",
			Labels: map[string]string{
				v1.ClusterNameLabelKey: "cluster1",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: "other-manager",
			},
		},
	}

	clusterWithLabelSelector := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-label-selector",
			Labels: map[string]string{
				"environment": "production",
				"region":      "us-west",
			},
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.29.0",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "platform", Value: "GCP"},
			},
			Conditions: []metav1.Condition{
				{
					Type:    v1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "Available",
					Message: "Cluster is available",
				},
			},
		},
	}

	profileLabelSelectorNs1 := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-label-selector",
			Namespace: "prod-ns1",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-label-selector",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster-label-selector",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileLabelSelectorNs2 := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-label-selector",
			Namespace: "region-ns1",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-label-selector",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster-label-selector",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	clusterMixed := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-mixed",
			Labels: map[string]string{
				v1beta2.ClusterSetLabel: "set1",
				"environment":           "production",
			},
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.30.0",
			},
			Conditions: []metav1.Condition{
				{
					Type:    v1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "Available",
					Message: "Cluster available",
				},
			},
		},
	}

	profileMixedExclusive := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-mixed",
			Namespace: "exclusive-ns",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-mixed",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster-mixed",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileMixedLabelSelector := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-mixed",
			Namespace: "labelselector-ns",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-mixed",
			},
		},
		Spec: cpv1alpha2.ClusterProfileSpec{
			DisplayName: "cluster-mixed",
			ClusterManager: cpv1alpha2.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	cases := []struct {
		name             string
		key              string
		clusters         []runtime.Object
		existingProfiles []runtime.Object
		expectedUpdates  int
		expectError      bool
	}{
		{
			name:             "update status for profiles in multiple namespaces",
			key:              "cluster1",
			clusters:         []runtime.Object{cluster1},
			existingProfiles: []runtime.Object{profile1Ns1, profile1Ns2},
			expectedUpdates:  2, // Should update both profiles
			expectError:      false,
		},
		{
			name:             "skip profiles not managed by OCM",
			key:              "cluster1",
			clusters:         []runtime.Object{cluster1},
			existingProfiles: []runtime.Object{profile1Ns1, profileNotManaged},
			expectedUpdates:  1, // Only update OCM-managed profile
			expectError:      false,
		},
		{
			name:             "no profiles exist",
			key:              "cluster1",
			clusters:         []runtime.Object{cluster1},
			existingProfiles: []runtime.Object{},
			expectedUpdates:  0,
			expectError:      false,
		},
		{
			name:             "cluster not found",
			key:              "nonexistent-cluster",
			clusters:         []runtime.Object{},
			existingProfiles: []runtime.Object{profile1Ns1},
			expectedUpdates:  0,
			expectError:      false,
		},
		{
			name:             "cluster selected by LabelSelector - multiple profiles",
			key:              "cluster-label-selector",
			clusters:         []runtime.Object{clusterWithLabelSelector},
			existingProfiles: []runtime.Object{profileLabelSelectorNs1, profileLabelSelectorNs2},
			expectedUpdates:  2, // Update both profiles created by different LabelSelector sets
			expectError:      false,
		},
		{
			name:             "cluster selected by both ExclusiveClusterSetLabel and LabelSelector",
			key:              "cluster-mixed",
			clusters:         []runtime.Object{clusterMixed},
			existingProfiles: []runtime.Object{profileMixedExclusive, profileMixedLabelSelector},
			expectedUpdates:  2, // Update profiles in both namespaces
			expectError:      false,
		},
		{
			name:             "cluster selected by LabelSelector - single profile",
			key:              "cluster-label-selector",
			clusters:         []runtime.Object{clusterWithLabelSelector},
			existingProfiles: []runtime.Object{profileLabelSelectorNs1},
			expectedUpdates:  1,
			expectError:      false,
		},
		{
			name:             "mixed managed and unmanaged profiles for LabelSelector cluster",
			key:              "cluster-label-selector",
			clusters:         []runtime.Object{clusterWithLabelSelector},
			existingProfiles: []runtime.Object{profileLabelSelectorNs1, profileNotManaged},
			expectedUpdates:  1, // Only update OCM-managed profile
			expectError:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)

			cpClient := cpfake.NewSimpleClientset(c.existingProfiles...)
			cpInformers := cpinformers.NewSharedInformerFactory(cpClient, 0)

			// Populate informers
			for _, cluster := range c.clusters {
				if err := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			// Add indexer for profiles
			cpInformer := cpInformers.Apis().V1alpha2().ClusterProfiles()
			err := cpInformer.Informer().AddIndexers(cache.Indexers{
				byClusterName: indexByClusterName,
			})
			if err != nil {
				t.Fatal(err)
			}

			for _, profile := range c.existingProfiles {
				if err := cpInformer.Informer().GetStore().Add(profile); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &clusterProfileStatusController{
				clusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				clusterProfileClient:  cpClient,
				clusterProfileLister:  cpInformer.Lister(),
				clusterProfileIndexer: cpInformer.Informer().GetIndexer(),
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, c.key)
			err = ctrl.sync(t.Context(), syncCtx, c.key)

			if c.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !c.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Count patch actions
			patchCount := 0
			for _, action := range cpClient.Actions() {
				if action.GetVerb() == "patch" {
					patchCount++
				}
			}

			// Each sync patches a profile at most once: status first, then labels
			// once status is current.
			if patchCount != c.expectedUpdates {
				t.Errorf("expected %d patches but got %d", c.expectedUpdates, patchCount)
			}
		})
	}
}

func TestStatusSyncLabelsFromCluster(t *testing.T) {
	cases := []struct {
		name           string
		cluster        *v1.ManagedCluster
		profile        *cpv1alpha2.ClusterProfile
		expectedLabels map[string]string
	}{
		{
			name: "cluster with ExclusiveClusterSetLabel",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha2.LabelClusterSetKey:     "set1",
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
		{
			name: "cluster without clusterset label (LabelSelector scenario)",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-no-set",
					Labels: map[string]string{
						"environment": "production",
						"region":      "us-west",
					},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-no-set",
					Namespace: "prod-ns",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha2.LabelClusterSetKey:     "", // Empty when no ClusterSetLabel
				v1.ClusterNameLabelKey:            "cluster-no-set",
			},
		},
		{
			name: "cluster with both clusterset and custom labels",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-mixed",
					Labels: map[string]string{
						v1beta2.ClusterSetLabel: "set1",
						"environment":           "production",
						"tier":                  "critical",
					},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-mixed",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha2.LabelClusterSetKey:     "set1",
				v1.ClusterNameLabelKey:            "cluster-mixed",
			},
		},
		{
			name: "cluster with empty labels",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-empty",
					Labels: map[string]string{},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-empty",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha2.LabelClusterSetKey:     "", // Empty when no ClusterSetLabel
				v1.ClusterNameLabelKey:            "cluster-empty",
			},
		},
		{
			name: "update existing profile labels",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-update",
					Labels: map[string]string{v1beta2.ClusterSetLabel: "new-set"},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-update",
					Namespace: "ns1",
					Labels: map[string]string{
						cpv1alpha2.LabelClusterSetKey: "old-set", // Should be updated
						"custom-label":                "keep-me",
					},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha2.LabelClusterSetKey:     "new-set",
				v1.ClusterNameLabelKey:            "cluster-update",
				"custom-label":                    "keep-me", // Custom labels preserved
			},
		},
		{
			name: "cluster with coordinated inventory member ID",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-member-id",
					Labels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "Prod_JP.cluster-01"},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-member-id", Namespace: "ns1", Labels: map[string]string{}},
			},
			expectedLabels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "Prod_JP.cluster-01"},
		},
		{
			name: "cluster with empty inventory member ID falls back to cluster name",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster-empty-id",
					Labels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: ""},
				},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-empty-id", Namespace: "ns1", Labels: map[string]string{}},
			},
			expectedLabels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "cluster-empty-id"},
		},
		{
			name: "cluster without inventory member ID falls back to cluster name",
			cluster: &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-no-id"},
			},
			profile: &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-no-id",
					Namespace: "ns1",
					Labels:    map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "stale-id"},
				},
			},
			expectedLabels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "cluster-no-id"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syncLabelsFromCluster(c.profile, c.cluster)

			// Verify expected labels
			for key, expectedValue := range c.expectedLabels {
				actualValue, exists := c.profile.Labels[key]
				if !exists {
					t.Errorf("expected label %s to exist", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("label %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestStatusControllerSyncInventoryMemberID(t *testing.T) {
	cluster := &v1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}}
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterfake.NewSimpleClientset(), 0)
	clusterStore := clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore()
	namespaces := []string{"ns1", "ns2"}
	profiles := []runtime.Object{}
	for _, namespace := range namespaces {
		profiles = append(profiles, &cpv1alpha2.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1", Namespace: namespace,
				Labels: map[string]string{
					cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
					v1.ClusterNameLabelKey:            "cluster1",
					"custom-label":                    "keep-me",
				},
				Annotations: map[string]string{"custom-annotation": "keep-me"},
			},
			Spec: cpv1alpha2.ClusterProfileSpec{ClusterManager: cpv1alpha2.ClusterManager{Name: ClusterProfileManagerName}},
		})
	}
	cpClient := cpfake.NewSimpleClientset(profiles...)
	cpInformers := cpinformers.NewSharedInformerFactory(cpClient, 0)
	cpInformer := cpInformers.Apis().V1alpha2().ClusterProfiles()
	if err := cpInformer.Informer().AddIndexers(cache.Indexers{byClusterName: indexByClusterName}); err != nil {
		t.Fatal(err)
	}
	for _, profile := range profiles {
		if err := cpInformer.Informer().GetStore().Add(profile); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := &clusterProfileStatusController{
		clusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
		clusterProfileClient:  cpClient,
		clusterProfileIndexer: cpInformer.Informer().GetIndexer(),
	}

	// Each case builds on the state left by the previous one.
	for _, tc := range []struct {
		name       string
		labels     map[string]string
		expectedID string
	}{
		{name: "backfill existing profiles", expectedID: "cluster1"},
		{name: "change coordinated ID", labels: map[string]string{cpv1alpha2.LabelInventoryMemberIDKey: "Prod_JP.cluster-02"}, expectedID: "Prod_JP.cluster-02"},
		{name: "remove coordinated ID", expectedID: "cluster1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster = cluster.DeepCopy()
			cluster.Labels = tc.labels
			if err := clusterStore.Update(cluster); err != nil {
				t.Fatal(err)
			}
			if err := ctrl.sync(t.Context(), testingcommon.NewFakeSyncContext(t, cluster.Name), cluster.Name); err != nil {
				t.Fatal(err)
			}
			for _, namespace := range namespaces {
				profile, err := cpClient.ApisV1alpha2().ClusterProfiles(namespace).Get(t.Context(), cluster.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey] != tc.expectedID {
					t.Errorf("profile %s/%s: expected member ID %q, got %q", namespace, cluster.Name, tc.expectedID, profile.Labels[cpv1alpha2.LabelInventoryMemberIDKey])
				}
				if profile.Labels["custom-label"] != "keep-me" || profile.Annotations["custom-annotation"] != "keep-me" {
					t.Error("overwrote custom metadata")
				}
				if err := cpInformer.Informer().GetStore().Update(profile); err != nil {
					t.Fatal(err)
				}
			}
			cpClient.ClearActions()
			if err := ctrl.sync(t.Context(), testingcommon.NewFakeSyncContext(t, cluster.Name), cluster.Name); err != nil {
				t.Fatal(err)
			}
			if actions := cpClient.Actions(); len(actions) != 0 {
				t.Errorf("unchanged profiles should not be patched, got %v", actions)
			}
		})
	}
}

func TestStatusFromCluster(t *testing.T) {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.28.0",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "platform", Value: "AWS"},
				{Name: "region", Value: "us-west-2"},
			},
			Conditions: []metav1.Condition{
				{
					Type:    v1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "Available",
					Message: "Cluster is available",
				},
				{
					Type:    v1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "Joined",
					Message: "Cluster joined",
				},
			},
		},
	}

	profile := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
		},
	}

	status := statusFromCluster(profile, cluster, &fieldpath.Set{})

	// Verify version
	if status.Version == nil || status.Version.Kubernetes != "v1.28.0" {
		t.Errorf("expected Kubernetes version v1.28.0, got %v", status.Version)
	}

	// Verify properties
	if len(status.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(status.Properties))
	}

	// Verify conditions
	availableCondition := meta.FindStatusCondition(status.Conditions, cpv1alpha2.ClusterConditionControlPlaneHealthy)
	if availableCondition == nil {
		t.Errorf("expected ControlPlaneHealthy condition")
	} else if availableCondition.Status != metav1.ConditionTrue {
		t.Errorf("expected ControlPlaneHealthy condition to be True, got %s", availableCondition.Status)
	}

	joinedCondition := meta.FindStatusCondition(status.Conditions, "Joined")
	if joinedCondition == nil {
		t.Errorf("expected Joined condition")
	} else if joinedCondition.Status != metav1.ConditionTrue {
		t.Errorf("expected Joined condition to be True, got %s", joinedCondition.Status)
	}
}

func TestStatusKeepsPreviouslyReportedConditionWhenClusterConditionIsMissing(t *testing.T) {
	lastTransition := metav1.Now()
	health := metav1.Condition{
		Type: cpv1alpha2.ClusterConditionControlPlaneHealthy, Status: metav1.ConditionTrue,
		Reason: "Available", LastTransitionTime: lastTransition,
	}
	profile := &cpv1alpha2.ClusterProfile{
		Status: cpv1alpha2.ClusterProfileStatus{Conditions: []metav1.Condition{
			health, {Type: "Joined", Status: metav1.ConditionTrue, Reason: "External", LastTransitionTime: lastTransition},
		}},
	}
	ownedFields := fieldpath.NewSet(conditionPath(health.Type))
	status := statusFromCluster(profile, &v1.ManagedCluster{}, ownedFields)
	if len(status.Conditions) != 1 || status.Conditions[0] != health {
		t.Fatalf("expected only the previously reported health condition, got %v", status.Conditions)
	}
}

func TestStatusControllerApplyStatus(t *testing.T) {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
		Status: v1.ManagedClusterStatus{
			Version:       v1.ManagedClusterVersion{Kubernetes: "v1.28.0"},
			ClusterClaims: []v1.ManagedClusterClaim{{Name: "platform", Value: "AWS"}},
			Conditions: []metav1.Condition{
				{Type: v1.ManagedClusterConditionAvailable, Status: metav1.ConditionTrue, Reason: "Available"},
			},
		},
	}
	clusterWithoutClaims := cluster.DeepCopy()
	clusterWithoutClaims.Status.ClusterClaims = nil
	clusterWithoutConditions := cluster.DeepCopy()
	clusterWithoutConditions.Status.Conditions = nil
	// statusFields returns the applied status with the given fields of the platform property.
	statusFields := func(platform string) *metav1.FieldsV1 {
		return metav1.NewFieldsV1(`{"f:status":{` +
			`"f:conditions":{"k:{\"type\":\"ControlPlaneHealthy\"}":{".":{},"f:lastTransitionTime":{},"f:message":{},"f:reason":{},"f:status":{},"f:type":{}}},` +
			`"f:properties":{"k:{\"name\":\"platform\"}":{` + platform + `}},` +
			`"f:version":{"f:kubernetes":{}}}}`)
	}
	appliedFields := statusFields(`".":{},"f:name":{},"f:value":{}`)
	entry := func(manager string, operation metav1.ManagedFieldsOperationType, fields *metav1.FieldsV1) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager: manager, Operation: operation, Subresource: "status",
			APIVersion: cpv1alpha2.GroupVersion.String(), FieldsType: "FieldsV1", FieldsV1: fields,
		}
	}
	managedFields := func(manager string, operation metav1.ManagedFieldsOperationType) []metav1.ManagedFieldsEntry {
		return []metav1.ManagedFieldsEntry{entry(manager, operation, appliedFields)}
	}
	platformFields := func(fields string) *metav1.FieldsV1 {
		return metav1.NewFieldsV1(`{"f:status":{"f:properties":{"k:{\"name\":\"platform\"}":{` + fields + `}}}}`)
	}
	// sharedWith adds a manager that also owns the given fields of the platform property.
	sharedWith := func(fields string) []metav1.ManagedFieldsEntry {
		return append(managedFields(statusFieldManager, metav1.ManagedFieldsOperationApply),
			entry("observer", metav1.ManagedFieldsOperationApply, platformFields(`".":{},"f:name":{},`+fields)))
	}
	expectRemoval := func(t *testing.T, actions []clienttesting.Action, profile *cpv1alpha2.ClusterProfile) {
		testingcommon.AssertActions(t, actions, "patch")
		patch := actions[0].(clienttesting.PatchActionImpl)
		if patch.GetPatchType() != types.JSONPatchType || patch.GetSubresource() != "status" {
			t.Errorf("expected a JSON patch on status, got %s on %q", patch.GetPatchType(), patch.GetSubresource())
		}
		var ops []map[string]any
		if err := json.Unmarshal(patch.GetPatch(), &ops); err != nil {
			t.Fatal(err)
		}
		if len(ops) == 0 || ops[0]["op"] != "replace" || ops[0]["path"] != "/metadata/resourceVersion" || ops[0]["value"] != "1" {
			t.Errorf("expected the removal to be conditional on resourceVersion, got %v", ops)
		}
		if len(profile.Status.Properties) != 0 {
			t.Errorf("expected the property to be removed, got %v", profile.Status.Properties)
		}
	}
	expectNoProperties := func(t *testing.T, actions []clienttesting.Action, _ *cpv1alpha2.ClusterProfile) {
		if status := assertStatusApply(t, actions); status["properties"] != nil {
			t.Errorf("expected no properties, got %v", status["properties"])
		}
	}
	expectTakeOver := func(t *testing.T, actions []clienttesting.Action, profile *cpv1alpha2.ClusterProfile) {
		testingcommon.AssertActions(t, actions, "patch")
		patch := actions[0].(clienttesting.PatchActionImpl)
		if patch.GetPatchType() != types.JSONPatchType || patch.GetSubresource() != "" {
			t.Errorf("expected a JSON patch on the main resource, got %s on %q", patch.GetPatchType(), patch.GetSubresource())
		}
		if len(profile.ManagedFields) != 1 || profile.ManagedFields[0].Manager != statusFieldManager ||
			profile.ManagedFields[0].Operation != metav1.ManagedFieldsOperationApply {
			t.Errorf("expected the fields to move to %s, got %v", statusFieldManager, profile.ManagedFields)
		}
		owned, _, err := statusFieldOwners(profile)
		if err != nil {
			t.Fatal(err)
		}
		if !owned.Has(fieldpath.MakePathOrDie("status", "version", "kubernetes")) || !owned.Has(valuePath("platform")) {
			t.Errorf("expected %s to own the version and the property value, got %v", statusFieldManager, owned)
		}
	}

	cases := []struct {
		name          string
		cluster       *v1.ManagedCluster
		managedFields []metav1.ManagedFieldsEntry
		validate      func(t *testing.T, actions []clienttesting.Action, profile *cpv1alpha2.ClusterProfile)
	}{
		{
			name:    "apply only the fields derived from the cluster",
			cluster: cluster,
			validate: func(t *testing.T, actions []clienttesting.Action, _ *cpv1alpha2.ClusterProfile) {
				status := assertStatusApply(t, actions)
				properties := status["properties"].([]any)
				if len(properties) != 1 || len(properties[0].(map[string]any)) != 2 {
					t.Errorf("expected only the claim's name and value, got %v", properties)
				}
			},
		},
		{
			name:          "skip when the applied status is current",
			cluster:       cluster,
			managedFields: managedFields(statusFieldManager, metav1.ManagedFieldsOperationApply),
			validate: func(t *testing.T, actions []clienttesting.Action, _ *cpv1alpha2.ClusterProfile) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:          "prune the property of a removed claim",
			cluster:       clusterWithoutClaims,
			managedFields: managedFields(statusFieldManager, metav1.ManagedFieldsOperationApply),
			validate:      expectNoProperties,
		},
		{
			name:          "remove the entry of a removed claim that another manager shares without its value",
			cluster:       clusterWithoutClaims,
			managedFields: sharedWith(`"f:lastObservedTime":{}`),
			validate:      expectRemoval,
		},
		{
			// Earlier releases could take over only the value of an entry another writer created.
			name:    "remove the entry of a removed claim whose value alone we own",
			cluster: clusterWithoutClaims,
			managedFields: []metav1.ManagedFieldsEntry{
				entry(statusFieldManager, metav1.ManagedFieldsOperationApply, platformFields(`"f:value":{}`)),
				entry("observer", metav1.ManagedFieldsOperationApply, platformFields(`".":{},"f:name":{}`)),
			},
			validate: expectRemoval,
		},
		{
			name:          "leave a removed claim's property to another manager that owns its value",
			cluster:       clusterWithoutClaims,
			managedFields: sharedWith(`"f:value":{}`),
			validate:      expectNoProperties,
		},
		{
			name:    "release the value of a removed claim that another manager also owns",
			cluster: clusterWithoutClaims,
			managedFields: []metav1.ManagedFieldsEntry{
				entry(statusFieldManager, metav1.ManagedFieldsOperationApply, statusFields(`"f:value":{}`)),
				entry("observer", metav1.ManagedFieldsOperationApply, platformFields(`".":{},"f:name":{},"f:value":{}`)),
			},
			validate: expectNoProperties,
		},
		{
			// Pruning the reason we took over from earlier releases would leave the condition without it.
			name:    "keep reapplying a condition whose reason alone we own after the cluster stops reporting it",
			cluster: clusterWithoutConditions,
			managedFields: []metav1.ManagedFieldsEntry{
				entry(statusFieldManager, metav1.ManagedFieldsOperationApply, metav1.NewFieldsV1(
					`{"f:status":{"f:conditions":{"k:{\"type\":\"ControlPlaneHealthy\"}":{"f:reason":{}}}}}`)),
			},
			validate: func(t *testing.T, actions []clienttesting.Action, _ *cpv1alpha2.ClusterProfile) {
				conditions, _ := assertStatusApply(t, actions)["conditions"].([]any)
				if len(conditions) != 1 || conditions[0].(map[string]any)["reason"] != "Available" {
					t.Errorf("expected the live ControlPlaneHealthy condition, got %v", conditions)
				}
			},
		},
		{
			name:          "take over status written by merge patches",
			cluster:       cluster,
			managedFields: managedFields("registration", metav1.ManagedFieldsOperationUpdate),
			validate:      expectTakeOver,
		},
		{
			name:    "take over merge-patched fields next to applied ones",
			cluster: cluster,
			managedFields: []metav1.ManagedFieldsEntry{
				entry(statusFieldManager, metav1.ManagedFieldsOperationApply, metav1.NewFieldsV1(`{"f:status":{"f:version":{"f:kubernetes":{}}}}`)),
				entry("registration", metav1.ManagedFieldsOperationUpdate, platformFields(`".":{},"f:name":{},"f:value":{}`)),
			},
			validate: expectTakeOver,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			profile := &cpv1alpha2.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1", Namespace: "ns1", UID: "uid1", ResourceVersion: "1", ManagedFields: c.managedFields,
				},
				Status: cpv1alpha2.ClusterProfileStatus{
					Version:    cpv1alpha2.ClusterVersion{Kubernetes: "v1.28.0"},
					Properties: []cpv1alpha2.Property{{Name: "platform", Value: "AWS"}},
					Conditions: []metav1.Condition{{
						Type: cpv1alpha2.ClusterConditionControlPlaneHealthy, Status: metav1.ConditionTrue,
						Reason: "Available", LastTransitionTime: metav1.Now(),
					}},
				},
			}
			syncLabelsFromCluster(profile, c.cluster)
			cpClient := cpfake.NewSimpleClientset(profile)
			ctrl := &clusterProfileStatusController{clusterProfileClient: cpClient, legacyStatusManager: "registration"}
			cached := profile.DeepCopy()
			if _, err := ctrl.updateClusterProfile(t.Context(), profile, c.cluster); err != nil {
				t.Fatal(err)
			}
			if !equality.Semantic.DeepEqual(cached, profile) {
				t.Errorf("expected the cached profile to stay unchanged, got %v", profile.ManagedFields)
			}
			actions := cpClient.Actions()
			actual, err := cpClient.ApisV1alpha2().ClusterProfiles("ns1").Get(t.Context(), "cluster1", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			c.validate(t, actions, actual)
		})
	}
}

// assertStatusApply checks the request is a forced, conditional apply and returns its status.
func assertStatusApply(t *testing.T, actions []clienttesting.Action) map[string]any {
	t.Helper()
	testingcommon.AssertActions(t, actions, "patch")
	patch := actions[0].(clienttesting.PatchActionImpl)
	if patch.GetPatchType() != types.ApplyPatchType || patch.GetSubresource() != "status" ||
		patch.PatchOptions.FieldManager != statusFieldManager || !ptr.Deref(patch.PatchOptions.Force, false) {
		t.Fatalf("expected a forced status apply by %s, got %s on %q with %+v",
			statusFieldManager, patch.GetPatchType(), patch.GetSubresource(), patch.PatchOptions)
	}
	applied := map[string]any{}
	if err := json.Unmarshal(patch.GetPatch(), &applied); err != nil {
		t.Fatal(err)
	}
	metadata := applied["metadata"].(map[string]any)
	if metadata["uid"] != "uid1" || metadata["resourceVersion"] != "1" {
		t.Errorf("expected the apply to be conditional on uid and resourceVersion, got %v", metadata)
	}
	return applied["status"].(map[string]any)
}

func TestStatusControllerQueueKeyMapping(t *testing.T) {
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
	}

	profile1 := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
	}

	// Profile without cluster-name label to test fallback
	profileNoLabel := &cpv1alpha2.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "profile-no-label",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha2.LabelClusterManagerKey: ClusterProfileManagerName,
				// v1.ClusterNameLabelKey is intentionally missing to test fallback
			},
		},
	}

	t.Run("clusterToQueueKey", func(t *testing.T) {
		ctrl := &clusterProfileStatusController{}
		keys := ctrl.clusterToQueueKey(cluster1)
		if len(keys) != 1 || keys[0] != "cluster1" {
			t.Errorf("expected [cluster1], got %v", keys)
		}
	})

	t.Run("profileToQueueKey with cluster-name label", func(t *testing.T) {
		ctrl := &clusterProfileStatusController{}
		keys := ctrl.profileToQueueKey(profile1)
		if len(keys) != 1 || keys[0] != "cluster1" {
			t.Errorf("expected [cluster1], got %v", keys)
		}
	})

	t.Run("profileToQueueKey without cluster-name label (fallback to name)", func(t *testing.T) {
		ctrl := &clusterProfileStatusController{}
		keys := ctrl.profileToQueueKey(profileNoLabel)
		if len(keys) != 1 || keys[0] != "profile-no-label" {
			t.Errorf("expected [profile-no-label] (fallback to name), got %v", keys)
		}
	})
}
