package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	authv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/pointer"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/ocm/test/integration/util"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/onsi/gomega"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	certificatesv1 "k8s.io/api/certificates/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/ocm/pkg/registration-operator/helpers"
)

type Tester struct {
	hubKubeConfigPath       string
	spokeKubeConfigPath     string
	HubKubeClient           kubernetes.Interface
	SpokeKubeClient         kubernetes.Interface
	HubAPIExtensionClient   apiextensionsclient.Interface
	HubClusterCfg           *rest.Config
	SpokeClusterCfg         *rest.Config
	OperatorClient          operatorclient.Interface
	ClusterClient           clusterclient.Interface
	HubWorkClient           workv1client.Interface
	SpokeWorkClient         workv1client.Interface
	AddOnClinet             addonclient.Interface
	SpokeDynamicClient      dynamic.Interface
	bootstrapHubSecret      *corev1.Secret
	EventuallyTimeout       time.Duration
	EventuallyInterval      time.Duration
	clusterManagerName      string
	clusterManagerNamespace string
	operatorNamespace       string
	klusterletOperator      string
}

// kubeconfigPath is the path of kubeconfig file, will be get from env "KUBECONFIG" by default.
// bootstrapHubSecret is the bootstrap hub kubeconfig secret, and the format is "namespace/secretName".
// Default of bootstrapHubSecret is helpers.KlusterletDefaultNamespace/helpers.BootstrapHubKubeConfig.
func NewTester(hubKubeConfigPath, spokeKubeConfigPath string, timeout time.Duration) *Tester {
	var tester = Tester{
		hubKubeConfigPath:       hubKubeConfigPath,
		spokeKubeConfigPath:     spokeKubeConfigPath,
		EventuallyTimeout:       timeout,           // seconds
		EventuallyInterval:      1 * time.Second,   // seconds
		clusterManagerName:      "cluster-manager", // same name as deploy/cluster-manager/config/samples
		clusterManagerNamespace: helpers.ClusterManagerDefaultNamespace,
		operatorNamespace:       "open-cluster-management",
		klusterletOperator:      "klusterlet",
	}

	return &tester
}

func (t *Tester) Init() error {
	var err error

	if t.hubKubeConfigPath == "" {
		t.hubKubeConfigPath = os.Getenv("KUBECONFIG")
	}
	if t.spokeKubeConfigPath == "" {
		t.spokeKubeConfigPath = os.Getenv("KUBECONFIG")
	}

	if t.HubClusterCfg, err = clientcmd.BuildConfigFromFlags("", t.hubKubeConfigPath); err != nil {
		klog.Errorf("failed to get HubClusterCfg from path %v . %v", t.hubKubeConfigPath, err)
		return err
	}
	if t.SpokeClusterCfg, err = clientcmd.BuildConfigFromFlags("", t.spokeKubeConfigPath); err != nil {
		klog.Errorf("failed to get SpokeClusterCfg from path %v . %v", t.spokeKubeConfigPath, err)
		return err
	}

	if t.HubKubeClient, err = kubernetes.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get KubeClient. %v", err)
		return err
	}
	if t.SpokeKubeClient, err = kubernetes.NewForConfig(t.SpokeClusterCfg); err != nil {
		klog.Errorf("failed to get KubeClient. %v", err)
		return err
	}

	if t.SpokeDynamicClient, err = dynamic.NewForConfig(t.SpokeClusterCfg); err != nil {
		klog.Errorf("failed to get DynamicClient. %v", err)
		return err
	}

	if t.HubAPIExtensionClient, err = apiextensionsclient.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get HubApiExtensionClient. %v", err)
		return err
	}
	if t.OperatorClient, err = operatorclient.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get OperatorClient. %v", err)
		return err
	}
	if t.ClusterClient, err = clusterclient.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get ClusterClient. %v", err)
		return err
	}
	if t.HubWorkClient, err = workv1client.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get WorkClient. %v", err)
		return err
	}
	if t.SpokeWorkClient, err = workv1client.NewForConfig(t.SpokeClusterCfg); err != nil {
		klog.Errorf("failed to get WorkClient. %v", err)
		return err
	}
	if t.AddOnClinet, err = addonclient.NewForConfig(t.HubClusterCfg); err != nil {
		klog.Errorf("failed to get AddOnClinet. %v", err)
		return err
	}

	return nil
}

