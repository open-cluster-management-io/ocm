package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/gomega"
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
	"k8s.io/client-go/util/retry"
)

const (
	eventuallyTimeout            = 60 // seconds
	eventuallyInterval           = 1  // seconds
	clusterManagerNamespace      = helpers.ClusterManagerNamespace
	klusterletDefaultNamespace   = helpers.KlusterletDefaultNamespace
	bootstrapHubKubeConfigSecret = helpers.BootstrapHubKubeConfigSecret
	hubRegistrationDeployment    = "cluster-manager-registration-controller"
	hubWebhookDeployment         = "cluster-manager-registration-webhook"
	operatorNamespace            = "open-cluster-management"
	klusterletOperator           = "klusterlet"
)

var (
	kubeClient         kubernetes.Interface
	clusterCfg         *rest.Config
	operatorClient     operatorclient.Interface
	clusterClient      clusterclient.Interface
	workClient         workv1client.Interface
	bootstrapHubSecret *corev1.Secret
)

func createKlusterlet(name, clusterName, agentNamespace string) (realClusterName string) {
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

	Expect(name).NotTo(BeEmpty())

	// create agentNamespace and bootstrap-hub-kubeconfig secret
	if agentNamespace != "" && agentNamespace != klusterletDefaultNamespace {
		_, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), agentNamespace, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			namespace := v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: agentNamespace,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), &namespace, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}

		secret := bootstrapHubSecret.DeepCopy()
		secret.ObjectMeta.ResourceVersion = ""
		secret.SetNamespace(agentNamespace)
		_, err = kubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	// create klusterlet CR
	realKlusterlet, err := operatorClient.OperatorV1().Klusterlets().Create(context.TODO(), klusterlet, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	realClusterName = realKlusterlet.Spec.ClusterName

	// the managed cluster should be created
	Eventually(func() error {
		clusters, err := clusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(clusters.Items) == 0 {
			return fmt.Errorf("there is no managed cluster created")
		}
		if len(clusters.Items) > 1 {
			return fmt.Errorf("there are %v managed clusters created: %v", len(clusters.Items), clusters.Items)
		}

		if realClusterName != "" && realClusterName != clusters.Items[0].Name {
			return fmt.Errorf("the managed cluster %v is not the one created %v", clusters.Items[0].Name, realClusterName)
		}

		if realClusterName == "" {
			realClusterName = clusters.Items[0].Name
		}

		return nil
	}, eventuallyTimeout*4, eventuallyInterval*4).Should(Succeed())

	// approve csr
	approveCSR(realClusterName)

	// accept client
	acceptsClient(realClusterName)
	return
}

func approveCSR(clusterName string) {
	var csrs *certificatesv1beta1.CertificateSigningRequestList
	var csrClient = kubeClient.CertificatesV1beta1().CertificateSigningRequests()
	var err error
	Eventually(func() error {
		csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name = %v", clusterName),
		})
		if err != nil {
			return err
		}
		if len(csrs.Items) == 0 {
			return fmt.Errorf("there is no csr related cluster %v", clusterName)
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	for i := range csrs.Items {
		csr := &csrs.Items[i]
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			if isCSRInTerminalState(&csr.Status) {
				return nil
			}

			csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
				Type:    certificatesv1beta1.CertificateApproved,
				Reason:  "Approved by E2E",
				Message: "Approved as part of e2e",
			})
			_, err := csrClient.UpdateApproval(context.TODO(), csr, metav1.UpdateOptions{})
			return err
		})
		Expect(err).NotTo(HaveOccurred())
	}
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

func acceptsClient(clusterName string) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
			clusterName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		managedCluster, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(),
			managedCluster, metav1.UpdateOptions{})
		return err
	})
	Expect(err).NotTo(HaveOccurred())
}

func managedClusterReady(clusterName string) error {
	managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
		clusterName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
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

func createWorkOfConfigMap(name, clusterName, configMapName, configMapNamespace string) {
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
	_, err := workClient.WorkV1().ManifestWorks(clusterName).
		Create(context.TODO(), manifestWork, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func cleanResources() {
	// clean the manifest works
	manifestWorks, err := workClient.WorkV1().ManifestWorks("").
		List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, work := range manifestWorks.Items {
		// ignore if failed to delete
		_ = workClient.WorkV1().ManifestWorks(work.Namespace).
			Delete(context.TODO(), work.Name, metav1.DeleteOptions{})
	}

	// clean the klusterlets
	klusterlets, err := operatorClient.OperatorV1().Klusterlets().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, klusterlet := range klusterlets.Items {
		err = operatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterlet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	// the klusterlets should be cleaned up
	Eventually(func() error {
		klusterlets, err := operatorClient.OperatorV1().Klusterlets().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(klusterlets.Items) != 0 {
			return fmt.Errorf("the klusterlets are not deleted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// clean the managed clusters
	clusters, err := clusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, cluster := range clusters.Items {
		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), cluster.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	}

	// the managed clusters should be cleaned up
	Eventually(func() error {
		clusters, err := clusterClient.ClusterV1().ManagedClusters().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(clusters.Items) != 0 {
			return fmt.Errorf("the manged clusters are not deleted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// the pods in open-cluster-management-agent ns should be cleaned up
	Eventually(func() error {
		var err error
		pods, err := kubeClient.CoreV1().Pods(klusterletDefaultNamespace).
			List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) != 0 {
			return fmt.Errorf("the pods in ns %v are not deleted", klusterletDefaultNamespace)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func checkHubReady() {
	// make sure open-cluster-management-hub namespace is created
	Eventually(func() error {
		_, err := kubeClient.CoreV1().Namespaces().
			Get(context.TODO(), clusterManagerNamespace, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// make sure hub deployments are created
	Eventually(func() error {
		_, err := kubeClient.AppsV1().Deployments(clusterManagerNamespace).
			Get(context.TODO(), hubRegistrationDeployment, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	Eventually(func() error {
		_, err := kubeClient.AppsV1().Deployments(clusterManagerNamespace).
			Get(context.TODO(), hubWebhookDeployment, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}

func checkKlusterletOperatorReady() {
	// make sure klusterlet operator deployment is created
	Eventually(func() error {
		_, err := kubeClient.AppsV1().Deployments(operatorNamespace).
			Get(context.TODO(), klusterletOperator, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// make sure open-cluster-management-agent namespace is created
	Eventually(func() error {
		_, err := kubeClient.CoreV1().Namespaces().
			Get(context.TODO(), klusterletDefaultNamespace, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// make sure bootstrap-hub-kubeconfig secret is created
	Eventually(func() error {
		var err error
		bootstrapHubSecret, err = kubeClient.CoreV1().Secrets(klusterletDefaultNamespace).
			Get(context.TODO(), bootstrapHubKubeConfigSecret, metav1.GetOptions{})
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	// make sure there is no pods in open-cluster-management-agent namespace
	Eventually(func() error {
		var err error
		pods, err := kubeClient.CoreV1().Pods(klusterletDefaultNamespace).
			List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) != 0 {
			return fmt.Errorf("the pods in ns %v are not deleted", klusterletDefaultNamespace)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
}
