package factory

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/server"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/healthz"
	genericapiserveroptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
)

// ControllerFlags provides the "normal" controller flags
type ControllerFlags struct {
	// KubeConfigFile points to a kubeconfig file if you don't want to use the in cluster config
	KubeConfigFile string
	// EnableLeaderElection enables the leader election for the controller
	EnableLeaderElection bool
	// ComponentNamespace is the namespace to run component
	ComponentNamespace string
}

// NewControllerFlags returns flags with default values set
func NewControllerFlags() *ControllerFlags {
	return &ControllerFlags{}
}

// AddFlags register and binds the default flags
func (f *ControllerFlags) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config

	flags.StringVar(&f.KubeConfigFile, "kubeconfig", f.KubeConfigFile, "Location of the master configuration file to run from.")
	flags.StringVar(&f.ComponentNamespace, "component-namespace", f.ComponentNamespace, "Namespace of the component.")
	flags.BoolVar(&f.EnableLeaderElection, "enable-leader-election", f.EnableLeaderElection, "Enables the leader election for the controller")
}

// ControllerCommandConfig holds values required to construct a command to run.
type ControllerCommandConfig struct {
	componentName string
	startFunc     StartFunc
	version       version.Info
	healthChecks  []healthz.HealthChecker

	basicFlags *ControllerFlags
}

// StartFunc is the function to call on leader election start
type StartFunc func(context.Context, *rest.Config) error

// NewControllerConfig returns a new ControllerCommandConfig which can be used to wire up all the boiler plate of a controller
// TODO add more methods around wiring health checks and the like
func NewControllerCommandConfig(componentName string, version version.Info, startFunc StartFunc) *ControllerCommandConfig {
	return &ControllerCommandConfig{
		startFunc:     startFunc,
		componentName: componentName,
		version:       version,

		basicFlags: NewControllerFlags(),
	}
}

func (c *ControllerCommandConfig) WithHealthChecks(healthChecks ...healthz.HealthChecker) *ControllerCommandConfig {
	c.healthChecks = append(c.healthChecks, healthChecks...)
	return c
}

func (c *ControllerCommandConfig) NewCommand() *cobra.Command {
	ctx := context.TODO()
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, args []string) {
			// boiler plate for the "normal" command
			rand.Seed(time.Now().UTC().UnixNano())
			logs.InitLogs()

			// handle SIGTERM and SIGINT by cancelling the context.
			shutdownCtx, cancel := context.WithCancel(ctx)
			shutdownHandler := server.SetupSignalHandler()
			go func() {
				defer cancel()
				<-shutdownHandler
				klog.Infof("Received SIGTERM or SIGINT signal, shutting down controller.")
			}()

			ctx, terminate := context.WithCancel(shutdownCtx)
			defer terminate()

			if err := c.StartController(ctx); err != nil {
				klog.Fatal(err)
			}
		},
	}

	c.basicFlags.AddFlags(cmd)

	return cmd
}

// StartController runs the controller. This is the recommend entrypoint when you don't need
// to customize the builder.
func (c *ControllerCommandConfig) StartController(ctx context.Context) error {
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", c.basicFlags.KubeConfigFile)
	if err != nil {
		return err
	}

	var server *genericapiserver.GenericAPIServer
	serverConfig, err := toServerConfig()
	if err != nil {
		return err
	}
	serverConfig.HealthzChecks = append(serverConfig.HealthzChecks, c.healthChecks...)

	server, err = serverConfig.Complete(nil).New(c.componentName, genericapiserver.NewEmptyDelegate())
	if err != nil {
		return err
	}

	go func() {
		if err := server.PrepareRun().Run(ctx.Done()); err != nil {
			klog.Fatal(err)
		}
		klog.Info("server exited")
	}()

	if !c.basicFlags.EnableLeaderElection {
		return c.startFunc(ctx, kubeConfig)
	}

	leaderConfig := rest.CopyConfig(kubeConfig)
	leaderElection, err := toLeaderElection(leaderConfig, c.componentName, c.basicFlags.ComponentNamespace)
	if err != nil {
		return err
	}

	// 10s is the graceful termination time we give the controllers to finish their workers.
	// when this time pass, we exit with non-zero code, killing all controller workers.
	// NOTE: The pod must set the termination graceful time.
	leaderElection.Callbacks.OnStartedLeading = c.getOnStartedLeadingFunc(kubeConfig, 10*time.Second)

	leaderelection.RunOrDie(ctx, leaderElection)
	return nil
}

