package source

import (
	"k8s.io/apimachinery/pkg/labels"

	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type manifestWorkLister struct {
	Lister workv1lister.ManifestWorkLister
}

func (l *manifestWorkLister) List(options types.ListOptions) ([]*workv1.ManifestWork, error) {
	return l.Lister.ManifestWorks(options.ClusterName).List(labels.Everything())
}
