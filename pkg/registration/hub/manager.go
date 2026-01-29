package hub

import (
	"context"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	generate "k8s.io/kubectl/pkg/generate"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

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
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub/addon"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterprofile"
	"open-cluster-management.io/ocm/pkg/registration/hub/clusterrole"
	"open-cluster-management.io/ocm/pkg/registration/hub/gc"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer"
	importeroptions "open-cluster-management.io/ocm/pkg/registration/hub/importer/options"
	cloudproviders "open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer/providers/capi"
	"open-cluster-management.io/ocm/pkg/registration/hub/lease"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclusterset"
	"open-cluster-management.io/ocm/pkg/registration/hub/managedclustersetbinding"
	"open-cluster-management.io/ocm/pkg/registration/hub/taint"
	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
)

// HubManagerOptions holds configuration for hub manager controller
type HubManagerOptions struct {
	ClusterAutoApprovalUsers   []string
	EnabledRegistrationDrivers []string
	GCResourceList             []string
	ImportOption               *importeroptions.Options
	HubClusterArn              string
	AutoApprovedCSRUsers       []string
	AutoApprovedARNPatterns    []string
	AwsResourceTags            []string
	Labels                     string
	// TODO (skeeey) introduce hub options for different drives to group these options
	AutoApprovedGRPCUsers []string
	GRPCCAFile            string
	GRPCCAKeyFile         string
	GRPCSigningDuration   time.Duration
}

// NewHubManagerOptions returns a HubManagerOptions
func NewHubManagerOptions() *HubManagerOptions {
	return &HubManagerOptions{
		GCResourceList: []string{"addon.open-cluster-management.io/v1alpha1/managedclusteraddons",
			"work.open-cluster-management.io/v1/manifestworks"},
		ImportOption:               importeroptions.New(),
		EnabledRegistrationDrivers: []string{operatorv1.CSRAuthType},
		GRPCSigningDuration:        720 * time.Hour,
	}
}

// AddFlags registers flags for manager
func (m *HubManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringSliceVar(&m.ClusterAutoApprovalUsers, "cluster-auto-approval-users", m.ClusterAutoApprovalUsers,
		"A bootstrap user list whose cluster registration requests can be automatically approved.")
	if err := fs.MarkDeprecated("cluster-auto-approval-users", "use auto-approved-csr-users flag instead"); err != nil {
		utilruntime.Must(err)
	}
	fs.StringSliceVar(&m.EnabledRegistrationDrivers, "enabled-registration-drivers", m.EnabledRegistrationDrivers,
		"A list of registration drivers enabled on hub.")
	fs.StringSliceVar(&m.GCResourceList, "gc-resource-list", m.GCResourceList,
		"A list GVR user can customize which are cleaned up after cluster is deleted. Format is group/version/resource, "+
			"and the default are managedclusteraddon and manifestwork. The resources will be deleted in order."+
			"The flag works only when ResourceCleanup feature gate is enable.")
	fs.StringVar(&m.HubClusterArn, "hub-cluster-arn", m.HubClusterArn,
		"Hub Cluster Arn required to connect to Hub and create IAM Roles and Policies")
	fs.StringSliceVar(&m.AutoApprovedCSRUsers, "auto-approved-csr-users", m.AutoApprovedCSRUsers,
		"A bootstrap user list whose cluster registration requests can be automatically approved.")
	fs.StringSliceVar(&m.AutoApprovedARNPatterns, "auto-approved-arn-patterns", m.AutoApprovedARNPatterns,
		"A list of AWS EKS ARN patterns such that an EKS cluster will be auto approved if its ARN matches with any of the patterns")
	fs.StringSliceVar(&m.AutoApprovedGRPCUsers, "auto-approved-grpc-users", m.AutoApprovedGRPCUsers,
		"A bootstrap user list via gRPC whose cluster registration requests can be automatically approved.")
	fs.StringSliceVar(&m.AwsResourceTags, "aws-resource-tags", m.AwsResourceTags, "A list of tags to apply to AWS resources created through the OCM controllers")
	fs.StringVar(&m.Labels, "labels", m.Labels,
		"Labels to be added to the resources created by registration controller. The format is key1=value1,key2=value2.")
	fs.StringVar(&m.GRPCCAFile, "grpc-ca-file", m.GRPCCAFile, "ca file to sign client cert for grpc")
	fs.StringVar(&m.GRPCCAKeyFile, "grpc-key-file", m.GRPCCAKeyFile, "ca key file to sign client cert for grpc")
	fs.DurationVar(&m.GRPCSigningDuration, "grpc-signing-duration", m.GRPCSigningDuration, "The max length of duration signed certificates will be given.")
	m.ImportOption.AddFlags(fs)
}

