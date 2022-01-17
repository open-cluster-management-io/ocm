package util

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func AssertWorkCondition(namespace, name string, workClient workclientset.Interface, expectedType string, expectedWorkStatus metav1.ConditionStatus,
	expectedManifestStatuses []metav1.ConditionStatus, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		work, err := workClient.WorkV1().ManifestWorks(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check manifest status conditions
		if ok := HaveManifestCondition(work.Status.ResourceStatus.Manifests, expectedType, expectedManifestStatuses); !ok {
			return false
		}

		// check work status condition
		return meta.IsStatusConditionPresentAndEqual(work.Status.Conditions, expectedType, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func AssertWorkGeneration(namespace, name string, workClient workclientset.Interface, expectedType string, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		work, err := workClient.WorkV1().ManifestWorks(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check manifest status conditions
		condition := meta.FindStatusCondition(work.Status.Conditions, expectedType)
		if condition == nil {
			return false
		}

		if condition.ObservedGeneration != work.Generation {
			return false
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if work is deleted
func AssertWorkDeleted(namespace, name, hubhash string, manifests []workapiv1.Manifest, workClient workclientset.Interface, kubeClient kubernetes.Interface, eventuallyTimeout, eventuallyInterval int) {
	// wait for deletion of manifest work
	gomega.Eventually(func() bool {
		_, err := workClient.WorkV1().ManifestWorks(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			return false
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	// wait for deletion of appliedmanifestwork
	appliedManifestWorkName := fmt.Sprintf("%s-%s", hubhash, name)
	AssertAppliedManifestWorkDeleted(appliedManifestWorkName, workClient, eventuallyTimeout, eventuallyInterval)

	// Once manifest work is deleted, all applied resources should have already been deleted too
	for _, manifest := range manifests {
		expected := manifest.Object.(*corev1.ConfigMap)
		_, err := kubeClient.CoreV1().ConfigMaps(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
	}
}

func AssertAppliedManifestWorkDeleted(name string, workClient workclientset.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		_, err := workClient.WorkV1().AppliedManifestWorks().Get(context.Background(), name, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			return false
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if finalizer is added
func AssertFinalizerAdded(namespace, name string, workClient workclientset.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		work, err := workClient.WorkV1().ManifestWorks(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		for _, finalizer := range work.Finalizers {
			if finalizer == "cluster.open-cluster-management.io/manifest-work-cleanup" {
				return true
			}
		}
		return false
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if all manifests are applied
func AssertExistenceOfConfigMaps(manifests []workapiv1.Manifest, kubeClient kubernetes.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() error {
		for _, manifest := range manifests {
			expected := manifest.Object.(*corev1.ConfigMap)
			actual, err := kubeClient.CoreV1().ConfigMaps(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !reflect.DeepEqual(actual.Data, expected.Data) {
				return fmt.Errorf("configmap should be equal to %v, but got %v", expected.Data, actual.Data)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

// check the existence of resource with GVR, namespace and name
func AssertExistenceOfResources(gvrs []schema.GroupVersionResource, namespaces, names []string, dynamicClient dynamic.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Expect(gvrs).To(gomega.HaveLen(len(namespaces)))
	gomega.Expect(gvrs).To(gomega.HaveLen(len(names)))

	gomega.Eventually(func() bool {
		for i := range gvrs {
			_, err := GetResource(namespaces[i], names[i], gvrs[i], dynamicClient)
			if err != nil {
				return false
			}
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if resource with GVR, namespace and name does not exists
func AssertNonexistenceOfResources(gvrs []schema.GroupVersionResource, namespaces, names []string, dynamicClient dynamic.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Expect(gvrs).To(gomega.HaveLen(len(namespaces)))
	gomega.Expect(gvrs).To(gomega.HaveLen(len(names)))

	gomega.Eventually(func() bool {
		for i := range gvrs {
			_, err := GetResource(namespaces[i], names[i], gvrs[i], dynamicClient)
			if !apierrors.IsNotFound(err) {
				return false
			}
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if applied resources in work status are updated correctly
func AssertAppliedResources(hubHash, workName string, gvrs []schema.GroupVersionResource, namespaces, names []string, workClient workclientset.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Expect(gvrs).To(gomega.HaveLen(len(namespaces)))
	gomega.Expect(gvrs).To(gomega.HaveLen(len(names)))

	var appliedResources []workapiv1.AppliedManifestResourceMeta
	for i := range gvrs {
		appliedResources = append(appliedResources, workapiv1.AppliedManifestResourceMeta{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     gvrs[i].Group,
				Resource:  gvrs[i].Resource,
				Namespace: namespaces[i],
				Name:      names[i],
			},
			Version: gvrs[i].Version,
		})
	}

	sort.SliceStable(appliedResources, func(i, j int) bool {
		switch {
		case appliedResources[i].Group != appliedResources[j].Group:
			return appliedResources[i].Group < appliedResources[j].Group
		case appliedResources[i].Version != appliedResources[j].Version:
			return appliedResources[i].Version < appliedResources[j].Version
		case appliedResources[i].Resource != appliedResources[j].Resource:
			return appliedResources[i].Resource < appliedResources[j].Resource
		case appliedResources[i].Namespace != appliedResources[j].Namespace:
			return appliedResources[i].Namespace < appliedResources[j].Namespace
		default:
			return appliedResources[i].Name < appliedResources[j].Name
		}
	})

	gomega.Eventually(func() bool {
		appliedManifestWorkName := fmt.Sprintf("%s-%s", hubHash, workName)
		appliedManifestWork, err := workClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// remove uid from each AppliedManifestResourceMeta
		var actualAppliedResources []workapiv1.AppliedManifestResourceMeta
		for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
			actualAppliedResources = append(actualAppliedResources, workapiv1.AppliedManifestResourceMeta{
				ResourceIdentifier: workapiv1.ResourceIdentifier{
					Group:     appliedResource.Group,
					Resource:  appliedResource.Resource,
					Namespace: appliedResource.Namespace,
					Name:      appliedResource.Name,
				},
				Version: appliedResource.Version,
			})
		}

		return reflect.DeepEqual(actualAppliedResources, appliedResources)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}
