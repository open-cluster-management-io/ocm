package statuscontroller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	appslister "k8s.io/client-go/listers/apps/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"

	operatorv1client "github.com/open-cluster-management/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "github.com/open-cluster-management/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "github.com/open-cluster-management/api/client/operator/listers/operator/v1"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
)

type klusterletStatusController struct {
	kubeClient       kubernetes.Interface
	secretLister     corelister.SecretLister
	deploymentLister appslister.DeploymentLister
	klusterletClient operatorv1client.KlusterletInterface
	klusterletLister operatorlister.KlusterletLister
}

const (
	klusterletNamespace            = "open-cluster-management-agent"
	bootstrapHubKubeConfigSecret   = "bootstrap-hub-kubeconfig"
	hubKubeConfigSecret            = "hub-kubeconfig-secret"
	klusterletRegistrationDegraded = "KlusterletRegistrationDegraded"
	klusterletWorKDegraded         = "KlusterletWorkDegraded"
)

// NewKlusterletStatusController returns a klusterletStatusController
func NewKlusterletStatusController(
	kubeClient kubernetes.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformer coreinformer.SecretInformer,
	deploymentInformer appsinformer.DeploymentInformer,
	recorder events.Recorder) factory.Controller {
	controller := &klusterletStatusController{
		kubeClient:       kubeClient,
		klusterletClient: klusterletClient,
		secretLister:     secretInformer.Lister(),
		deploymentLister: deploymentInformer.Lister(),
		klusterletLister: klusterletInformer.Lister(),
	}
	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(controller.queueKeyFunc, secretInformer.Informer(), deploymentInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, klusterletInformer.Informer()).
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
	klusterlet = klusterlet.DeepCopy()

	klusterletNS := klusterlet.Spec.Namespace
	if klusterletNS == "" {
		klusterletNS = klusterletNamespace
	}

	registrationDegradedCondition := operatorapiv1.StatusCondition{
		Type:    klusterletRegistrationDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  "RegistrationFunctional",
		Message: "Registration is managing credentials",
	}
	workDegradedCondition := operatorapiv1.StatusCondition{
		Type:    klusterletWorKDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  "WorkFunctional",
		Message: "Work is managing manifests",
	}

	// Check if bootstrap secret exists
	_, err = k.kubeClient.CoreV1().Secrets(klusterletNS).Get(ctx, bootstrapHubKubeConfigSecret, metav1.GetOptions{})
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to get bootstrap secret %q %q: %v", klusterletNS, bootstrapHubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "BootStrapSecretMissing"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}
	// TODO verify if bootstrap secret works

	// Check if hub kubeconfig secret exists
	hubConfigSecret, err := k.kubeClient.CoreV1().Secrets(klusterletNS).Get(ctx, hubKubeConfigSecret, metav1.GetOptions{})
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to get hub kubeconfig secret %q %q: %v", klusterletNS, hubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "HubKubeConfigSecretMissing"
		// Work condition will be the same as registration
		workDegradedCondition.Message = registrationDegradedCondition.Message
		workDegradedCondition.Status = registrationDegradedCondition.Status
		workDegradedCondition.Reason = registrationDegradedCondition.Reason

		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
			helpers.UpdateKlusterletConditionFn(workDegradedCondition),
		)
		return err
	}

	// If cluster name is empty, read cluster name from hub config secret
	if klusterlet.Spec.ClusterName == "" {
		clusterName := hubConfigSecret.Data["cluster-name"]
		if clusterName == nil {
			registrationDegradedCondition.Message = fmt.Sprintf(
				"Failed to get cluster name from `kubectl get secret -n %q %q -ojsonpath='{.data.cluster-name}`.  This is set by the klusterlet registration deployment.", hubConfigSecret.Namespace, hubConfigSecret.Name)
			registrationDegradedCondition.Status = metav1.ConditionTrue
			registrationDegradedCondition.Reason = "ClusterNameMissing"
			// Work condition will be the same as registration
			workDegradedCondition.Message = registrationDegradedCondition.Message
			workDegradedCondition.Status = registrationDegradedCondition.Status
			workDegradedCondition.Reason = registrationDegradedCondition.Reason

			_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
				helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
				helpers.UpdateKlusterletConditionFn(workDegradedCondition),
			)
			return err
		}
	}

	// If hub kubeconfig does not exist, return err.
	if hubConfigSecret.Data["kubeconfig"] == nil {
		registrationDegradedCondition.Message = fmt.Sprintf(
			"Failed to get kubeconfig from `kubectl get secret -n %q %q -ojsonpath='{.data.kubeconfig}`.  This is set by the klusterlet registration deployment, but the CSR must be approved by the cluster-admin on the hub.", hubConfigSecret.Namespace, hubConfigSecret.Name)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "KubeConfigMissing"
		// Work condition will be the same as registration
		workDegradedCondition.Message = registrationDegradedCondition.Message
		workDegradedCondition.Status = registrationDegradedCondition.Status
		workDegradedCondition.Reason = registrationDegradedCondition.Reason

		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
			helpers.UpdateKlusterletConditionFn(workDegradedCondition),
		)
		return err
	}
	// TODO it is possible to verify the kubeconfig actually works.

	// Check deployment status
	registrationDeploymentName := fmt.Sprintf("%s-registration-agent", klusterlet.Name)
	registrationDeployment, err := k.kubeClient.AppsV1().Deployments(klusterletNS).Get(ctx, registrationDeploymentName, metav1.GetOptions{})
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to get registration deployment %q %q: %v", klusterletNS, registrationDeploymentName, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "GetRegistrationDeploymentFailed"
	} else if unavailablePod := helpers.NumOfUnavailablePod(registrationDeployment); unavailablePod > 0 {
		registrationDegradedCondition.Message = fmt.Sprintf("%v of requested instances are unavailable of registration deployment %q %q", unavailablePod, klusterletNS, registrationDeploymentName)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "UnavailableRegistrationPod"
	}

	workDeploymentName := fmt.Sprintf("%s-work-agent", klusterlet.Name)
	workDeployment, err := k.kubeClient.AppsV1().Deployments(klusterletNS).Get(ctx, workDeploymentName, metav1.GetOptions{})
	if err != nil {
		workDegradedCondition.Message = fmt.Sprintf("Failed to get work deployment %q %q: %v", klusterletNS, workDeploymentName, err)
		workDegradedCondition.Status = metav1.ConditionTrue
		workDegradedCondition.Reason = "GetWorkDeploymentFailed"
	} else if unavailablePod := helpers.NumOfUnavailablePod(workDeployment); unavailablePod > 0 {
		workDegradedCondition.Message = fmt.Sprintf("%v of requested instances are unavailable of work deployment %q %q", unavailablePod, klusterletNS, workDeploymentName)
		workDegradedCondition.Status = metav1.ConditionTrue
		workDegradedCondition.Reason = "UnavailableWorkPod"
	}

	helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
		helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		helpers.UpdateKlusterletConditionFn(workDegradedCondition),
	)
	return nil
}

func (k *klusterletStatusController) queueKeyFunc(obj runtime.Object) string {
	accessor, _ := meta.Accessor(obj)
	namespace := accessor.GetNamespace()
	name := accessor.GetName()

	// return empty key if secret ot deployment is not interesting
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	interestedObjectFound := false
	switch gvk.Kind {
	case "Secret":
		if name == hubKubeConfigSecret || name == bootstrapHubKubeConfigSecret {
			interestedObjectFound = true
		}
	case "Deployment":
		if strings.HasSuffix(name, "registration-agent") || strings.HasSuffix(name, "work-agent") {
			interestedObjectFound = true
		}
	}

	if !interestedObjectFound {
		return ""
	}

	klusterlets, err := k.klusterletLister.List(labels.Everything())
	if err != nil {
		return ""
	}

	for _, klusterlet := range klusterlets {
		klusterletNS := klusterlet.Spec.Namespace
		if klusterletNS == "" {
			klusterletNS = klusterletNamespace
		}
		if namespace == klusterletNS {
			return klusterlet.Name
		}

		return ""
	}

	return ""
}
