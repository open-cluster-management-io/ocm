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

	operatorv1client "github.com/open-cluster-management/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "github.com/open-cluster-management/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "github.com/open-cluster-management/api/client/operator/listers/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

const registrationDegraded = "HubRegistrationDegraded"

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

	if err != nil {
		return err
	}

	// Check registration deployment status
	registrationDeploymentName := fmt.Sprintf("%s-registration-controller", clusterManager.Name)
	registrationDeployment, err := s.deploymentLister.Deployments(helpers.ClusterManagerNamespace).Get(registrationDeploymentName)
	if err != nil {
		_, _, err := helpers.UpdateClusterManagerStatus(ctx, s.clusterManagerClient, clusterManager.Name,
			helpers.UpdateClusterManagerConditionFn(metav1.Condition{
				Type:    registrationDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  "GetRegistrationDeploymentFailed",
				Message: fmt.Sprintf("Failed to get registration deployment %q %q: %v", helpers.ClusterManagerNamespace, registrationDeploymentName, err),
			}),
		)
		return err
	}

	if unavailablePod := helpers.NumOfUnavailablePod(registrationDeployment); unavailablePod > 0 {
		_, _, err := helpers.UpdateClusterManagerStatus(ctx, s.clusterManagerClient, clusterManager.Name,
			helpers.UpdateClusterManagerConditionFn(metav1.Condition{
				Type:    registrationDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  "UnavailableRegistrationPod",
				Message: fmt.Sprintf("%v of requested instances are unavailable of registration deployment %q %q", unavailablePod, helpers.ClusterManagerNamespace, registrationDeploymentName),
			}),
		)
		return err
	}

	_, _, err = helpers.UpdateClusterManagerStatus(ctx, s.clusterManagerClient, clusterManager.Name,
		helpers.UpdateClusterManagerConditionFn(metav1.Condition{
			Type:    registrationDegraded,
			Status:  metav1.ConditionFalse,
			Reason:  "RegistrationFunctional",
			Message: "Registration is managing credentials",
		}),
	)
	return err
}
