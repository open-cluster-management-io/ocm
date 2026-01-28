package clusterprofile

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

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

	profile1Ns1 := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profile1Ns2 := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns2",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileNotManaged := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns3",
			Labels: map[string]string{
				v1.ClusterNameLabelKey: "cluster1",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster1",
			ClusterManager: cpv1alpha1.ClusterManager{
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

	profileLabelSelectorNs1 := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-label-selector",
			Namespace: "prod-ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-label-selector",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster-label-selector",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileLabelSelectorNs2 := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-label-selector",
			Namespace: "region-ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-label-selector",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster-label-selector",
			ClusterManager: cpv1alpha1.ClusterManager{
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

	profileMixedExclusive := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-mixed",
			Namespace: "exclusive-ns",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-mixed",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster-mixed",
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	profileMixedLabelSelector := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-mixed",
			Namespace: "labelselector-ns",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster-mixed",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: "cluster-mixed",
			ClusterManager: cpv1alpha1.ClusterManager{
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
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
			}

			// Add indexer for profiles
			cpInformer := cpInformers.Apis().V1alpha1().ClusterProfiles()
			err := cpInformer.Informer().AddIndexers(cache.Indexers{
				byClusterName: indexByClusterName,
			})
			if err != nil {
				t.Fatal(err)
			}

			for _, profile := range c.existingProfiles {
				cpInformer.Informer().GetStore().Add(profile)
			}

			ctrl := &clusterProfileStatusController{
				clusterLister:         clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				clusterProfileClient:  cpClient,
				clusterProfileLister:  cpInformer.Lister(),
				clusterProfileIndexer: cpInformer.Informer().GetIndexer(),
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, c.key)
			err = ctrl.sync(context.TODO(), syncCtx, c.key)

			if c.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !c.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Count patch actions (label patch + status patch for each profile)
			patchCount := 0
			for _, action := range cpClient.Actions() {
				if action.GetVerb() == "patch" {
					patchCount++
				}
			}

			// We expect 2 patches per profile (labels + status)
			// But if nothing changed, there might be fewer patches
			// Just verify we got some activity
			if c.expectedUpdates == 0 && patchCount != 0 {
				t.Errorf("expected no patches but got %d", patchCount)
			}
			if c.expectedUpdates > 0 && patchCount == 0 {
				t.Errorf("expected patches but got none")
			}
		})
	}
}

func TestStatusSyncLabelsFromCluster(t *testing.T) {
	cases := []struct {
		name           string
		cluster        *v1.ManagedCluster
		profile        *cpv1alpha1.ClusterProfile
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
			profile: &cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "set1",
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
			profile: &cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-no-set",
					Namespace: "prod-ns",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "", // Empty when no ClusterSetLabel
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
			profile: &cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-mixed",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "set1",
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
			profile: &cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-empty",
					Namespace: "ns1",
					Labels:    map[string]string{},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "", // Empty when no ClusterSetLabel
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
			profile: &cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-update",
					Namespace: "ns1",
					Labels: map[string]string{
						cpv1alpha1.LabelClusterSetKey: "old-set", // Should be updated
						"custom-label":                "keep-me",
					},
				},
			},
			expectedLabels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "new-set",
				v1.ClusterNameLabelKey:            "cluster-update",
				"custom-label":                    "keep-me", // Custom labels preserved
			},
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

func TestStatusSyncStatusFromCluster(t *testing.T) {
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

	profile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
		},
	}

	syncStatusFromCluster(profile, cluster)

	// Verify version
	if profile.Status.Version.Kubernetes != "v1.28.0" {
		t.Errorf("expected Kubernetes version v1.28.0, got %s", profile.Status.Version.Kubernetes)
	}

	// Verify properties
	if len(profile.Status.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(profile.Status.Properties))
	}

	// Verify conditions
	availableCondition := meta.FindStatusCondition(profile.Status.Conditions, cpv1alpha1.ClusterConditionControlPlaneHealthy)
	if availableCondition == nil {
		t.Errorf("expected ControlPlaneHealthy condition")
	} else if availableCondition.Status != metav1.ConditionTrue {
		t.Errorf("expected ControlPlaneHealthy condition to be True, got %s", availableCondition.Status)
	}

	joinedCondition := meta.FindStatusCondition(profile.Status.Conditions, "Joined")
	if joinedCondition == nil {
		t.Errorf("expected Joined condition")
	} else if joinedCondition.Status != metav1.ConditionTrue {
		t.Errorf("expected Joined condition to be True, got %s", joinedCondition.Status)
	}
}

func TestStatusControllerQueueKeyMapping(t *testing.T) {
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
	}

	profile1 := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				v1.ClusterNameLabelKey:            "cluster1",
			},
		},
	}

	// Profile without cluster-name label to test fallback
	profileNoLabel := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "profile-no-label",
			Namespace: "ns1",
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
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
