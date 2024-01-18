package handler

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

// NewManifestWorkAgentHandler returns a ResourceHandler for a ManifestWork on managed cluster. It sends the kube events
// with ManifestWorWatcher after CloudEventAgentClient received the ManifestWork specs from source, then the
// ManifestWorkInformer handles the kube events in its local cache.
func NewManifestWorkAgentHandler(lister workv1lister.ManifestWorkNamespaceLister, watcher *watcher.ManifestWorkWatcher) generic.ResourceHandler[*workv1.ManifestWork] {
	return func(action types.ResourceAction, work *workv1.ManifestWork) error {
		switch action {
		case types.Added:
			watcher.Receive(watch.Event{Type: watch.Added, Object: work})
		case types.Modified:
			lastWork, err := lister.Get(work.Name)
			if err != nil {
				return err
			}

			resourceVersion, err := strconv.ParseInt(work.ResourceVersion, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse the resourceVersion of the manifestwork %s, %v", work.Name, err)
			}

			lastResourceVersion, err := strconv.ParseInt(lastWork.ResourceVersion, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse the resourceVersion of the manifestwork %s, %v", lastWork.Name, err)
			}

			if resourceVersion <= lastResourceVersion {
				klog.Infof("The work %s resource version is less than or equal to cached, ignore", work.Name)
				return nil
			}

			updatedWork := work.DeepCopy()

			// restore the fields that are maintained by local agent
			updatedWork.Labels = lastWork.Labels
			updatedWork.Annotations = lastWork.Annotations
			updatedWork.Finalizers = lastWork.Finalizers
			updatedWork.Status = lastWork.Status

			watcher.Receive(watch.Event{Type: watch.Modified, Object: updatedWork})
		case types.Deleted:
			// the manifestwork is deleting on the source, we just update its deletion timestamp.
			lastWork, err := lister.Get(work.Name)
			if errors.IsNotFound(err) {
				return nil
			}

			if err != nil {
				return err
			}

			updatedWork := lastWork.DeepCopy()
			updatedWork.DeletionTimestamp = work.DeletionTimestamp
			watcher.Receive(watch.Event{Type: watch.Modified, Object: updatedWork})
		default:
			return fmt.Errorf("unsupported resource action %s", action)
		}

		return nil
	}
}
