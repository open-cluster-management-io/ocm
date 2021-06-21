package statuscontroller

import (
	"context"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	appslister "k8s.io/client-go/listers/apps/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"
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
	klusterletRegistration         = "Registration"
	klusterletWork                 = "Work"
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
		WithInformersQueueKeyFunc(helpers.KlusterletSecretQueueKeyFunc(controller.klusterletLister), secretInformer.Informer()).
		WithInformersQueueKeyFunc(helpers.KlusterletDeploymentQueueKeyFunc(controller.klusterletLister), deploymentInformer.Informer()).
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

	registrationDegradedCondition := checkAgentDegradedCondition(
		ctx, k.kubeClient,
		klusterletRegistration, klusterletRegistrationDegraded,
		klusterletAgent{
			clusterName:    klusterlet.Spec.ClusterName,
			deploymentName: fmt.Sprintf("%s-registration-agent", klusterlet.Name),
			namespace:      klusterletNS,
			getSSARFunc:    getRegistrationSelfSubjectAccessReviews,
		},
		[]degradedCheckFunc{checkBootstrapSecret, checkHubConfigSecret, checkAgentDeployment},
	)
	workDegradedCondition := checkAgentDegradedCondition(
		ctx, k.kubeClient,
		klusterletWork, klusterletWorKDegraded,
		klusterletAgent{
			clusterName:    klusterlet.Spec.ClusterName,
			deploymentName: fmt.Sprintf("%s-work-agent", klusterlet.Name),
			namespace:      klusterletNS,
			getSSARFunc:    getWorkSelfSubjectAccessReviews,
		},
		[]degradedCheckFunc{checkHubConfigSecret, checkAgentDeployment},
	)

	_, _, err = helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
		helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		helpers.UpdateKlusterletConditionFn(workDegradedCondition),
	)
	return err
}

type klusterletAgent struct {
	clusterName    string
	deploymentName string
	namespace      string
	getSSARFunc    getSelfSubjectAccessReviewsFunc
}

func checkAgentDegradedCondition(
	ctx context.Context, kubeClient kubernetes.Interface,
	agentName, degradedType string,
	agent klusterletAgent,
	degradedCheckFns []degradedCheckFunc) metav1.Condition {
	degradedConditionReasons := []string{}
	degradedConditionMessages := []string{}
	for _, degradedCheckFn := range degradedCheckFns {
		currCond := degradedCheckFn(ctx, kubeClient, agent)
		if currCond == nil {
			continue
		}
		degradedConditionReasons = append(degradedConditionReasons, currCond.Reason)
		degradedConditionMessages = append(degradedConditionMessages, currCond.Message)
	}

	if len(degradedConditionReasons) == 0 {
		return metav1.Condition{
			Type:    degradedType,
			Status:  metav1.ConditionFalse,
			Reason:  fmt.Sprintf("%sFunctional", agentName),
			Message: fmt.Sprintf("%s is functioning correctly", agentName),
		}
	}

	return metav1.Condition{
		Type:    degradedType,
		Status:  metav1.ConditionTrue,
		Reason:  strings.Join(degradedConditionReasons, ","),
		Message: strings.Join(degradedConditionMessages, "\n"),
	}
}

type degradedCheckFunc func(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) *metav1.Condition

// Check bootstrap secret, if the secret is invalid, return registration degraded condition
func checkBootstrapSecret(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) *metav1.Condition {
	// Check if bootstrap secret exists
	bootstrapSecret, err := kubeClient.CoreV1().Secrets(agent.namespace).Get(ctx, helpers.BootstrapHubKubeConfig, metav1.GetOptions{})
	if err != nil {
		return &metav1.Condition{
			Reason:  "BootstrapSecretMissing",
			Message: fmt.Sprintf("Failed to get bootstrap secret %q %q: %v", agent.namespace, helpers.BootstrapHubKubeConfig, err),
		}
	}

	// Check if bootstrap secret works by building kube client
	bootstrapClient, err := buildKubeClientWithSecret(bootstrapSecret)
	if err != nil {
		return &metav1.Condition{
			Reason: "BootstrapSecretError",
			Message: fmt.Sprintf("Failed to build bootstrap kube client with bootstrap secret %q %q: %v",
				agent.namespace, helpers.BootstrapHubKubeConfig, err),
		}
	}

	// Check the bootstrap client permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err := createSelfSubjectAccessReviews(ctx, bootstrapClient, getBootstrapSelfSubjectAccessReviews())
	if err != nil {
		return &metav1.Condition{
			Reason: "BootstrapSecretError",
			Message: fmt.Sprintf("Failed to create %+v with bootstrap secret %q %q: %v",
				failedReview, agent.namespace, helpers.BootstrapHubKubeConfig, err),
		}
	}
	if !allowed {
		return &metav1.Condition{
			Reason: "BootstrapSecretUnauthorized",
			Message: fmt.Sprintf("Operation for resource %+v is not allowed with bootstrap secret %q %q",
				failedReview.Spec.ResourceAttributes, agent.namespace, helpers.BootstrapHubKubeConfig),
		}
	}

	return nil
}

