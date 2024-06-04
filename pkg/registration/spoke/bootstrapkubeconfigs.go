package spoke

import (
	"context"
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
)

// selectBootstrapKubeConfigs has 2 main steps:
// 1. if find the matched bootrapkubeconfig then check it first, if it's valid then continue to use it,
// no need to select another bootstrapkubeconfig.
// 2. selects the first available bootstrap kubeconfig from the given list of configurations.
// If no suitable kubeconfig is found, it returns -1 and an error indicating that no bootstrap kubeconfig is available for the specified managed cluster.
func selectBootstrapKubeConfigs(ctx context.Context,
	managedCluster, hubKubeConfigFilePath string, bootstrapKubeConfigFilePaths []string) (int, error) {
	logger := log.FromContext(ctx)

	for index, fp := range bootstrapKubeConfigFilePaths {
		equal, err := compareServerEndpoint(fp, hubKubeConfigFilePath)
		if err != nil {
			logger.Error(err, "failed to compare hub and bootstrap kubeconfigs", "index", index)
			continue
		}
		if equal {
			err := checkBootstrapKubeConfigValid(ctx, managedCluster, fp)
			if err != nil {
				logger.Error(err, "failed to check matched bootstrap kubeconfig", "index", index)
				break
			}
			logger.Info("found matched bootstrap kubeconfig and it's valid, no need to reselect another one", "index", index)
			return index, nil
		}
	}

	for index, fp := range bootstrapKubeConfigFilePaths {
		err := checkBootstrapKubeConfigValid(ctx, managedCluster, fp)
		if err != nil {
			logger.Error(err, "failed to check bootstrap kubeconfig", "index", index)
			continue
		}
		return index, nil
	}

	// TODO: return index because some legacy code depends on the file type. Change to return *rest.Config in the future. @xuezhaojun
	return -1, fmt.Errorf("no bootstrap kubeconfig is available for managed cluster %s", managedCluster)
}

func compareServerEndpoint(bootstrapKubeConfigFilePath, hubKubeConfigFilePath string) (bool, error) {
	_, bootstrapServer, _, _, err := parseKubeconfig(
		bootstrapKubeConfigFilePath)
	if err != nil {
		return false, err
	}

	_, hubServer, _, _, err := parseKubeconfig(
		hubKubeConfigFilePath)
	if err != nil {
		return false, err
	}

	return bootstrapServer == hubServer, nil
}

// An "valid" bootstrap kubeconfig means:
// 1. It has the right permissions by creating self subject access reviews.
// 2. If a managed cluster exists and the hubAcceptsClient flag is set to true.
func checkBootstrapKubeConfigValid(ctx context.Context, managedCluster, bootstrapKubeConfigFilePath string) error {
	bootstrapKubeConfig, err := clientcmd.BuildConfigFromFlags("", bootstrapKubeConfigFilePath)
	if err != nil {
		return fmt.Errorf("failed to build bootstrap kubeconfig: %w", err)
	}

	// Send sarr check if bootstrapkubeconfig has right permission
	bootstrapKubeClient, err := kubernetes.NewForConfig(bootstrapKubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create bootstrap kube client: %w", err)
	}
	allowed, failedReview, err := commonhelpers.CreateSelfSubjectAccessReviews(ctx, bootstrapKubeClient, commonhelpers.GetBootstrapSSARs())
	if err != nil {
		return fmt.Errorf("failed to create self subject access reviews: %w", err)
	}
	if !allowed {
		return fmt.Errorf("failed to create self subject access reviews: %v", failedReview)
	}

	// Get managed cluster, if managed cluster exist, then check the hubAcceptClient
	bootstrapClusterClient, err := clusterv1client.NewForConfig(bootstrapKubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create bootstrap cluster client: %w", err)
	}

	mc, err := bootstrapClusterClient.ClusterV1().ManagedClusters().Get(ctx, managedCluster, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get managed cluster: %w", err)
	}
	if !mc.Spec.HubAcceptsClient {
		return fmt.Errorf("hub does not accept client")
	}
	return nil
}

// reSelectChecker is a health checker that checks if the bootstrap kubeconfig should be reselected.
//
// It is used by 2 controllers: the hubTimeoutController and hubAcceptController.
//
// If timeout to connect to a hub or the hubAcceptsClient flag is set to false, then shouldReSelect
// is set to true. And then checker will return an error, trigger the agent to restart and reselect
// the bootstrap kubeconfig.
type reSelectChecker struct {
	shouldReSelect bool
}

func (b *reSelectChecker) Check(_ *http.Request) error {
	if b.shouldReSelect {
		return fmt.Errorf("reselect bootstrap kubeconfig")
	}
	return nil
}

func (b *reSelectChecker) Name() string {
	return "reSelectChecker"
}
