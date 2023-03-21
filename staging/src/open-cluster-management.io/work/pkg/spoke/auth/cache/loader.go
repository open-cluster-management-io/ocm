package cache

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/auth/store"
)

type manifestWorkExecutorCachesLoader interface {
	// loadAllValuableCaches gets all manifestworks with service account executor at the current time, and then
	// stores these executors and corresponding resources in the form of store.ExecutorCaches data structure.
	// Note that this method only guarantees the correctness of all keys in store.ExecutorCaches, and the
	// value is usually fake. so callers are recommended to only use this to know what executors and resources
	// should be cached in the current state of the cluster.
	loadAllValuableCaches(*store.ExecutorCaches)
}

type defaultManifestWorkExecutorCachesLoader struct {
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	restMapper         meta.RESTMapper
}

func (g *defaultManifestWorkExecutorCachesLoader) loadAllValuableCaches(retainableCache *store.ExecutorCaches) {
	if retainableCache == nil {
		return
	}
	mws, err := g.manifestWorkLister.List(labels.Everything())
	if err != nil {
		klog.Infof("cleanup cache, list manifestworks error: %v", err)
		return
	}

	for _, mw := range mws {
		var executor string
		if mw.Spec.Executor == nil {
			continue
		}
		if mw.Spec.Executor.Subject.Type != workapiv1.ExecutorSubjectTypeServiceAccount {
			continue
		}
		if mw.Spec.Executor.Subject.ServiceAccount == nil {
			continue
		}

		executor = store.ExecutorKey(
			mw.Spec.Executor.Subject.ServiceAccount.Namespace, mw.Spec.Executor.Subject.ServiceAccount.Name)

		for index, manifest := range mw.Spec.Workload.Manifests {
			// parse the required and set resource meta
			required := &unstructured.Unstructured{}
			if err := required.UnmarshalJSON(manifest.Raw); err != nil {
				klog.Infof("UnmarshalJSON for the manifest work %s index %d failed %v", mw.Name, index, err)
				continue
			}

			resMeta, gvr, err := helper.BuildResourceMeta(index, required, g.restMapper)
			if err != nil {
				klog.Infof("Build resource meta for the manifest work %s index %d failed %v", mw.Name, index, err)
				continue
			}

			// check if the resource to be applied should be owned by the manifest work
			ownedByTheWork := helper.OwnedByTheWork(gvr, resMeta.Namespace, resMeta.Name,
				mw.Spec.DeleteOption)

			retainableCache.Upsert(executor, store.Dimension{
				Group:         resMeta.Group,
				Version:       resMeta.Version,
				Resource:      resMeta.Resource,
				Namespace:     resMeta.Namespace,
				Name:          resMeta.Name,
				ExecuteAction: store.GetExecuteAction(ownedByTheWork),
			},
				nil, // nil means we do not know if it is allowed or not
			)

		}
	}
}
