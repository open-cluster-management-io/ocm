package registration_test

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/hub"
)

var _ = Describe("ManagedCluster set hubAcceptsClient from true to false", Ordered, Label("managedcluster-accept"), func() {
	var managedCluster *clusterv1.ManagedCluster
	var testCustomLabel = "custom-label"
	var testCustomLabelValue = "custom-value"
	var testCustomLabel2 = "custom-label2"
	var testCustomLabelValue2 = "custom-value2"

	BeforeAll(func() {
		// stop the hub and start new hub with the updated option
		stopHub()

		awsHubOptionWithLabelSetting := hub.NewHubManagerOptions()
		awsHubOptionWithLabelSetting.Labels = fmt.Sprintf("%s=%s,%s=%s", testCustomLabel, testCustomLabelValue, testCustomLabel2, testCustomLabelValue2)
		startHub(awsHubOptionWithLabelSetting)

		// stop hub with awsOption and restart hub with default option
		DeferCleanup(func() {
			stopHub()
			startHub(hubOption)
		})
	})

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
		// Checks if the namespace has the required labels
		Eventually(func() error {
			namespace, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if namespace.Labels != nil && namespace.Labels[testCustomLabel] != "" &&
				namespace.Labels[testCustomLabel] != testCustomLabelValue || namespace.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("namespace %s should  have custom label", managedCluster.Name)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// Check rbac files should be created
		Eventually(func() error {
			// check clusterrole is correct
			clusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(clusterRole.Rules) != 4 {
				return fmt.Errorf("expected 4 rules, got %d rules", len(clusterRole.Rules))
			}
			if clusterRole.Labels[testCustomLabel] != testCustomLabelValue || clusterRole.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("clusterRole %s does not have expected labels", mclClusterRoleName(managedCluster.Name))
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			clusterRoleBinding, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(),
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if clusterRoleBinding.Labels[testCustomLabel] != testCustomLabelValue || clusterRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("clusterRoleBinding %s does not have expected labels", mclClusterRoleBindingName(managedCluster.Name))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			registrationRoleBinding, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).
				Get(context.Background(), registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if registrationRoleBinding.Labels[testCustomLabel] != testCustomLabelValue || registrationRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("roleBinding %s does not have expected labels", registrationRoleBindingName(managedCluster.Name))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			workRoleBinding, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if workRoleBinding.Labels[testCustomLabel] != testCustomLabelValue || workRoleBinding.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("workRoleBinding %s does not have expected labels", workRoleBindingName(managedCluster.Name))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			registrationClusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				"open-cluster-management:managedcluster:registration", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if registrationClusterRole.Labels[testCustomLabel] != testCustomLabelValue || registrationClusterRole.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("clusterRole %s open-cluster-management:managedcluster:registration does not have expected labels",
					mclClusterRoleName(managedCluster.Name))
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			workClusterRole, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				"open-cluster-management:managedcluster:work", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if workClusterRole.Labels[testCustomLabel] != testCustomLabelValue || workClusterRole.Labels[testCustomLabel2] != testCustomLabelValue2 {
				return fmt.Errorf("clusterRole %s open-cluster-management:managedcluster:work does not have expected labels", mclClusterRoleName(managedCluster.Name))
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		manifestWork := testinghelpers.NewManifestWork(managedCluster.Name, "test-work-1", []string{}, nil, nil, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
	It("should set hubAcceptsClient to false", func() {
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, v1.ManagedClusterConditionHubAccepted) {
				return fmt.Errorf("managed cluster %s should have hub accepted condition", managedCluster.Name)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
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

		// Work rolebinding should be deleted after manifestworks are removed
		err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Delete(context.Background(), "test-work-1", metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			// Work rolebinding should be deleted
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(), workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, 3*eventuallyTimeout, eventuallyInterval).Should(BeTrue())

	})
})
