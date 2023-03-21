package statuscontroller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	appslister "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

const registrationDegraded = "HubRegistrationDegraded"
const placementDegraded = "HubPlacementDegraded"

type clusterManagerStatusController struct {
	deploymentLister     appslister.DeploymentLister
	clusterManagerClient operatorv1client.ClusterManagerInterface
	clusterManagerLister operatorlister.ClusterManagerLister
}

// NewClusterManagerStatusController creates hub cluster manager status controller
func NewClusterManagerStatusController(
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	recorder events.Recorder) factory.Controller {
	controller := &clusterManagerStatusController{
		deploymentLister:     deploymentInformer.Lister(),
		clusterManagerClient: clusterManagerClient,
		clusterManagerLister: clusterManagerInformer.Lister(),
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(
			helpers.ClusterManagerDeploymentQueueKeyFunc(controller.clusterManagerLister), deploymentInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterManagerInformer.Informer()).
		ToController("ClusterManagerStatusController", recorder)
}

func (s *clusterManagerStatusController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	if clusterManagerName == "" {
		return nil
	}

	klog.Infof("Reconciling ClusterManager %q", clusterManagerName)

	clusterManager, err := s.clusterManagerLister.Get(clusterManagerName)
	// ClusterManager not found, could have been deleted, do nothing.
	if errors.IsNotFound(err) {
		return nil
	}

	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManagerName, clusterManager.Spec.DeployOption.Mode)

	if err != nil {
		return err
	}

	registrationCond := s.updateStatusOfRegistration(ctx, clusterManager.Name, clusterManagerNamespace)
	registrationCond.ObservedGeneration = clusterManager.Generation
	placementCond := s.updateStatusOfPlacement(ctx, clusterManager.Name, clusterManagerNamespace)
	placementCond.ObservedGeneration = clusterManager.Generation

	_, _, err = helpers.UpdateClusterManagerStatus(ctx, s.clusterManagerClient, clusterManagerName,
		helpers.UpdateClusterManagerConditionFn(registrationCond, placementCond),
	)

	return err
}

// updateStatusOfRegistration checks registration deployment status and updates condition of clustermanager
func (s *clusterManagerStatusController) updateStatusOfRegistration(ctx context.Context, clusterManagerName, clusterManagerNamespace string) metav1.Condition {
	// Check registration deployment status
	registrationDeploymentName := fmt.Sprintf("%s-registration-controller", clusterManagerName)
	registrationDeployment, err := s.deploymentLister.Deployments(clusterManagerNamespace).Get(registrationDeploymentName)
	if err != nil {
		return metav1.Condition{
			Type:    registrationDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  "GetRegistrationDeploymentFailed",
			Message: fmt.Sprintf("Failed to get registration deployment %q %q: %v", clusterManagerNamespace, registrationDeploymentName, err),
		}
	}

	if unavailablePod := helpers.NumOfUnavailablePod(registrationDeployment); unavailablePod > 0 {
		return metav1.Condition{
			Type:    registrationDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  "UnavailableRegistrationPod",
			Message: fmt.Sprintf("%v of requested instances are unavailable of registration deployment %q %q", unavailablePod, clusterManagerNamespace, registrationDeploymentName),
		}
	}

	return metav1.Condition{
		Type:    registrationDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  "RegistrationFunctional",
		Message: "Registration is managing credentials",
	}
}

// updateStatusOfRegistration checks placement deployment status and updates condition of clustermanager
func (s *clusterManagerStatusController) updateStatusOfPlacement(ctx context.Context, clusterManagerName, clusterManagerNamespace string) metav1.Condition {
	// Check registration deployment status
	placementDeploymentName := fmt.Sprintf("%s-placement-controller", clusterManagerName)
	placementDeployment, err := s.deploymentLister.Deployments(clusterManagerNamespace).Get(placementDeploymentName)
	if err != nil {
		return metav1.Condition{
			Type:    placementDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  "GetPlacementDeploymentFailed",
			Message: fmt.Sprintf("Failed to get placement deployment %q %q: %v", clusterManagerNamespace, placementDeploymentName, err),
		}
	}

	if unavailablePod := helpers.NumOfUnavailablePod(placementDeployment); unavailablePod > 0 {
		return metav1.Condition{
			Type:    placementDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  "UnavailablePlacementPod",
			Message: fmt.Sprintf("%v of requested instances are unavailable of placement deployment %q %q", unavailablePod, clusterManagerNamespace, placementDeploymentName),
		}
	}

	return metav1.Condition{
		Type:    placementDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  "PlacementFunctional",
		Message: "Placement is scheduling placement decisions",
	}
}
