package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
)

var WorkCreatedCondition = metav1.Condition{Type: "Created", Status: metav1.ConditionTrue}
var WorkUpdatedCondition = metav1.Condition{Type: "Updated", Status: metav1.ConditionTrue}

func AssertWorkFinalizers(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(work.Finalizers) != 1 {
		return fmt.Errorf("expected finalizers on the work, but got %v", work.Finalizers)
	}

	return nil
}

func AssertUpdatedWork(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(work.Spec.Workload.Manifests) != 2 {
		return fmt.Errorf("unexpected work spec %v", len(work.Spec.Workload.Manifests))
	}

	return nil
}

func AssertWorkStatus(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string, condition metav1.Condition) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if !meta.IsStatusConditionTrue(work.Status.Conditions, condition.Type) {
		return fmt.Errorf("unexpected status %v", work.Status.Conditions)
	}

	return nil
}

func AddWorkFinalizer(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// add finalizers firstly
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"uid":             work.GetUID(),
			"resourceVersion": work.GetResourceVersion(),
			"finalizers":      []string{"work-test-finalizer"},
		},
	})
	if err != nil {
		return err
	}

	_, err = workClient.Patch(ctx, work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func RemoveWorkFinalizer(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if work.DeletionTimestamp.IsZero() {
		return fmt.Errorf("expected work is deleting, but got %v", work)
	}

	// remove the finalizers
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"uid":             work.GetUID(),
			"resourceVersion": work.GetResourceVersion(),
			"finalizers":      []string{},
		},
	})
	if err != nil {
		return err
	}

	_, err = workClient.Patch(ctx, work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func UpdateWork(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string, withVersion bool) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	newWork := work.DeepCopy()
	if withVersion {
		gen, ok := newWork.Annotations[common.CloudEventsResourceVersionAnnotationKey]
		if !ok {
			return fmt.Errorf("the generation annotation is not found")
		}

		generation, err := strconv.Atoi(gen)
		if err != nil {
			return err
		}

		newWork.Annotations[common.CloudEventsResourceVersionAnnotationKey] = fmt.Sprintf("%d", generation+1)
	}

	newWork.Spec.Workload.Manifests = []workv1.Manifest{
		NewManifest("test1"),
		NewManifest("test2"),
	}

	patchBytes := patchWork(work, newWork)
	_, err = workClient.Patch(ctx, work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})

	return err
}

func UpdateWorkStatus(ctx context.Context, workClient workv1client.ManifestWorkInterface, name string, condition metav1.Condition) error {
	work, err := workClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// update the work status
	newWork := work.DeepCopy()
	newConditions := append(work.Status.Conditions, condition)
	newWork.Status = workv1.ManifestWorkStatus{Conditions: newConditions}

	oldData, err := json.Marshal(work)
	if err != nil {
		return err
	}

	newData, err := json.Marshal(newWork)
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return err
	}

	_, err = workClient.Patch(ctx, work.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return err
	}

	return nil
}

func NewManifestWork(namespace, name string, withVersion bool) *workv1.ManifestWork {
	work := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					NewManifest("test"),
				},
			},
		},
	}

	if withVersion {
		work.Annotations = map[string]string{
			common.CloudEventsResourceVersionAnnotationKey: "1",
		}
	}

	return work
}

func NewManifestWorkWithStatus(namespace, name string) *workv1.ManifestWork {
	work := NewManifestWork(namespace, name, true)
	work.Status = workv1.ManifestWorkStatus{Conditions: []metav1.Condition{{Type: "Created", Status: metav1.ConditionTrue}}}
	return work
}

func NewManifest(name string) workv1.Manifest {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      name,
		},
		Data: map[string]string{"test": "test"},
	}
	manifest := workv1.Manifest{}
	manifest.Object = cm
	return manifest
}

func patchWork(old, new *workv1.ManifestWork) []byte {
	oldData, err := json.Marshal(old)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	newData, err := json.Marshal(new)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	return patchBytes
}
