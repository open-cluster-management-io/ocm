package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	apiserviceName = "v1.admission.work.open-cluster-management.io"
	admissionName  = "manifestworkvalidators.admission.work.open-cluster-management.io"
)

var _ = ginkgo.Describe("ManifestWork admission webhook", func() {
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
	})

	ginkgo.Context("Creating a manifestwork", func() {
		ginkgo.It("Should respond bad request when creating a manifestwork with no manifests", func() {
			work := newManifestWork(clusterName, "", []runtime.Object{}...)
			_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: manifests should not be empty",
				admissionName,
			)))
		})

		ginkgo.It("Should respond bad request when creating a manifest with no name", func() {
			work := newManifestWork(clusterName, "", []runtime.Object{newConfigmap("default", "", nil, nil)}...)
			_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: name must be set in manifest",
				admissionName,
			)))
		})
	})

	ginkgo.Context("Updating a manifestwork", func() {
		var work *workapiv1.ManifestWork
		var err error

		ginkgo.BeforeEach(func() {
			work = newManifestWork(clusterName, "", []runtime.Object{newConfigmap("default", "cm1", nil, nil)}...)
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Should respond bad request when a manifestwork with invalid manifests", func() {
			manifest := workapiv1.Manifest{}
			manifest.Object = newConfigmap("default", "", nil, nil)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: name must be set in manifest",
				admissionName,
			)))
		})

		ginkgo.It("Should respond bad request when the size of manifests is more than the limit", func() {
			manifests := []workapiv1.Manifest{
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test1", 10*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test2", 10*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test3", 10*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test4", 10*1024),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Object: newSecretBySize("default", "test5", 10*1024),
					},
				},
			}

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests...)
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			})

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.HavePrefix(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: the size of manifests is", admissionName)))
		})

		ginkgo.It("Should respond bad request when no permission for nil executor", func() {
			ns := clusterName
			hubUser := "sa1"
			roleName := "role1"
			_, err = spokeKubeClient.RbacV1().Roles(ns).Create(
				context.TODO(), &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ns,
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
			_, err = spokeKubeClient.RbacV1().RoleBindings(ns).Create(
				context.TODO(), &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ns,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: ns,
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

			work := newManifestWork(ns, "", []runtime.Object{newConfigmap("default", "cm1", nil, nil)}...)

			// impersonate as a hub user without execute-as permission
			impersonatedConfig := *restConfig
			impersonatedConfig.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", ns, hubUser)
			impersonatedHubWorkClient, err := workclientset.NewForConfig(&impersonatedConfig)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err = impersonatedHubWorkClient.WorkV1().ManifestWorks(ns).Create(
				context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(errors.IsBadRequest(err)).Should(gomega.BeTrue())
			gomega.Expect(err.Error()).Should(gomega.Equal(fmt.Sprintf(
				"admission webhook \"%s\" denied the request: user system:serviceaccount:%s:%s cannot manipulate the Manifestwork with executor /klusterlet-work-sa in namespace %s",
				admissionName, ns, hubUser, ns,
			)))
		})
	})
})
