package registration_test

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("ManagedCluster set hubAcceptsClient from true to false", Label("managedcluster-accept"), func() {
	var managedCluster *clusterv1.ManagedCluster
	BeforeEach(func() {
		managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
		managedCluster = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Check rbac files should be created
		Eventually(func() error {
			_, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}

			// check clusterrole is correct
			clusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(clusterRole.Rules) != 4 {
				return fmt.Errorf("expected 4 rules, got %d rules", len(clusterRole.Rules))
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).
				Get(context.Background(), registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("should set hubAcceptsClient to false", func() {
		// Set hubAcceptsClient to false
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mc.Spec.HubAcceptsClient = false
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), mc, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// Check rbac files:
		// * clusterrole should remain, but with a different scope
		// * clusterrolebinding should remain
		// * registrationRolebinding should be deleted
		// * workRolebinding should be deleted
		Eventually(func() error {
			// Check clusterrole is updated with reduced permissions
			clusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Should only have rule for get/list/watch managedcluster
			if len(clusterRole.Rules) != 1 {
				return fmt.Errorf("expected 1 rule, got %d rules", len(clusterRole.Rules))
			}

			rule := clusterRole.Rules[0]
			expectedRule := rbacv1.PolicyRule{
				APIGroups:     []string{"cluster.open-cluster-management.io"},
				Resources:     []string{"managedclusters"},
				ResourceNames: []string{managedCluster.Name},
				Verbs:         []string{"get", "list", "watch"},
			}
			if !reflect.DeepEqual(rule, expectedRule) {
				return fmt.Errorf("cluster role %s does not have expected rule", mclClusterRoleName(managedCluster.Name))
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			// ClusterRoleBinding should still exist
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			// Registration rolebinding should be deleted
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).
				Get(context.Background(), registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("registration rolebinding should be deleted")
			}
			if !errors.IsNotFound(err) {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			// Work rolebinding should be deleted
			wrb, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			if err == nil {
				// Here we check DeletionTimestamp because there is finalizer "cluster.open-cluster-management.io/manifest-work-cleanup" on the rolebinding.
				if wrb.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work rolebinding should be deleted")
				}
				return nil
			}
			if !errors.IsNotFound(err) {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	})
})
