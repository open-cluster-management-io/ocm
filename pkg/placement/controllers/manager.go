package hub

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
	"open-cluster-management.io/ocm/pkg/placement/controllers/scheduling"
	"open-cluster-management.io/ocm/pkg/placement/debugger"
)

// RunControllerManager starts the placement scheduling controller.
// This controller requires leader election and runs the scheduling logic.
// To be run alongside RunDebugServer using different ports.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName)
	}
	ctx = klog.NewContext(ctx, logger)

	clusterClient, err := clusterclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	return RunControllerManagerWithInformers(ctx, controllerContext, kubeClient, clusterClient, clusterInformers)
}

func RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	kubeClient kubernetes.Interface,
	clusterClient clusterclient.Interface,
	clusterInformers clusterinformers.SharedInformerFactory,
) error {
	recorder, err := events.NewEventRecorder(ctx, clusterscheme.Scheme, kubeClient.EventsV1(), "placement-controller")
	if err != nil {
		return err
	}

	metrics := metrics.NewScheduleMetrics(clock.RealClock{})

	scheduler := scheduling.NewPluginScheduler(
		scheduling.NewSchedulerHandler(
			clusterClient,
			clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
			clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Lister(),
			clusterInformers.Cluster().V1().ManagedClusters().Lister(),
			recorder, metrics),
	)

	schedulingController := scheduling.NewSchedulingController(
		ctx,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		clusterInformers.Cluster().V1alpha1().AddOnPlacementScores(),
		scheduler,
		recorder, metrics,
	)

	go clusterInformers.Start(ctx.Done())

	go schedulingController.Run(ctx, 1)

	<-ctx.Done()

	return nil
}

// RunDebugServer starts only the debug service without scheduling controller.
// This server does not require leader election and only provides debug endpoints.
func RunDebugServer(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName)
	}
	ctx = klog.NewContext(ctx, logger)

	if controllerContext.Server == nil {
		return fmt.Errorf("server is required for debug server but was nil")
	}

	debug, clusterInformers, err := NewDebuggerWithInformers(ctx, controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	installDebugger(controllerContext.Server.Handler.NonGoRestfulMux, debug)
	klog.FromContext(ctx).Info("Debug service installed")

	go clusterInformers.Start(ctx.Done())

	<-ctx.Done()

	return nil
}

// NewDebuggerWithInformers creates a debugger with informers. This is useful for testing.
// The caller is responsible for starting the informers.
func NewDebuggerWithInformers(
	ctx context.Context,
	kubeConfig *rest.Config,
) (*debugger.Debugger, clusterinformers.SharedInformerFactory, error) {
	clusterClient, err := clusterclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	scheduler := scheduling.NewPluginScheduler(
		scheduling.NewSchedulerHandler(
			clusterClient,
			clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister(),
			clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Lister(),
			clusterInformers.Cluster().V1().ManagedClusters().Lister(),
			nil, nil), // Debug server doesn't need event recorder or metrics
	)

	debug := debugger.NewDebugger(
		scheduler,
		kubeClient,
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings(),
	)

	return debug, clusterInformers, nil
}

func installDebugger(mux *mux.PathRecorderMux, d *debugger.Debugger) {
	mux.HandlePrefix(debugger.DebugPath, http.HandlerFunc(d.Handler))
}
