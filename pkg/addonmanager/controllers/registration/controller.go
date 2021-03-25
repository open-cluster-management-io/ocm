package registration

import (
	"context"
	"fmt"

	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addonv1alpha1client "github.com/open-cluster-management/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "github.com/open-cluster-management/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "github.com/open-cluster-management/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// managedClusterController reconciles instances of ManagedCluster on the hub.
type addonConfigurationController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
}

func NewAddonConfigurationController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &addonConfigurationController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		agentAddons:               agentAddons,
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("addon-registration-controller")),
	}

	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, addonInformers.Informer()).
		WithSync(c.sync).ToController(fmt.Sprintf("addon-registration-controller"), recorder)
}

func (c *addonConfigurationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	managedClusterAddon = managedClusterAddon.DeepCopy()

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}

	if registrationOption.PermissionConfig != nil {
		err = registrationOption.PermissionConfig(managedCluster, managedClusterAddon)
		if err != nil {
			return err
		}
	}

	if registrationOption.CSRConfigurations == nil {
		return nil
	}
	configs := registrationOption.CSRConfigurations(managedCluster)
	if apiequality.Semantic.DeepEqual(configs, managedClusterAddon.Status.Registrations) {
		return nil
	}

	managedClusterAddon.Status.Registrations = configs
	meta.SetStatusCondition(&managedClusterAddon.Status.Conditions, metav1.Condition{
		Type:    "RegistrationApplied",
		Status:  metav1.ConditionTrue,
		Reason:  "RegistrationConfigured",
		Message: "Registration of the addon agent is configured",
	})
	_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).UpdateStatus(ctx, managedClusterAddon, metav1.UpdateOptions{})
	return err
}
