package workapplier

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeWork(name, namespace string, obj runtime.Object) *workapiv1.ManifestWork {
	rawObject, _ := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)

	return &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{
					{
						RawExtension: runtime.RawExtension{Raw: rawObject},
					},
				},
			},
			DeleteOption:    nil,
			ManifestConfigs: nil,
		},
	}
}

func TestWorkApplierWithTypedClient(t *testing.T) {
	fakeWorkClient := fakework.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	fakeWorkLister := workInformerFactory.Work().V1().ManifestWorks().Lister()
	workApplier := NewWorkApplierWithTypedClient(fakeWorkClient, fakeWorkLister)

	work := newFakeWork("test", "test", addontesting.NewUnstructured("batch/v1", "Job", "default", "test"))
	_, err := workApplier.Apply(context.TODO(), work)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}

	addontesting.AssertActions(t, fakeWorkClient.Actions(), "create")

	// IF work is not changed, we should not update
	newWorkCopy := work.DeepCopy()
	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work); err != nil {
		t.Errorf("failed to add work to store with err %v", err)
	}
	_, err = workApplier.Apply(context.TODO(), newWorkCopy)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// Update work spec to update it
	newWork := newFakeWork("test", "test", addontesting.NewUnstructured("batch/v1", "Job", "default", "test"))
	newWork.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan}
	fakeWorkClient.ClearActions()
	_, err = workApplier.Apply(context.TODO(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "patch")

	// Do not update if generation is not changed
	work.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground}
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}

	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}
	_, err = workApplier.Apply(context.TODO(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// change generation will cause update
	work.Generation = 1
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}

	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}
	_, err = workApplier.Apply(context.TODO(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "patch")

	fakeWorkClient.ClearActions()
	err = workApplier.Delete(context.TODO(), newWork.Namespace, newWork.Name)
	if err != nil {
		t.Errorf("failed to delete work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "delete")

}

func TestNewWorkApplierWithClient(t *testing.T) {
	var testScheme = scheme.Scheme
	testScheme.AddKnownTypes(workapiv1.GroupVersion, &workapiv1.ManifestWork{})
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects().Build()
	workApplier := NewWorkApplierWithRuntimeClient(fakeClient)
	newWork := newFakeWork("test", "test", addontesting.NewUnstructured("batch/v1", "Job", "default", "test"))

	_, err := workApplier.Apply(context.TODO(), newWork)
	if err != nil {
		t.Errorf("failed to apply work. err %v", err)
	}

	_, err = workApplier.getWork(context.TODO(), newWork.Namespace, newWork.Name)
	if err != nil {
		t.Errorf("failed to get work. err %v", err)
	}

	newWork.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground}
	_, err = workApplier.Apply(context.TODO(), newWork)
	if err != nil {
		t.Errorf("failed to apply work. err %v", err)
	}

	getWork, err := workApplier.getWork(context.TODO(), newWork.Namespace, newWork.Name)
	if err != nil {
		t.Errorf("failed to get work. err %v", err)
	}
	if getWork.Spec.DeleteOption == nil {
		t.Errorf("failed to update work")
	}

	err = workApplier.Delete(context.TODO(), newWork.Namespace, newWork.Name)
	if err != nil {
		t.Errorf("failed to delete work. err %v", err)
	}

	_, err = workApplier.getWork(context.TODO(), newWork.Namespace, newWork.Name)
	if err == nil || !errors.IsNotFound(err) {
		t.Errorf("the work should be deleted. err %v", err)
	}

}