func (t *Tester) SetEventuallyTimeout(timeout time.Duration) *Tester {
	t.EventuallyInterval = timeout
	return t
}

func (t *Tester) SetEventuallyInterval(timeout time.Duration) *Tester {
	t.EventuallyTimeout = timeout
	return t
}

func (t *Tester) SetOperatorNamespace(ns string) *Tester {
	t.operatorNamespace = ns
	return t
}

func (t *Tester) SetBootstrapHubSecret(bootstrapHubSecret string) error {
	var err error
	var bootstrapHubSecretName = helpers.BootstrapHubKubeConfig
	var bootstrapHubSecretNamespace = helpers.KlusterletDefaultNamespace
	if bootstrapHubSecret != "" {
		bootstrapHubSecretNamespace, bootstrapHubSecretName, err = cache.SplitMetaNamespaceKey(bootstrapHubSecret)
		if err != nil {
			klog.Errorf("the format of bootstrapHubSecret %v is invalid. %v", bootstrapHubSecret, err)
			return err
		}
	}
	if t.bootstrapHubSecret, err = t.SpokeKubeClient.CoreV1().Secrets(bootstrapHubSecretNamespace).
		Get(context.TODO(), bootstrapHubSecretName, metav1.GetOptions{}); err != nil {
		klog.Errorf("failed to get bootstrapHubSecret %v in ns %v. %v", bootstrapHubSecretName,
			bootstrapHubSecretNamespace, err)
		return err
	}
	t.bootstrapHubSecret.ObjectMeta.ResourceVersion = ""
	t.bootstrapHubSecret.ObjectMeta.Namespace = ""
	return nil
}

func (t *Tester) CreateKlusterlet(name, clusterName, klusterletNamespace string, mode operatorapiv1.InstallMode) (*operatorapiv1.Klusterlet, error) {
	if name == "" {
		return nil, fmt.Errorf("the name should not be null")
	}
	if klusterletNamespace == "" {
		klusterletNamespace = helpers.KlusterletDefaultNamespace
	}

	var klusterlet = &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration:latest",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work:latest",
			ExternalServerURLs: []operatorapiv1.ServerURL{
				{
					URL: "https://localhost",
				},
			},
			ClusterName: clusterName,
			Namespace:   klusterletNamespace,
			DeployOption: operatorapiv1.KlusterletDeployOption{
				Mode: mode,
			},
		},
	}

	agentNamespace := helpers.AgentNamespace(klusterlet)
	klog.Infof("klusterlet: %s/%s, \t mode: %v, \t agent namespace: %s", klusterlet.Name, klusterlet.Namespace, mode, agentNamespace)

	// create agentNamespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNamespace,
			Annotations: map[string]string{
				"workload.openshift.io/allowed": "management",
			},
		},
	}
	if _, err := t.SpokeKubeClient.CoreV1().Namespaces().Get(context.TODO(), agentNamespace, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("failed to get ns %v. %v", agentNamespace, err)
			return nil, err
		}

		if _, err := t.SpokeKubeClient.CoreV1().Namespaces().Create(context.TODO(),
			namespace, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create ns %v. %v", namespace, err)
			return nil, err
		}
	}

	// create bootstrap-hub-kubeconfig secret
	secret := t.bootstrapHubSecret.DeepCopy()
	if _, err := t.SpokeKubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
			return nil, err
		}
		if _, err = t.SpokeKubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create secret %v in ns %v. %v", secret, agentNamespace, err)
			return nil, err
		}
	}

	if mode == operatorapiv1.InstallModeHosted {
		// create external-managed-kubeconfig, will use the same cluster to simulate the Hosted mode.
		secret.Namespace = agentNamespace
		secret.Name = helpers.ExternalManagedKubeConfig
		if _, err := t.HubKubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
				return nil, err
			}
			if _, err = t.HubKubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
				klog.Errorf("failed to create secret %v in ns %v. %v", secret, agentNamespace, err)
				return nil, err
			}
		}
	}

	// create klusterlet CR
	realKlusterlet, err := t.OperatorClient.OperatorV1().Klusterlets().Create(context.TODO(),
		klusterlet, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create klusterlet %v . %v", klusterlet.Name, err)
		return nil, err
	}

	return realKlusterlet, nil
}

