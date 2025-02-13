package registration_test

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

var (
	mclClusterRoleName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s", clusterName)
	}
	mclClusterRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s", clusterName)
	}
	registrationRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s:registration", clusterName)
	}
	workRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s:work", clusterName)
	}
)

var _ = ginkgo.Describe("Cluster deleting", func() {
	var managedCluster *clusterv1.ManagedCluster

	ginkgo.BeforeEach(func() {
		managedClusterName := fmt.Sprintf("managedcluster-%s", rand.String(6))
		managedCluster = &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
	})

	ginkgo.It("Cluster is deleting, all addons and non-addon works should be deleted", func() {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		addon1 := testinghelpers.NewManagedClusterAddons("addon1", managedCluster.Name, []string{"hold"}, nil)
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Create(context.Background(),
			addon1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		manifestWork1 := testinghelpers.NewManifestWork(managedCluster.Name, "work1", []string{"hold"}, nil, nil, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		manifestWork2 := testinghelpers.NewManifestWork(managedCluster.Name, "work2", []string{"hold"}, nil,
			map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork2, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		manifestWork3 := testinghelpers.NewManifestWork(managedCluster.Name, "work3", []string{"hold"}, nil,
			map[string]string{clusterv1.CleanupPriorityAnnotationKey: "20"}, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork3, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// check rbac are created
		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(),
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// check finalizers are added
		gomega.Eventually(func() error {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(),
				managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !commonhelpers.HasFinalizer(cluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
				return fmt.Errorf("managedCluster.Finalizers does not contain api-resource-cleanup")
			}
			if !commonhelpers.HasFinalizer(cluster.Finalizers, commonhelpers.GcFinalizer) {
				return fmt.Errorf("managedCluster.Finalizers does not contain resource-cleanup")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// delete cluster
		err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedCluster.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// addons should be deleting and manifestworks should not be deleting
		gomega.Eventually(func() error {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if addon.DeletionTimestamp.IsZero() {
				return fmt.Errorf("addon is not deleting")
			}

			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, work := range works.Items {
				if !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on addon1
		gomega.Eventually(func() error {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			addon.Finalizers = []string{}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Update(context.Background(),
				addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// addon should be deleted. work1 should be deleting ,the other work should not be deleting
		gomega.Eventually(func() bool {
			_, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(works.Items) != 3 {
				return fmt.Errorf("work count is not 3")
			}
			for _, work := range works.Items {
				if work.Name == "work1" && work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work1 is not deleting")
				}
				if work.Name == "work2" && !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work2 is not deleting")
				}
				if work.Name == "work3" && !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work3 is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on work1
		gomega.Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(),
				"work1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(context.Background(),
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// work1 should be deleted, work3 should be deleting, work2 should not be deleting
		gomega.Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(works.Items) != 2 {
				return fmt.Errorf("work count is not 2")
			}
			for _, work := range works.Items {
				if work.Name == "work3" && work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work3 is not deleting")
				}
				if work.Name == "work2" && !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work2 is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on work3
		gomega.Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(),
				"work3", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(context.Background(),
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// work3 should be deleted, work2 should be deleting
		gomega.Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(works.Items) != 1 {
				return fmt.Errorf("work count is not 1")
			}
			for _, work := range works.Items {
				if work.Name == "work2" && !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work2 is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// managedCluster should have deleting condition
		gomega.Eventually(func() error {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			condition := v1helpers.FindCondition(cluster.Status.Conditions, clusterv1.ManagedClusterConditionDeleting)
			if condition == nil {
				return fmt.Errorf("deleting condition is not found")
			}
			if condition.Reason != clusterv1.ConditionDeletingReasonResourceRemaining {
				return fmt.Errorf("deleting condition reason is not %q", clusterv1.ConditionDeletingReasonResourceRemaining)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on work2
		gomega.Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(),
				"work2", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(context.Background(),
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// all rbac should be deleted
		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(),
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// managedCluster should be deleted
		gomega.Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})

	ginkgo.It("Cluster is gone, all addons/works and cluster rabc should be deleted", func() {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		addon1 := testinghelpers.NewManagedClusterAddons("addon1", managedCluster.Name, []string{"hold"}, nil)
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Create(context.Background(),
			addon1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		manifestWork1 := testinghelpers.NewManifestWork(managedCluster.Name, "work1", []string{"hold"}, nil, nil, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(context.Background(), manifestWork1, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// check rbac are created
		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(),
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		gomega.Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// delete cluster and make sure cluster is gone
		gomega.Eventually(func() error {
			err = clusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedCluster.Name, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedCluster.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			cluster.SetFinalizers([]string{})
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), cluster, metav1.UpdateOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("wait for the cluster is gone")
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// addons should be deleting and manifestworks should not be deleting
		gomega.Eventually(func() error {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if addon.DeletionTimestamp.IsZero() {
				return fmt.Errorf("addon is not deleting")
			}

			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, work := range works.Items {
				if !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on addon1
		gomega.Eventually(func() error {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			addon.Finalizers = []string{}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Update(context.Background(),
				addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// remove finalizer on work1
		gomega.Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(),
				"work1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(context.Background(),
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		// addon and work should be deleted
		gomega.Eventually(func() bool {
			_, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.Background(),
				"addon1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// work1 should be deleted
		gomega.Eventually(func() bool {
			_, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(context.Background(),
				"work1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// all rbac should be deleted
		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(),
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(),
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(context.Background(),
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	})
})
