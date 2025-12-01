package ssarcontroller

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// SSARReSyncTime is exposed so that integration tests can crank up the controller sync speed.
var SSARReSyncTime = 30 * time.Second

type ssarController struct {
	kubeClient       kubernetes.Interface
	secretInformers  map[string]coreinformer.SecretInformer
	patcher          patcher.Patcher[*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus]
	klusterletLister operatorlister.KlusterletLister
	*klusterletLocker
}

type klusterletLocker struct {
	sync.RWMutex
	klusterletInChecking map[string]struct{}
}

func NewKlusterletSSARController(
	kubeClient kubernetes.Interface,
	klusterletClient operatorv1client.KlusterletInterface,
	klusterletInformer operatorinformer.KlusterletInformer,
	secretInformers map[string]coreinformer.SecretInformer,
) factory.Controller {
	controller := &ssarController{
		kubeClient: kubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](klusterletClient),
		klusterletLister: klusterletInformer.Lister(),
		secretInformers:  secretInformers,
		klusterletLocker: &klusterletLocker{
			klusterletInChecking: make(map[string]struct{}),
		},
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeysFunc(helpers.KlusterletSecretQueueKeyFunc(controller.klusterletLister),
			secretInformers[helpers.HubKubeConfig].Informer(),
			secretInformers[helpers.BootstrapHubKubeConfig].Informer(),
			secretInformers[helpers.ExternalManagedKubeConfig].Informer()).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, klusterletInformer.Informer()).
		ToController("KlusterletSSARController")
}

func (l *klusterletLocker) inSSARChecking(klusterletName string) bool {
	l.RLock()
	defer l.RUnlock()
	_, ok := l.klusterletInChecking[klusterletName]
	return ok
}

func (l *klusterletLocker) addSSARChecking(klusterletName string) {
	l.Lock()
	defer l.Unlock()
	l.klusterletInChecking[klusterletName] = struct{}{}
}

func (l *klusterletLocker) deleteSSARChecking(klusterletName string) {
	l.Lock()
	defer l.Unlock()
	delete(l.klusterletInChecking, klusterletName)
}

func (c *ssarController) sync(ctx context.Context, controllerContext factory.SyncContext, klusterletName string) error {
	logger := klog.FromContext(ctx)

	if klusterletName == "" {
		return nil
	}
	klusterlet, err := c.klusterletLister.Get(klusterletName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}
	klusterlet = klusterlet.DeepCopy()

	// if the ssar checking is already processing, requeue it after 30s.
	if c.inSSARChecking(klusterletName) {
		logger.V(4).Info("Reconciling Klusterlet is already processing now", "name", klusterletName)
		controllerContext.Queue().AddAfter(klusterletName, SSARReSyncTime)
		return nil
	}

	c.addSSARChecking(klusterletName)
	go func(klusterlet *operatorapiv1.Klusterlet) {
		defer c.deleteSSARChecking(klusterletName)

		newKlusterlet := klusterlet.DeepCopy()

		logger.V(4).Info("Checking hub kubeconfig for klusterlet", "name", klusterletName)
		agentNamespace := helpers.AgentNamespace(klusterlet)

		hubConfigDegradedCondition := checkAgentDegradedCondition(
			ctx, c.kubeClient,
			operatorapiv1.ConditionHubConnectionDegraded,
			klusterletAgent{
				clusterName: klusterlet.Spec.ClusterName,
				namespace:   agentNamespace,
			},
			klusterlet.Generation,
			checkHubConfigSecret,
		)

		// the hub kubeconfig is functional, the bootstrap kubeconfig check is not needed,
		// ignore it to avoid sending additional sar requests
		if hubConfigDegradedCondition.Status == metav1.ConditionFalse {
			meta.SetStatusCondition(&newKlusterlet.Status.Conditions, hubConfigDegradedCondition)
			_, err := c.patcher.PatchStatus(ctx, newKlusterlet, newKlusterlet.Status, klusterlet.Status)
			if err != nil {
				logger.Error(err, "Update Klusterlet Status Failed")
				controllerContext.Queue().AddAfter(klusterletName, SSARReSyncTime)
			}

			return
		}

		// If the MultipleHubs feature is enabled, there is no need to check the bootstrap kubeconfig secret.
		// The ssar check to bootstrapkubeconfigs is done at the start of the registration agent.
		// TODO: @xuezhaojun Need to find a way to reflect the connectivity status of the bootstrapkubeconfig secrets to the klusterlet status.
		multiplehubsEnabled := klusterlet.Spec.RegistrationConfiguration != nil &&
			helpers.FeatureGateEnabled(klusterlet.Spec.RegistrationConfiguration.FeatureGates, ocmfeature.DefaultSpokeRegistrationFeatureGates, ocmfeature.MultipleHubs)
		if !multiplehubsEnabled {
			bootstrapDegradedCondition := checkAgentDegradedCondition(
				ctx, c.kubeClient,
				operatorapiv1.ConditionHubConnectionDegraded,
				klusterletAgent{
					clusterName: klusterlet.Spec.ClusterName,
					namespace:   agentNamespace,
				},
				klusterlet.Generation,
				checkBootstrapSecret,
			)

			// The status is always true here since the hub kubeconfig check fails. Need to add the additional
			// message relating to bootstrap kubeconfig check here.
			meta.SetStatusCondition(&newKlusterlet.Status.Conditions, metav1.Condition{
				Type:               operatorapiv1.ConditionHubConnectionDegraded,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: klusterlet.Generation,
				Reason:             bootstrapDegradedCondition.Reason + "," + hubConfigDegradedCondition.Reason,
				Message:            bootstrapDegradedCondition.Message + "\n" + hubConfigDegradedCondition.Message,
			})
			_, err := c.patcher.PatchStatus(ctx, newKlusterlet, newKlusterlet.Status, klusterlet.Status)
			if err != nil {
				logger.Error(err, "Update Klusterlet Status Failed")
				controllerContext.Queue().AddAfter(klusterletName, SSARReSyncTime)
			}

			// We need to requeue when only hubKubeConfigSecret is not authorized, since we do not know whether managercluster
			// is accepted or permission is created.
			if hubConfigDegradedCondition.Reason == operatorapiv1.ReasonHubKubeConfigUnauthorized &&
				bootstrapDegradedCondition.Reason == "BootstrapSecretFunctional" {
				controllerContext.Queue().AddAfter(klusterletName, SSARReSyncTime)
			}
		}
	}(klusterlet)

	return nil
}

