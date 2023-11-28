package source

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	"open-cluster-management.io/api/cloudevents/generic"
	"open-cluster-management.io/api/cloudevents/generic/types"
	"open-cluster-management.io/api/cloudevents/work/watcher"
	workv1 "open-cluster-management.io/api/work/v1"
)

const ManifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"

func newManifestWorkStatusHandler(lister workv1lister.ManifestWorkLister, watcher *watcher.ManifestWorkWatcher) generic.ResourceHandler[*workv1.ManifestWork] {
	return func(action types.ResourceAction, work *workv1.ManifestWork) error {
		switch action {
		case types.StatusModified:
			works, err := lister.ManifestWorks(work.Namespace).List(labels.Everything())
			if err != nil {
				return err
			}

			var lastWork *workv1.ManifestWork
			for _, w := range works {
				if w.UID == work.UID {
					lastWork = w
					break
				}
			}

			if lastWork == nil {
				return fmt.Errorf("failed to find last work with id %s", work.UID)
			}

			if work.Generation < lastWork.Generation {
				klog.Infof("The work %s generation %d is less than cached generation %d, ignore",
					work.UID, work.Generation, lastWork.Generation)
				return nil
			}

			// no status change
			if equality.Semantic.DeepEqual(lastWork.Status, work.Status) {
				return nil
			}

			// restore the fields that are maintained by local agent
			work.Name = lastWork.Name
			work.Namespace = lastWork.Namespace
			work.Labels = lastWork.Labels
			work.Annotations = lastWork.Annotations
			work.DeletionTimestamp = lastWork.DeletionTimestamp
			work.Spec = lastWork.Spec

			if meta.IsStatusConditionTrue(work.Status.Conditions, ManifestsDeleted) {
				work.Finalizers = []string{}
				klog.Infof("delete work %s/%s in the source", work.Namespace, work.Name)
				watcher.Receive(watch.Event{Type: watch.Deleted, Object: work})
				return nil
			}

			// the work is handled by agent, we make sure the finalizer here
			work.Finalizers = []string{ManifestWorkFinalizer}
			watcher.Receive(watch.Event{Type: watch.Modified, Object: work})
		default:
			return fmt.Errorf("unsupported resource action %s", action)
		}

		return nil
	}
}
