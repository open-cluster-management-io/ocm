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
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/addon"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	placementcontrollers "open-cluster-management.io/ocm/pkg/placement/controllers"
	registrationhub "open-cluster-management.io/ocm/pkg/registration/hub"
	workhub "open-cluster-management.io/ocm/pkg/work/hub"
)

type HubOption struct {
	CommonOption       *commonoptions.Options
	registrationOption *registrationhub.HubManagerOptions
	workOption         *workhub.WorkHubManagerOptions
}

func NewHubOption() *HubOption {
	return &HubOption{
		CommonOption:       commonoptions.NewOptions(),
		registrationOption: registrationhub.NewHubManagerOptions(),
		workOption:         workhub.NewWorkHubManagerOptions(),
	}
}

func (o *HubOption) AddFlags(fs *pflag.FlagSet) {
	o.CommonOption.AddFlags(fs)
	o.registrationOption.AddFlags(fs)
	o.workOption.AddFlags(fs)
}

func (o *HubOption) RunManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(controllerContext.KubeConfig)
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
	replicaSetInformers := workv1informers.NewSharedInformerFactory(workClient, 30*time.Minute)
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
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}))
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 30*time.Minute)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 30*time.Minute)

	// Create an error channel to collect errors from controllers
	errCh := make(chan error, 4)

	// start registration controller
	go func() {
		err := o.registrationOption.RunControllerManagerWithInformers(
			ctx, controllerContext,
			kubeClient, metadataClient, clusterClient, clusterProfileClient, addOnClient,
			kubeInfomers, clusterInformers, clusterProfileInformers, workInformers, addOnInformers)
		if err != nil {
			klog.Errorf("failed to start registration controller: %v", err)
			errCh <- err
		}
	}()

	// start placement controller
	go func() {
		err := placementcontrollers.RunControllerManagerWithInformers(
			ctx, controllerContext, kubeClient, clusterClient, clusterInformers)
		if err != nil {
			klog.Errorf("failed to start placement controller: %v", err)
			errCh <- err
		}
	}()

	// start addon controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go func() {
			err := addon.RunControllerManagerWithInformers(
				ctx, controllerContext, kubeClient, addOnClient, workClient,
				clusterInformers, addOnInformers, workInformers, dynamicInformers)
			if err != nil {
				klog.Errorf("failed to start addon controller: %v", err)
				errCh <- err
			}
		}()
	}

	// start work controller
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		go func() {
			hubConfig := workhub.NewWorkHubManagerConfig(o.workOption)
			err := hubConfig.RunControllerManagerWithInformers(
				ctx, controllerContext, workClient, replicaSetInformers, workInformers, clusterInformers)
			if err != nil {
				klog.Errorf("failed to start work controller: %v", err)
				errCh <- err
			}
		}()
	}

	// Wait for context cancellation or first error
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}
