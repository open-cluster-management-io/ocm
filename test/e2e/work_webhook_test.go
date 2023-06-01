package e2e

import (
	"context"
	"fmt"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/ocm/test/integration/util"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/retry"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// Test cases with lable "sanity-check" could be ran on an existing enviroment with validating webhook installed
// and well configured as sanity check. Resource leftovers should be cleaned up on both hub and managed cluster.
var _ = ginkgo.Describe("ManifestWork admission webhook", ginkgo.Label("validating-webhook", "sanity-check"), func() {
	var nameSuffix string
	var workName, klusterletName, clusterName string

	ginkgo.BeforeEach(func() {
		nameSuffix = rand.String(5)
		workName = fmt.Sprintf("w1-%s", nameSuffix)

		if deployKlusterlet {
			klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
			clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
			agentNamespace := fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
			_, err := t.CreateApprovedKlusterlet(klusterletName, clusterName, agentNamespace, operatorapiv1.InstallModeDefault)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	})

	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete manifestwork %v/%v", clusterName, workName))
		gomega.Expect(t.cleanManifestWorks(clusterName, workName)).To(gomega.BeNil())
		if deployKlusterlet {
			ginkgo.By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
			gomega.Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(gomega.BeNil())
		}
	})

	ginkgo.Context("Creating a manifestwork", func() {
		ginkgo.It("Should respond bad request when creating a manifestwork with no manifests", func() {
			work := newManifestWork(clusterName, workName, []runtime.Object{}...)
			_, err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})

		ginkgo.It("Should respond bad request when creating a manifest with no name", func() {
			work := newManifestWork(clusterName, workName, []runtime.Object{util.NewConfigmap("default", "", nil, nil)}...)
			_, err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
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
				_, err := t.HubKubeClient.RbacV1().Roles(clusterName).Create(
					context.TODO(), &rbacv1.Role{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: clusterName,
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
				_, err = t.HubKubeClient.RbacV1().RoleBindings(clusterName).Create(
					context.TODO(), &rbacv1.RoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: clusterName,
							Name:      roleName,
						},
						Subjects: []rbacv1.Subject{
							{
								Kind:      "ServiceAccount",
								Namespace: clusterName,
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
				err := t.HubKubeClient.RbacV1().Roles(clusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				}

				// delete the temporary rolebinding
				err = t.HubKubeClient.RbacV1().RoleBindings(clusterName).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
				}
			})

			ginkgo.It("Should respond bad request when no permission for nil executor", func() {
				work := newManifestWork(clusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)

				// impersonate as a hub user without execute-as permission
				impersonatedConfig := *t.HubClusterCfg
				impersonatedConfig.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", clusterName, hubUser)
				impersonatedHubWorkClient, err := workclientset.NewForConfig(&impersonatedConfig)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				_, err = impersonatedHubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			})
		})
	})

	ginkgo.Context("Updating a manifestwork", func() {
		var err error

		ginkgo.BeforeEach(func() {
			work := newManifestWork(clusterName, workName, []runtime.Object{util.NewConfigmap("default", "cm1", nil, nil)}...)
			_, err = t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Should respond bad request when a manifestwork with invalid manifests", func() {
			manifest := workapiv1.Manifest{}
			manifest.Object = util.NewConfigmap("default", "", nil, nil)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
				_, err = t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), work, metav1.UpdateOptions{})
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
				work, err := t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests...)
				_, err = t.HubWorkClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
		})
	})
})
