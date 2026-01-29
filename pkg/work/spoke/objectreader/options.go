package objectreader

import (
	"github.com/spf13/pflag"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
)

type Options struct {
	MaxFeedbackWatch int32
}

func NewOptions() *Options {
	return &Options{
		MaxFeedbackWatch: 50,
	}
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.Int32Var(&o.MaxFeedbackWatch, "max-feedback-watch",
		o.MaxFeedbackWatch, "The maximum number of watch for feedback results")
}

func (o *Options) NewObjectReader(dynamicClient dynamic.Interface, workInformer workinformers.ManifestWorkInformer) (ObjectReader, error) {
	if err := workInformer.Informer().AddIndexers(map[string]cache.IndexFunc{
		byWorkIndex: indexWorkByResource,
	}); err != nil {
		return nil, err
	}

	return &objectReader{
		dynamicClient: dynamicClient,
		informers:     map[informerKey]*informerWithCancel{},
		indexer:       workInformer.Informer().GetIndexer(),
		maxWatch:      o.MaxFeedbackWatch,
	}, nil
}
