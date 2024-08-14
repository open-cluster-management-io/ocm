package framework

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func (hub *Hub) GetManagedCluster(clusterName string) (*clusterv1.ManagedCluster, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("the name of managedcluster should not be null")
	}
	return hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
}

func (hub *Hub) CheckManagedClusterStatus(clusterName string) error {
	managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
		clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	var okCount = 0
	for _, condition := range managedCluster.Status.Conditions {
		if (condition.Type == clusterv1.ManagedClusterConditionHubAccepted ||
			condition.Type == clusterv1.ManagedClusterConditionJoined ||
			condition.Type == clusterv1.ManagedClusterConditionAvailable) &&
			condition.Status == metav1.ConditionTrue {
			okCount++
		}
	}

	if okCount == 3 {
		return nil
	}

	return fmt.Errorf("cluster %s condtions are not ready: %v", clusterName, managedCluster.Status.Conditions)
}

func (hub *Hub) CheckManagedClusterStatusConditions(clusterName string,
	expectedConditions map[string]metav1.ConditionStatus) error {
	if clusterName == "" {
		return fmt.Errorf("the name of managedcluster should not be null")
	}

	cluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// expect the managed cluster to be not available
	for conditionType, conditionStatus := range expectedConditions {
		condition := meta.FindStatusCondition(cluster.Status.Conditions, conditionType)
		if condition == nil {
			return fmt.Errorf("managed cluster %s is not in expected status, expect %s to be %s, but not found",
				clusterName, conditionType, conditionStatus)
		}
		if condition.Status != conditionStatus {
			return fmt.Errorf("managed cluster %s is not in expected status, expect %s to be %s, but got %s",
				clusterName, conditionType, conditionStatus, condition.Status)
		}
	}

	return nil
}

func (hub *Hub) DeleteManageClusterAndRelatedNamespace(clusterName string) error {
	Eventually(func() error {
		err := hub.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}).Should(Succeed())

	// delete namespace created by hub automatically
	Eventually(func() error {
		err := hub.KubeClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		// some managed cluster just created, but the csr is not approved,
		// so there is not a related namespace
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}).Should(Succeed())

	return nil
}

func (hub *Hub) AcceptManageCluster(clusterName string) error {
	managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(),
		clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	managedCluster.Spec.HubAcceptsClient = true
	managedCluster.Spec.LeaseDurationSeconds = 5
	_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(),
		managedCluster, metav1.UpdateOptions{})
	return err
}

func (hub *Hub) ApproveManagedClusterCSR(clusterName string) error {
	var csrs *certificatesv1.CertificateSigningRequestList
	var csrClient = hub.KubeClient.CertificatesV1().CertificateSigningRequests()
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
			continue
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

func (hub *Hub) SetHubAcceptsClient(clusterName string, hubAcceptClient bool) error {
	managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get managed cluster: %w", err)
	}

	if managedCluster.Spec.HubAcceptsClient != hubAcceptClient {
		managedCluster.Spec.HubAcceptsClient = hubAcceptClient
		_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update managed cluster: %w", err)
		}
	}

	return nil
}

func (hub *Hub) SetLeaseDurationSeconds(clusterName string, leaseDurationSeconds int32) error {
	managedCluster, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get managed cluster: %w", err)
	}

	managedCluster.Spec.LeaseDurationSeconds = leaseDurationSeconds
	_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update managed cluster: %w", err)
	}
	return nil
}
