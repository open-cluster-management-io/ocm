package agentdeploy

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestCache(t *testing.T) {
	cache := newWorkCache()

	requiredWork := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "cluster1",
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Object: addontesting.NewUnstructured("v1", "ConfigMap", "test", "default"),
						},
					},
				},
			},
		},
	}

	existingWork := requiredWork.DeepCopy()
	existingWork.Generation = 1

	cache.updateCache(requiredWork, existingWork)

	if !cache.safeToSkipApply(requiredWork, existingWork) {
		t.Errorf("Expect skip apply")
	}

	existingWork.Generation = 2

	if cache.safeToSkipApply(requiredWork, existingWork) {
		t.Errorf("should update work when generation is changed")
	}

	existingWork.Generation = 1
	requiredWork.Spec.Workload.Manifests = []workapiv1.Manifest{
		{
			RawExtension: runtime.RawExtension{
				Object: addontesting.NewUnstructured("v1", "ConfigMap", "test1", "default"),
			},
		},
	}

	if cache.safeToSkipApply(requiredWork, existingWork) {
		t.Errorf("should update work when required spec is changed")
	}

	cache.removeCache(requiredWork.Name, requiredWork.Namespace)

	if cache.safeToSkipApply(requiredWork, existingWork) {
		t.Errorf("should update work if related cache is not found")
	}
}
