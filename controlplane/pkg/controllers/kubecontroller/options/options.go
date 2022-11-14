package options

import (
	v1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clientset "k8s.io/client-go/kubernetes"
	clientgokubescheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics"
	cmoptions "k8s.io/controller-manager/options"
	kubectrlmgrconfigv1alpha1 "k8s.io/kube-controller-manager/config/v1alpha1"
	kubectrlmgrconfig "k8s.io/kubernetes/pkg/controller/apis/config"
	kubectrlmgrconfigscheme "k8s.io/kubernetes/pkg/controller/apis/config/scheme"
	"k8s.io/kubernetes/pkg/controller/garbagecollector"
	garbagecollectorconfig "k8s.io/kubernetes/pkg/controller/garbagecollector/config"

	kubecontrollerconfig "open-cluster-management.io/ocm-controlplane/pkg/controllers/kubecontroller/config"

	// add the kubernetes feature gates
	_ "k8s.io/kubernetes/pkg/features"
)

// KubeControllerManagerOptions is the main context object for the kube-controller manager.
type KubeControllerManagerOptions struct {
	Generic *cmoptions.GenericControllerManagerConfigurationOptions

	CSRSigningController       *CSRSigningControllerOptions
	GarbageCollectorController *GarbageCollectorControllerOptions
	NamespaceController        *NamespaceControllerOptions

	Metrics *metrics.Options
	Logs    *logs.Options

	ShowHiddenMetricsForVersion string
}

// NewKubeControllerManagerOptions creates a new KubeControllerManagerOptions with a default config.
func NewKubeControllerManagerOptions() (*KubeControllerManagerOptions, error) {
	componentConfig, err := NewDefaultComponentConfig()
	if err != nil {
		return nil, err
	}

	s := KubeControllerManagerOptions{
		Generic: cmoptions.NewGenericControllerManagerConfigurationOptions(&componentConfig.Generic),

		CSRSigningController: &CSRSigningControllerOptions{
			&componentConfig.CSRSigningController,
		},
		GarbageCollectorController: &GarbageCollectorControllerOptions{
			&componentConfig.GarbageCollectorController,
		},
		NamespaceController: &NamespaceControllerOptions{
			&componentConfig.NamespaceController,
		},
		Metrics: metrics.NewOptions(),
		Logs:    logs.NewOptions(),
	}

	gcIgnoredResources := make([]garbagecollectorconfig.GroupResource, 0, len(garbagecollector.DefaultIgnoredResources()))
	for r := range garbagecollector.DefaultIgnoredResources() {
		gcIgnoredResources = append(gcIgnoredResources, garbagecollectorconfig.GroupResource{Group: r.Group, Resource: r.Resource})
	}

	s.GarbageCollectorController.GCIgnoredResources = gcIgnoredResources

	return &s, nil
}

// NewDefaultComponentConfig returns kube-controller manager configuration object.
func NewDefaultComponentConfig() (kubectrlmgrconfig.KubeControllerManagerConfiguration, error) {
	versioned := kubectrlmgrconfigv1alpha1.KubeControllerManagerConfiguration{}
	kubectrlmgrconfigscheme.Scheme.Default(&versioned)

	internal := kubectrlmgrconfig.KubeControllerManagerConfiguration{}
	if err := kubectrlmgrconfigscheme.Scheme.Convert(&versioned, &internal, nil); err != nil {
		return internal, err
	}
	return internal, nil
}

// Flags returns flags for a specific APIServer by section name
func (s *KubeControllerManagerOptions) Flags() cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}

	s.CSRSigningController.AddFlags(fss.FlagSet("csrsigning controller"))
	s.GarbageCollectorController.AddFlags(fss.FlagSet("garbagecollector controller"))
	s.NamespaceController.AddFlags(fss.FlagSet("namespace controller"))

	return fss
}

// ApplyTo fills up controller manager config with options.
func (s *KubeControllerManagerOptions) ApplyTo(c *kubecontrollerconfig.Config) error {
	if err := s.Generic.ApplyTo(&c.ComponentConfig.Generic); err != nil {
		return err
	}

	if err := s.CSRSigningController.ApplyTo(&c.ComponentConfig.CSRSigningController); err != nil {
		return err
	}
	if err := s.GarbageCollectorController.ApplyTo(&c.ComponentConfig.GarbageCollectorController); err != nil {
		return err
	}
	if err := s.NamespaceController.ApplyTo(&c.ComponentConfig.NamespaceController); err != nil {
		return err
	}

	return nil
}

// Validate is used to validate the options and config before launching the controller manager
func (s *KubeControllerManagerOptions) Validate() error {
	var errs []error

	errs = append(errs, s.CSRSigningController.Validate()...)
	errs = append(errs, s.GarbageCollectorController.Validate()...)
	errs = append(errs, s.NamespaceController.Validate()...)

	return utilerrors.NewAggregate(errs)
}

// Config return a controller manager config objective
func (s KubeControllerManagerOptions) Config(cfg *restclient.Config) (*kubecontrollerconfig.Config, error) {
	client, err := clientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventRecorder := eventBroadcaster.NewRecorder(clientgokubescheme.Scheme, v1.EventSource{Component: "kube-controller"})

	c := &kubecontrollerconfig.Config{
		Client:           client,
		Kubeconfig:       cfg,
		EventBroadcaster: eventBroadcaster,
		EventRecorder:    eventRecorder,
	}
	if err := s.ApplyTo(c); err != nil {
		return nil, err
	}

	return c, nil
}
