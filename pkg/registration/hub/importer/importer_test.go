package importer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
	cloudproviders "open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
)

func TestSync(t *testing.T) {
	now := metav1.Now()
	cases := []struct {
		name     string
		provider *fakeProvider
		key      string
		cluster  *clusterv1.ManagedCluster
		validate func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "import succeed",
			provider: &fakeProvider{isOwned: true},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, ManagedClusterConditionImported) {
					t.Errorf("expected managed cluster to be imported")
				}
			},
		},
		{
			name:     "no cluster",
			provider: &fakeProvider{isOwned: true},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster2"}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:     "cluster in terminating state",
			provider: &fakeProvider{isOwned: true},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1", DeletionTimestamp: &now}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:     "not owned by the provider",
			provider: &fakeProvider{isOwned: false},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:     "clients for remote cluster is not generated with requeue error",
			provider: &fakeProvider{isOwned: true, noClients: true, kubeConfigErr: helpers.NewRequeueError("test", 1*time.Minute)},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(managedCluster.Status.Conditions, ManagedClusterConditionImported) {
					t.Errorf("expected managed cluster to be imported")
				}
			},
		},
		{
			name:     "clients for remote cluster is not generated",
			provider: &fakeProvider{isOwned: true, noClients: true},
			key:      "cluster1",
			cluster:  &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			validate: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !meta.IsStatusConditionFalse(managedCluster.Status.Conditions, ManagedClusterConditionImported) {
					t.Errorf("expected managed cluster to be imported")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := fakeclusterclient.NewSimpleClientset(c.cluster)
			clusterInformer := clusterinformers.NewSharedInformerFactory(
				clusterClient, 10*time.Minute).Cluster().V1().ManagedClusters()
			clusterStore := clusterInformer.Informer().GetStore()
			if err := clusterStore.Add(c.cluster); err != nil {
				t.Fatal(err)
			}
			importer := &Importer{
				providers:     []cloudproviders.Interface{c.provider},
				clusterClient: clusterClient,
				clusterLister: clusterInformer.Lister(),
				patcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
			}
			err := importer.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, c.key), c.key)
			if err != nil {
				t.Fatal(err)
			}
			c.validate(t, clusterClient.Actions())
		})
	}
}

type fakeProvider struct {
	isOwned       bool
	noClients     bool
	kubeConfigErr error
}

// KubeConfig is to return the config to connect to the target cluster.
func (f *fakeProvider) Clients(_ context.Context, _ *clusterv1.ManagedCluster) (*providers.Clients, error) {
	if f.kubeConfigErr != nil {
		return nil, f.kubeConfigErr
	}
	if f.noClients {
		return nil, nil
	}
	return &providers.Clients{
		KubeClient: kubefake.NewClientset(),
		// due to https://github.com/kubernetes/kubernetes/issues/126850, still need to use NewSimpleClientset
		APIExtClient:   fakeapiextensions.NewSimpleClientset(),
		OperatorClient: fakeoperatorclient.NewSimpleClientset(),
		DynamicClient:  fakedynamic.NewSimpleDynamicClient(runtime.NewScheme()),
	}, nil
}

// IsManagedClusterOwner check if the provider is used to manage this cluster
func (f *fakeProvider) IsManagedClusterOwner(_ *clusterv1.ManagedCluster) bool {
	return f.isOwned
}

// Register registers the provider to the importer. The provider should enqueue the resource
// into the queue with the name of the managed cluster
func (f *fakeProvider) Register(_ factory.SyncContext) {}

// Run starts the provider
func (f *fakeProvider) Run(_ context.Context) {}