// Check hub-kubeconfig-secret, if the secret is invalid, return degraded condition
func checkHubConfigSecret(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) *metav1.Condition {
	hubConfigSecret, err := kubeClient.CoreV1().Secrets(agent.namespace).Get(ctx, helpers.HubKubeConfig, metav1.GetOptions{})
	if err != nil {
		return &metav1.Condition{
			Reason:  "HubKubeConfigSecretMissing",
			Message: fmt.Sprintf("Failed to get hub kubeconfig secret %q %q: %v", agent.namespace, helpers.HubKubeConfig, err),
		}
	}

	if hubConfigSecret.Data["kubeconfig"] == nil {
		return &metav1.Condition{
			Reason: "HubKubeConfigMissing",
			Message: fmt.Sprintf("Failed to get kubeconfig from `kubectl get secret -n %q %q -ojsonpath='{.data.kubeconfig}'`. "+
				"This is set by the klusterlet registration deployment, but the CSR must be approved by the cluster-admin on the hub.",
				hubConfigSecret.Namespace, hubConfigSecret.Name),
		}
	}

	hubClient, err := buildKubeClientWithSecret(hubConfigSecret)
	if err != nil {
		return &metav1.Condition{
			Reason: "HubKubeConfigError",
			Message: fmt.Sprintf("Failed to build hub kube client with hub config secret %q %q: %v",
				hubConfigSecret.Namespace, hubConfigSecret.Name, err),
		}
	}

	clusterName := agent.clusterName
	// If cluster name is empty, read cluster name from hub config secret
	if clusterName == "" {
		if hubConfigSecret.Data["cluster-name"] == nil {
			return &metav1.Condition{
				Reason: "ClusterNameMissing",
				Message: fmt.Sprintf(
					"Failed to get cluster name from `kubectl get secret -n %q %q -ojsonpath='{.data.cluster-name}`."+
						" This is set by the klusterlet registration deployment.", hubConfigSecret.Namespace, hubConfigSecret.Name),
			}
		}
		clusterName = string(hubConfigSecret.Data["cluster-name"])
	}

	// Check the hub kubeconfig permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err := createSelfSubjectAccessReviews(ctx, hubClient, agent.getSSARFunc(agent.clusterName))
	if err != nil {
		return &metav1.Condition{
			Reason: "HubKubeConfigError",
			Message: fmt.Sprintf("Failed to create %+v with hub config secret %q %q: %v",
				failedReview, hubConfigSecret.Namespace, hubConfigSecret.Name, err),
		}
	}
	if !allowed {
		return &metav1.Condition{
			Reason: "HubKubeConfigUnauthorized",
			Message: fmt.Sprintf("Operation for resource %+v is not allowed with hub config secret %q %q",
				failedReview.Spec.ResourceAttributes, hubConfigSecret.Namespace, hubConfigSecret.Name),
		}
	}

	return nil
}

// Check agent deployment, if the desired replicas is not equal to available replicas, return degraded condition
func checkAgentDeployment(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) *metav1.Condition {
	deployment, err := kubeClient.AppsV1().Deployments(agent.namespace).Get(ctx, agent.deploymentName, metav1.GetOptions{})
	if err != nil {
		return &metav1.Condition{
			Reason:  "GetDeploymentFailed",
			Message: fmt.Sprintf("Failed to get deployment %q %q: %v", agent.namespace, agent.deploymentName, err),
		}
	}
	if unavailablePod := helpers.NumOfUnavailablePod(deployment); unavailablePod > 0 {
		return &metav1.Condition{
			Reason: "UnavailablePods",
			Message: fmt.Sprintf("%v of requested instances are unavailable of deployment %q %q",
				unavailablePod, agent.namespace, agent.deploymentName),
		}
	}
	return nil
}

