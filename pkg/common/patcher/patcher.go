package patcher

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// Patcher is just the Patch API with a generic to keep use sites type safe.
// This is inspired by the commiter code in https://github.com/kcp-dev/kcp/blob/main/pkg/reconciler/committer/committer.go
type PatchClient[R runtime.Object] interface {
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (R, error)
}

type Patcher[R runtime.Object, Sp any, St any] interface {
	AddFinalizer(context.Context, R, ...string) (bool, error)
	RemoveFinalizer(context.Context, R, ...string) error
	PatchStatus(context.Context, R, St, St) (bool, error)
	PatchSpec(context.Context, R, Sp, Sp) (bool, error)
	PatchLabelAnnotations(context.Context, R, metav1.ObjectMeta, metav1.ObjectMeta) (bool, error)
}

// Resource is a generic wrapper around resources so we can generate patches.
type Resource[Sp any, St any] struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              Sp `json:"spec"`
	Status            St `json:"status,omitempty"`
}

type patcher[R runtime.Object, Sp any, St any] struct {
	client PatchClient[R]
}

func NewPatcher[R runtime.Object, Sp any, St any](client PatchClient[R]) *patcher[R, Sp, St] {
	p := &patcher[R, Sp, St]{
		client: client,
	}
	return p
}

func (p *patcher[R, Sp, St]) AddFinalizer(ctx context.Context, object R, finalizers ...string) (bool, error) {

	accessor, err := meta.Accessor(object)
	if err != nil {
		return false, err
	}

	existingFinalizers := accessor.GetFinalizers()
	finalizersToAdd := []string{}
	for _, finalizer := range finalizers {
		hasFinalizer := false
		for i := range existingFinalizers {
			if existingFinalizers[i] == finalizer {
				hasFinalizer = true
				break
			}
		}

		if !hasFinalizer {
			finalizersToAdd = append(finalizersToAdd, finalizer)
		}
	}

	if len(finalizersToAdd) > 0 {
		finalizerBytes, err := json.Marshal(append(existingFinalizers, finalizersToAdd...))
		if err != nil {
			return false, err
		}
		patch := fmt.Sprintf("{\"metadata\": {\"finalizers\": %s}}", string(finalizerBytes))

		_, err = p.client.Patch(
			ctx, accessor.GetName(), types.MergePatchType, []byte(patch), metav1.PatchOptions{})
		return true, err
	}

	return false, nil
}

func (p *patcher[R, Sp, St]) RemoveFinalizer(ctx context.Context, object R, finalizers ...string) error {
	accessor, err := meta.Accessor(object)
	if err != nil {
		return err
	}

	copiedFinalizers := []string{}
	existingFinalizers := accessor.GetFinalizers()
	for i := range existingFinalizers {
		matchFinalizer := false
		for _, finalizer := range finalizers {
			if existingFinalizers[i] == finalizer {
				matchFinalizer = true
				break
			}
		}

		if !matchFinalizer {
			copiedFinalizers = append(copiedFinalizers, existingFinalizers[i])
		}
	}

	if len(existingFinalizers) != len(copiedFinalizers) {
		finalizerBytes, err := json.Marshal(copiedFinalizers)
		if err != nil {
			return err
		}
		patch := fmt.Sprintf("{\"metadata\": {\"finalizers\": %s}}", string(finalizerBytes))

		_, err = p.client.Patch(
			ctx, accessor.GetName(), types.MergePatchType, []byte(patch), metav1.PatchOptions{})
		return err
	}

	return nil
}

func (p *patcher[R, Sp, St]) patch(ctx context.Context, object R, newObject, oldObject *Resource[Sp, St], subresources ...string) error {
	accessor, err := meta.Accessor(object)
	if err != nil {
		return err
	}

	oldData, err := json.Marshal(oldObject)
	if err != nil {
		return fmt.Errorf("failed to Marshal old data for %s: %w", accessor.GetName(), err)
	}

	newObject.UID = accessor.GetUID()
	newObject.ResourceVersion = accessor.GetResourceVersion()
	newData, err := json.Marshal(newObject)
	if err != nil {
		return fmt.Errorf("failed to Marshal new data for %s: %w", accessor.GetName(), err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for %s: %w", accessor.GetName(), err)
	}

	_, err = p.client.Patch(
		ctx, accessor.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{}, subresources...)
	if err != nil {
		klog.V(2).Infof("Object with type %T and name %s is patched with patch %s", object, accessor.GetName(), string(patchBytes))
	}
	return err
}

func (p *patcher[R, Sp, St]) PatchStatus(ctx context.Context, object R, newStatus, oldStatus St) (bool, error) {
	statusChanged := !equality.Semantic.DeepEqual(oldStatus, newStatus)
	if !statusChanged {
		return false, nil
	}

	oldObject := &Resource[Sp, St]{Status: oldStatus}
	newObject := &Resource[Sp, St]{Status: newStatus}

	return true, p.patch(ctx, object, newObject, oldObject, "status")
}

func (p *patcher[R, Sp, St]) PatchSpec(ctx context.Context, object R, newSpec, oldSpec Sp) (bool, error) {
	specChanged := !equality.Semantic.DeepEqual(newSpec, oldSpec)
	if !specChanged {
		return false, nil
	}

	oldObject := &Resource[Sp, St]{Spec: oldSpec}
	newObject := &Resource[Sp, St]{Spec: newSpec}
	return true, p.patch(ctx, object, newObject, oldObject)
}

func (p *patcher[R, Sp, St]) PatchLabelAnnotations(ctx context.Context, object R, newObject, oldObject metav1.ObjectMeta) (bool, error) {
	annotationPatch := p.mapPatch(newObject.Annotations, oldObject.Annotations)
	labelPatch := p.mapPatch(newObject.Labels, oldObject.Labels)
	if len(annotationPatch) == 0 && len(labelPatch) == 0 {
		return false, nil
	}

	accessor, err := meta.Accessor(object)
	if err != nil {
		return false, err
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"UID":             accessor.GetUID(),
			"resourceVersion": accessor.GetResourceVersion(),
		},
	}

	if len(annotationPatch) > 0 {
		if err := unstructured.SetNestedField(patch, annotationPatch, "metadata", "annotations"); err != nil {
			return false, err // should never happen
		}
	}
	if len(labelPatch) > 0 {
		if err := unstructured.SetNestedField(patch, labelPatch, "metadata", "labels"); err != nil {
			return false, err // should never happen
		}
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return false, err
	}

	_, err = p.client.Patch(
		ctx, accessor.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return true, err
}

func (p *patcher[R, Sp, St]) mapPatch(newMap, oldMap map[string]string) map[string]interface{} {
	mapPatch := map[string]interface{}{}
	for k, v := range newMap {
		if oldMap == nil {
			mapPatch[k] = v
		}

		if oldV, ok := oldMap[k]; !ok || v != oldV {
			mapPatch[k] = v
		}
	}

	for k := range oldMap {
		if newMap == nil {
			mapPatch[k] = nil
		}
		if _, ok := newMap[k]; !ok {
			mapPatch[k] = nil
		}
	}

	return mapPatch
}
