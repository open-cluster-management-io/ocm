package registration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Cluster deleting", func() {
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
	})

	It("Cluster is deleting, all addons and non-addon works should be deleted", func(ctx SpecContext) {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		addon1 := testinghelpers.NewManagedClusterAddons("addon1", managedCluster.Name, []string{"hold"}, nil)
		_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Create(ctx,
			addon1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		manifestWork1 := testinghelpers.NewManifestWork(managedCluster.Name, "work1", []string{"hold"}, nil, nil, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(ctx, manifestWork1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		manifestWork2 := testinghelpers.NewManifestWork(managedCluster.Name, "work2", []string{"hold"}, nil,
			map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(ctx, manifestWork2, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		manifestWork3 := testinghelpers.NewManifestWork(managedCluster.Name, "work3", []string{"hold"}, nil,
			map[string]string{clusterv1.CleanupPriorityAnnotationKey: "20"}, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(ctx, manifestWork3, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// check rbac are created
		Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx,
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx,
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// check finalizers are added
		Eventually(func() error {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx,
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// delete cluster
		err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedCluster.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// addons should be deleting and manifestworks should not be deleting
		Eventually(func() error {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if addon.DeletionTimestamp.IsZero() {
				return fmt.Errorf("addon is not deleting")
			}

			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, work := range works.Items {
				if !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on addon1
		Eventually(func() error {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			addon.Finalizers = []string{}
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Update(ctx,
				addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// addon should be deleted. work1 should be deleting ,the other work should not be deleting
		Eventually(func() bool {
			_, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(ctx, metav1.ListOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on work1
		Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(ctx,
				"work1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(ctx,
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// work1 should be deleted, work3 should be deleting, work2 should not be deleting
		Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(ctx, metav1.ListOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on work3
		Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(ctx,
				"work3", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(ctx,
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// work3 should be deleted, work2 should be deleting
		Eventually(func() error {
			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(ctx, metav1.ListOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// managedCluster should have deleting condition
		Eventually(func() error {
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
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
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on work2
		Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(ctx,
				"work2", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(ctx,
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// all rbac should be deleted
		Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoles().Get(ctx,
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx,
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// managedCluster should be deleted
		Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("Cluster is gone, all addons/works and cluster rabc should be deleted", func(ctx SpecContext) {
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, managedCluster, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		addon1 := testinghelpers.NewManagedClusterAddons("addon1", managedCluster.Name, []string{"hold"}, nil)
		_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Create(ctx,
			addon1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		manifestWork1 := testinghelpers.NewManifestWork(managedCluster.Name, "work1", []string{"hold"}, nil, nil, nil)
		_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Create(ctx, manifestWork1, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// check rbac are created
		Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx,
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx,
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Eventually(func() error {
			if _, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{}); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// delete cluster and make sure cluster is gone
		Eventually(func() error {
			err = clusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedCluster.Name, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			cluster, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			cluster.SetFinalizers([]string{})
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx, cluster, metav1.UpdateOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("wait for the cluster is gone")
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// addons should be deleting and manifestworks should not be deleting
		Eventually(func() error {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if addon.DeletionTimestamp.IsZero() {
				return fmt.Errorf("addon is not deleting")
			}

			works, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, work := range works.Items {
				if !work.DeletionTimestamp.IsZero() {
					return fmt.Errorf("work is not deleting")
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on addon1
		Eventually(func() error {
			addon, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			addon.Finalizers = []string{}
			_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Update(ctx,
				addon, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// remove finalizer on work1
		Eventually(func() error {
			work, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(ctx,
				"work1", metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}

			work.Finalizers = []string{}
			_, err = workClient.WorkV1().ManifestWorks(managedCluster.Name).Update(ctx,
				work, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// addon and work should be deleted
		Eventually(func() bool {
			_, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(managedCluster.Name).Get(ctx,
				"addon1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// work1 should be deleted
		Eventually(func() bool {
			_, err := workClient.WorkV1().ManifestWorks(managedCluster.Name).Get(ctx,
				"work1", metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// all rbac should be deleted
		Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoles().Get(ctx,
				mclClusterRoleName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx,
				mclClusterRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				registrationRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() bool {
			_, err := kubeClient.RbacV1().RoleBindings(managedCluster.Name).Get(ctx,
				workRoleBindingName(managedCluster.Name), metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

	})
})
