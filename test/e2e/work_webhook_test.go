package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
var _ = ginkgo.Describe("ManifestWork admission webhook", ginkgo.Label("validating-webhook", "sanity-check"), func() {
	var nameSuffix string
	var workName string

	ginkgo.BeforeEach(func() {
		nameSuffix = rand.String(5)
		workName = fmt.Sprintf("w1-%s", nameSuffix)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete manifestwork %v/%v", universalClusterName, workName))
		gomega.Expect(hub.CleanManifestWorks(universalClusterName, workName)).To(gomega.BeNil())
	})

	ginkgo.Context("Creating a manifestwork", func() {
		ginkgo.It("Should respond bad request when creating a manifestwork with no manifests", func() {
			work := newManifestWork(universalClusterName, workName)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})

		ginkgo.It("Should respond bad request when creating a manifest with no name", func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "", nil, nil)}...)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})

		ginkgo.It("Should respond bad request when creating a manifestwork with duplicate manifests", func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{
				util.NewConfigmap("default", "cm1", nil, nil),
				util.NewConfigmap("default", "cm1", nil, nil), // duplicate
			}...)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})

		ginkgo.Context("executor", func() {
			var hubUser string
			var roleName string

			ginkgo.BeforeEach(func() {
				hubUser = fmt.Sprintf("sa-%s", nameSuffix)
				roleName = fmt.Sprintf("role-%s", nameSuffix)

				if !nilExecutorValidating {
					ginkgo.Skip(fmt.Sprintf("feature gate %q is disabled", "NilExecutorValidating"))
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
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			})

			ginkgo.AfterEach(func() {
				// delete the temporary role
				err := hub.KubeClient.RbacV1().Roles(universalClusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				}

				// delete the temporary rolebinding
				err = hub.KubeClient.RbacV1().RoleBindings(universalClusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				}
			})

			ginkgo.It("Should respond bad request when no permission for nil executor", func() {
				work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)

				// impersonate as a hub user without execute-as permission
				hubClusterCfg, err := clientcmd.BuildConfigFromFlags("", hubKubeconfig)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				impersonatedConfig := *hubClusterCfg
				impersonatedConfig.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", universalClusterName, hubUser)
				impersonatedHubWorkClient, err := workclientset.NewForConfig(&impersonatedConfig)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				_, err = impersonatedHubWorkClient.WorkV1().ManifestWorks(universalClusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				if !errors.IsBadRequest(err) {
					// not bad request, assert true=false to show the error message
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				}
			})
		})
	})

	ginkgo.Context("Updating a manifestwork", func() {
		var err error

		ginkgo.BeforeEach(func() {
			work := newManifestWork(universalClusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Should respond bad request when a manifestwork with invalid manifests", func() {
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "", nil, nil)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})

		ginkgo.It("Should respond bad request when the size of manifests is more than the limit", func() {
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
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests...)
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})
	})
})
