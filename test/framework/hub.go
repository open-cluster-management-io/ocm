package framework

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// Hub represents a hub cluster, it holds:
// * the clients to interact with the hub cluster
// * the metadata of the hub
// * the runtime data of the hub
type Hub struct {
	*OCMClients
	ClusterManagerName      string
	ClusterManagerNamespace string
	ClusterCfg              *rest.Config
}

func NewHub(kubeconfig string) (*Hub, error) {
	clusterCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	clients, err := NewOCMClients(clusterCfg)
	if err != nil {
		return nil, err
	}
	return &Hub{
		OCMClients: clients,
		// the name of the ClusterManager object is constantly "cluster-manager" at the moment; The same name as deploy/cluster-manager/config/samples
		ClusterManagerName:      "cluster-manager",
		ClusterManagerNamespace: helpers.ClusterManagerDefaultNamespace,
		ClusterCfg:              clusterCfg,
	}, nil
}

func (hub *Hub) CheckHubReady() error {
	ctx := context.TODO()

	cm, err := hub.GetCluserManager()
	if err != nil {
		return fmt.Errorf("failed to get cluster manager: %w", err)
	}

	err = CheckClusterManagerStatus(cm)
	if err != nil {
		return fmt.Errorf("failed to check cluster manager status: %w", err)
	}

	// make sure open-cluster-management-hub namespace is created
	if _, err := hub.KubeClient.CoreV1().Namespaces().
		Get(context.TODO(), hub.ClusterManagerNamespace, metav1.GetOptions{}); err != nil {
		return err
	}

	// make sure deployments are ready
	deployments := []string{
		fmt.Sprintf("%s-registration-controller", hub.ClusterManagerName),
		fmt.Sprintf("%s-registration-webhook", hub.ClusterManagerName),
		fmt.Sprintf("%s-work-webhook", hub.ClusterManagerName),
		fmt.Sprintf("%s-placement-controller", hub.ClusterManagerName),
	}
	for _, deployment := range deployments {
		if err = CheckDeploymentReady(ctx, hub.KubeClient, hub.ClusterManagerNamespace, deployment); err != nil {
			return fmt.Errorf("failed to check deployment %s: %w", deployment, err)
		}
	}

	// if manifestworkreplicaset feature is enabled, check the work controller
	if cm.Spec.WorkConfiguration != nil &&
		helpers.FeatureGateEnabled(cm.Spec.WorkConfiguration.FeatureGates, ocmfeature.DefaultHubWorkFeatureGates, ocmfeature.ManifestWorkReplicaSet) {
		if err = CheckDeploymentReady(ctx, hub.KubeClient, hub.ClusterManagerNamespace, fmt.Sprintf("%s-work-controller", hub.ClusterManagerName)); err != nil {
			return fmt.Errorf("failed to check work controller: %w", err)
		}
	}

	// if addonManager feature is enabled, check the addonManager controller
	if cm.Spec.AddOnManagerConfiguration != nil &&
		helpers.FeatureGateEnabled(cm.Spec.AddOnManagerConfiguration.FeatureGates, ocmfeature.DefaultHubAddonManagerFeatureGates, ocmfeature.AddonManagement) {
		if err = CheckDeploymentReady(ctx, hub.KubeClient, hub.ClusterManagerNamespace,
			fmt.Sprintf("%s-addon-manager-controller", hub.ClusterManagerName)); err != nil {
			return fmt.Errorf("failed to check addon manager controller: %w", err)
		}
	}

	return nil
}
