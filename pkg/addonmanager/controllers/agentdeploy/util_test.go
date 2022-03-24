package agentdeploy

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestApplyWork(t *testing.T) {
	cache := newWorkCache()
	fakeWorkClient := fakework.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	syncContext := addontesting.NewFakeSyncContext(t, "test")

	work, _, _ := buildManifestWorkFromObject("cluster1", "addon1", []runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})

	_, err := applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, syncContext.Recorder(), work)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}

	addontesting.AssertActions(t, fakeWorkClient.Actions(), "create")

	// IF work is not changed, we should not update
	newWorkCopy := work.DeepCopy()
	fakeWorkClient.ClearActions()
	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work)
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, syncContext.Recorder(), newWorkCopy)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// Update work spec to update it
	newWork, _, _ := buildManifestWorkFromObject("cluster1", "addon1", []runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})
	newWork.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan}
	fakeWorkClient.ClearActions()
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, syncContext.Recorder(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "update")

	// Do not update if generation is not changed
	work.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground}
	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work)

	fakeWorkClient.ClearActions()
	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work)
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, syncContext.Recorder(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// change generation will cause update
	work.Generation = 1
	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work)

	fakeWorkClient.ClearActions()
	workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work)
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, syncContext.Recorder(), newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "update")
}
