package util

import (
	"context"
	"reflect"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
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
		return HaveCondition(work.Status.Conditions, expectedType, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

// check if work is deleted
func AssertWorkDeleted(namespace, name string, manifests []workapiv1.Manifest, workClient workclientset.Interface, kubeClient kubernetes.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		_, err := workClient.WorkV1().ManifestWorks(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			return false
		}

		for _, manifest := range manifests {
			expected := manifest.Object.(*corev1.ConfigMap)
			_, err = kubeClient.CoreV1().ConfigMaps(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
			if !apierrors.IsNotFound(err) {
				return false
			}
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
func AssertManifestsApplied(manifests []workapiv1.Manifest, kubeClient kubernetes.Interface, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		for _, manifest := range manifests {
			expected := manifest.Object.(*corev1.ConfigMap)
			actual, err := kubeClient.CoreV1().ConfigMaps(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}

			if !reflect.DeepEqual(actual.Data, expected.Data) {
				return false
			}
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}
