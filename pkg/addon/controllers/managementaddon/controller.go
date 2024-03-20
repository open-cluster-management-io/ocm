package managementaddon

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// clusterManagementAddonController reconciles cma on the hub.
type clusterManagementAddonController struct {
	patcher patcher.Patcher[
		*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus]
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
}

func NewManagementAddonController(
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformers addoninformerv1alpha1.ClusterManagementAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterManagementAddonController{
		patcher: patcher.NewPatcher[
			*addonv1alpha1.ClusterManagementAddOn, addonv1alpha1.ClusterManagementAddOnSpec, addonv1alpha1.ClusterManagementAddOnStatus](
			addonClient.AddonV1alpha1().ClusterManagementAddOns()),
		clusterManagementAddonLister: clusterManagementAddonInformers.Lister(),
	}

	return factory.New().WithInformersQueueKeysFunc(
		queue.QueueKeyByMetaName,
		clusterManagementAddonInformers.Informer()).
		WithSync(c.sync).ToController("management-addon-controller", recorder)

}

func (c *clusterManagementAddonController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	addonName := syncCtx.QueueKey()
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling addon", "addonName", addonName)

	cma, err := c.clusterManagementAddonLister.Get(addonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// If cma annotation "addon.open-cluster-management.io/lifecycle: self" is not set,
	// force add "addon.open-cluster-management.io/lifecycle: addon-manager" .
	// The migration plan refer to https://github.com/open-cluster-management-io/ocm/issues/355.
	cmaCopy := cma.DeepCopy()
	if cmaCopy.Annotations == nil {
		cmaCopy.Annotations = map[string]string{}
	}
	if cmaCopy.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey] != addonv1alpha1.AddonLifecycleSelfManageAnnotationValue {
		cmaCopy.Annotations[addonv1alpha1.AddonLifecycleAnnotationKey] = addonv1alpha1.AddonLifecycleAddonManagerAnnotationValue
	}

	_, err = c.patcher.PatchLabelAnnotations(ctx, cmaCopy, cmaCopy.ObjectMeta, cma.ObjectMeta)
	return err
}
