package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/auth/store"
)

// SubjectAccessReviewCheckFn is a function to checks if the executor has permission to operate
// the gvr resource by subjectaccessreview
type SubjectAccessReviewCheckFn func(ctx context.Context, executor *workapiv1.ManifestWorkSubjectServiceAccount,
	gvr schema.GroupVersionResource, namespace, name string, ownedByTheWork bool) error

type sarCacheValidator struct {
	kubeClient kubernetes.Interface
	// executorCaches caches the subject access review results of a specific resource for executors
	executorCaches *store.ExecutorCaches
	// manifestWorkExecutorCachesLoader can load all valuable caches in the current state cluster into an
	// executor cache data structure
	manifestWorkExecutorCachesLoader manifestWorkExecutorCachesLoader
	validator                        *basic.SarValidator
	spokeInformer                    informers.SharedInformerFactory
	cacheController                  factory.Controller
}

// NewExecutorCacheValidator creates a sarCacheValidator
func NewExecutorCacheValidator(
	ctx context.Context,
	recorder events.Recorder,
	spokeKubeClient kubernetes.Interface,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	restMapper meta.RESTMapper,
	validator *basic.SarValidator,
) *sarCacheValidator {

	manifestWorkExecutorCachesLoader := &defaultManifestWorkExecutorCachesLoader{
		manifestWorkLister: manifestWorkLister,
		restMapper:         restMapper,
	}

	executorCaches := store.NewExecutorCache()

	// the spokeKubeInformerFactory will only be used for the executor cache controller, and we do not want to
	// update the cache very frequently, set resync period to every day
	spokeKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(spokeKubeClient, 24*time.Hour)

	v := &sarCacheValidator{
		kubeClient:                       spokeKubeClient,
		validator:                        validator,
		executorCaches:                   executorCaches,
		manifestWorkExecutorCachesLoader: manifestWorkExecutorCachesLoader,
		spokeInformer:                    spokeKubeInformerFactory,
	}

	v.cacheController = NewExecutorCacheController(ctx, recorder,
		v.spokeInformer.Rbac().V1().ClusterRoleBindings(),
		v.spokeInformer.Rbac().V1().RoleBindings(),
		v.spokeInformer.Rbac().V1().ClusterRoles(),
		v.spokeInformer.Rbac().V1().Roles(),
		manifestWorkExecutorCachesLoader,
		executorCaches,
		v.validator.CheckSubjectAccessReviews,
	)

	return v
}

// Start starts the informer and the controller
// It's an error to call Start more than once.
// Start blocks; call via go.
func (v *sarCacheValidator) Start(ctx context.Context) {
	// initialize the caches skelton in order to let others caches operands know which caches are necessary,
	// otherwise, the roleBindingExecutorsMapper and clusterRoleBindingExecutorsMapper in the cache controller
	// have no chance to initialize after the work pod restarts
	v.manifestWorkExecutorCachesLoader.loadAllValuableCaches(v.executorCaches)

	v.spokeInformer.Start(ctx.Done())
	v.cacheController.Run(ctx, 1)
}

// Validate checks whether the executor has permission to operate the specific gvr resource.
// it will first try to get the subject access review checking result from caches, if there is no result in caches,
// then it will send sar requests to the api server and store the result into caches.
func (v *sarCacheValidator) Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
	gvr schema.GroupVersionResource, namespace, name string,
	ownedByTheWork bool, obj *unstructured.Unstructured) error {
	if executor == nil {
		return nil
	}

	if err := v.validator.ExecutorBasicCheck(executor); err != nil {
		return err
	}

	sa := executor.Subject.ServiceAccount
	executorKey := store.ExecutorKey(sa.Namespace, sa.Name)
	dimension := store.Dimension{
		Namespace:     namespace,
		Name:          name,
		Resource:      gvr.Resource,
		Group:         gvr.Group,
		Version:       gvr.Version,
		ExecuteAction: store.GetExecuteAction(ownedByTheWork),
	}

	allowed, _ := v.executorCaches.Get(executorKey, dimension)
	if allowed == nil {
		err := v.validator.CheckSubjectAccessReviews(ctx, sa, gvr, namespace, name, ownedByTheWork)
		updateSARCheckResultToCache(v.executorCaches, executorKey, dimension, err)
		if err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("Get auth from cache executor %s, dimension: %+v allow: %v", executorKey, dimension, *allowed)
		if !*allowed {
			return &basic.NotAllowedError{
				Err: fmt.Errorf("not allowed to apply the resource %s %s, %s %s",
					gvr.Group, gvr.Resource, namespace, name),
				RequeueTime: 60 * time.Second,
			}
		}
	}

	return v.validator.CheckEscalation(ctx, sa, gvr, namespace, name, obj)
}

// updateSARCheckResultToCache updates the subjectAccessReview checking result to the executor cache
func updateSARCheckResultToCache(executorCaches *store.ExecutorCaches, executorKey string,
	dimension store.Dimension, result error) {
	if result == nil {
		executorCaches.Upsert(executorKey, dimension, pointer.Bool(true))
	}

	var authError *basic.NotAllowedError
	if errors.As(result, &authError) {
		executorCaches.Upsert(executorKey, dimension, pointer.Bool(false))
	}
}