func (t *Tester) CreateApprovedKlusterlet(name, clusterName, klusterletNamespace string, mode operatorapiv1.InstallMode) (*operatorapiv1.Klusterlet, error) {
	klusterlet, err := t.CreateKlusterlet(name, clusterName, klusterletNamespace, operatorapiv1.InstallModeDefault)
	if err != nil {
		return nil, err
	}

	gomega.Eventually(func() error {
		_, err = t.GetCreatedManagedCluster(clusterName)
		return err
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

	gomega.Eventually(func() error {
		return t.ApproveCSR(clusterName)
	}, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

	gomega.Eventually(func() error {
		return t.AcceptsClient(clusterName)
	}, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

	gomega.Eventually(func() error {
		return t.CheckManagedClusterStatus(clusterName)
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

	return klusterlet, nil
}

func (t *Tester) CreatePureHostedKlusterlet(name, clusterName string) (*operatorapiv1.Klusterlet, error) {
	if name == "" {
		return nil, fmt.Errorf("the name should not be null")
	}

	var klusterlet = &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration:latest",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work:latest",
			ExternalServerURLs: []operatorapiv1.ServerURL{
				{
					URL: "https://localhost",
				},
			},
			ClusterName: clusterName,
			DeployOption: operatorapiv1.KlusterletDeployOption{
				Mode: operatorapiv1.InstallModeHosted,
			},
		},
	}

	// create klusterlet CR
	realKlusterlet, err := t.OperatorClient.OperatorV1().Klusterlets().Create(context.TODO(),
		klusterlet, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create klusterlet %v . %v", klusterlet.Name, err)
		return nil, err
	}

	return realKlusterlet, nil
}

func (t *Tester) GetCreatedManagedCluster(clusterName string) (*clusterv1.ManagedCluster, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("the name of managedcluster should not be null")
	}

	cluster, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cluster, nil
}

func (t *Tester) ApproveCSR(clusterName string) error {
	var csrs *certificatesv1.CertificateSigningRequestList
	var csrClient = t.HubKubeClient.CertificatesV1().CertificateSigningRequests()
	var err error

	if csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name = %v", clusterName)}); err != nil {
		return err
	}
	if len(csrs.Items) == 0 {
		return fmt.Errorf("there is no csr related cluster %v", clusterName)
	}

	for i := range csrs.Items {
		csr := &csrs.Items[i]
		if csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{}); err != nil {
			return err
		}

		if isCSRInTerminalState(&csr.Status) {
			return nil
		}

		csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
			Type:    certificatesv1.CertificateApproved,
			Status:  corev1.ConditionTrue,
			Reason:  "Approved by E2E",
			Message: "Approved as part of e2e",
		})
		_, err = csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func isCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

func (t *Tester) AcceptsClient(clusterName string) error {
	managedCluster, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
		clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	managedCluster.Spec.HubAcceptsClient = true
	managedCluster.Spec.LeaseDurationSeconds = 5
	_, err = t.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(),
		managedCluster, metav1.UpdateOptions{})
	return err
}

