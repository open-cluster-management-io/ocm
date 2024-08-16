package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub/addon"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterrole"
	"open-cluster-management.io/ocm/pkg/registration/hub/gc"
	"open-cluster-management.io/ocm/pkg/registration/hub/lease"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclustersetbinding"
	"open-cluster-management.io/ocm/pkg/registration/hub/taint"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

// HubManagerOptions holds configuration for hub manager controller
type HubManagerOptions struct {
	ClusterAutoApprovalUsers []string
	GCResourceList           []string
}

// NewHubManagerOptions returns a HubManagerOptions
func NewHubManagerOptions() *HubManagerOptions {
	return &HubManagerOptions{
		GCResourceList: []string{"addon.open-cluster-management.io/v1alpha1/managedclusteraddons",
			"work.open-cluster-management.io/v1/manifestworks"},
	}
}

// AddFlags registers flags for manager
func (m *HubManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringSliceVar(&m.ClusterAutoApprovalUsers, "cluster-auto-approval-users", m.ClusterAutoApprovalUsers,
		"A bootstrap user list whose cluster registration requests can be automatically approved.")
	fs.StringSliceVar(&m.GCResourceList, "gc-resource-list", m.GCResourceList,
		"A list GVR user can customize which are cleaned up after cluster is deleted. Format is group/version/resource, "+
			"and the default are managedclusteraddon and manifestwork. The resources will be deleted in order."+
			"The flag works only when ResourceCleanup feature gate is enable.")
}

// RunControllerManager starts the controllers on hub to manage spoke cluster registration.
func (m *HubManagerOptions) RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	metadataClient, err := metadata.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	addOnClient, err := addonclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactory(workClient, 30*time.Minute)
	kubeInfomers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 30*time.Minute, kubeinformers.WithTweakListOptions(
		func(listOptions *metav1.ListOptions) {
			// Note all kube resources managed by registration should have the cluster label, and should not have
			// the addon label.
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      clusterv1.ClusterNameLabelKey,
						Operator: metav1.LabelSelectorOpExists,
					},
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpDoesNotExist,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}))
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 30*time.Minute)

	return m.RunControllerManagerWithInformers(
		ctx, controllerContext,
		kubeClient, metadataClient, clusterClient, addOnClient,
		kubeInfomers, clusterInformers, workInformers, addOnInformers,
	)
}

func (m *HubManagerOptions) RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	kubeClient kubernetes.Interface,
	metadataClient metadata.Interface,
	clusterClient clusterv1client.Interface,
	addOnClient addonclient.Interface,
	kubeInformers kubeinformers.SharedInformerFactory,
	clusterInformers clusterv1informers.SharedInformerFactory,
	workInformers workv1informers.SharedInformerFactory,
	addOnInformers addoninformers.SharedInformerFactory,
) error {
	csrApprover, err := csr.NewCSRApprover(kubeClient, kubeInformers, m.ClusterAutoApprovalUsers, controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	approver := register.NewAggregatedApprover(csrApprover)

	managedClusterController := managedcluster.NewManagedClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Rbac().V1().Roles(),
		kubeInformers.Rbac().V1().ClusterRoles(),
		kubeInformers.Rbac().V1().RoleBindings(),
		kubeInformers.Rbac().V1().ClusterRoleBindings(),
		approver,
		controllerContext.EventRecorder,
	)

	taintController := taint.NewTaintController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	mcRecorder, err := commonhelpers.NewEventRecorder(ctx, clusterscheme.Scheme, kubeClient, "registration-controller")
	if err != nil {
		return err
	}
	leaseController := lease.NewClusterLeaseController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Coordination().V1().Leases(),
		controllerContext.EventRecorder,
		mcRecorder,
	)

	clockSyncController := lease.NewClockSyncController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Coordination().V1().Leases(),
		controllerContext.EventRecorder,
	)

	managedClusterSetController := managedclusterset.NewManagedClusterSetController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		controllerContext.EventRecorder,
	)

	managedClusterSetBindingController := managedclustersetbinding.NewManagedClusterSetBindingController(
		clusterClient,
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
		controllerContext.EventRecorder,
	)

	clusterroleController := clusterrole.NewManagedClusterClusterroleController(
		kubeClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Rbac().V1().ClusterRoles(),
		controllerContext.EventRecorder,
	)

	addOnHealthCheckController := addon.NewManagedClusterAddOnHealthCheckController(
		addOnClient,
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	addOnFeatureDiscoveryController := addon.NewAddOnFeatureDiscoveryController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		controllerContext.EventRecorder,
	)

	var defaultManagedClusterSetController, globalManagedClusterSetController factory.Controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		defaultManagedClusterSetController = managedclusterset.NewDefaultManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
		globalManagedClusterSetController = managedclusterset.NewGlobalManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
	}

	gcController := gc.NewGCController(
		kubeInformers.Rbac().V1().ClusterRoles().Lister(),
		kubeInformers.Rbac().V1().ClusterRoleBindings().Lister(),
		kubeInformers.Rbac().V1().RoleBindings().Lister(),
		clusterInformers.Cluster().V1().ManagedClusters(),
		workInformers.Work().V1().ManifestWorks().Lister(),
		clusterClient,
		kubeClient,
		metadataClient,
		approver,
		controllerContext.EventRecorder,
		m.GCResourceList,
		features.HubMutableFeatureGate.Enabled(ocmfeature.ResourceCleanup),
	)

	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go kubeInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())

	go managedClusterController.Run(ctx, 1)
	go taintController.Run(ctx, 1)
	go approver.Run(ctx, 1)
	go leaseController.Run(ctx, 1)
	go clockSyncController.Run(ctx, 1)
	go managedClusterSetController.Run(ctx, 1)
	go managedClusterSetBindingController.Run(ctx, 1)
	go clusterroleController.Run(ctx, 1)
	go addOnHealthCheckController.Run(ctx, 1)
	go addOnFeatureDiscoveryController.Run(ctx, 1)
	if features.HubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		go defaultManagedClusterSetController.Run(ctx, 1)
		go globalManagedClusterSetController.Run(ctx, 1)
	}

	go gcController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
