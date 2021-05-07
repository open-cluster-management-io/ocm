package testing

import (
	"time"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

func NewClusterInformerFactory(clusterClient clusterclient.Interface, objects ...runtime.Object) clusterinformers.SharedInformerFactory {
	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
	clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
	clusterSetStore := clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSets().Informer().GetStore()
	clusterSetBindingStore := clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSetBindings().Informer().GetStore()
	placementStore := clusterInformerFactory.Cluster().V1alpha1().Placements().Informer().GetStore()
	placementDecisionStore := clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions().Informer().GetStore()

	for _, obj := range objects {
		switch obj.(type) {
		case *clusterapiv1.ManagedCluster:
			clusterStore.Add(obj)
		case *clusterapiv1alpha1.ManagedClusterSet:
			clusterSetStore.Add(obj)
		case *clusterapiv1alpha1.ManagedClusterSetBinding:
			clusterSetBindingStore.Add(obj)
		case *clusterapiv1alpha1.Placement:
			placementStore.Add(obj)
		case *clusterapiv1alpha1.PlacementDecision:
			placementDecisionStore.Add(obj)
		}
	}

	return clusterInformerFactory
}
