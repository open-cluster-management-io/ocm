package statuscontroller

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslister "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

type klusterletStatusController struct {
	kubeClient       kubernetes.Interface
	deploymentLister appslister.DeploymentLister
	patcher          patcher.Patcher[*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus]
	klusterletLister operatorlister.KlusterletLister
}

const (
	klusterletRegistrationDesiredDegraded = "RegistrationDesiredDegraded"
	klusterletWorkDesiredDegraded         = "WorkDesiredDegraded"
	klusterletAvailable                   = "Available"
	klusterletApplied                     = "Applied"
)

// NewKlusterletStatusController returns a klusterletStatusController
func NewKlusterletStatusController(
	kubeClient kubernetes.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	recorder events.Recorder) factory.Controller {
	controller := &klusterletStatusController{
		kubeClient: kubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](klusterletClient),
		deploymentLister: deploymentInformer.Lister(),
		klusterletLister: klusterletInformer.Lister(),
	}
	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeysFunc(helpers.KlusterletDeploymentQueueKeyFunc(controller.klusterletLister), deploymentInformer.Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, klusterletInformer.Informer()).
		ToController("KlusterletStatusController", recorder)
}

func (k *klusterletStatusController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	klusterletName := controllerContext.QueueKey()
	if klusterletName == "" {
		return nil
	}
	klog.V(4).Infof("Reconciling Klusterlet %q", klusterletName)

	klusterlet, err := k.klusterletLister.Get(klusterletName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// Do nothing when the klusterlet is not applied yet
	if meta.FindStatusCondition(klusterlet.Status.Conditions, klusterletApplied) == nil {
		return nil
	}

	newKlusterlet := klusterlet.DeepCopy()

	agentNamespace := helpers.AgentNamespace(klusterlet)
	registrationDeploymentName := fmt.Sprintf("%s-registration-agent", klusterlet.Name)
	workDeploymentName := fmt.Sprintf("%s-work-agent", klusterlet.Name)

	if helpers.IsSingleton(klusterlet.Spec.DeployOption.Mode) {
		registrationDeploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
		workDeploymentName = registrationDeploymentName
	}

	availableCondition := checkAgentsDeploymentAvailable(
		ctx, k.kubeClient,
		[]klusterletAgent{
			{
				deploymentName: registrationDeploymentName,
				namespace:      agentNamespace,
			},
			{
				deploymentName: workDeploymentName,
				namespace:      agentNamespace,
			},
		},
	)
	availableCondition.ObservedGeneration = klusterlet.Generation
	meta.SetStatusCondition(&newKlusterlet.Status.Conditions, availableCondition)

	registrationDesiredCondition := checkAgentDeploymentDesired(ctx,
		k.kubeClient, agentNamespace, registrationDeploymentName, klusterletRegistrationDesiredDegraded)
	registrationDesiredCondition.ObservedGeneration = klusterlet.Generation
	meta.SetStatusCondition(&newKlusterlet.Status.Conditions, registrationDesiredCondition)

	workDesiredCondition := checkAgentDeploymentDesired(ctx, k.kubeClient, agentNamespace, workDeploymentName, klusterletWorkDesiredDegraded)
	workDesiredCondition.ObservedGeneration = klusterlet.Generation
	meta.SetStatusCondition(&newKlusterlet.Status.Conditions, workDesiredCondition)

	_, err = k.patcher.PatchStatus(ctx, newKlusterlet, newKlusterlet.Status, klusterlet.Status)
	return err
}

type klusterletAgent struct {
	deploymentName string
	namespace      string
}

// Check agent deployment, if the desired replicas is not equal to available replicas, return degraded condition
func checkAgentDeploymentDesired(ctx context.Context, kubeClient kubernetes.Interface, namespace, deploymentName, conditionType string) metav1.Condition {
	deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "GetDeploymentFailed",
			Message: fmt.Sprintf("Failed to get deployment %q %q: %v", namespace, deploymentName, err),
		}
	}
	if unavailablePod := helpers.NumOfUnavailablePod(deployment); unavailablePod > 0 {
		return metav1.Condition{
			Type:   conditionType,
			Status: metav1.ConditionTrue,
			Reason: "UnavailablePods",
			Message: fmt.Sprintf("%v of requested instances are unavailable of deployment %q %q",
				unavailablePod, namespace, deploymentName),
		}
	}
	return metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  "DeploymentsFunctional",
		Message: fmt.Sprintf("deployments replicas are desired: %d", *deployment.Spec.Replicas),
	}
}

// Check agent deployments, if both of them have at least 1 available replicas, return available condition
func checkAgentsDeploymentAvailable(ctx context.Context, kubeClient kubernetes.Interface, agents []klusterletAgent) metav1.Condition {
	var availableMessages []string
	for _, agent := range agents {
		deployment, err := kubeClient.AppsV1().Deployments(agent.namespace).Get(ctx, agent.deploymentName, metav1.GetOptions{})
		if err != nil {
			return metav1.Condition{
				Type:    klusterletAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "GetDeploymentFailed",
				Message: fmt.Sprintf("Failed to get deployment %q %q: %v", agent.namespace, agent.deploymentName, err),
			}
		}
		if deployment.Status.AvailableReplicas <= 0 {
			return metav1.Condition{
				Type:   klusterletAvailable,
				Status: metav1.ConditionFalse,
				Reason: "NoAvailablePods",
				Message: fmt.Sprintf("%v of requested instances are available of deployment %q %q",
					deployment.Status.AvailableReplicas, agent.namespace, agent.deploymentName),
			}
		}
		availableMessages = append(availableMessages, agent.deploymentName)
	}

	return metav1.Condition{
		Type:    klusterletAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "klusterletAvailable",
		Message: fmt.Sprintf("deployments are ready: %s", strings.Join(availableMessages, ",")),
	}
}
