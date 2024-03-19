package work

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
)

// ManifestWorkLister list the ManifestWorks from a ManifestWorkInformer's local cache.
type ManifestWorkLister struct {
	Lister workv1lister.ManifestWorkLister
}

// List returns the ManifestWorks from a ManifestWorkInformer's local cache.
func (l *ManifestWorkLister) List(options types.ListOptions) ([]*workv1.ManifestWork, error) {
	selector := labels.Everything()
	if options.Source != types.SourceAll {
		req, err := labels.NewRequirement(common.CloudEventsOriginalSourceLabelKey, selection.Equals, []string{options.Source})
		if err != nil {
			return nil, err
		}

		selector = labels.NewSelector().Add(*req)
	}

	return l.Lister.ManifestWorks(options.ClusterName).List(selector)
}
