package hub

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/client-go/kubernetes"
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

// RunControllerManager starts the controllers on hub to make placement decisions.
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

	if controllerContext.Server != nil {
		debug := debugger.NewDebugger(
			scheduler,
			clusterInformers.Cluster().V1beta1().Placements(),
			clusterInformers.Cluster().V1().ManagedClusters(),
		)

		installDebugger(controllerContext.Server.Handler.NonGoRestfulMux, debug)
	}

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

func installDebugger(mux *mux.PathRecorderMux, d *debugger.Debugger) {
	mux.HandlePrefix(debugger.DebugPath, http.HandlerFunc(d.Handler))
}
