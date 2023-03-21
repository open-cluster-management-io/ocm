package auth

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	k8scache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/auth/cache"
)

// ExecutorValidator validates whether the executor has permission to perform the requests
// to the local managed cluster
type ExecutorValidator interface {
	// Validate whether the work executor subject has permission to operate the specific manifest,
	// if there is no permission will return a basic.NotAllowedError.
	Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor, gvr schema.GroupVersionResource,
		namespace, name string, ownedByTheWork bool, obj *unstructured.Unstructured) error
}

type validatorFactory struct {
	config               *rest.Config
	kubeClient           kubernetes.Interface
	manifestWorkInformer workinformers.ManifestWorkInformer
	clusterName          string
	recorder             events.Recorder
	restMapper           meta.RESTMapper
}

func NewFactory(
	config *rest.Config,
	kubeClient kubernetes.Interface,
	manifestWorkInformer workinformers.ManifestWorkInformer,
	clusterName string,
	recorder events.Recorder,
	restMapper meta.RESTMapper) *validatorFactory {
	return &validatorFactory{
		config:               config,
		kubeClient:           kubeClient,
		manifestWorkInformer: manifestWorkInformer,
		clusterName:          clusterName,
		recorder:             recorder,
		restMapper:           restMapper,
	}
}

func (f *validatorFactory) NewExecutorValidator(ctx context.Context, isCacheValidator bool) ExecutorValidator {
	klog.Infof("Executor caches enabled: %v", isCacheValidator)
	sarValidator := basic.NewSARValidator(f.config, f.kubeClient)
	if !isCacheValidator {
		return sarValidator
	}

	cacheValidator := cache.NewExecutorCacheValidator(
		ctx,
		f.recorder,
		f.kubeClient,
		f.manifestWorkInformer.Lister().ManifestWorks(f.clusterName),
		f.restMapper,
		sarValidator,
	)

	go func() {
		// Wait for cache synced before starting to make sure all manifestworks could be processed
		k8scache.WaitForNamedCacheSync("ExecutorCacheValidator", ctx.Done(),
			f.manifestWorkInformer.Informer().HasSynced)
		cacheValidator.Start(ctx)
	}()

	return cacheValidator
}
