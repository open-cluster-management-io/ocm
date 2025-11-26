package managedcluster

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutclusterfake "sigs.k8s.io/about-api/pkg/generated/clientset/versioned/fake"
	aboutinformers "sigs.k8s.io/about-api/pkg/generated/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func init() {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
}

func TestSync(t *testing.T) {
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
		{
			name:    "sync a joined managed cluster with same property and claims",
			cluster: testinghelpers.NewJoinedManagedCluster(),
			properties: []runtime.Object{
				&aboutv1alpha1.ClusterProperty{
					ObjectMeta: metav1.ObjectMeta{
						Name: "key1",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "value1",
					},
				},
				&aboutv1alpha1.ClusterProperty{
					ObjectMeta: metav1.ObjectMeta{
						Name: "key2",
					},
					Spec: aboutv1alpha1.ClusterPropertySpec{
						Value: "value2",
					},
				},
			},
			claims: []runtime.Object{
				&clusterv1alpha1.ClusterClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "key1",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "value3",
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
						Name:  "key1",
						Value: "value1",
					},
					{
						Name:  "key2",
						Value: "value2",
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
	kubeClient := kubefake.NewClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
	err := features.SpokeMutableFeatureGate.Set("ClusterProperty=true")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			aboutClusterClient := aboutclusterfake.NewSimpleClientset()
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

			fakeHubClient := kubefake.NewClientset()
			ctx := context.TODO()
			hubEventRecorder, err := events.NewEventRecorder(ctx,
				clusterscheme.Scheme, fakeHubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := newManagedClusterStatusController(
				testinghelpers.TestManagedClusterName,
				"test-hub-hash",
				clusterClient,
				kubefake.NewSimpleClientset(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				kubeInformerFactory.Core().V1().Namespaces(),
				discoveryClient,
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
				clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties(),
				kubeInformerFactory.Core().V1().Nodes(),
				20,
				[]string{},
				hubEventRecorder,
			)

			syncErr := ctrl.sync(ctx, testingcommon.NewFakeSyncContext(t, ""), "")
			testingcommon.AssertError(t, syncErr, c.expectedErr)

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestExposeClaims(t *testing.T) {
	cases := []struct {
		name                         string
		cluster                      *clusterv1.ManagedCluster
		claims                       []*clusterv1alpha1.ClusterClaim
		properties                   []*aboutv1alpha1.ClusterProperty
		maxCustomClusterClaims       int
		reservedClusterClaimSuffixes []string
		validateActions              func(t *testing.T, actions []clienttesting.Action)
		expectedErr                  string
	}{
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
						Name: "id-test.k8s.io",
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
			name:    "keep custom reserved cluster claims",
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test.reserved.io",
					},
					Spec: clusterv1alpha1.ClusterClaimSpec{
						Value: "test",
					},
				},
			},
			maxCustomClusterClaims:       2,
			reservedClusterClaimSuffixes: []string{"reserved.io"},
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
						Name:  "test.reserved.io",
						Value: "test",
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
	kubeClient := kubefake.NewClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)
	err := features.SpokeMutableFeatureGate.Set("ClusterProperty=true")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			aboutClusterClient := aboutclusterfake.NewSimpleClientset()
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

			fakeHubClient := kubefake.NewClientset()
			ctx := context.TODO()
			hubEventRecorder, err := events.NewEventRecorder(ctx,
				clusterscheme.Scheme, fakeHubClient.EventsV1(), "test")
			if err != nil {
				t.Fatal(err)
			}
			ctrl := newManagedClusterStatusController(
				testinghelpers.TestManagedClusterName,
				"test-hub-hash",
				clusterClient,
				kubefake.NewSimpleClientset(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				kubeInformerFactory.Core().V1().Namespaces(),
				discoveryClient,
				clusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
				clusterPropertyInformerFactory.About().V1alpha1().ClusterProperties(),
				kubeInformerFactory.Core().V1().Nodes(),
				c.maxCustomClusterClaims,
				c.reservedClusterClaimSuffixes,
				hubEventRecorder,
			)

			syncErr := ctrl.sync(ctx, testingcommon.NewFakeSyncContext(t, c.cluster.Name), c.cluster.Name)
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