func (t *Tester) CheckManagedClusterStatus(clusterName string) error {
	managedCluster, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
		clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	var okCount = 0
	for _, condition := range managedCluster.Status.Conditions {
		if (condition.Type == clusterv1.ManagedClusterConditionHubAccepted ||
			condition.Type == clusterv1.ManagedClusterConditionJoined ||
			condition.Type == clusterv1.ManagedClusterConditionAvailable) &&
			condition.Status == v1beta1.ConditionTrue {
			okCount++
		}
	}

	if okCount == 3 {
		return nil
	}

	return fmt.Errorf("cluster %s condtions are not ready: %v", clusterName, managedCluster.Status.Conditions)
}

func (t *Tester) CreateWorkOfConfigMap(name, clusterName, configMapName, configMapNamespace string) (*workapiv1.ManifestWork, error) {
	manifest := workapiv1.Manifest{}
	manifest.Object = util.NewConfigmap(configMapNamespace, configMapName, map[string]string{"a": "b"}, []string{})
	manifestWork := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{
					manifest,
				},
			},
		},
	}

	return t.HubWorkClient.WorkV1().ManifestWorks(clusterName).
		Create(context.TODO(), manifestWork, metav1.CreateOptions{})
}

func (t *Tester) checkKlusterletStatus(klusterletName, condType, reason string, status metav1.ConditionStatus) error {
	klusterlet, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cond := meta.FindStatusCondition(klusterlet.Status.Conditions, condType)
	if cond == nil {
		return fmt.Errorf("cannot find condition type %s", condType)
	}

	if cond.Reason != reason {
		return fmt.Errorf("condition reason is not matched, expect %s, got %s", reason, cond.Reason)
	}

	if cond.Status != status {
		return fmt.Errorf("condition status is not matched, expect %s, got %s", status, cond.Status)
	}

	return nil
}

