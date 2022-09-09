package workapplier

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func NewWorkApplierWithRuntimeClient(workClient client.Client) *WorkApplier {
	return &WorkApplier{
		cache: newWorkCache(),
		getWork: func(ctx context.Context, namespace, name string) (*workapiv1.ManifestWork, error) {
			work := &workapiv1.ManifestWork{}
			err := workClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, work)
			return work, err
		},
		deleteWork: func(ctx context.Context, namespace, name string) error {
			work := &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			return workClient.Delete(ctx, work)
		},
		patchWork: func(ctx context.Context, namespace, name string, pt types.PatchType, data []byte) (*workapiv1.ManifestWork, error) {
			work := &workapiv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			if err := workClient.Patch(ctx, work, client.RawPatch(pt, data)); err != nil {
				return nil, err
			}
			if err := workClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, work); err != nil {
				return nil, err
			}
			return work, nil
		},
		createWork: func(ctx context.Context, work *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
			err := workClient.Create(ctx, work)
			return work, err
		},
	}
}

func (w *WorkApplier) Apply(ctx context.Context, work *workapiv1.ManifestWork) (*workapiv1.ManifestWork, error) {
	existingWork, err := w.getWork(ctx, work.Namespace, work.Name)
	existingWork = existingWork.DeepCopy()
	if err != nil {
		if errors.IsNotFound(err) {
			existingWork, err = w.createWork(ctx, work)
			if err == nil {
				w.cache.updateCache(work, existingWork)
				return existingWork, nil
			}
			return nil, err
		}
		return nil, err
	}

	if w.cache.safeToSkipApply(work, existingWork) {
		return existingWork, nil
	}

	if manifestWorkSpecEqual(work.Spec, existingWork.Spec) {
		return existingWork, nil
	}

	oldData, err := json.Marshal(&workapiv1.ManifestWork{
		Spec: existingWork.Spec,
	})
	if err != nil {
		return existingWork, err
	}

	newData, err := json.Marshal(&workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			UID:             existingWork.UID,
			ResourceVersion: existingWork.ResourceVersion,
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

func manifestsEqual(new, old []workapiv1.Manifest) bool {
	if len(new) != len(old) {
		return false
	}

	for i := range new {
		if !equality.Semantic.DeepEqual(new[i].Raw, old[i].Raw) {
			return false
		}
	}
	return true
}

func manifestWorkSpecEqual(new, old workapiv1.ManifestWorkSpec) bool {
	if !manifestsEqual(new.Workload.Manifests, old.Workload.Manifests) {
		return false
	}
	if !equality.Semantic.DeepEqual(new.ManifestConfigs, old.ManifestConfigs) {
		return false
	}
	if !equality.Semantic.DeepEqual(new.DeleteOption, old.DeleteOption) {
		return false
	}
	return true
}
