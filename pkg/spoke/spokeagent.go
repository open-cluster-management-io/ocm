package spoke

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	"github.com/open-cluster-management/work/pkg/spoke/controllers"
)

// WorkloadAgentOptions defines the flags for workload agent
type WorkloadAgentOptions struct {
	SpokeKubeconfigFile string
	SpokeClusterName    string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewWorkloadAgentOptions() *WorkloadAgentOptions {
	return &WorkloadAgentOptions{}
}

// AddFlags register and binds the default flags
func (o *WorkloadAgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.SpokeKubeconfigFile, "spoke-kubeconfig", o.SpokeKubeconfigFile, "Location of kubeconfig file to connect to spoke cluster.")
	flags.StringVar(&o.SpokeClusterName, "spoke-cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *WorkloadAgentOptions) RunWorkloadAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build hub client and informer
	hubWorkClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	// Only watch the cluster namespace on hub
	workInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(hubWorkClient, 5*time.Minute, workinformers.WithNamespace(o.SpokeClusterName))

	manifestWorkController := controllers.NewManifestWorkController(
		hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName),
		workInformerFactory.Work().V1().ManifestWorks(),
		workInformerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks(o.SpokeClusterName),
	)

	go workInformerFactory.Start(ctx.Done())
	go manifestWorkController.Run(ctx, controllerContext.EventRecorder)
	<-ctx.Done()
	return nil
}
