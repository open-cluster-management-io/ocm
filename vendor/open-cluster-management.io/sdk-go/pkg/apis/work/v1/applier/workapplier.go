package applier

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type WorkApplier struct {
	cache      *workCache
	getWork    func(ctx context.Context, namespace, name string) (*workapiv1.ManifestWork, error)
	deleteWork func(ctx context.Context, namespace, name string) error
	patchWork  func(ctx context.Context, namespace, name string, pt types.PatchType, data []byte) (*workapiv1.ManifestWork, error)
	createWork func(ctx context.Context, work *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error)
}

func NewWorkApplierWithTypedClient(workClient workv1client.Interface,
	workLister worklister.ManifestWorkLister) *WorkApplier {
	return &WorkApplier{
		cache: newWorkCache(),
		getWork: func(ctx context.Context, namespace, name string) (*workapiv1.ManifestWork, error) {
			return workLister.ManifestWorks(namespace).Get(name)
		},
		deleteWork: func(ctx context.Context, namespace, name string) error {
			return workClient.WorkV1().ManifestWorks(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		},
		createWork: func(ctx context.Context, work *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
			return workClient.WorkV1().ManifestWorks(work.Namespace).Create(ctx, work, metav1.CreateOptions{})
		},
		patchWork: func(ctx context.Context, namespace, name string, pt types.PatchType, data []byte) (*workapiv1.ManifestWork, error) {
			return workClient.WorkV1().ManifestWorks(namespace).Patch(ctx, name, pt, data, metav1.PatchOptions{})
		},
	}
}

func (w *WorkApplier) Apply(ctx context.Context, work *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
	existingWork, err := w.getWork(ctx, work.Namespace, work.Name)
	existingWork = existingWork.DeepCopy()
	if errors.IsNotFound(err) {
		existingWork, err = w.createWork(ctx, work)
		switch {
		case errors.IsAlreadyExists(err):
			return work, nil
		case err != nil:
			return nil, err
		default:
			w.cache.updateCache(work, existingWork)
			return existingWork, nil
		}
	}

	if err != nil {
		return nil, err
	}

	if w.cache.safeToSkipApply(work, existingWork) {
		return existingWork, nil
	}

	if ManifestWorkEqual(work, existingWork) {
		return existingWork, nil
	}

	oldData, err := json.Marshal(&workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Labels:          existingWork.Labels,
			Annotations:     existingWork.Annotations,
			OwnerReferences: existingWork.OwnerReferences,
		},
		Spec: existingWork.Spec,
	})
	if err != nil {
		return existingWork, err
	}

	newData, err := json.Marshal(&workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			UID:             existingWork.UID,
			ResourceVersion: existingWork.ResourceVersion,
			Labels:          work.Labels,
			Annotations:     work.Annotations,
			OwnerReferences: work.OwnerReferences,
		},
		Spec: work.Spec,
	})
	if err != nil {
		return existingWork, err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return existingWork, fmt.Errorf("failed to create patch for addon %s: %w", existingWork.Name, err)
	}

	klog.V(2).Infof("Patching work %s/%s with %s", existingWork.Namespace, existingWork.Name, string(patchBytes))
	updated, err := w.patchWork(ctx, existingWork.Namespace, existingWork.Name, types.MergePatchType, patchBytes)
	if err == nil {
		w.cache.updateCache(work, existingWork)
		return updated, nil
	}
	return nil, err
}

func (w *WorkApplier) Delete(ctx context.Context, namespace, name string) error {
	err := w.deleteWork(ctx, namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	w.cache.removeCache(name, namespace)
	return nil
}

func shouldUpdateMap(required, existing map[string]string) bool {
	if len(required) != len(existing) {
		return true
	}
	for key, value := range required {
		if existing[key] != value {
			return true
		}
	}
	return false
}

func ManifestWorkEqual(new, old *workapiv1.ManifestWork) bool {
	mutatedNewWork := mutateWork(new)
	mutatedOldWork := mutateWork(old)
	if !equality.Semantic.DeepEqual(mutatedNewWork.Spec, mutatedOldWork.Spec) {
		return false
	}

	if shouldUpdateMap(mutatedNewWork.Annotations, mutatedOldWork.Annotations) {
		return false
	}
	if shouldUpdateMap(mutatedNewWork.Labels, mutatedOldWork.Labels) {
		return false
	}
	if !equality.Semantic.DeepEqual(mutatedNewWork.OwnerReferences, mutatedOldWork.OwnerReferences) {
		return false
	}
	return true
}

// mutate work to easy compare works.
func mutateWork(work *workapiv1.ManifestWork) *workapiv1.ManifestWork {
	mutatedWork := work.DeepCopy()
	newManifests := []workapiv1.Manifest{}
	for _, manifest := range work.Spec.Workload.Manifests {
		unstructuredManifest := &unstructured.Unstructured{}
		if err := unstructuredManifest.UnmarshalJSON(manifest.Raw); err != nil {
			klog.Errorf("failed to unmarshal work manifest.err: %v", err)
			return mutatedWork
		}

		// the filed creationTimestamp should be removed before compare since it is added during transition from raw data to runtime object.
		unstructured.RemoveNestedField(unstructuredManifest.Object, "metadata", "creationTimestamp")
		unstructured.RemoveNestedField(unstructuredManifest.Object, "spec", "template", "metadata", "creationTimestamp")

		newManifestRaw, err := unstructuredManifest.MarshalJSON()
		if err != nil {
			klog.Errorf("failed to 	marshal work manifest.err: %v", err)
			return mutatedWork
		}
		newManifests = append(newManifests, workapiv1.Manifest{RawExtension: runtime.RawExtension{Raw: newManifestRaw}})
	}

	mutatedWork.Spec.Workload.Manifests = newManifests
	return mutatedWork
}
