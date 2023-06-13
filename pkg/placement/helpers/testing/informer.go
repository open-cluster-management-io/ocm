package testing

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

func NewClusterInformerFactory(clusterClient clusterclient.Interface, objects ...runtime.Object) clusterinformers.SharedInformerFactory {
	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
	clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
	clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
	clusterSetBindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
	placementStore := clusterInformerFactory.Cluster().V1beta1().Placements().Informer().GetStore()
	placementDecisionStore := clusterInformerFactory.Cluster().V1beta1().PlacementDecisions().Informer().GetStore()
	addOnPlacementStore := clusterInformerFactory.Cluster().V1alpha1().AddOnPlacementScores().Informer().GetStore()

	for _, obj := range objects {
		switch obj.(type) {
		case *clusterapiv1.ManagedCluster:
			_ = clusterStore.Add(obj)
		case *clusterapiv1beta2.ManagedClusterSet:
			_ = clusterSetStore.Add(obj)
		case *clusterapiv1beta2.ManagedClusterSetBinding:
			_ = clusterSetBindingStore.Add(obj)
		case *clusterapiv1beta1.Placement:
			_ = placementStore.Add(obj)
		case *clusterapiv1beta1.PlacementDecision:
			_ = placementDecisionStore.Add(obj)
		case *clusterapiv1alpha1.AddOnPlacementScore:
			_ = addOnPlacementStore.Add(obj)
		}
	}

	return clusterInformerFactory
}
