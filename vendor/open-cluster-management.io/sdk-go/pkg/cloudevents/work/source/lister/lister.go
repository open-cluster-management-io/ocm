package lister

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"
)

// WatcherStoreLister list the ManifestWorks from the WorkClientWatcherStore.
type WatcherStoreLister struct {
	store store.WorkClientWatcherStore
}

func NewWatcherStoreLister(store store.WorkClientWatcherStore) *WatcherStoreLister {
	return &WatcherStoreLister{
		store: store,
	}
}

// List returns the ManifestWorks from the WorkClientWatcherCache with list options.
func (l *WatcherStoreLister) List(options types.ListOptions) ([]*workv1.ManifestWork, error) {
	list, err := l.store.List(options.ClusterName, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	works := []*workv1.ManifestWork{}
	for _, work := range list.Items {
		// Currently, the source client only support the ManifestBundle
		// TODO: when supporting multiple cloud events data types, need a way
		// to known the work event data type
		if options.CloudEventsDataType != payload.ManifestBundleEventDataType {
			continue
		}
		works = append(works, &work)
	}

	return works, nil
}