func buildKubeClientWithSecret(secret *corev1.Secret) (kubernetes.Interface, error) {
	restConfig, err := helpers.LoadClientConfigFromSecret(secret)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(restConfig)
}

func createSelfSubjectAccessReviews(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	selfSubjectAccessReviews []authorizationv1.SelfSubjectAccessReview) (bool, *authorizationv1.SelfSubjectAccessReview, error) {
	for i := range selfSubjectAccessReviews {
		subjectAccessReview := selfSubjectAccessReviews[i]
		ssar, err := kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &subjectAccessReview, metav1.CreateOptions{})
		if err != nil {
			return false, &subjectAccessReview, err
		}
		if !ssar.Status.Allowed {
			return false, &subjectAccessReview, nil
		}
	}
	return true, nil, nil
}

func getBootstrapSelfSubjectAccessReviews() []authorizationv1.SelfSubjectAccessReview {
	reviews := []authorizationv1.SelfSubjectAccessReview{}
	clusterResource := authorizationv1.ResourceAttributes{
		Group:    "cluster.open-cluster-management.io",
		Resource: "managedclusters",
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterResource, "create", "get")...)

	certResource := authorizationv1.ResourceAttributes{
		Group:    "certificates.k8s.io",
		Resource: "certificatesigningrequests",
	}
	return append(reviews, generateSelfSubjectAccessReviews(certResource, "create", "get", "list", "watch")...)
}

type getSelfSubjectAccessReviewsFunc func(string) []authorizationv1.SelfSubjectAccessReview

func getRegistrationSelfSubjectAccessReviews(clusterName string) []authorizationv1.SelfSubjectAccessReview {
	reviews := []authorizationv1.SelfSubjectAccessReview{}
	certResource := authorizationv1.ResourceAttributes{
		Group:    "certificates.k8s.io",
		Resource: "certificatesigningrequests",
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(certResource, "get", "list", "watch")...)

	clusterResource := authorizationv1.ResourceAttributes{
		Group:    "cluster.open-cluster-management.io",
		Resource: "managedclusters",
		Name:     clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterResource, "get", "list", "update", "watch")...)

	clusterStatusResource := authorizationv1.ResourceAttributes{
		Group:       "cluster.open-cluster-management.io",
		Resource:    "managedclusters",
		Subresource: "status",
		Name:        clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterStatusResource, "patch", "update")...)

	clusterCertResource := authorizationv1.ResourceAttributes{
		Group:       "register.open-cluster-management.io",
		Resource:    "managedclusters",
		Subresource: "clientcertificates",
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(clusterCertResource, "renew")...)

	leaseResource := authorizationv1.ResourceAttributes{
		Group:     "coordination.k8s.io",
		Resource:  "leases",
		Name:      fmt.Sprintf("cluster-lease-%s", clusterName),
		Namespace: clusterName,
	}
	return append(reviews, generateSelfSubjectAccessReviews(leaseResource, "get", "update")...)
}

func getWorkSelfSubjectAccessReviews(clusterName string) []authorizationv1.SelfSubjectAccessReview {
	reviews := []authorizationv1.SelfSubjectAccessReview{}
	eventResource := authorizationv1.ResourceAttributes{
		Resource:  "events",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(eventResource, "create", "patch", "update")...)

	eventResource = authorizationv1.ResourceAttributes{
		Group:     "events.k8s.io",
		Resource:  "events",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(eventResource, "create", "patch", "update")...)

	workResource := authorizationv1.ResourceAttributes{
		Group:     "work.open-cluster-management.io",
		Resource:  "manifestworks",
		Namespace: clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(workResource, "get", "list", "watch", "update")...)

	workStatusResource := authorizationv1.ResourceAttributes{
		Group:       "work.open-cluster-management.io",
		Resource:    "manifestworks",
		Subresource: "status",
		Namespace:   clusterName,
	}
	reviews = append(reviews, generateSelfSubjectAccessReviews(workStatusResource, "patch", "update")...)
	return reviews
}

func generateSelfSubjectAccessReviews(resource authorizationv1.ResourceAttributes, verbs ...string) []authorizationv1.SelfSubjectAccessReview {
	reviews := []authorizationv1.SelfSubjectAccessReview{}
	for _, verb := range verbs {
		reviews = append(reviews, authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:       resource.Group,
					Resource:    resource.Resource,
					Subresource: resource.Subresource,
					Name:        resource.Name,
					Namespace:   resource.Namespace,
					Verb:        verb,
				},
			},
		})
	}
	return reviews
}
