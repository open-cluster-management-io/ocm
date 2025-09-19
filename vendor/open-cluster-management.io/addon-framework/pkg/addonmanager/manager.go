package addonmanager

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"

	"open-cluster-management.io/addon-framework/pkg/index"
)

// addonManager is the implementation of AddonManager with the base implementation.
type addonManager struct {
	*BaseAddonManagerImpl
}

// Start starts all registered addon agents and controllers.
func (a *addonManager) Start(ctx context.Context) error {
	kubeClient, err := kubernetes.NewForConfig(a.GetConfig())
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(a.GetConfig())
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(a.GetConfig())
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(a.GetConfig())
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(a.GetConfig())
	if err != nil {
		return err
	}

	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	var addonNames []string
	for key := range a.GetAddonAgents() {
		addonNames = append(addonNames, key)
	}
	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute,
		kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)

	workInformers := workv1informers.NewSharedInformerFactoryWithOptions(workClient, 10*time.Minute,
		workv1informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   addonNames,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)

	// addonDeployController
	err = workInformers.Work().V1().ManifestWorks().Informer().AddIndexers(
		cache.Indexers{
			index.ManifestWorkByAddon:           index.IndexManifestWorkByAddon,
			index.ManifestWorkByHostedAddon:     index.IndexManifestWorkByHostedAddon,
			index.ManifestWorkHookByHostedAddon: index.IndexManifestWorkHookByHostedAddon,
		},
	)
	if err != nil {
		return err
	}

	err = addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ManagedClusterAddonByNamespace: index.IndexManagedClusterAddonByNamespace, // agentDeployController
			index.AddonByConfig:                  index.IndexAddonByConfig,                  // addonConfigController
		},
	)
	if err != nil {
		return err
	}

	err = addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ClusterManagementAddonByConfig: index.IndexClusterManagementAddonByConfig, // cmaConfigController
		})
	if err != nil {
		return err
	}

	err = a.StartWithInformers(ctx, workClient, workInformers.Work().V1().ManifestWorks(), kubeInformers, addonInformers, clusterInformers, dynamicInformers)
	if err != nil {
		return err
	}

	kubeInformers.Start(ctx.Done())
	workInformers.Start(ctx.Done())
	addonInformers.Start(ctx.Done())
	clusterInformers.Start(ctx.Done())
	dynamicInformers.Start(ctx.Done())
	return nil
}

// New returns a new Manager for creating addon agents.
func New(config *rest.Config) (AddonManager, error) {
	return &addonManager{
		NewBaseAddonManagerImpl(config),
	}, nil
}