func (t *Tester) cleanManifestWorks(clusterName, workName string) error {
	err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), workName, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	gomega.Eventually(func() bool {
		_, err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
		return errors.IsNotFound(err)
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

	return nil
}

func (t *Tester) cleanKlusterletResources(klusterletName, clusterName string) error {
	if klusterletName == "" {
		return fmt.Errorf("the klusterlet name should not be null")
	}

	// clean the klusterlets
	err := t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	gomega.Eventually(func() bool {
		_, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			klog.Infof("klusterlet %s deleted successfully", klusterletName)
			return true
		}
		if err != nil {
			klog.Infof("get klusterlet %s error: %v", klusterletName, err)
		}
		return false
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

	// clean the managed clusters
	err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	gomega.Eventually(func() bool {
		_, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			klog.Infof("managed cluster %s deleted successfully", clusterName)
			return true
		}
		if err != nil {
			klog.Infof("get managed cluster %s error: %v", klusterletName, err)
		}
		return false
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeTrue())

	return nil
}

func (t *Tester) CheckHubReady() error {
	cm, err := t.checkClusterManagerStatus()
	if err != nil {
		return err
	}
	// make sure open-cluster-management-hub namespace is created
	if _, err := t.HubKubeClient.CoreV1().Namespaces().
		Get(context.TODO(), t.clusterManagerNamespace, metav1.GetOptions{}); err != nil {
		return err
	}

	// make sure hub deployments are created
	hubRegistrationDeployment := fmt.Sprintf("%s-registration-controller", t.clusterManagerName)
	hubRegistrationWebhookDeployment := fmt.Sprintf("%s-registration-webhook", t.clusterManagerName)
	hubWorkWebhookDeployment := fmt.Sprintf("%s-work-webhook", t.clusterManagerName)
	hubWorkControllerDeployment := fmt.Sprintf("%s-work-controller", t.clusterManagerName)
	hubPlacementDeployment := fmt.Sprintf("%s-placement-controller", t.clusterManagerName)
	addonManagerDeployment := fmt.Sprintf("%s-addon-manager-controller", t.clusterManagerName)
	if _, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
		Get(context.TODO(), hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
		return err
	}
	gomega.Eventually(func() error {
		registrationWebhookDeployment, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
			Get(context.TODO(), hubRegistrationWebhookDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		replicas := *registrationWebhookDeployment.Spec.Replicas
		readyReplicas := registrationWebhookDeployment.Status.ReadyReplicas
		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", hubRegistrationWebhookDeployment, replicas, readyReplicas)
		}
		return nil
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		workWebhookDeployment, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
			Get(context.TODO(), hubWorkWebhookDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		replicas := *workWebhookDeployment.Spec.Replicas
		readyReplicas := workWebhookDeployment.Status.ReadyReplicas
		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", hubWorkWebhookDeployment, replicas, readyReplicas)
		}
		return nil
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())

	var hubWorkControllerEnabled, addonManagerControllerEnabled bool
	if cm.Spec.WorkConfiguration != nil {
		hubWorkControllerEnabled = helpers.FeatureGateEnabled(cm.Spec.WorkConfiguration.FeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.ManifestWorkReplicaSet)
	}

	if cm.Spec.AddOnManagerConfiguration != nil {
		addonManagerControllerEnabled = helpers.FeatureGateEnabled(cm.Spec.AddOnManagerConfiguration.FeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates, ocmfeature.AddonManagement)
	}

	if hubWorkControllerEnabled {
		gomega.Eventually(func() error {
			workHubControllerDeployment, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
				Get(context.TODO(), hubWorkControllerDeployment, metav1.GetOptions{})
			if err != nil {
				return err
			}
			replicas := *workHubControllerDeployment.Spec.Replicas
			readyReplicas := workHubControllerDeployment.Status.ReadyReplicas
			if readyReplicas != replicas {
				return fmt.Errorf("deployment %s should have %d but got %d ready replicas", hubWorkControllerDeployment, replicas, readyReplicas)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())
	}

	if _, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
		Get(context.TODO(), hubPlacementDeployment, metav1.GetOptions{}); err != nil {
		return err
	}

	if addonManagerControllerEnabled {
		gomega.Eventually(func() error {
			addonManagerControllerDeployment, err := t.HubKubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
				Get(context.TODO(), addonManagerDeployment, metav1.GetOptions{})
			if err != nil {
				return err
			}
			replicas := *addonManagerControllerDeployment.Spec.Replicas
			readyReplicas := addonManagerControllerDeployment.Status.ReadyReplicas
			if readyReplicas != replicas {
				return fmt.Errorf("deployment %s should have %d but got %d ready replicas", addonManagerDeployment, replicas, readyReplicas)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())
	}
	return nil
}

func (t *Tester) EnableWorkFeature(feature string) error {
	cm, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if cm.Spec.WorkConfiguration == nil {
		cm.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{}
	}

	if len(cm.Spec.WorkConfiguration.FeatureGates) == 0 {
		cm.Spec.WorkConfiguration.FeatureGates = make([]operatorapiv1.FeatureGate, 0)
	}

	for idx, f := range cm.Spec.WorkConfiguration.FeatureGates {
		if f.Feature == feature {
			if f.Mode == operatorapiv1.FeatureGateModeTypeEnable {
				return nil
			}
			cm.Spec.WorkConfiguration.FeatureGates[idx].Mode = operatorapiv1.FeatureGateModeTypeEnable
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
			return err
		}
	}

	featureGate := operatorapiv1.FeatureGate{
		Feature: feature,
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	}

	cm.Spec.WorkConfiguration.FeatureGates = append(cm.Spec.WorkConfiguration.FeatureGates, featureGate)
	_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
	return err
}

func (t *Tester) RemoveWorkFeature(feature string) error {
	clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for indx, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
		if fg.Feature == feature {
			clusterManager.Spec.WorkConfiguration.FeatureGates[indx].Mode = operatorapiv1.FeatureGateModeTypeDisable
			break
		}
	}
	_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
	return err
}

func (t *Tester) checkClusterManagerStatus() (*operatorapiv1.ClusterManager, error) {
	cm, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), t.clusterManagerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if meta.IsStatusConditionFalse(cm.Status.Conditions, "Applied") {
		return nil, fmt.Errorf("components of cluster manager are not all applied")
	}
	if meta.IsStatusConditionFalse(cm.Status.Conditions, "ValidFeatureGates") {
		return nil, fmt.Errorf("feature gates are not all valid")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubRegistrationDegraded") {
		return nil, fmt.Errorf("HubRegistration is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubPlacementDegraded") {
		return nil, fmt.Errorf("HubPlacement is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "Progressing") {
		return nil, fmt.Errorf("ClusterManager is still progressing")
	}

	return cm, nil
}

func (t *Tester) CheckKlusterletOperatorReady() error {
	// make sure klusterlet operator deployment is created
	_, err := t.SpokeKubeClient.AppsV1().Deployments(t.operatorNamespace).
		Get(context.TODO(), t.klusterletOperator, metav1.GetOptions{})
	return err
}

// GetRandomClusterName gets the clusterName generated by registration randomly.
// the cluster name is the random name if it has not prefix "e2e-".
// TODO: get random cluster name from event
func (t *Tester) GetRandomClusterName() (string, error) {
	managedClusterList, err := t.ClusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, managedCluster := range managedClusterList.Items {
		clusterName := managedCluster.Name
		if !strings.HasPrefix(clusterName, "e2e-") {
			return clusterName, nil
		}
	}
	return "", fmt.Errorf("there is no managedCluster with the random name")
}

// TODO: only output the details of created resources during e2e
func (t *Tester) OutputDebugLogs() {
	klusterletes, err := t.OperatorClient.OperatorV1().Klusterlets().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list klusterlets. error: %v", err)
	}
	for _, klusterlet := range klusterletes.Items {
		klog.Infof("klusterlet %v : %#v \n", klusterlet.Name, klusterlet)
	}

	managedClusters, err := t.ClusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list managedClusters. error: %v", err)
	}
	for _, managedCluster := range managedClusters.Items {
		klog.Infof("managedCluster %v : %#v \n", managedCluster.Name, managedCluster)
	}

	registrationPods, err := t.SpokeKubeClient.CoreV1().Pods("").List(context.Background(),
		metav1.ListOptions{LabelSelector: "app=klusterlet-registration-agent"})
	if err != nil {
		klog.Errorf("failed to list registration pods. error: %v", err)
	}

	manifestWorkPods, err := t.SpokeKubeClient.CoreV1().Pods("").List(context.Background(),
		metav1.ListOptions{LabelSelector: "app=klusterlet-manifestwork-agent"})
	if err != nil {
		klog.Errorf("failed to get manifestwork pods. error: %v", err)
	}

	agentPods := append(registrationPods.Items, manifestWorkPods.Items...)
	for _, pod := range agentPods {
		klog.Infof("klusterlet agent pod %v/%v\n", pod.Namespace, pod.Name)
		logs, err := t.SpokePodLog(pod.Name, pod.Namespace, int64(10))
		if err != nil {
			klog.Errorf("failed to get pod %v/%v log. error: %v", pod.Namespace, pod.Name, err)
			continue
		}
		klog.Infof("pod %v/%v logs:\n %v \n", pod.Namespace, pod.Name, logs)
	}

	manifestWorks, err := t.HubWorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list manifestWorks. error: %v", err)
	}
	for _, manifestWork := range manifestWorks.Items {
		klog.Infof("manifestWork %v/%v : %#v \n", manifestWork.Namespace, manifestWork.Name, manifestWork)
	}
}

func (t *Tester) SpokePodLog(podName, nameSpace string, lines int64) (string, error) {
	podLogs, err := t.SpokeKubeClient.CoreV1().Pods(nameSpace).
		GetLogs(podName, &corev1.PodLogOptions{TailLines: &lines}).Stream(context.TODO())
	if err != nil {
		return "", err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (t *Tester) CreateManagedClusterAddOn(managedClusterNamespace, addOnName string) error {
	_, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).Create(
		context.TODO(),
		&addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: managedClusterNamespace,
				Name:      addOnName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func (t *Tester) CreateManagedClusterAddOnLease(addOnInstallNamespace, addOnName string) error {
	if _, err := t.HubKubeClient.CoreV1().Namespaces().Create(
		context.TODO(),
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnInstallNamespace,
			},
		},
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	_, err := t.HubKubeClient.CoordinationV1().Leases(addOnInstallNamespace).Create(
		context.TODO(),
		&coordv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: addOnInstallNamespace,
			},
			Spec: coordv1.LeaseSpec{
				RenewTime: &metav1.MicroTime{Time: time.Now()},
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func (t *Tester) CheckManagedClusterAddOnStatus(managedClusterNamespace, addOnName string) error {
	addOn, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(managedClusterNamespace).Get(context.TODO(), addOnName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if addOn.Status.Conditions == nil {
		return fmt.Errorf("there is no conditions in addon %v/%v", managedClusterNamespace, addOnName)
	}

	if !meta.IsStatusConditionTrue(addOn.Status.Conditions, "Available") {
		return fmt.Errorf("the addon %v/%v available condition is not true", managedClusterNamespace, addOnName)
	}

	return nil
}

func (t *Tester) DeleteExternalKubeconfigSecret(klusterlet *operatorapiv1.Klusterlet) error {
	agentNamespace := helpers.AgentNamespace(klusterlet)
	err := t.HubKubeClient.CoreV1().Secrets(agentNamespace).Delete(context.TODO(),
		helpers.ExternalManagedKubeConfig, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("failed to delete external managed secret in ns %v. %v", agentNamespace, err)
		return err
	}

	return nil
}

func (t *Tester) CreateFakeExternalKubeconfigSecret(klusterlet *operatorapiv1.Klusterlet) error {
	agentNamespace := helpers.AgentNamespace(klusterlet)
	klog.Infof("klusterlet: %s/%s, \t, \t agent namespace: %s",
		klusterlet.Name, klusterlet.Namespace, agentNamespace)

	bsSecret, err := t.HubKubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(),
		t.bootstrapHubSecret.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get bootstrap secret %v in ns %v. %v", bsSecret, agentNamespace, err)
		return err
	}

	// create external-managed-kubeconfig, will use the same cluster to simulate the Hosted mode.
	secret, err := changeHostOfKubeconfigSecret(*bsSecret, "https://kube-apiserver.i-am-a-fake-server:6443")
	if err != nil {
		klog.Errorf("failed to change host of the kubeconfig secret in. %v", err)
		return err
	}
	secret.Namespace = agentNamespace
	secret.Name = helpers.ExternalManagedKubeConfig
	secret.ResourceVersion = ""

	_, err = t.HubKubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create external managed secret %v in ns %v. %v", bsSecret, agentNamespace, err)
		return err
	}

	return nil
}

func (t *Tester) BuildClusterClient(saNamespace, saName string, clusterPolicyRules, policyRules []rbacv1.PolicyRule) (clusterclient.Interface, error) {
	var err error

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: saNamespace,
			Name:      saName,
		},
	}
	_, err = t.HubKubeClient.CoreV1().ServiceAccounts(saNamespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	// create cluster role/rolebinding
	if len(clusterPolicyRules) > 0 {
		clusterRoleName := fmt.Sprintf("%s-clusterrole", saName)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRoleName,
			},
			Rules: clusterPolicyRules,
		}
		_, err = t.HubKubeClient.RbacV1().ClusterRoles().Create(context.TODO(), clusterRole, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-clusterrolebinding", saName),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: saNamespace,
					Name:      saName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRoleName,
			},
		}
		_, err = t.HubKubeClient.RbacV1().ClusterRoleBindings().Create(context.TODO(), clusterRoleBinding, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// create cluster role/rolebinding
	if len(policyRules) > 0 {
		roleName := fmt.Sprintf("%s-role", saName)
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: saNamespace,
				Name:      roleName,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"cluster.open-cluster-management.io"},
					Resources: []string{"managedclustersetbindings"},
					Verbs:     []string{"create", "get", "update"},
				},
			},
		}
		_, err = t.HubKubeClient.RbacV1().Roles(saNamespace).Create(context.TODO(), role, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: saNamespace,
				Name:      fmt.Sprintf("%s-rolebinding", saName),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: saNamespace,
					Name:      saName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     roleName,
			},
		}
		_, err = t.HubKubeClient.RbacV1().RoleBindings(saNamespace).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	tokenRequest, err := t.HubKubeClient.CoreV1().ServiceAccounts(saNamespace).CreateToken(
		context.TODO(),
		saName,
		&authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: pointer.Int64(8640 * 3600),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, err
	}

	unauthorizedClusterClient, err := clusterclient.NewForConfig(&rest.Config{
		Host: t.HubClusterCfg.Host,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: t.HubClusterCfg.CAData,
		},
		BearerToken: tokenRequest.Status.Token,
	})
	return unauthorizedClusterClient, err
}

