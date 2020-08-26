package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds

	guestbookCrdJson = `{
		"apiVersion": "apiextensions.k8s.io/v1beta1",
		"kind": "CustomResourceDefinition",
		"metadata": {
			"name": "guestbooks.my.domain"
		},
		"spec": {
			"conversion": {
				"strategy": "None"
			},
			"group": "my.domain",
			"names": {
				"kind": "Guestbook",
				"listKind": "GuestbookList",
				"plural": "guestbooks",
				"singular": "guestbook"
			},
			"preserveUnknownFields": true,
			"scope": "Namespaced",
			"validation": {
				"openAPIV3Schema": {
					"properties": {
						"apiVersion": {
							"type": "string"
						},
						"kind": {
							"type": "string"
						},
						"metadata": {
							"type": "object"
						},
						"spec": {
							"properties": {
								"foo": {
									"type": "string"
								}
							},
							"type": "object"
						},
						"status": {
							"type": "object"
						}
					},
					"type": "object"
				}
			},
			"version": "v1",
			"versions": [
				{
					"name": "v1",
					"served": true,
					"storage": true
				}
			]
		}
	}`

	guestbookCrJson = `{
		"apiVersion": "my.domain/v1",
		"kind": "Guestbook",
		"metadata": {
			"name": "guestbook1",
			"namespace": "default"
		},
		"spec": {
			"foo": "bar"
		}
	}`
)

var _ = ginkgo.Describe("Work agent", func() {
	ginkgo.Context("Work CRUD", func() {
		var ns1 string
		var ns2 string
		var err error

		ginkgo.BeforeEach(func() {
			ns1 = fmt.Sprintf("ns1-%s", nameSuffix)
			ns2 = fmt.Sprintf("ns2-%s", nameSuffix)

			// create ns2
			ns := &corev1.Namespace{}
			ns.Name = ns2
			_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

		ginkgo.AfterEach(func() {
			// delete ns2
			err = spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), ns2, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Should create, update and delete manifestwork successfully", func() {
			ginkgo.By("create manifestwork")
			cmFinalizers := []string{"cluster.open-cluster-management.io/testing"}
			objects := []runtime.Object{
				newConfigmap(ns1, "cm1", nil, nil),
				newNamespace(ns1),
				newConfigmap(ns1, "cm2", nil, nil),
				newConfigmap(ns2, "cm3", nil, cmFinalizers),
			}
			work := newManifestWork(clusterName, fmt.Sprintf("w1-%s", nameSuffix), objects...)
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if resources are applied for manifests
			gomega.Eventually(func() bool {
				_, err := spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return false
				}

				_, err = spokeKubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
				if err != nil {
					return false
				}

				_, err = spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return false
				}

				_, err = spokeKubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				if err != nil {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// check status conditions in manifestwork status
			gomega.Eventually(func() bool {
				work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestApplied), expectedManifestStatuses); !ok {
					return false
				}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestAvailable), expectedManifestStatuses); !ok {
					return false
				}

				// check work status condition
				return haveCondition(work.Status.Conditions, string(workapiv1.WorkApplied), metav1.ConditionTrue) &&
					haveCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionTrue)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// get the corresponding AppliedManifestWork
			var appliedManifestWork *workapiv1.AppliedManifestWork
			gomega.Eventually(func() bool {
				appliedManifestWorkList, err := hubWorkClient.WorkV1().AppliedManifestWorks().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return false
				}

				for _, item := range appliedManifestWorkList.Items {
					if strings.HasSuffix(item.Name, work.Name) {
						appliedManifestWork = &item
						return true
					}
				}

				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// check applied resources in manifestwork status
			expectedAppliedResources := []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "configmaps", Namespace: ns1, Name: "cm1"},
				{Version: "v1", Resource: "configmaps", Namespace: ns1, Name: "cm2"},
				{Version: "v1", Resource: "configmaps", Namespace: ns2, Name: "cm3"},
				{Version: "v1", Resource: "namespaces", Name: ns1},
			}
			actualAppliedResources := []workapiv1.AppliedManifestResourceMeta{}
			for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
				actualAppliedResources = append(actualAppliedResources, workapiv1.AppliedManifestResourceMeta{
					Group:     appliedResource.Group,
					Version:   appliedResource.Version,
					Resource:  appliedResource.Resource,
					Namespace: appliedResource.Namespace,
					Name:      appliedResource.Name,
				})
			}
			gomega.Expect(reflect.DeepEqual(actualAppliedResources, expectedAppliedResources)).To(gomega.BeTrue())

			ginkgo.By("update manifestwork")
			cmData := map[string]string{"x": "y"}
			newObjects := []runtime.Object{
				objects[1],
				objects[2],
				newConfigmap(ns2, "cm3", cmData, cmFinalizers),
			}
			newWork := newManifestWork(clusterName, work.Name, newObjects...)
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests = newWork.Spec.Workload.Manifests
				work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			// check if cm1 is removed from applied resources list in status
			gomega.Eventually(func() bool {
				appliedManifestWork, err = hubWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWork.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, resource := range appliedManifestWork.Status.AppliedResources {
					if resource.Name == "cm1" {
						return false
					}
				}

				work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestApplied), expectedManifestStatuses); !ok {
					return false
				}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestAvailable), expectedManifestStatuses); !ok {
					return false
				}

				// check work status condition
				return haveCondition(work.Status.Conditions, string(workapiv1.WorkApplied), metav1.ConditionTrue) &&
					haveCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionTrue)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// check if cm1 is deleted
			_, err = spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// check if cm3 is updated
			gomega.Eventually(func() bool {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				if err != nil {
					return false
				}

				if !reflect.DeepEqual(cm.Data, cmData) {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("delete manifestwork")
			err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// remove finalizer from cm3 in 2 seconds
			timer := time.NewTimer(2 * time.Second)
			go func() {
				<-timer.C
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				if err == nil {
					cm.Finalizers = nil
					_, _ = spokeKubeClient.CoreV1().ConfigMaps(ns2).Update(context.Background(), cm, metav1.UpdateOptions{})
				}
			}()

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Once manifest work is deleted, its corresponding appliedManifestWorks should be deleted as well
			_, err = hubWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWork.Name, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// Once manifest work is deleted, all applied resources should have already been deleted too
			_, err = spokeKubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})
	})

	ginkgo.Context("With CRD/CR", func() {
		var crNamespace string
		var workName string
		var err error

		ginkgo.BeforeEach(func() {
			crNamespace = fmt.Sprintf("ns3-%s", nameSuffix)
			workName = fmt.Sprintf("w2-%s", nameSuffix)

			// create namespace for cr
			ns := &corev1.Namespace{}
			ns.Name = crNamespace
			_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			// delete work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), workName, metav1.DeleteOptions{})
			if err != nil {
				gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			}

			// delete namespace for cr
			err = spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), crNamespace, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should create crd/cr with aggregated cluster role successfully", func() {
			crd, err := newCrd()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterRoleName := fmt.Sprintf("cr-%s", nameSuffix)
			clusterRole := newAggregatedClusterRole(clusterRoleName, "my.domain", "guestbooks")

			cr, err := newCr(crNamespace, "cr1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			objects := []runtime.Object{crd, clusterRole, cr}
			work := newManifestWork(clusterName, workName, objects...)
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check status conditions in manifestwork status
			gomega.Eventually(func() bool {
				work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				// check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestApplied), expectedManifestStatuses); !ok {
					return false
				}
				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.ManifestAvailable), expectedManifestStatuses); !ok {
					return false
				}

				// check work status condition
				return haveCondition(work.Status.Conditions, string(workapiv1.WorkApplied), metav1.ConditionTrue) &&
					haveCondition(work.Status.Conditions, string(workapiv1.WorkAvailable), metav1.ConditionTrue)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})
})

