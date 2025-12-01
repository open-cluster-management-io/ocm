package statuscontroller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	appslister "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

type clusterManagerStatusController struct {
	deploymentLister     appslister.DeploymentLister
	patcher              patcher.Patcher[*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus]
	clusterManagerLister operatorlister.ClusterManagerLister
}

// NewClusterManagerStatusController creates hub cluster manager status controller
func NewClusterManagerStatusController(
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	deploymentInformer appsinformer.DeploymentInformer) factory.Controller {
	controller := &clusterManagerStatusController{
		deploymentLister:     deploymentInformer.Lister(),
		clusterManagerLister: clusterManagerInformer.Lister(),
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			clusterManagerClient),
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeysFunc(
			helpers.ClusterManagerDeploymentQueueKeyFunc(controller.clusterManagerLister), deploymentInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterManagerInformer.Informer()).
		ToController("ClusterManagerStatusController")
}

func (s *clusterManagerStatusController) sync(ctx context.Context, controllerContext factory.SyncContext, clusterManagerName string) error {
	logger := klog.FromContext(ctx).WithValues("clusterManager", clusterManagerName)
	if clusterManagerName == "" {
		return nil
	}

	logger.V(4).Info("Reconciling ClusterManager")

	clusterManager, err := s.clusterManagerLister.Get(clusterManagerName)
	// ClusterManager not found, could have been deleted, do nothing.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if meta.FindStatusCondition(clusterManager.Status.Conditions, operatorapiv1.ConditionClusterManagerApplied) == nil {
		return nil
	}

	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManager.Spec.DeployOption.Mode)
	newClusterManager := clusterManager.DeepCopy()

	registrationCond := s.updateStatusOfRegistration(clusterManager.Name, clusterManagerNamespace)
	registrationCond.ObservedGeneration = clusterManager.Generation
	meta.SetStatusCondition(&newClusterManager.Status.Conditions, registrationCond)
	placementCond := s.updateStatusOfPlacement(clusterManager.Name, clusterManagerNamespace)
	placementCond.ObservedGeneration = clusterManager.Generation
	meta.SetStatusCondition(&newClusterManager.Status.Conditions, placementCond)

	_, err = s.patcher.PatchStatus(ctx, newClusterManager, newClusterManager.Status, clusterManager.Status)
	return err
}

// updateStatusOfRegistration checks registration deployment status and updates condition of clustermanager
func (s *clusterManagerStatusController) updateStatusOfRegistration(clusterManagerName, clusterManagerNamespace string) metav1.Condition {
	// Check registration deployment status
	deployments := []string{
		fmt.Sprintf("%s-registration-controller", clusterManagerName),
		fmt.Sprintf("%s-registration-webhook", clusterManagerName),
	}

	for _, deploymentName := range deployments {
		registrationDeployment, err := s.deploymentLister.Deployments(clusterManagerNamespace).Get(deploymentName)
		if err != nil {
			return metav1.Condition{
				Type:    operatorapiv1.ConditionHubRegistrationDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  operatorapiv1.ReasonGetRegistrationDeploymentFailed,
				Message: fmt.Sprintf("Failed to get registration deployment %q %q: %v", clusterManagerNamespace, deploymentName, err),
			}
		}

		if unavailablePod := helpers.NumOfUnavailablePod(registrationDeployment); unavailablePod > 0 {
			return metav1.Condition{
				Type:   operatorapiv1.ConditionHubRegistrationDegraded,
				Status: metav1.ConditionTrue,
				Reason: operatorapiv1.ReasonUnavailableRegistrationPod,
				Message: fmt.Sprintf("%v of requested instances are unavailable of registration deployment %q %q",
					unavailablePod, clusterManagerNamespace, deploymentName),
			}
		}
	}

	return metav1.Condition{
		Type:    operatorapiv1.ConditionHubRegistrationDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  operatorapiv1.ReasonRegistrationFunctional,
		Message: "Registration is managing credentials",
	}
}

// updateStatusOfRegistration checks placement deployment status and updates condition of clustermanager
func (s *clusterManagerStatusController) updateStatusOfPlacement(clusterManagerName, clusterManagerNamespace string) metav1.Condition {
	// Check registration deployment status
	placementDeploymentName := fmt.Sprintf("%s-placement-controller", clusterManagerName)
	placementDeployment, err := s.deploymentLister.Deployments(clusterManagerNamespace).Get(placementDeploymentName)
	if err != nil {
		return metav1.Condition{
			Type:    operatorapiv1.ConditionHubPlacementDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  operatorapiv1.ReasonGetPlacementDeploymentFailed,
			Message: fmt.Sprintf("Failed to get placement deployment %q %q: %v", clusterManagerNamespace, placementDeploymentName, err),
		}
	}

	if unavailablePod := helpers.NumOfUnavailablePod(placementDeployment); unavailablePod > 0 {
		return metav1.Condition{
			Type:   operatorapiv1.ConditionHubPlacementDegraded,
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonUnavailablePlacementPod,
			Message: fmt.Sprintf("%v of requested instances are unavailable of placement deployment %q %q",
				unavailablePod, clusterManagerNamespace, placementDeploymentName),
		}
	}

	return metav1.Condition{
		Type:    operatorapiv1.ConditionHubPlacementDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  operatorapiv1.ReasonPlacementFunctional,
		Message: "Placement is scheduling placement decisions",
	}
}
