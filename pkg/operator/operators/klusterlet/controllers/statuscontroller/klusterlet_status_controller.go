package statuscontroller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
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

type klusterletStatusController struct {
	kubeClient       kubernetes.Interface
	deploymentLister appslister.DeploymentLister
	patcher          patcher.Patcher[*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus]
	klusterletLister operatorlister.KlusterletLister
}

// NewKlusterletStatusController returns a klusterletStatusController
func NewKlusterletStatusController(
	kubeClient kubernetes.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	deploymentInformer appsinformer.DeploymentInformer) factory.Controller {
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
		ToController("KlusterletStatusController")
}

func (k *klusterletStatusController) sync(ctx context.Context, controllerContext factory.SyncContext, klusterletName string) error {
	logger := klog.FromContext(ctx).WithValues("klusterlet", klusterletName)
	if klusterletName == "" {
		return nil
	}
	logger.V(4).Info("Reconciling Klusterlet")

	klusterlet, err := k.klusterletLister.Get(klusterletName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// Do nothing when the klusterlet is not applied yet
	if meta.FindStatusCondition(klusterlet.Status.Conditions, operatorapiv1.ConditionKlusterletApplied) == nil {
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
		k.kubeClient, agentNamespace, registrationDeploymentName, operatorapiv1.ConditionRegistrationDesiredDegraded)
	registrationDesiredCondition.ObservedGeneration = klusterlet.Generation
	meta.SetStatusCondition(&newKlusterlet.Status.Conditions, registrationDesiredCondition)

	workDesiredCondition := checkAgentDeploymentDesired(ctx, k.kubeClient, agentNamespace, workDeploymentName, operatorapiv1.ConditionWorkDesiredDegraded)
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
			Reason:  operatorapiv1.ReasonKlusterletGetDeploymentFailed,
			Message: fmt.Sprintf("Failed to get deployment %q %q: %v", namespace, deploymentName, err),
		}
	}
	if unavailablePod := helpers.NumOfUnavailablePod(deployment); unavailablePod > 0 {
		return metav1.Condition{
			Type:   conditionType,
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonKlusterletUnavailablePods,
			Message: fmt.Sprintf("%v of requested instances are unavailable of deployment %q %q",
				unavailablePod, namespace, deploymentName),
		}
	}
	return metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  operatorapiv1.ReasonKlusterletDeploymentsFunctional,
		Message: fmt.Sprintf("deployments replicas are desired: %d", *deployment.Spec.Replicas),
	}
}

// Check agent deployments, if both of them have at least 1 available replicas, return available condition
func checkAgentsDeploymentAvailable(ctx context.Context, kubeClient kubernetes.Interface, agents []klusterletAgent) metav1.Condition {
	var availableMessages []string
	var components = sets.New[string]()

	for _, agent := range agents {
		componentID := fmt.Sprintf("%s-%s", agent.namespace, agent.deploymentName)
		if components.Has(componentID) {
			continue
		}
		components.Insert(componentID)
		deployment, err := kubeClient.AppsV1().Deployments(agent.namespace).Get(ctx, agent.deploymentName, metav1.GetOptions{})
		if err != nil {
			return metav1.Condition{
				Type:    operatorapiv1.ConditionKlusterletAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  operatorapiv1.ReasonKlusterletGetDeploymentFailed,
				Message: fmt.Sprintf("Failed to get deployment %q %q: %v", agent.namespace, agent.deploymentName, err),
			}
		}
		if deployment.Status.AvailableReplicas <= 0 {
			return metav1.Condition{
				Type:   operatorapiv1.ConditionKlusterletAvailable,
				Status: metav1.ConditionFalse,
				Reason: operatorapiv1.ReasonKlusterletNoAvailablePods,
				Message: fmt.Sprintf("%v of requested instances are available of deployment %q %q",
					deployment.Status.AvailableReplicas, agent.namespace, agent.deploymentName),
			}
		}
		availableMessages = append(availableMessages, agent.deploymentName)
	}

	return metav1.Condition{
		Type:    operatorapiv1.ConditionKlusterletAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  operatorapiv1.ReasonKlusterletAvailable,
		Message: fmt.Sprintf("deployments are ready: %s", strings.Join(availableMessages, ",")),
	}
}
