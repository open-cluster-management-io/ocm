package e2e

import (
	"context"
	"fmt"
	"k8s.io/klog"
	"os"
	"time"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	operatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned"
	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

type Tester struct {
	KubeClient                 kubernetes.Interface
	ClusterCfg                 *rest.Config
	OperatorClient             operatorclient.Interface
	ClusterClient              clusterclient.Interface
	WorkClient                 workv1client.Interface
	bootstrapHubSecret         *corev1.Secret
	EventuallyTimeout          time.Duration
	EventuallyInterval         time.Duration
	clusterManagerNamespace    string
	klusterletDefaultNamespace string
	hubRegistrationDeployment  string
	hubWebhookDeployment       string
	operatorNamespace          string
	klusterletOperator         string
}

// kubeconfigPath is the path of kubeconfig file, will be get from env "KUBECONFIG" by default.
// bootstrapHubSecret is the bootstrap hub kubeconfig secret, and the format is "namespace/secretName".
// Default of bootstrapHubSecret is helpers.KlusterletDefaultNamespace/helpers.BootstrapHubKubeConfigSecret.
func NewTester(kubeconfigPath string) (*Tester, error) {
	var err error
	var tester = Tester{
		EventuallyTimeout:          60 * time.Second, // seconds
		EventuallyInterval:         1 * time.Second,  // seconds
		clusterManagerNamespace:    helpers.ClusterManagerNamespace,
		klusterletDefaultNamespace: helpers.KlusterletDefaultNamespace,
		hubRegistrationDeployment:  "cluster-manager-registration-controller",
		hubWebhookDeployment:       "cluster-manager-registration-webhook",
		operatorNamespace:          "open-cluster-management",
		klusterletOperator:         "klusterlet",
	}

	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("KUBECONFIG")
	}
	if tester.ClusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
		klog.Errorf("failed to get ClusterCfg from path %v . %v", kubeconfigPath, err)
		return nil, err
	}
	if tester.KubeClient, err = kubernetes.NewForConfig(tester.ClusterCfg); err != nil {
		klog.Errorf("failed to get KubeClient. %v", err)
		return nil, err
	}
	if tester.OperatorClient, err = operatorclient.NewForConfig(tester.ClusterCfg); err != nil {
		klog.Errorf("failed to get OperatorClient. %v", err)
		return nil, err
	}
	if tester.ClusterClient, err = clusterclient.NewForConfig(tester.ClusterCfg); err != nil {
		klog.Errorf("failed to get ClusterClient. %v", err)
		return nil, err
	}
	if tester.WorkClient, err = workv1client.NewForConfig(tester.ClusterCfg); err != nil {
		klog.Errorf("failed to get WorkClient. %v", err)
		return nil, err
	}

	return &tester, nil
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
	var bootstrapHubSecretName = helpers.BootstrapHubKubeConfigSecret
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

func (t *Tester) CreateKlusterlet(name, clusterName, agentNamespace string) (*operatorapiv1.Klusterlet, error) {
	if name == "" {
		return nil, fmt.Errorf("the name should not be null")
	}

	var klusterlet = &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			ExternalServerURLs: []operatorapiv1.ServerURL{
				{
					URL: "https://localhost",
				},
			},
			ClusterName: clusterName,
			Namespace:   agentNamespace,
		},
	}

	if agentNamespace == "" {
		agentNamespace = t.klusterletDefaultNamespace
	}

	// create agentNamespace
	namespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNamespace,
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
	var csrs *certificatesv1beta1.CertificateSigningRequestList
	var csrClient = t.KubeClient.CertificatesV1beta1().CertificateSigningRequests()
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
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{}); err != nil {
				return err
			}

			if isCSRInTerminalState(&csr.Status) {
				return nil
			}

			csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
				Type:    certificatesv1beta1.CertificateApproved,
				Reason:  "Approved by E2E",
				Message: "Approved as part of e2e",
			})
			_, err = csrClient.UpdateApproval(context.TODO(), csr, metav1.UpdateOptions{})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func isCSRInTerminalState(status *certificatesv1beta1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return true
		}
	}
	return false
}

func (t *Tester) AcceptsClient(clusterName string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		managedCluster, err := t.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
			clusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		managedCluster, err = t.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(),
			managedCluster, metav1.UpdateOptions{})
		return err
	})
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

	return fmt.Errorf("condtions are not ready: %v", managedCluster.Status.Conditions)
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

func (t *Tester) cleanKlusterletResources(klusterletName string) error {
	if klusterletName == "" {
		return fmt.Errorf("the klusterlet name should not be null")
	}

	clusterName, err := t.GetClusterNameFromKlusterlet(klusterletName)
	if err != nil {
		return err
	}

	// clean the manifest works
	manifestWorks, err := t.WorkClient.WorkV1().ManifestWorks(clusterName).
		List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, work := range manifestWorks.Items {
		// ignore if failed to delete
		_ = t.WorkClient.WorkV1().ManifestWorks(work.Namespace).
			Delete(context.TODO(), work.Name, metav1.DeleteOptions{})
	}

	// clean the klusterlets
	err = t.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	// clean the managed clusters
	err = t.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

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

	if _, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
		Get(context.TODO(), t.hubWebhookDeployment, metav1.GetOptions{}); err != nil {
		return err
	}
	return nil
}

func (t *Tester) CheckKlusterletOperatorReady() error {
	// make sure klusterlet operator deployment is created
	_, err := t.KubeClient.AppsV1().Deployments(t.operatorNamespace).
		Get(context.TODO(), t.klusterletOperator, metav1.GetOptions{})
	return err
}

func (t *Tester) GetClusterNameFromKlusterlet(klusterletName string) (string, error) {
	if klusterletName == "" {
		return "", fmt.Errorf("the klusterlet name should not be null")
	}

	klusterlet, err := t.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(),
		klusterletName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	clusterName := klusterlet.Spec.ClusterName
	if clusterName != "" {
		return clusterName, nil
	}

	klusterletNamespace := klusterlet.Spec.Namespace
	if klusterletNamespace == "" {
		klusterletNamespace = helpers.KlusterletDefaultNamespace
	}

	hubKubeconfigSecret, err := t.KubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.TODO(),
		"hub-kubeconfig-secret", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	clusterNameByte, ok := hubKubeconfigSecret.Data["cluster-name"]
	if !ok {
		return "", fmt.Errorf("there is no cluster-name in secret, %+v", hubKubeconfigSecret)
	}

	return string(clusterNameByte), nil
}
