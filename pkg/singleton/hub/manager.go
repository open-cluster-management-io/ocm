package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"

	"open-cluster-management.io/ocm/pkg/addon"
	"open-cluster-management.io/ocm/pkg/features"
	placementcontrollers "open-cluster-management.io/ocm/pkg/placement/controllers"
	registrationhub "open-cluster-management.io/ocm/pkg/registration/hub"
	workhub "open-cluster-management.io/ocm/pkg/work/hub"
)

const sourceID = "ocm-controller"

type Option struct {
	RegistrationOption *registrationhub.HubManagerOptions
	WorkOption         *workhub.WorkHubManagerOptions
}

func NewOption() *Option {
	return &Option{
		RegistrationOption: registrationhub.NewHubManagerOptions(),
		WorkOption:         workhub.NewWorkHubManagerOptions(),
	}
}

func (o *Option) AddFlags(fs *pflag.FlagSet) {
	o.RegistrationOption.AddFlags(fs)
	o.WorkOption.AddFlags(fs)
}

func (o *Option) RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
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

	clusterProfileClient, err := cpclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	addOnClient, err := addonclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute)
	clusterProfileInformers := cpinformerv1alpha1.NewSharedInformerFactory(clusterProfileClient, 30*time.Minute)
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

	var workClient workclientset.Interface
	var watcherStore *store.SourceInformerWatcherStore

	if o.WorkOption.WorkDriver == "kube" {
		config := controllerContext.KubeConfig
		if o.WorkOption.WorkDriverConfig != "" {
			config, err = clientcmd.BuildConfigFromFlags("", o.WorkOption.WorkDriverConfig)
			if err != nil {
				return err
			}
		}

		workClient, err = workclientset.NewForConfig(config)
		if err != nil {
			return err
		}
	} else {
		// For cloudevents drivers, we build ManifestWork client that implements the
		// ManifestWorkInterface and ManifestWork informer based on different driver configuration.
		// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.

		watcherStore = store.NewSourceInformerWatcherStore(ctx)

		_, config, err := generic.NewConfigLoader(o.WorkOption.WorkDriver, o.WorkOption.WorkDriverConfig).
			LoadConfig()
		if err != nil {
			return err
		}

		clientHolder, err := work.NewClientHolderBuilder(config).
			WithClientID(o.WorkOption.CloudEventsClientID).
			WithSourceID(sourceID).
			WithCodecs(codec.NewManifestBundleCodec()).
			WithWorkClientWatcherStore(watcherStore).
			NewSourceClientHolder(ctx)
		if err != nil {
			return err
		}

		workClient = clientHolder.WorkInterface()
	}

	workInformers := workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute)
	informer := workInformers.Work().V1().ManifestWorks()

	// For cloudevents work client, we use the informer store as the client store
	if watcherStore != nil {
		watcherStore.SetStore(informer.Informer().GetStore())
	}

	// start registration component
	go func() {
		err := o.RegistrationOption.RunControllerManagerWithInformers(
			ctx, controllerContext,
			kubeClient, metadataClient, clusterClient, clusterProfileClient, addOnClient,
			kubeInfomers, clusterInformers, clusterProfileInformers, workInformers, addOnInformers,
		)
		if err != nil {
			klog.Fatal(err)
		}
	}()

	// start placement component
	go func() {
		err := placementcontrollers.RunControllerManagerWithInformers(
			ctx, controllerContext, kubeClient, clusterClient, clusterInformers)
		if err != nil {
			klog.Fatal(err)
		}
	}()

	// start work component
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		// build a hub work client for ManifestWorkReplicaSets
		replicaSetsClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
		if err != nil {
			return err
		}
		go func() {
			err := workhub.RunControllerManagerWithInformers(
				ctx, controllerContext, replicaSetsClient, workClient, informer, clusterInformers)
			if err != nil {
				klog.Fatal(err)
			}
		}()
	}

	// start addon component
	if features.HubMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		dynamicClient, err := dynamic.NewForConfig(controllerContext.KubeConfig)
		if err != nil {
			return err
		}
		dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)
		go func() {
			err := addon.RunControllerManagerWithInformers(
				ctx, controllerContext, kubeClient, addOnClient, workClient,
				clusterInformers, addOnInformers, workInformers, dynamicInformers,
			)
			if err != nil {
				klog.Fatal(err)
			}
		}()
	}

	<-ctx.Done()
	return nil
}
