package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

const (
	apiserviceName = "v1.admission.cluster.open-cluster-management.io"
	admissionName  = "managedclustervalidators.admission.cluster.open-cluster-management.io"
	invalidURL     = "127.0.0.1:8001"
	validURL       = "https://127.0.0.1:8443"
	saNamespace    = "default"
)

var _ = ginkgo.Describe("Managed cluster admission hook", func() {
	ginkgo.BeforeEach(func() {
		// make sure the api service v1.admission.cluster.open-cluster-management.io is available
		gomega.Eventually(func() bool {
			apiService, err := hubAPIServiceClient.APIServices().Get(context.TODO(), apiserviceName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if len(apiService.Status.Conditions) == 0 {
				return false
			}
			return apiService.Status.Conditions[0].Type == apiregistrationv1.Available &&
				apiService.Status.Conditions[0].Status == apiregistrationv1.ConditionTrue
		}, 60*time.Second, 1*time.Second).Should(gomega.BeTrue())
		// make sure the managedcluster can be created successfully
		gomega.Eventually(func() bool {
			clusterName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
			managedCluster := newManagedCluster(clusterName, false, validURL)
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
			if err != nil {
				return false
			}
			clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
			return true
		}, 60*time.Second, 1*time.Second).Should(gomega.BeTrue())
	})

	ginkgo.Context("Creating a managed cluster", func() {
		ginkgo.It("Should have the default LeaseDurationSeconds", func() {
			clusterName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
			ginkgo.By(fmt.Sprintf("create a managed cluster %q", clusterName))

			_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), newManagedCluster(clusterName, false, validURL), metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(managedCluster.Spec.LeaseDurationSeconds).To(gomega.Equal(int32(60)))
		})

		ginkgo.It("Should respond bad request when creating a managed cluster with invalid external server URLs", func() {
			clusterName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))
			ginkgo.By(fmt.Sprintf("create a managed cluster %q with an invalid external server URL %q", clusterName, invalidURL))

			managedCluster := newManagedCluster(clusterName, false, invalidURL)

			_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: url \"%s\" is invalid in client configs",
				admissionName,
				invalidURL,
			)))
		})

		ginkgo.It("Should forbid the request when creating an accepted managed cluster by unauthorized user", func() {
			sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
			clusterName := fmt.Sprintf("webhook-spoke-%s", rand.String(6))

			ginkgo.By(fmt.Sprintf("create an managed cluster %q with unauthorized service account %q", clusterName, sa))

			// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
			// but cannot change the ManagedCluster HubAcceptsClient field
			unauthorizedClient, err := buildUnauthorizedClusterClient(sa)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			managedCluster := newManagedCluster(clusterName, true, validURL)

			_, err = unauthorizedClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: user \"system:serviceaccount:%s:%s\" cannot update the HubAcceptsClient field",
				admissionName,
				saNamespace,
				sa,
			)))
		})
	})

	ginkgo.Context("Updating a managed cluster", func() {
		var clusterName string

		ginkgo.BeforeEach(func() {
			clusterName = fmt.Sprintf("webhook-spoke-%s", rand.String(6))
			managedCluster := newManagedCluster(clusterName, false, validURL)
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), managedCluster, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Should not update the LeaseDurationSeconds to zero", func() {
			ginkgo.By(fmt.Sprintf("try to update managed cluster %q LeaseDurationSeconds to zero", clusterName))
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				managedCluster.Spec.LeaseDurationSeconds = 0
				_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
				return err
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(managedCluster.Spec.LeaseDurationSeconds).To(gomega.Equal(int32(60)))
		})

		ginkgo.It("Should respond bad request when updating a managed cluster with invalid external server URLs", func() {
			ginkgo.By(fmt.Sprintf("update managed cluster %q with an invalid external server URL %q", clusterName, invalidURL))

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				managedCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				managedCluster.Spec.ManagedClusterClientConfigs[0].URL = invalidURL
				_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
				return err
			})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: url \"%s\" is invalid in client configs",
				admissionName,
				invalidURL,
			)))
		})

		ginkgo.It("Should forbid the request when updating an unaccepted managed cluster to accepted by unauthorized user", func() {
			sa := fmt.Sprintf("webhook-sa-%s", rand.String(6))
			ginkgo.By(fmt.Sprintf("accept managed cluster %q by an unauthorized user %q", clusterName, sa))

			// prepare an unauthorized cluster client from a service account who can create/get/update ManagedCluster
			// but cannot change the ManagedCluster HubAcceptsClient field
			unauthorizedClient, err := buildUnauthorizedClusterClient(sa)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				managedCluster, err := unauthorizedClient.ClusterV1().ManagedClusters().Get(context.TODO(), clusterName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				managedCluster.Spec.HubAcceptsClient = true
				_, err = unauthorizedClient.ClusterV1().ManagedClusters().Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
				return err
			})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsForbidden(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: user \"system:serviceaccount:%s:%s\" cannot update the HubAcceptsClient field",
				admissionName,
				saNamespace,
				sa,
			)))
		})
	})
})

func newManagedCluster(name string, accepted bool, externalURL string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: accepted,
			ManagedClusterClientConfigs: []clusterv1.ClientConfig{
				{
					URL: externalURL,
				},
			},
		},
	}
}

func buildUnauthorizedClusterClient(saName string) (clusterv1client.Interface, error) {
	var err error

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: saNamespace,
			Name:      saName,
		},
	}
	_, err = hubClient.CoreV1().ServiceAccounts(saNamespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	clusterRoleName := fmt.Sprintf("%s-clusterrole", saName)
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cluster.open-cluster-management.io"},
				Resources: []string{"managedclusters"},
				Verbs:     []string{"create", "get", "update"},
			},
		},
	}
	_, err = hubClient.RbacV1().ClusterRoles().Create(context.TODO(), clusterRole, metav1.CreateOptions{})
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
	_, err = hubClient.RbacV1().ClusterRoleBindings().Create(context.TODO(), clusterRoleBinding, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	var tokenSecret *corev1.Secret
	err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		secrets, err := hubClient.CoreV1().Secrets(saNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}

		for _, secret := range secrets.Items {
			if strings.HasPrefix(secret.Name, fmt.Sprintf("%s-token-", saName)) {
				tokenSecret = &secret
				return true, nil
			}
		}

		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if tokenSecret == nil {
		return nil, fmt.Errorf("the %s token secret cannot be found in %s namespace", sa, saNamespace)
	}

	unauthorizedClusterClient, err := clusterv1client.NewForConfig(&restclient.Config{
		Host: clusterCfg.Host,
		TLSClientConfig: restclient.TLSClientConfig{
			CAData: clusterCfg.CAData,
		},
		BearerToken: string(tokenSecret.Data["token"]),
	})
	return unauthorizedClusterClient, err
}
