package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	addonclient "github.com/open-cluster-management/api/client/addon/clientset/versioned"
	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E suite")
}

var (
	managedClusterName string
	hubKubeClient      kubernetes.Interface
	hubAddOnClient     addonclient.Interface
	hubClusterClient   clusterclient.Interface
	clusterCfg         *rest.Config
)

// This suite is sensitive to the following environment variables:
//
// - MANAGED_CLUSTER_NAME sets the name of the cluster
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	managedClusterName = os.Getenv("MANAGED_CLUSTER_NAME")
	if managedClusterName == "" {
		managedClusterName = "cluster1"
	}
	err := func() error {
		var err error
		clusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}

		hubKubeClient, err = kubernetes.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubAddOnClient, err = addonclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubClusterClient, err = clusterclient.NewForConfig(clusterCfg)

		return err
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	var csrs *certificatesv1.CertificateSigningRequestList
	// Waiting for the CSR for ManagedCluster to exist
	err = wait.Poll(1*time.Second, 120*time.Second, func() (bool, error) {
		var err error
		csrs, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name = %v", managedClusterName),
		})
		if err != nil {
			return false, err
		}

		if len(csrs.Items) >= 1 {
			return true, nil
		}

		return false, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	// Approving all pending CSRs
	for i := range csrs.Items {
		csr := &csrs.Items[i]
		if !strings.HasPrefix(csr.Name, managedClusterName) {
			continue
		}

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			csr, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
				Type:    certificatesv1.CertificateApproved,
				Status:  corev1.ConditionTrue,
				Reason:  "Approved by E2E",
				Message: "Approved as part of Loopback e2e",
			})
			_, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
			return err
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	var managedCluster *clusterv1.ManagedCluster
	// Waiting for ManagedCluster to exist
	err = wait.Poll(1*time.Second, 120*time.Second, func() (bool, error) {
		var err error
		managedCluster, err = hubClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	// Accepting ManagedCluster
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		managedCluster, err = hubClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
		return err
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
