package spoke

import (
	"context"
	"fmt"
	"time"

	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/controllers/appliedmanifestcontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/finalizercontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/manifestcontroller"
	"open-cluster-management.io/work/pkg/spoke/controllers/statuscontroller"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
)

// WorkloadAgentOptions defines the flags for workload agent
type WorkloadAgentOptions struct {
	HubKubeconfigFile   string
	SpokeKubeconfigFile string
	SpokeClusterName    string
	QPS                 float32
	Burst               int
	StatusSyncInterval  time.Duration
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewWorkloadAgentOptions() *WorkloadAgentOptions {
	return &WorkloadAgentOptions{
		QPS:                50,
		Burst:              100,
		StatusSyncInterval: 30 * time.Second,
	}
}

// AddFlags register and binds the default flags
func (o *WorkloadAgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeKubeconfigFile, "spoke-kubeconfig", o.SpokeKubeconfigFile,
		"Location of kubeconfig file to connect to spoke cluster. If this is not set, will use '--kubeconfig' to build client to connect to the managed cluster.")
	flags.StringVar(&o.SpokeClusterName, "spoke-cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.Float32Var(&o.QPS, "spoke-kube-api-qps", o.QPS, "QPS to use while talking with apiserver on spoke cluster.")
	flags.IntVar(&o.Burst, "spoke-kube-api-burst", o.Burst, "Burst to use while talking with apiserver on spoke cluster.")
	flags.DurationVar(&o.StatusSyncInterval, "status-sync-interval", o.StatusSyncInterval, "Interval to sync resource status to hub.")
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *WorkloadAgentOptions) RunWorkloadAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build hub client and informer
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	hubhash := helper.HubHash(hubRestConfig.Host)

	hubWorkClient, err := workclientset.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}
	// Only watch the cluster namespace on hub
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(hubWorkClient, 5*time.Minute, workinformers.WithNamespace(o.SpokeClusterName))

	// load spoke client config and create spoke clients,
	// the work agent may not running in the spoke/managed cluster.
	spokeRestConfig, err := o.spokeKubeConfig(controllerContext)
	if err != nil {
		return err
	}

	spokeRestConfig.QPS = o.QPS
	spokeRestConfig.Burst = o.Burst
	spokeDynamicClient, err := dynamic.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeKubeClient, err := kubernetes.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeAPIExtensionClient, err := apiextensionsclient.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeWorkClient, err := workclientset.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeWorkInformerFactory := workinformers.NewSharedInformerFactory(spokeWorkClient, 5*time.Minute)
	restMapper, err := apiutil.NewDynamicRESTMapper(spokeRestConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		return err
	}

	manifestWorkController := manifestcontroller.NewManifestWorkController(
		ctx,
		controllerContext.EventRecorder,
		spokeDynamicClient,
		spokeKubeClient,
		spokeAPIExtensionClient,
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
		restMapper,
	)
	addFinalizerController := finalizercontroller.NewAddFinalizerController(
		controllerContext.EventRecorder,
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
	)
	appliedManifestWorkFinalizeController := finalizercontroller.NewAppliedManifestWorkFinalizeController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
	)
	manifestWorkFinalizeController := finalizercontroller.NewManifestWorkFinalizeController(
		controllerContext.EventRecorder,
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)
	appliedManifestWorkController := appliedmanifestcontroller.NewAppliedManifestWorkController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubhash,
	)
	availableStatusController := statuscontroller.NewAvailableStatusController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
		o.StatusSyncInterval,
	)

	go workInformerFactory.Start(ctx.Done())
	go spokeWorkInformerFactory.Start(ctx.Done())
	go addFinalizerController.Run(ctx, 1)
	go appliedManifestWorkFinalizeController.Run(ctx, 1)
	go appliedManifestWorkController.Run(ctx, 1)
	go manifestWorkController.Run(ctx, 1)
	go manifestWorkFinalizeController.Run(ctx, 1)
	go availableStatusController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}

// spokeKubeConfig builds kubeconfig for the spoke/managed cluster
func (o *WorkloadAgentOptions) spokeKubeConfig(controllerContext *controllercmd.ControllerContext) (*rest.Config, error) {
	if o.SpokeKubeconfigFile == "" {
		return controllerContext.KubeConfig, nil
	}

	spokeRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.SpokeKubeconfigFile)
	if err != nil {
		return nil, fmt.Errorf("unable to load spoke kubeconfig from file %q: %w", o.SpokeKubeconfigFile, err)
	}
	return spokeRestConfig, nil
}
