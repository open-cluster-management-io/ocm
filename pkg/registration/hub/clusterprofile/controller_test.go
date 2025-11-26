package clusterprofile

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncClusterProfile(t *testing.T) {
	managedCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testinghelpers.TestManagedClusterName,
			Labels: map[string]string{v1beta2.ClusterSetLabel: "default"},
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.25.3",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "claim1", Value: "value1"},
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionTrue,
				},
				{
					Type:   v1.ManagedClusterConditionJoined,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	expectedCreatedClusterProfile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testinghelpers.TestManagedClusterName,
			Namespace: ClusterProfileNamespace,
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: testinghelpers.TestManagedClusterName,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	expectedPatchedClusterProfileLabels := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testinghelpers.TestManagedClusterName,
			Namespace: ClusterProfileNamespace,
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "default",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: testinghelpers.TestManagedClusterName,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
	}

	expectedPatchedClusterProfileStatus := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testinghelpers.TestManagedClusterName,
			Namespace: ClusterProfileNamespace,
			Labels: map[string]string{
				cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
				cpv1alpha1.LabelClusterSetKey:     "default",
			},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: testinghelpers.TestManagedClusterName,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: ClusterProfileManagerName,
			},
		},
		Status: cpv1alpha1.ClusterProfileStatus{
			Version: cpv1alpha1.ClusterVersion{
				Kubernetes: "v1.25.3",
			},
			Properties: []cpv1alpha1.Property{
				{Name: "claim1", Value: "value1"},
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionTrue,
				},
				{
					Type:   v1.ManagedClusterConditionJoined,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	cases := []struct {
		name                string
		autoApprovalEnabled bool
		mc                  []runtime.Object
		cp                  []runtime.Object
		validateActions     func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "create clusterprofile",
			mc:   []runtime.Object{managedCluster},
			cp:   []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				clusterprofile := actions[0].(clienttesting.CreateAction).GetObject().(*cpv1alpha1.ClusterProfile)
				if !reflect.DeepEqual(clusterprofile.Labels, expectedCreatedClusterProfile.Labels) {
					t.Errorf("expect clusterprofile labels %v but get %v", expectedCreatedClusterProfile.Labels, clusterprofile.Labels)
				}
				if !reflect.DeepEqual(clusterprofile.Spec, expectedCreatedClusterProfile.Spec) {
					t.Errorf("expect clusterprofile spec %v but get %v", expectedCreatedClusterProfile.Spec, clusterprofile.Spec)
				}
			},
		},
		{
			name: "patch clusterprofile clusterset labels",
			mc:   []runtime.Object{managedCluster},
			cp:   []runtime.Object{expectedCreatedClusterProfile},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				clusterprofile := &cpv1alpha1.ClusterProfile{}
				err := json.Unmarshal(patch, clusterprofile)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(clusterprofile.Labels[cpv1alpha1.LabelClusterSetKey], expectedPatchedClusterProfileLabels.Labels[cpv1alpha1.LabelClusterSetKey]) {
					t.Errorf("expect clusterprofile labels %v but get %v", expectedPatchedClusterProfileLabels.Labels, clusterprofile.Labels)
				}
			},
		},
		{
			name: "patch clusterprofile status",
			mc:   []runtime.Object{managedCluster},
			cp:   []runtime.Object{expectedPatchedClusterProfileLabels},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				clusterprofile := &cpv1alpha1.ClusterProfile{}
				err := json.Unmarshal(patch, clusterprofile)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(clusterprofile.Status.Version, expectedPatchedClusterProfileStatus.Status.Version) ||
					!reflect.DeepEqual(clusterprofile.Status.Properties, expectedPatchedClusterProfileStatus.Status.Properties) ||
					len(expectedPatchedClusterProfileStatus.Status.Conditions) != 2 {
					t.Errorf("expect clusterprofile status %v but get %v", expectedPatchedClusterProfileStatus.Status, clusterprofile.Status)
				}
			},
		},
		{
			name: "deleting clusterprofile",
			mc:   []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			cp:   []runtime.Object{expectedPatchedClusterProfileStatus},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name: "deleted clusterprofile",
			mc:   []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			cp:   []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "no managed cluster",
			mc:   []runtime.Object{},
			cp:   []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "clusterprofile not managed by ocm",
			mc:   []runtime.Object{managedCluster},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: ClusterProfileNamespace,
					Labels: map[string]string{
						cpv1alpha1.LabelClusterManagerKey: "not-open-cluster-management",
					},
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: "not-open-cluster-management",
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.mc...)
			clusterProfileClient := cpfake.NewSimpleClientset(c.cp...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterProfileInformerFactory := cpinformers.NewSharedInformerFactory(clusterProfileClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.mc {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}
			clusterProfileStore := clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Informer().GetStore()
			for _, clusterprofile := range c.cp {
				if err := clusterProfileStore.Add(clusterprofile); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := clusterProfileController{
				clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterProfileClient,
				clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Lister(),
				patcher.NewPatcher[
					*cpv1alpha1.ClusterProfile, cpv1alpha1.ClusterProfileSpec, cpv1alpha1.ClusterProfileStatus](
					clusterProfileClient.ApisV1alpha1().ClusterProfiles(ClusterProfileNamespace)),
			}
			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName), testinghelpers.TestManagedClusterName)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterProfileClient.Actions())
		})
	}
}
