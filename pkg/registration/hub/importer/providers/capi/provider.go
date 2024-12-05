package capi

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
)

var gvr = schema.GroupVersionResource{
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
	kubeconfig *rest.Config, clusterInformer clusterinformerv1.ManagedClusterInformer) providers.Interface {
	dynamicClient := dynamic.NewForConfigOrDie(kubeconfig)
	kubeClient := kubernetes.NewForConfigOrDie(kubeconfig)

	dynamicInformer := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 30*time.Minute)

	utilruntime.Must(clusterInformer.Informer().AddIndexers(cache.Indexers{
		ByCAPIResource: indexByCAPIResource,
	}))

	return &CAPIProvider{
		informer:              dynamicInformer,
		lister:                dynamicInformer.ForResource(gvr).Lister(),
		kubeClient:            kubeClient,
		managedClusterIndexer: clusterInformer.Informer().GetIndexer(),
	}
}

func (c *CAPIProvider) KubeConfig(cluster *clusterv1.ManagedCluster) (*rest.Config, error) {
	clusterKey := capiNameFromManagedCluster(cluster)
	namespace, name, err := cache.SplitMetaNamespaceKey(clusterKey)
	if err != nil {
		return nil, err
	}
	_, err = c.lister.ByNamespace(namespace).Get(name)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	secret, err := c.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name+"-kubeconfig", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
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
	return configOverride.ClientConfig()
}

func (c *CAPIProvider) Register(syncCtx factory.SyncContext) {
	_, err := c.informer.ForResource(gvr).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
		syncCtx.Queue().Add(fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetName()))
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
	name := fmt.Sprintf("%s/%s", cluster.Name, cluster.Namespace)
	if len(cluster.Annotations) > 0 {
		if key, ok := cluster.Annotations[CAPIAnnotationKey]; ok {
			return key
		}
	}
	return name
}
