package capi

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
)

var ClusterAPIGVR = schema.GroupVersionResource{
	Group:    "cluster.x-k8s.io",
	Version:  "v1beta1",
	Resource: "clusters",
}

const (
	ByCAPIResource    = "by-capi-resource"
	CAPIAnnotationKey = "cluster.x-k8s.io/cluster"
)

type CAPIProvider struct {
	informer              dynamicinformer.DynamicSharedInformerFactory
	lister                cache.GenericLister
	kubeClient            kubernetes.Interface
	managedClusterIndexer cache.Indexer
}

func NewCAPIProvider(
	kubeconfig *rest.Config,
	clusterInformer clusterinformerv1.ManagedClusterInformer) providers.Interface {
	dynamicClient := dynamic.NewForConfigOrDie(kubeconfig)
	kubeClient := kubernetes.NewForConfigOrDie(kubeconfig)

	dynamicInformer := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 30*time.Minute)

	utilruntime.Must(clusterInformer.Informer().AddIndexers(cache.Indexers{
		ByCAPIResource: indexByCAPIResource,
	}))

	return &CAPIProvider{
		informer:              dynamicInformer,
		lister:                dynamicInformer.ForResource(ClusterAPIGVR).Lister(),
		kubeClient:            kubeClient,
		managedClusterIndexer: clusterInformer.Informer().GetIndexer(),
	}
}

func (c *CAPIProvider) Clients(ctx context.Context, cluster *clusterv1.ManagedCluster) (*providers.Clients, error) {
	logger := klog.FromContext(ctx)
	clusterKey := capiNameFromManagedCluster(cluster)
	namespace, name, err := cache.SplitMetaNamespaceKey(clusterKey)
	if err != nil {
		return nil, err
	}
	capiCluster, err := c.lister.ByNamespace(namespace).Get(name)
	switch {
	case apierrors.IsNotFound(err):
		logger.V(4).Info("cluster is not found", "name", name, "namespace", namespace)
		// TODO(qiujian16) need to consider requeue in this case, since secrets is not watched.
		return nil, nil
	case err != nil:
		return nil, err
	}

	// check phase field of capi cluster
	capiClusterUnstructured, ok := capiCluster.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("invalid cluster type: %T", capiCluster)
	}
	status, exists, err := unstructured.NestedString(capiClusterUnstructured.Object, "status", "phase")
	if err != nil {
		return nil, err
	}
	if !exists || status != "Provisioned" {
		return nil, nil
	}

	secret, err := c.kubeClient.CoreV1().Secrets(namespace).Get(ctx, name+"-kubeconfig", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.V(4).Info(
			"kubeconfig secret is not found", "name", name+"-kubeconfig", "namespace", namespace)
		return nil, helpers.NewRequeueError("kubeconfig secret is not found", 1*time.Minute)
	case err != nil:
		return nil, err
	}

	data, ok := secret.Data["value"]
	if !ok {
		return nil, errors.Errorf("missing key %q in secret data", name)
	}

	configOverride, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, err
	}

	config, err := configOverride.ClientConfig()
	if err != nil {
		return nil, err
	}
	return providers.NewClient(config)
}

func (c *CAPIProvider) Register(syncCtx factory.SyncContext) {
	_, err := c.informer.ForResource(ClusterAPIGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueManagedClusterByCAPI(obj, syncCtx)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.enqueueManagedClusterByCAPI(newObj, syncCtx)
		},
	})
	utilruntime.HandleError(err)
}

func (c *CAPIProvider) IsManagedClusterOwner(cluster *clusterv1.ManagedCluster) bool {
	clusterKey := capiNameFromManagedCluster(cluster)
	namespace, name, _ := cache.SplitMetaNamespaceKey(clusterKey)
	_, err := c.lister.ByNamespace(namespace).Get(name)
	return err == nil
}

func (c *CAPIProvider) Run(ctx context.Context) {
	c.informer.Start(ctx.Done())
}

func (c *CAPIProvider) enqueueManagedClusterByCAPI(obj interface{}, syncCtx factory.SyncContext) {
	accessor, _ := meta.Accessor(obj)
	objs, err := c.managedClusterIndexer.ByIndex(ByCAPIResource, fmt.Sprintf(
		"%s/%s", accessor.GetNamespace(), accessor.GetName()))
	if err != nil {
		return
	}
	for _, obj := range objs {
		accessor, _ := meta.Accessor(obj)
		syncCtx.Queue().Add(accessor.GetName())
	}
}

func indexByCAPIResource(obj interface{}) ([]string, error) {
	cluster, ok := obj.(*clusterv1.ManagedCluster)
	if !ok {
		return []string{}, nil
	}
	return []string{capiNameFromManagedCluster(cluster)}, nil
}

func capiNameFromManagedCluster(cluster *clusterv1.ManagedCluster) string {
	if len(cluster.Annotations) > 0 {
		if key, ok := cluster.Annotations[CAPIAnnotationKey]; ok {
			return key
		}
	}
	return fmt.Sprintf("%s/%s", cluster.Name, cluster.Name)
}
