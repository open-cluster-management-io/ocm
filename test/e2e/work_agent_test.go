package e2e

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

var _ = ginkgo.Describe("Work agent", func() {
	var clusterName = "cluster1"
	var ns1 = fmt.Sprintf("ns1-%s", rand.String(8))
	var ns2 = fmt.Sprintf("ns2-%s", rand.String(8))
	var err error

	ginkgo.BeforeEach(func() {
		// create ns2
		ns := &corev1.Namespace{}
		ns.Name = ns2
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
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
		work := newManifestWork(clusterName, "", objects...)
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
			if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.WorkApplied), expectedManifestStatuses); !ok {
				return false
			}

			// check work status condition
			return haveCondition(work.Status.Conditions, string(workapiv1.WorkApplied), metav1.ConditionTrue)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// check applied resources in manifestwork status
		expectedAppliedResources := []workapiv1.AppliedManifestResourceMeta{
			{Version: "v1", Resource: "configmaps", Namespace: ns1, Name: "cm1"},
			{Version: "v1", Resource: "configmaps", Namespace: ns1, Name: "cm2"},
			{Version: "v1", Resource: "configmaps", Namespace: ns2, Name: "cm3"},
			{Version: "v1", Resource: "namespaces", Name: ns1},
		}
		gomega.Expect(reflect.DeepEqual(work.Status.AppliedResources, expectedAppliedResources)).To(gomega.BeTrue())

		ginkgo.By("update manifestwork")
		cmData := map[string]string{"x": "y"}
		newObjects := []runtime.Object{
			objects[1],
			objects[2],
			newConfigmap(ns2, "cm3", cmData, cmFinalizers),
		}
		newWork := newManifestWork(clusterName, work.Name, newObjects...)
		work.Spec.Workload.Manifests = newWork.Spec.Workload.Manifests
		work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Update(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// check if cm1 is removed from applied resources list in status
		gomega.Eventually(func() bool {
			work, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}

			for _, resource := range work.Status.AppliedResources {
				if resource.Name == "cm1" {
					return false
				}
			}

			// check manifest status conditions
			expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
			if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, string(workapiv1.WorkApplied), expectedManifestStatuses); !ok {
				return false
			}

			// check work status condition
			return haveCondition(work.Status.Conditions, string(workapiv1.WorkApplied), metav1.ConditionTrue)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// check if cm1 is deleted
		_, err := spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
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
		go func() {
			time.Sleep(2 * time.Second)
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

		// Once manifest work is deleted, all applied resources should have already been deleted too
		_, err = spokeKubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

		_, err = spokeKubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

		_, err = spokeKubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
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
