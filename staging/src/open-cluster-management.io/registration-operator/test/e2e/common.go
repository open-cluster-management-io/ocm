package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

type Tester struct {
	kubeconfigPath                   string
	KubeClient                       kubernetes.Interface
	HubAPIExtensionClient            apiextensionsclient.Interface
	ClusterCfg                       *rest.Config
	OperatorClient                   operatorclient.Interface
	ClusterClient                    clusterclient.Interface
	WorkClient                       workv1client.Interface
	AddOnClinet                      addonclient.Interface
	bootstrapHubSecret               *corev1.Secret
	EventuallyTimeout                time.Duration
	EventuallyInterval               time.Duration
	clusterManagerNamespace          string
	klusterletDefaultNamespace       string
	hubRegistrationDeployment        string
	hubRegistrationWebhookDeployment string
	hubWorkWebhookDeployment         string
	hubPlacementDeployment           string
	operatorNamespace                string
	klusterletOperator               string
}

// kubeconfigPath is the path of kubeconfig file, will be get from env "KUBECONFIG" by default.
// bootstrapHubSecret is the bootstrap hub kubeconfig secret, and the format is "namespace/secretName".
// Default of bootstrapHubSecret is helpers.KlusterletDefaultNamespace/helpers.BootstrapHubKubeConfig.
func NewTester(kubeconfigPath string) *Tester {
	var tester = Tester{
		kubeconfigPath:                   kubeconfigPath,
		EventuallyTimeout:                60 * time.Second, // seconds
		EventuallyInterval:               1 * time.Second,  // seconds
		clusterManagerNamespace:          helpers.ClusterManagerDefaultNamespace,
		klusterletDefaultNamespace:       helpers.KlusterletDefaultNamespace,
		hubRegistrationDeployment:        "cluster-manager-registration-controller",
		hubRegistrationWebhookDeployment: "cluster-manager-registration-webhook",
		hubWorkWebhookDeployment:         "cluster-manager-work-webhook",
		hubPlacementDeployment:           "cluster-manager-placement-controller",
		operatorNamespace:                "open-cluster-management",
		klusterletOperator:               "klusterlet",
	}

	return &tester
}

func (t *Tester) Init() error {
	var kubeconfigPath string
	var err error

	if t.kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("KUBECONFIG")
	} else {
		kubeconfigPath = t.kubeconfigPath
	}

	if t.ClusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
		klog.Errorf("failed to get ClusterCfg from path %v . %v", kubeconfigPath, err)
		return err
	}
	if t.KubeClient, err = kubernetes.NewForConfig(t.ClusterCfg); err != nil {
		klog.Errorf("failed to get KubeClient. %v", err)
		return err
	}

	if t.HubAPIExtensionClient, err = apiextensionsclient.NewForConfig(t.ClusterCfg); err != nil {
		klog.Errorf("failed to get HubApiExtensionClient. %v", err)
		return err
	}
	if t.OperatorClient, err = operatorclient.NewForConfig(t.ClusterCfg); err != nil {
		klog.Errorf("failed to get OperatorClient. %v", err)
		return err
	}
	if t.ClusterClient, err = clusterclient.NewForConfig(t.ClusterCfg); err != nil {
		klog.Errorf("failed to get ClusterClient. %v", err)
		return err
	}
	if t.WorkClient, err = workv1client.NewForConfig(t.ClusterCfg); err != nil {
		klog.Errorf("failed to get WorkClient. %v", err)
		return err
	}
	if t.AddOnClinet, err = addonclient.NewForConfig(t.ClusterCfg); err != nil {
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
	if t.bootstrapHubSecret, err = t.KubeClient.CoreV1().Secrets(bootstrapHubSecretNamespace).
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
		klusterletNamespace = t.klusterletDefaultNamespace
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
	if _, err := t.KubeClient.CoreV1().Namespaces().Get(context.TODO(), agentNamespace, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("failed to get ns %v. %v", agentNamespace, err)
			return nil, err
		}

		if _, err := t.KubeClient.CoreV1().Namespaces().Create(context.TODO(),
			namespace, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create ns %v. %v", namespace, err)
			return nil, err
		}
	}

	// create bootstrap-hub-kubeconfig secret
	secret := t.bootstrapHubSecret.DeepCopy()
	if _, err := t.KubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
			return nil, err
		}
		if _, err = t.KubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create secret %v in ns %v. %v", secret, agentNamespace, err)
			return nil, err
		}
	}

	if mode == operatorapiv1.InstallModeHosted {
		// create external-managed-kubeconfig, will use the same cluster to simulate the Hosted mode.
		secret.Namespace = agentNamespace
		secret.Name = helpers.ExternalManagedKubeConfig
		if _, err := t.KubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
				return nil, err
			}
			if _, err = t.KubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
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
	var csrClient = t.KubeClient.CertificatesV1().CertificateSigningRequests()
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