func newManifestWork(namespace, name string, objects ...runtime.Object) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{}

	work.Namespace = namespace
	if name != "" {
		work.Name = name
	} else {
		work.GenerateName = "work-"
	}

	var manifests []workapiv1.Manifest
	for _, object := range objects {
		manifest := workapiv1.Manifest{}
		manifest.Object = object
		manifests = append(manifests, manifest)
	}
	work.Spec.Workload.Manifests = manifests

	return work
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newConfigmap(namespace, name string, data map[string]string, finalizers []string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			Finalizers: finalizers,
		},
		Data: data,
	}
	return cm
}

func haveCondition(conditions []workapiv1.StatusCondition, expectedType string, expectedStatus metav1.ConditionStatus) bool {
	found := false
	for _, condition := range conditions {
		if condition.Type != expectedType {
			continue
		}
		found = true

		if condition.Status != expectedStatus {
			return false
		}
		return true
	}

	return found
}

func haveManifestCondition(conditions []workapiv1.ManifestCondition, expectedType string, expectedStatuses []metav1.ConditionStatus) bool {
	if len(conditions) != len(expectedStatuses) {
		return false
	}

	for index, condition := range conditions {
		expectedStatus := expectedStatuses[index]
		if expectedStatus == "" {
			continue
		}

		if ok := haveCondition(condition.Conditions, expectedType, expectedStatus); !ok {
			return false
		}
	}

	return true
}

func newCrd() (crd *unstructured.Unstructured, err error) {
	crd, err = loadResourceFromJSON(guestbookCrdJson)
	return crd, err
}

func newCr(namespace, name string) (cr *unstructured.Unstructured, err error) {
	cr, err = loadResourceFromJSON(guestbookCrJson)
	if err != nil {
		return nil, err
	}

	cr.SetNamespace(namespace)
	cr.SetName(name)
	return cr, nil
}

func loadResourceFromJSON(json string) (*unstructured.Unstructured, error) {
	obj := unstructured.Unstructured{}
	err := obj.UnmarshalJSON([]byte(json))
	return &obj, err
}

func newAggregatedClusterRole(name, apiGroup, resource string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"rbac.authorization.k8s.io/aggregate-to-admin": "true",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{apiGroup},
				Resources: []string{resource},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
}
