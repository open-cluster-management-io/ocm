package clientcertmanager

import (
	"context"

	addoninformerv1alpha1 "github.com/open-cluster-management/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "github.com/open-cluster-management/api/client/addon/listers/addon/v1alpha1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

type certificateManagerController struct {
	clusterName     string
	hubClientConfig *restclient.Config
	kubeClient      kubernetes.Interface
	hubAddonLister  addonlisterv1alpha1.ManagedClusterAddOnLister
	secretInformer  corev1informers.SecretInformer
}

func NewCertificateManagetController(
	clusterName string,
	kubeClient kubernetes.Interface,
	hubClientConfig *restclient.Config,
	hubAddonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	secretInformer corev1informers.SecretInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &certificateManagerController{
		clusterName:     clusterName,
		kubeClient:      kubeClient,
		hubClientConfig: hubClientConfig,
		hubAddonLister:  hubAddonInformers.Lister(),
		secretInformer:  secretInformer,
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			hubAddonInformers.Informer()).
		WithSync(c.sync).
		ToController("ClientCertManagerController", recorder)
}

func (c *certificateManagerController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	//TODO: implement the reconciliation logic
	return nil
}
