package managedcluster

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutinformers "sigs.k8s.io/about-api/pkg/generated/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	aboutapifakeclientset "open-cluster-management.io/ocm/pkg/registration/helpers"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSync(t *testing.T) {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	cases := []struct {
		name            string
		cluster         runtime.Object
		properties      []runtime.Object
		claims          []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedErr     string
	}{
		{
			name:            "sync no managed cluster",
			validateActions: testingcommon.AssertNoActions,
			expectedErr: "unable to get managed cluster \"testmanagedcluster\" " +
				"from hub: managedcluster.cluster.open-cluster-management.io \"testmanagedcluster\" not found",
		},
		{
			name:            "skip when managed cluster does not join the hub yet",
			cluster:         testinghelpers.NewManagedCluster(),
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:    "sync a joined managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			properties: []runtime.Object{
				&aboutv1alpha1.ClusterProperty{
					ObjectMeta: metav1.ObjectMeta{
						Name: "name",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "test",
					},
				},
			},
			claims: []runtime.Object{
				&clusterv1alpha1.ClusterClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "a",
						Value: "b",
					},
					{
						Name:  "name",
						Value: "test",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
	}

	apiServer, discoveryClient := newDiscoveryServer(t, nil)
	defer apiServer.Close()
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			aboutClusterClient := aboutapifakeclientset.NewFakeClientset(objects...)
			clusterPropertyInformerFactory := aboutinformers.NewSharedInformerFactory(aboutClusterClient, time.Minute*10)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			if c.cluster != nil {
				if err := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(c.cluster); err != nil {
					t.Fatal(err)
				}
			}

			for _, claim := range c.claims {
				if err := clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Informer().GetStore().Add(claim); err != nil {
					t.Fatal(err)
				}
			}

			for _, property := range c.properties {
				if err := clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties().Informer().GetStore().Add(property); err != nil {
					t.Fatal(err)
				}
			}

			fakeHubClient := fakekube.NewSimpleClientset()
			ctx := context.TODO()
			hubEventRecorder, err := helpers.NewEventRecorder(ctx,
				clusterscheme.Scheme, fakeHubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := newManagedClusterStatusController(
				testinghelpers.TestManagedClusterName,
				clusterClient,
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				discoveryClient,
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
				clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties(),
				kubeInformerFactory.Core().V1().Nodes(),
				20,
				eventstesting.NewTestingEventRecorder(t),
				hubEventRecorder,
			)

			syncErr := ctrl.sync(ctx, testingcommon.NewFakeSyncContext(t, ""))
			testingcommon.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestExposeClaims(t *testing.T) {
	cases := []struct {
		name                   string
		cluster                *clusterv1.ManagedCluster
		properties             []*aboutv1alpha1.ClusterProperty
		claims                 []*clusterv1alpha1.ClusterClaim
		maxCustomClusterClaims int
		validateActions        func(t *testing.T, actions []clienttesting.Action)
		expectedErr            string
	}{
		{
			name:    "sync claims into status of the managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "a",
						Value: "b",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name:    "sync properties into status of the managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			properties: []*aboutv1alpha1.ClusterProperty{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "b",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "a",
						Value: "b",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name:    "truncate custom cluster claims",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "e",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "f",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "id.k8s.io",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "cluster1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "d",
					},
				},
			},
			maxCustomClusterClaims: 2,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "id.k8s.io",
						Value: "cluster1",
					},
					{
						Name:  "a",
						Value: "b",
					},
					{
						Name:  "c",
						Value: "d",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name: "remove properties from managed cluster",
			cluster: newManagedClusterWithProperties([]aboutv1alpha1.ClusterProperty{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "a",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "b",
					},
				},
			}),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				actual := cluster.Status.ClusterClaims
				if len(actual) > 0 {
					t.Errorf("expected no cluster claim but got: %v", actual)
				}
			},
		},
		{
			name: "remove claims from managed cluster",
			cluster: newManagedCluster([]clusterv1.ManagedClusterClaim{
				{
					Name:  "a",
					Value: "b",
				},
			}),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				actual := cluster.Status.ClusterClaims
				if len(actual) > 0 {
					t.Errorf("expected no cluster claim but got: %v", actual)
				}
			},
		},
		{
			name:    "sync non-customized-only properties into status of the managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			properties: []*aboutv1alpha1.ClusterProperty{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "a",
						Labels: map[string]string{labelCustomizedOnly: ""},
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "b",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "d",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "c",
						Value: "d",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
		{
			name:    "sync non-customized-only claims into status of the managed cluster",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			claims: []*clusterv1alpha1.ClusterClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "a",
						Labels: map[string]string{labelCustomizedOnly: ""},
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "b",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "d",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				cluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, cluster)
				if err != nil {
					t.Fatal(err)
				}
				expected := []clusterv1.ManagedClusterClaim{
					{
						Name:  "c",
						Value: "d",
					},
				}
				actual := cluster.Status.ClusterClaims
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("expected cluster claim %v but got: %v", expected, actual)
				}
			},
		},
	}

	apiServer, discoveryClient := newDiscoveryServer(t, nil)
	defer apiServer.Close()
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			aboutClusterClient := aboutapifakeclientset.NewFakeClientset(objects...)
			clusterPropertyInformerFactory := aboutinformers.NewSharedInformerFactory(aboutClusterClient, time.Minute*10)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			if c.cluster != nil {
				if err := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(c.cluster); err != nil {
					t.Fatal(err)
				}
			}

			for _, claim := range c.claims {
				if err := clusterInformerFactory.Cluster().V1alpha1().ClusterClaims().Informer().GetStore().Add(claim); err != nil {
					t.Fatal(err)
				}
			}

			for _, property := range c.properties {
				if err := clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties().Informer().GetStore().Add(property); err != nil {
					t.Fatal(err)
				}
			}

			if c.maxCustomClusterClaims == 0 {
				c.maxCustomClusterClaims = 20
			}

			fakeHubClient := fakekube.NewSimpleClientset()
			ctx := context.TODO()
			hubEventRecorder, err := helpers.NewEventRecorder(ctx,
				clusterscheme.Scheme, fakeHubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := newManagedClusterStatusController(
				testinghelpers.TestManagedClusterName,
				clusterClient,
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				discoveryClient,
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
				clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties(),
				kubeInformerFactory.Core().V1().Nodes(),
				c.maxCustomClusterClaims,
				eventstesting.NewTestingEventRecorder(t),
				hubEventRecorder,
			)

			syncErr := ctrl.sync(ctx, testingcommon.NewFakeSyncContext(t, c.cluster.Name))
			testingcommon.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func newManagedCluster(claims []clusterv1.ManagedClusterClaim) *clusterv1.ManagedCluster {
	cluster := testinghelpers.NewJoinedManagedCluster()
	cluster.Status.ClusterClaims = claims
	return cluster
}

func newManagedClusterWithProperties(properties []aboutv1alpha1.ClusterProperty) *clusterv1.ManagedCluster {
	cluster := testinghelpers.NewJoinedManagedCluster()
	claims := make([]clusterv1.ManagedClusterClaim, len(properties))
	for _, property := range properties {
		claims = append(claims, clusterv1.ManagedClusterClaim{
			Name:  property.Name,
			Value: property.Spec.Value,
		})
	}
	cluster.Status.ClusterClaims = claims
	return cluster
}
