package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

// Test cases with lable "sanity-check" could be ran on an existing environment with validating webhook installed
// and well configured as sanity check. Resource leftovers should be cleaned up on both hub and managed cluster.
var _ = Describe("ManifestWork admission webhook", Label("validating-webhook", "sanity-check"), func() {
	var nameSuffix string
	var workName string

	BeforeEach(func() {
		nameSuffix = rand.String(5)
		workName = fmt.Sprintf("w1-%s", nameSuffix)
	})

	AfterEach(func() {
		By(fmt.Sprintf("delete manifestwork %v/%v", universalClusterName, workName))
		Expect(hub.CleanManifestWorks(universalClusterName, workName)).To(BeNil())
	})

	Context("Creating a manifestwork", func() {
		It("Should respond bad request when creating a manifestwork with no manifests", func() {
			work := newManifestWork(universalClusterName, workName)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			Expect(err).To(HaveOccurred())
			Expect(errors.IsBadRequest(err)).Should(BeTrue())
		})

		It("Should respond bad request when creating a manifest with no name", func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "", nil, nil)}...)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			Expect(err).To(HaveOccurred())
			Expect(errors.IsBadRequest(err)).Should(BeTrue())
		})

		It("Should respond bad request when creating a manifestwork with duplicate manifests", func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{
				util.NewConfigmap("default", "cm1", nil, nil),
				util.NewConfigmap("default", "cm1", nil, nil), // duplicate
			}...)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			Expect(err).To(HaveOccurred())
			Expect(errors.IsBadRequest(err)).Should(BeTrue())
		})

		Context("executor", func() {
			var hubUser string
			var roleName string

			BeforeEach(func() {
				hubUser = fmt.Sprintf("sa-%s", nameSuffix)
				roleName = fmt.Sprintf("role-%s", nameSuffix)

				if !nilExecutorValidating {
					Skip(fmt.Sprintf("feature gate %q is disabled", "NilExecutorValidating"))
				}

				// create a temporary role
				_, err := hub.KubeClient.RbacV1().Roles(universalClusterName).Create(
					context.TODO(), &rbacv1.Role{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: universalClusterName,
							Name:      roleName,
						},
						Rules: []rbacv1.PolicyRule{
							{
								Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
								APIGroups: []string{"work.open-cluster-management.io"},
								Resources: []string{"manifestworks"},
							},
						},
					}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())

				// create a temporary rolebinding
				_, err = hub.KubeClient.RbacV1().RoleBindings(universalClusterName).Create(
					context.TODO(), &rbacv1.RoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: universalClusterName,
							Name:      roleName,
						},
						Subjects: []rbacv1.Subject{
							{
								Kind:      "ServiceAccount",
								Namespace: universalClusterName,
								Name:      hubUser,
							},
						},
						RoleRef: rbacv1.RoleRef{
							APIGroup: "rbac.authorization.k8s.io",
							Kind:     "Role",
							Name:     roleName,
						},
					}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				// delete the temporary role
				err := hub.KubeClient.RbacV1().Roles(universalClusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}

				// delete the temporary rolebinding
				err = hub.KubeClient.RbacV1().RoleBindings(universalClusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			})

			It("Should respond bad request when no permission for nil executor", func() {
				work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)

				// impersonate as a hub user without execute-as permission
				hubClusterCfg, err := clientcmd.BuildConfigFromFlags("", hubKubeconfig)
				Expect(err).ToNot(HaveOccurred())
				impersonatedConfig := *hubClusterCfg
				impersonatedConfig.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", universalClusterName, hubUser)
				impersonatedHubWorkClient, err := workclientset.NewForConfig(&impersonatedConfig)
				Expect(err).ToNot(HaveOccurred())
				_, err = impersonatedHubWorkClient.WorkV1().ManifestWorks(universalClusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				Expect(err).To(HaveOccurred())
				if !errors.IsBadRequest(err) {
					// not bad request, assert true=false to show the error message
					Expect(err).ToNot(HaveOccurred())
				}
			})
		})
	})

	Context("Updating a manifestwork", func() {
		var err error

		BeforeEach(func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should respond bad request when a manifestwork with invalid manifests", func() {
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "", nil, nil)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			Expect(err).To(HaveOccurred())
			Expect(errors.IsBadRequest(err)).Should(BeTrue())
		})

		It("Should respond bad request when the size of manifests is more than the limit", func() {
			manifests := []workapiv1.Manifest{
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test1", 100*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test2", 100*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test3", 100*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test4", 100*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test5", 100*1024),
					},
				},
			}

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests...)
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			Expect(err).To(HaveOccurred())
			Expect(errors.IsBadRequest(err)).Should(BeTrue())
		})
	})
})