// cleanupClusterClient delete cluster-scope resource created by func "buildClusterClient",
// the namespace-scope resources should be deleted by an additional namespace deleting func.
// It is recommended be invoked as a pair with the func "buildClusterClient"
func (t *Tester) CleanupClusterClient(saNamespace, saName string) error {
	err := t.HubKubeClient.CoreV1().ServiceAccounts(saNamespace).Delete(context.TODO(), saName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete sa %q/%q failed: %v", saNamespace, saName, err)
	}

	// delete cluster role and cluster role binding if exists
	clusterRoleName := fmt.Sprintf("%s-clusterrole", saName)
	err = t.HubKubeClient.RbacV1().ClusterRoles().Delete(context.TODO(), clusterRoleName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete cluster role %q failed: %v", clusterRoleName, err)
	}
	clusterRoleBindingName := fmt.Sprintf("%s-clusterrolebinding", saName)
	err = t.HubKubeClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), clusterRoleBindingName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete cluster role binding %q failed: %v", clusterRoleBindingName, err)
	}

	return nil
}

func (t *Tester) DeleteManageClusterAndRelatedNamespace(clusterName string) error {
	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		err := t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("delete managed cluster %q failed: %v", clusterName, err)
	}

	// delete namespace created by hub automaticly
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := t.HubKubeClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		// some managed cluster just created, but the csr is not approved,
		// so there is not a related namespace
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete related namespace %q failed: %v", clusterName, err)
	}

	return nil
}

func changeHostOfKubeconfigSecret(secret corev1.Secret, apiServerURL string) (*corev1.Secret, error) {
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found")
	}

	if kubeconfigData == nil {
		return nil, fmt.Errorf("failed to get kubeconfig from secret: %s", secret.GetName())
	}

	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from secret: %s", secret.GetName())
	}

	if len(kubeconfig.Clusters) == 0 {
		return nil, fmt.Errorf("there is no cluster in kubeconfig from secret: %s", secret.GetName())
	}

	for k := range kubeconfig.Clusters {
		kubeconfig.Clusters[k].Server = apiServerURL
	}

	newKubeconfig, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to write new kubeconfig to secret: %s", secret.GetName())
	}

	secret.Data = map[string][]byte{
		"kubeconfig": newKubeconfig,
	}

	klog.Info("Set the cluster server URL in %v secret", "apiServerURL", secret.Name, apiServerURL)
	return &secret, nil
}