func newConfigmap(namespace, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: data,
	}
}

func (t *Tester) CreateWorkOfConfigMap(name, clusterName, configMapName, configMapNamespace string) (*workapiv1.ManifestWork, error) {
	manifest := workapiv1.Manifest{}
	manifest.Object = newConfigmap(configMapNamespace, configMapName, map[string]string{"a": "b"})
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

	return t.WorkClient.WorkV1().ManifestWorks(clusterName).
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

func (t *Tester) cleanKlusterletResources(klusterletName, clusterName string) error {
	if klusterletName == "" {
		return fmt.Errorf("the klusterlet name should not be null")
	}

	// clean the klusterlets
	err := t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
	if err != nil {
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
	// make sure open-cluster-management-hub namespace is created
	if _, err := t.KubeClient.CoreV1().Namespaces().
		Get(context.TODO(), t.clusterManagerNamespace, metav1.GetOptions{}); err != nil {
		return err
	}

	// make sure hub deployments are created
	if _, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
		Get(context.TODO(), t.hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
		return err
	}
	gomega.Eventually(func() error {
		registrationWebhookDeployment, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
			Get(context.TODO(), t.hubRegistrationWebhookDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		replicas := *registrationWebhookDeployment.Spec.Replicas
		readyReplicas := registrationWebhookDeployment.Status.ReadyReplicas
		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", t.hubRegistrationWebhookDeployment, replicas, readyReplicas)
		}
		return nil
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		workWebhookDeployment, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
			Get(context.TODO(), t.hubWorkWebhookDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		replicas := *workWebhookDeployment.Spec.Replicas
		readyReplicas := workWebhookDeployment.Status.ReadyReplicas
		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", t.hubWorkWebhookDeployment, replicas, readyReplicas)
		}
		return nil
	}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.BeNil())

	if _, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
		Get(context.TODO(), t.hubPlacementDeployment, metav1.GetOptions{}); err != nil {
		return err
	}
	return nil
}

func (t *Tester) CheckClusterManagerStatus() error {
	cms, err := t.OperatorClient.OperatorV1().ClusterManagers().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(cms.Items) == 0 {
		return fmt.Errorf("ClusterManager not found")
	}
	cm := cms.Items[0]
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubRegistrationDegraded") {
		return fmt.Errorf("HubRegistration is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "HubPlacementDegraded") {
		return fmt.Errorf("HubPlacement is degraded")
	}
	if !meta.IsStatusConditionFalse(cm.Status.Conditions, "Progressing") {
		return fmt.Errorf("ClusterManager is still progressing")
	}
	return nil
}

func (t *Tester) CheckKlusterletOperatorReady() error {
	// make sure klusterlet operator deployment is created
	_, err := t.KubeClient.AppsV1().Deployments(t.operatorNamespace).
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

	registrationPods, err := t.KubeClient.CoreV1().Pods("").List(context.Background(),
		metav1.ListOptions{LabelSelector: "app=klusterlet-registration-agent"})
	if err != nil {
		klog.Errorf("failed to list registration pods. error: %v", err)
	}

	manifestWorkPods, err := t.KubeClient.CoreV1().Pods("").List(context.Background(),
		metav1.ListOptions{LabelSelector: "app=klusterlet-manifestwork-agent"})
	if err != nil {
		klog.Errorf("failed to get manifestwork pods. error: %v", err)
	}

	agentPods := append(registrationPods.Items, manifestWorkPods.Items...)
	for _, pod := range agentPods {
		klog.Infof("klusterlet agent pod %v/%v\n", pod.Namespace, pod.Name)
		logs, err := t.PodLog(pod.Name, pod.Namespace, int64(10))
		if err != nil {
			klog.Errorf("failed to get pod %v/%v log. error: %v", pod.Namespace, pod.Name, err)
			continue
		}
		klog.Infof("pod %v/%v logs:\n %v \n", pod.Namespace, pod.Name, logs)
	}

	manifestWorks, err := t.WorkClient.WorkV1().ManifestWorks("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list manifestWorks. error: %v", err)
	}
	for _, manifestWork := range manifestWorks.Items {
		klog.Infof("manifestWork %v/%v : %#v \n", manifestWork.Namespace, manifestWork.Name, manifestWork)
	}
}

func (t *Tester) PodLog(podName, nameSpace string, lines int64) (string, error) {
	podLogs, err := t.KubeClient.CoreV1().Pods(nameSpace).
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
	if _, err := t.KubeClient.CoreV1().Namespaces().Create(
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

	_, err := t.KubeClient.CoordinationV1().Leases(addOnInstallNamespace).Create(
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
