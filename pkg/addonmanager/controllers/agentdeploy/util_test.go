package agentdeploy

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestApplyWork(t *testing.T) {
	cache := newWorkCache()
	fakeWorkClient := fakework.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

	managedClusterAddon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "addon1",
		},
	}
	work, _, _ := newManagedManifestWorkBuilder(false).buildManifestWorkFromObject("cluster1", managedClusterAddon,
		[]runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})

	_, err := applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, work)
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
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWorkCopy)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// Update work spec to update it
	newWork, _, _ := newManagedManifestWorkBuilder(false).buildManifestWorkFromObject("cluster1", managedClusterAddon,
		[]runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})
	newWork.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan}
	fakeWorkClient.ClearActions()
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
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
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
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
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "patch")
}
