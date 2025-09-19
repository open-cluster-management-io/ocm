package addonmanager

import (
	"context"

	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
)

// BaseAddonManager is the interface to initialize a manager on hub to manage the addon
// agents on all managedcluster
type BaseAddonManager interface {
	// AddAgent register an addon agent to the manager.
	AddAgent(addon agent.AgentAddon) error

	// Trigger triggers a reconcile loop in the manager. Currently it
	// only trigger the deploy controller.
	Trigger(clusterName, addonName string)

	// StartWithInformers starts all registered addon agent with the given informers.
	StartWithInformers(ctx context.Context,
		workClient workclientset.Interface,
		workInformers workv1informers.ManifestWorkInformer,
		kubeInformers kubeinformers.SharedInformerFactory,
		addonInformers addoninformers.SharedInformerFactory,
		clusterInformers clusterv1informers.SharedInformerFactory,
		dynamicInformers dynamicinformer.DynamicSharedInformerFactory) error
}

// AddonManager is the interface based on BaseAddonManager to initialize a manager on hub
// to manage the addon agents and controllers
type AddonManager interface {
	BaseAddonManager

	// Start starts all registered addon agents and controllers.
	Start(ctx context.Context) error
}
