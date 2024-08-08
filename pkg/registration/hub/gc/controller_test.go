package gc

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	fakemetadataclient "k8s.io/client-go/metadata/fake"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

func TestGController(t *testing.T) {
	cases := []struct {
		name        string
		key         string
		cluster     *clusterv1.ManagedCluster
		expectedErr string
	}{
		{
			name:        "invalid key",
			key:         factory.DefaultQueueKey,
			cluster:     testinghelpers.NewDeletingManagedCluster(),
			expectedErr: "",
		},
		{
			name:        "valid key",
			key:         testinghelpers.TestManagedClusterName,
			cluster:     testinghelpers.NewDeletingManagedCluster(),
			expectedErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakeclient.NewSimpleClientset()
			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*10)

			metadataClient := fakemetadataclient.NewSimpleMetadataClient(scheme.Scheme)

			clusterClient := fakeclusterclient.NewSimpleClientset(c.cluster)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			if c.cluster != nil {
				if err := clusterStore.Add(c.cluster); err != nil {
					t.Fatal(err)
				}
			}

			workClient := fakeworkclient.NewSimpleClientset()
			workInformerFactory := workinformers.NewSharedInformerFactory(workClient, 5*time.Minute)

			_ = NewGCController(
				kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
				kubeInformerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
				kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				workInformerFactory.Work().V1().ManifestWorks().Lister(),
				clusterClient,
				kubeClient,
				metadataClient,
				register.NewNoopApprover(),
				events.NewInMemoryRecorder(""),
				[]string{"addon.open-cluster-management.io/v1alpha1/managedclusteraddons",
					"work.open-cluster-management.io/v1/manifestworks"},
				true,
			)

			clusterPatcher := patcher.NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters())

			ctrl := &GCController{
				clusterLister:  clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterPatcher: clusterPatcher,
				gcReconcilers: []gcReconciler{
					newGCResourcesController(metadataClient, []schema.GroupVersionResource{addonGvr, workGvr},
						events.NewInMemoryRecorder("")),
					newGCClusterRbacController(kubeClient, clusterPatcher,
						clusterInformerFactory.Cluster().V1().ManagedClusters(),
						kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
						kubeInformerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
						kubeInformerFactory.Rbac().V1().RoleBindings().Lister(),
						workInformerFactory.Work().V1().ManifestWorks().Lister(),
						register.NewNoopApprover(),
						events.NewInMemoryRecorder(""),
						true),
				},
			}

			controllerContext := testingcommon.NewFakeSyncContext(t, c.key)
			err := ctrl.sync(context.TODO(), controllerContext)
			testingcommon.AssertError(t, err, c.expectedErr)
		})
	}
}
