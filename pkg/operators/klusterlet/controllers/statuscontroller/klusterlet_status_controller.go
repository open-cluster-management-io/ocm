package statuscontroller

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"

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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

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
	bootstrapSecret, err := k.kubeClient.CoreV1().Secrets(klusterletNS).Get(ctx, helpers.BootstrapHubKubeConfigSecret, metav1.GetOptions{})
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to get bootstrap secret %q %q: %v", klusterletNS, helpers.BootstrapHubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "BootStrapSecretMissing"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}

	// Check if bootstrap secret works by building kube client
	bootstrapClient, err := buildKubeClientWithSecret(bootstrapSecret)
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to build kube client with bootstrap secret %q %q: %v", klusterletNS, helpers.BootstrapHubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "BootstrapSecretError"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}

	// Check the bootstrap client permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err := createSelfSubjectAccessReviews(ctx, bootstrapClient, getBootstrapSelfSubjectAccessReviews())
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to create %+v with bootstrap secret %q %q: %v", failedReview, klusterletNS, helpers.BootstrapHubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "BootstrapSecretError"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}
	if !allowed {
		registrationDegradedCondition.Message = fmt.Sprintf("Operation for resource %+v is not allowed with bootstrap secret %q %q", failedReview.Spec.ResourceAttributes, klusterletNS, helpers.BootstrapHubKubeConfigSecret)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "BootstrapSecretUnauthorized"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}

	// Check if hub kubeconfig secret exists
	hubConfigSecret, err := k.kubeClient.CoreV1().Secrets(klusterletNS).Get(ctx, helpers.HubKubeConfigSecret, metav1.GetOptions{})
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to get hub kubeconfig secret %q %q: %v", klusterletNS, helpers.HubKubeConfigSecret, err)
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
	clusterName := klusterlet.Spec.ClusterName
	if clusterName == "" {
		if hubConfigSecret.Data["cluster-name"] == nil {
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
		clusterName = string(hubConfigSecret.Data["cluster-name"])
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

	// Check if hub config secret works by building kube client with its kubeconfig
	hubClient, err := buildKubeClientWithSecret(hubConfigSecret)
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to build kube client with hub config secret %q %q: %v", hubConfigSecret.Namespace, hubConfigSecret.Name, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "HubConfigSecretError"
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

	// Check the hub client (registration and work) permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err = createSelfSubjectAccessReviews(ctx, hubClient, getRegistrationSelfSubjectAccessReviews(clusterName))
	if err != nil {
		registrationDegradedCondition.Message = fmt.Sprintf("Failed to create %+v with hub config secret %q %q: %v", failedReview, klusterletNS, helpers.BootstrapHubKubeConfigSecret, err)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "HubConfigSecretError"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}
	if !allowed {
		registrationDegradedCondition.Message = fmt.Sprintf("Operation for resource %+v is not allowed with hub config secret %q %q", failedReview.Spec.ResourceAttributes, klusterletNS, helpers.BootstrapHubKubeConfigSecret)
		registrationDegradedCondition.Status = metav1.ConditionTrue
		registrationDegradedCondition.Reason = "HubConfigSecretUnauthorized"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(registrationDegradedCondition),
		)
		return err
	}

	allowed, failedReview, err = createSelfSubjectAccessReviews(ctx, hubClient, getWorkSelfSubjectAccessReviews(clusterName))
	if err != nil {
		workDegradedCondition.Message = fmt.Sprintf("Failed to create %+v with hub config secret %q %q: %v", failedReview, klusterletNS, helpers.BootstrapHubKubeConfigSecret, err)
		workDegradedCondition.Status = metav1.ConditionTrue
		workDegradedCondition.Reason = "HubConfigSecretError"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(workDegradedCondition),
		)
		return err
	}
	if !allowed {
		workDegradedCondition.Message = fmt.Sprintf("Operation for resource %+v is not allowed with hub config secret %q %q", failedReview.Spec.ResourceAttributes, klusterletNS, helpers.BootstrapHubKubeConfigSecret)
		workDegradedCondition.Status = metav1.ConditionTrue
		workDegradedCondition.Reason = "HubConfigSecretUnauthorized"
		_, _, err := helpers.UpdateKlusterletStatus(ctx, k.klusterletClient, klusterletName,
			helpers.UpdateKlusterletConditionFn(workDegradedCondition),
		)
		return err
	}

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

func buildKubeClientWithSecret(secret *corev1.Secret) (kubernetes.Interface, error) {
	tempdir, err := ioutil.TempDir("", "kube")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempdir)

	for key, data := range secret.Data {
		if err := ioutil.WriteFile(path.Join(tempdir, key), data, 0644); err != nil {
			return nil, err
		}
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", path.Join(tempdir, "kubeconfig"))
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
