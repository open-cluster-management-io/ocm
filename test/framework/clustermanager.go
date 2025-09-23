package framework

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

func (hub *Hub) GetCluserManager() (*operatorapiv1.ClusterManager, error) {
	return hub.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), hub.ClusterManagerName, metav1.GetOptions{})
}

func CheckClusterManagerStatus(cm *operatorapiv1.ClusterManager) error {
	if cm.Status.ObservedGeneration != cm.Generation {
		return fmt.Errorf("clusterManager generation does not match ObservedGeneration")
	}
	if meta.IsStatusConditionFalse(cm.Status.Conditions, "Applied") {
		return fmt.Errorf("components of cluster manager are not all applied")
	}
	if meta.IsStatusConditionFalse(cm.Status.Conditions, "ValidFeatureGates") {
		return fmt.Errorf("feature gates are not all valid")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubRegistrationDegraded") {
		return fmt.Errorf("HubRegistration is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubPlacementDegraded") {
		return fmt.Errorf("HubPlacement is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "Progressing") {
		return fmt.Errorf("ClusterManager is still progressing")
	}
	return nil
}