type klusterletAgent struct {
	clusterName string
	namespace   string
}

func checkAgentDegradedCondition(
	ctx context.Context, kubeClient kubernetes.Interface,
	degradedType string,
	agent klusterletAgent,
	generation int64,
	degradedCheckFn degradedCheckFunc) metav1.Condition {
	currCond := degradedCheckFn(ctx, kubeClient, agent)
	currCond.Type = degradedType
	currCond.ObservedGeneration = generation
	return currCond
}

type degradedCheckFunc func(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) metav1.Condition

// Check bootstrap secret, if the secret is invalid, return registration degraded condition
func checkBootstrapSecret(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) metav1.Condition {
	// Check if bootstrap secret exists
	bootstrapSecret, err := kubeClient.CoreV1().Secrets(agent.namespace).Get(ctx, helpers.BootstrapHubKubeConfig, metav1.GetOptions{})
	if err != nil {
		return metav1.Condition{
			Status:  metav1.ConditionTrue,
			Reason:  "BootstrapSecretMissing",
			Message: fmt.Sprintf("Failed to get bootstrap secret %q %q: %v", agent.namespace, helpers.BootstrapHubKubeConfig, err),
		}
	}

	// Check if bootstrap secret works by building kube client
	bootstrapClient, host, err := buildKubeClientWithSecret(bootstrapSecret)
	if err != nil {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: "BootstrapSecretError",
			Message: fmt.Sprintf("Failed to build bootstrap kube client with bootstrap secret %q/%q to apiserver %s: %v",
				agent.namespace, helpers.BootstrapHubKubeConfig, host, err),
		}
	}

	// Check the bootstrap client permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err := commonhelpers.CreateSelfSubjectAccessReviews(ctx, bootstrapClient, commonhelpers.GetBootstrapSSARs())
	if err != nil {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: "BootstrapSecretError",
			Message: fmt.Sprintf("Failed to create %+v with bootstrap secret %q %q: %v",
				failedReview, agent.namespace, helpers.BootstrapHubKubeConfig, err),
		}
	}
	if !allowed {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: "BootstrapSecretUnauthorized",
			Message: fmt.Sprintf("Operation for resource %+v is not allowed with bootstrap secret %q/%q to apiserver %s",
				failedReview.Spec.ResourceAttributes, agent.namespace, helpers.BootstrapHubKubeConfig, host),
		}
	}

	return metav1.Condition{
		Status: metav1.ConditionFalse,
		Reason: "BootstrapSecretFunctional",
		Message: fmt.Sprintf("Bootstrap secret %s/%s to apiserver %s is configured correctly",
			agent.namespace, helpers.BootstrapHubKubeConfig, host),
	}
}