func (c *ControllerCommandConfig) getOnStartedLeadingFunc(kubeConfig *rest.Config, gracefulTerminationDuration time.Duration) func(ctx context.Context) {
	return func(ctx context.Context) {
		stoppedCh := make(chan struct{})
		go func() {
			defer close(stoppedCh)
			if err := c.startFunc(ctx, kubeConfig); err != nil {
				klog.Warningf("failed to start controller with error: %v", err)
				os.Exit(1)
			}
		}()

		select {
		case <-ctx.Done(): // context closed means the process likely received signal to terminate
		case <-stoppedCh:
			// if context was not cancelled (it is not "done"), but the startFunc terminated, it means it terminated prematurely
			// when this happen, it means the controllers terminated without error.
			if ctx.Err() == nil {
				klog.Warningf("graceful termination failed, controllers terminated prematurely")
				os.Exit(1)
			}
		}

		select {
		case <-time.After(gracefulTerminationDuration): // when context was closed above, give controllers extra time to terminate gracefully
			klog.Warningf("graceful termination failed, some controllers failed to shutdown in %s", gracefulTerminationDuration)
			os.Exit(1)
		case <-stoppedCh: // stoppedCh here means the controllers finished termination and we exit 0
		}
	}
}

func toLeaderElection(clientConfig *rest.Config, component, namespace string) (leaderelection.LeaderElectionConfig, error) {
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return leaderelection.LeaderElectionConfig{}, err
	}

	var identity string
	if hostname, err := os.Hostname(); err != nil {
		// on errors, make sure we're unique
		identity = string(uuid.NewUUID())
	} else {
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		identity = hostname + "_" + string(uuid.NewUUID())
	}

	var electionNamespace string
	if len(namespace) > 0 {
		electionNamespace = namespace
	} else {
		data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return leaderelection.LeaderElectionConfig{}, err
		}

		electionNamespace = strings.TrimSpace(string(data))
		if len(electionNamespace) == 0 {
			return leaderelection.LeaderElectionConfig{}, fmt.Errorf("leader election namespace is not set")
		}
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubeClient.CoreV1().RESTClient()).Events("")})
	eventRecorder := eventBroadcaster.NewRecorder(clientgoscheme.Scheme, corev1.EventSource{Component: component})
	rl, err := resourcelock.New(
		resourcelock.LeasesResourceLock,
		electionNamespace,
		component,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      identity,
			EventRecorder: eventRecorder,
		})
	if err != nil {
		return leaderelection.LeaderElectionConfig{}, err
	}

	return leaderelection.LeaderElectionConfig{
		Lock:            rl,
		ReleaseOnCancel: true,
		LeaseDuration:   137 * time.Second,
		RenewDeadline:   107 * time.Second,
		RetryPeriod:     26 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStoppedLeading: func() {
				defer os.Exit(0)
				klog.Warningf("leader election lost")
			},
		},
	}, nil
}

func toServerConfig() (*genericapiserver.Config, error) {
	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)
	config := genericapiserver.NewConfig(serializer.NewCodecFactory(scheme))

	servingOptions := genericapiserveroptions.NewSecureServingOptions()
	servingOptions.BindPort = 8443
	temporaryCertDir, err := os.MkdirTemp("", "serving-cert")
	if err != nil {
		return nil, err
	}
	servingOptions.ServerCert.CertDirectory = temporaryCertDir
	servingOptions.ServerCert.PairName = "tls"
	if err := servingOptions.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, err
	}

	servingOptionsWithLoopback := servingOptions.WithLoopback()

	if err := servingOptionsWithLoopback.ApplyTo(&config.SecureServing, &config.LoopbackClientConfig); err != nil {
		return nil, err
	}

	return config, nil
}