// RunControllerManager starts the controllers on hub to manage spoke cluster registration.
func (m *HubManagerOptions) RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName)
	}
	ctx = klog.NewContext(ctx, logger)

	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// copy a separate config for gc controller and increase the gc controller's throughput.
	metadataKubeConfig := rest.CopyConfig(controllerContext.KubeConfig)
	metadataKubeConfig.QPS = controllerContext.KubeConfig.QPS * 2
	metadataKubeConfig.Burst = controllerContext.KubeConfig.Burst * 2
	metadataClient, err := metadata.NewForConfig(metadataKubeConfig)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterProfileClient, err := cpclientset.NewForConfig(controllerContext.KubeConfig)
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
	clusterProfileInformers := cpinformerv1alpha1.NewSharedInformerFactory(clusterProfileClient, 30*time.Minute)
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
		kubeClient, metadataClient, clusterClient, clusterProfileClient, addOnClient, kubeInfomers,
		clusterInformers, clusterProfileInformers, workInformers, addOnInformers,
	)
}

func (m *HubManagerOptions) RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	kubeClient kubernetes.Interface,
	metadataClient metadata.Interface,
	clusterClient clusterv1client.Interface,
	clusterProfileClient cpclientset.Interface,
	addOnClient addonclient.Interface,
	kubeInformers kubeinformers.SharedInformerFactory,
	clusterInformers clusterv1informers.SharedInformerFactory,
	clusterProfileInformers cpinformerv1alpha1.SharedInformerFactory,
	workInformers workv1informers.SharedInformerFactory,
	addOnInformers addoninformers.SharedInformerFactory,
) error {
	var drivers []register.HubDriver
	for _, enabledRegistrationDriver := range m.EnabledRegistrationDrivers {
		switch enabledRegistrationDriver {
		case operatorv1.CSRAuthType:
			autoApprovedCSRUsers := m.ClusterAutoApprovalUsers
			if len(m.AutoApprovedCSRUsers) > 0 {
				autoApprovedCSRUsers = m.AutoApprovedCSRUsers
			}
			csrDriver, err := csr.NewCSRHubDriver(kubeClient, kubeInformers, autoApprovedCSRUsers)
			if err != nil {
				return err
			}
			drivers = append(drivers, csrDriver)
		case operatorv1.AwsIrsaAuthType:
			awsIRSAHubDriver, err := awsirsa.NewAWSIRSAHubDriver(ctx, m.HubClusterArn, m.AutoApprovedARNPatterns, m.AwsResourceTags)
			if err != nil {
				return err
			}
			drivers = append(drivers, awsIRSAHubDriver)
		case operatorv1.GRPCAuthType:
			grpcHubDriver, err := grpc.NewGRPCHubDriver(
				kubeClient, kubeInformers,
				m.GRPCCAKeyFile, m.GRPCCAFile, m.GRPCSigningDuration,
				m.AutoApprovedGRPCUsers)
			if err != nil {
				return err
			}
			drivers = append(drivers, grpcHubDriver)
		}
	}
	hubDriver := register.NewAggregatedHubDriver(drivers...)
	labelsMap := make(map[string]string)
	if m.Labels != "" {
		var err error
		labelsMap, err = generate.ParseLabels(m.Labels)
		if err != nil {
			return err
		}
	}
	managedClusterController := managedcluster.NewManagedClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Rbac().V1().Roles(),
		kubeInformers.Rbac().V1().ClusterRoles(),
		kubeInformers.Rbac().V1().RoleBindings(),
		kubeInformers.Rbac().V1().ClusterRoleBindings(),
		workInformers.Work().V1().ManifestWorks(),
		hubDriver,
		labelsMap,
	)

	taintController := taint.NewTaintController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
	)

	mcRecorder, err := events.NewEventRecorder(ctx, clusterscheme.Scheme, kubeClient.EventsV1(), "registration-controller")
	if err != nil {
		return err
	}
	leaseController := lease.NewClusterLeaseController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Coordination().V1().Leases(),
		mcRecorder,
	)

	clockSyncController := lease.NewClockSyncController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Coordination().V1().Leases(),
	)

	managedClusterSetController := managedclusterset.NewManagedClusterSetController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
	)

	managedNamespaceController := managedcluster.NewManagedNamespaceController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
	)

	managedClusterSetBindingController := managedclustersetbinding.NewManagedClusterSetBindingController(
		clusterClient,
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
	)

	clusterroleController := clusterrole.NewManagedClusterClusterroleController(
		kubeClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInformers.Rbac().V1().ClusterRoles(),
		labelsMap,
	)

	addOnHealthCheckController := addon.NewManagedClusterAddOnHealthCheckController(
		addOnClient,
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		clusterInformers.Cluster().V1().ManagedClusters(),
	)

	addOnFeatureDiscoveryController := addon.NewAddOnFeatureDiscoveryController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
	)

	var defaultManagedClusterSetController, globalManagedClusterSetController factory.Controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		defaultManagedClusterSetController = managedclusterset.NewDefaultManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		)
		globalManagedClusterSetController = managedclusterset.NewGlobalManagedClusterSetController(
			clusterClient.ClusterV1beta2(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		)
	}

	var clusterProfileLifecycleController factory.Controller
	var clusterProfileStatusController factory.Controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ClusterProfile) {
		clusterProfileLifecycleController = clusterprofile.NewClusterProfileLifecycleController(
			kubeClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
			clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
			clusterProfileClient,
			clusterProfileInformers.Apis().V1alpha1().ClusterProfiles(),
		)

		clusterProfileStatusController = clusterprofile.NewClusterProfileStatusController(
			clusterInformers.Cluster().V1().ManagedClusters(),
			clusterProfileClient,
			clusterProfileInformers.Apis().V1alpha1().ClusterProfiles(),
		)
	}

	var providers []cloudproviders.Interface
	var clusterImporter factory.Controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ClusterImporter) {
		providers = []cloudproviders.Interface{
			capi.NewCAPIProvider(controllerContext.KubeConfig, clusterInformers.Cluster().V1().ManagedClusters()),
		}

		renderers, err := importeroptions.GetImporterRenderers(
			m.ImportOption, kubeClient, controllerContext.OperatorNamespace)
		if err != nil {
			return err
		}

		clusterImporter = importer.NewImporter(
			renderers,
			clusterClient,
			clusterInformers.Cluster().V1().ManagedClusters(),
			providers,
		)
	}

	gcController := gc.NewGCController(
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterClient,
		metadataClient,
		m.GCResourceList,
	)

	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go kubeInformers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ClusterProfile) {
		go clusterProfileInformers.Start(ctx.Done())
	}

	go managedClusterController.Run(ctx, 1)
	go taintController.Run(ctx, 1)
	go hubDriver.Run(ctx, 1)
	go leaseController.Run(ctx, 1)
	go clockSyncController.Run(ctx, 1)
	go managedClusterSetController.Run(ctx, 1)
	go managedNamespaceController.Run(ctx, 1)
	go managedClusterSetBindingController.Run(ctx, 1)
	go clusterroleController.Run(ctx, 1)
	go addOnHealthCheckController.Run(ctx, 1)
	go addOnFeatureDiscoveryController.Run(ctx, 1)
	if features.HubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		go defaultManagedClusterSetController.Run(ctx, 1)
		go globalManagedClusterSetController.Run(ctx, 1)
	}
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ClusterProfile) {
		go clusterProfileLifecycleController.Run(ctx, 1)
		go clusterProfileStatusController.Run(ctx, 1)
	}
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ClusterImporter) {
		for _, provider := range providers {
			go provider.Run(ctx)
		}
		go clusterImporter.Run(ctx, 1)
	}

	if features.HubMutableFeatureGate.Enabled(ocmfeature.ResourceCleanup) {
		go gcController.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}