// Check hub-kubeconfig-secret, if the secret is invalid, return degraded condition
func checkHubConfigSecret(ctx context.Context, kubeClient kubernetes.Interface, agent klusterletAgent) metav1.Condition {
	hubConfigSecret, err := kubeClient.CoreV1().Secrets(agent.namespace).Get(ctx, helpers.HubKubeConfig, metav1.GetOptions{})
	if err != nil {
		return metav1.Condition{
			Status:  metav1.ConditionTrue,
			Reason:  operatorapiv1.ReasonHubKubeConfigSecretMissing,
			Message: fmt.Sprintf("Failed to get hub kubeconfig secret %q %q: %v", agent.namespace, helpers.HubKubeConfig, err),
		}
	}

	if hubConfigSecret.Data["kubeconfig"] == nil {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonHubKubeConfigMissing,
			Message: fmt.Sprintf("Failed to get kubeconfig from `kubectl get secret -n %q %q -ojsonpath='{.data.kubeconfig}'`. "+
				"This is set by the klusterlet registration deployment, but the CSR must be approved by the cluster-admin on the hub.",
				hubConfigSecret.Namespace, hubConfigSecret.Name),
		}
	}

	hubClient, host, err := buildKubeClientWithSecret(hubConfigSecret)
	if err != nil {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonHubKubeConfigError,
			Message: fmt.Sprintf("Failed to build hub kube client with hub config secret %q %q: %v",
				hubConfigSecret.Namespace, hubConfigSecret.Name, err),
		}
	}

	clusterName := agent.clusterName
	// If cluster name is empty, read cluster name from hub config secret
	if clusterName == "" {
		if hubConfigSecret.Data["cluster-name"] == nil {
			return metav1.Condition{
				Status: metav1.ConditionTrue,
				Reason: operatorapiv1.ReasonClusterNameMissing,
				Message: fmt.Sprintf(
					"Failed to get cluster name from `kubectl get secret -n %q %q -ojsonpath='{.data.cluster-name}'`."+
						" This is set by the klusterlet registration deployment.", hubConfigSecret.Namespace, hubConfigSecret.Name),
			}
		}
		clusterName = string(hubConfigSecret.Data["cluster-name"])
	}

	// Check the hub kubeconfig permissions by creating SelfSubjectAccessReviews
	allowed, failedReview, err := commonhelpers.CreateSelfSubjectAccessReviews(ctx, hubClient,
		commonhelpers.GetHubConfigSSARs(clusterName))
	if err != nil {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonHubKubeConfigError,
			Message: fmt.Sprintf("Failed to create %+v with hub config secret %q/%q to apiserver %s: %v",
				failedReview, hubConfigSecret.Namespace, hubConfigSecret.Name, host, err),
		}
	}
	if !allowed {
		return metav1.Condition{
			Status: metav1.ConditionTrue,
			Reason: operatorapiv1.ReasonHubKubeConfigUnauthorized,
			Message: fmt.Sprintf("Operation for resource %+v is not allowed with hub config secret %q/%q to apiserver %s",
				failedReview.Spec.ResourceAttributes, hubConfigSecret.Namespace, hubConfigSecret.Name, host),
		}
	}

	return metav1.Condition{
		Status: metav1.ConditionFalse,
		Reason: operatorapiv1.ReasonHubConnectionFunctional,
		Message: fmt.Sprintf("Hub kubeconfig secret %s/%s to apiserver %s is working",
			agent.namespace, helpers.HubKubeConfig, host),
	}
}

func buildKubeClientWithSecret(secret *corev1.Secret) (kubernetes.Interface, string, error) {
	restConfig, err := helpers.LoadClientConfigFromSecret(secret)
	if err != nil {
		return nil, "", err
	}

	// reduce qps and burst of client, because too many managed clusters registration on hub and send ssar requests at once could cause resource pressure
	restConfig.QPS = 2
	restConfig.Burst = 5

	// TODO(@Promacanthus): When server field is a domain name in kubeconfig, such as https://xxx.yyy.zzz, and there is no such hostname in the DNS,
	// we need to set the hostAliases for the hub Api server in klusterlet cr and .pod.spec.hostAliases.
	// The hostAlias configuration will be used by klusterlet operator, registration agent and work agent to communicate with hub api server.
	// Setup this manually is not a good idea, so we can set a custom dialer in rest.Config and use the ip address to setup connection.
	//
	// For example:
	//
	// restConfig.Dial = func(_ context.Context, network, _ string) (net.Conn, error) {
	// 	return net.Dial(network, "this is the hub api server ip address")
	// }

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, "", err
	}

	return client, restConfig.Host, nil
}
