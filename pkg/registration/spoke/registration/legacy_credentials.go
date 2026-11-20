package registration

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
)

// legacyCredentialsPollInterval is how often the cleanup checks whether the hub has the cluster.
var legacyCredentialsPollInterval = 10 * time.Second

// CleanupLegacyHubCredentials deletes the hub credential Secrets a cluster kept before it moved to
// a secretless driver (spiffe): the hub kubeconfig and the bootstrap kubeconfig. It waits until
// the agent sees its cluster, accepted, through its new hub clients, so the switch has worked
// before the old credentials go; until then an admin can still switch back.
func CleanupLegacyHubCredentials(ctx context.Context, clusterName string, clusters clusterv1informer.ManagedClusterInformer,
	secrets corev1client.SecretInterface, names ...string) {
	logger := klog.FromContext(ctx)
	err := wait.PollUntilContextCancel(ctx, legacyCredentialsPollInterval, true, func(ctx context.Context) (bool, error) {
		if !clusters.Informer().HasSynced() {
			return false, nil
		}
		cluster, err := clusters.Lister().Get(clusterName)
		if err != nil {
			return false, nil
		}
		return cluster.Spec.HubAcceptsClient, nil
	})
	if err != nil {
		return
	}
	pending := map[string]bool{}
	for _, name := range names {
		if name != "" {
			pending[name] = true
		}
	}
	// A failed delete is tried again until it succeeds or the agent stops: the old credential
	// must not stay behind.
	_ = wait.PollUntilContextCancel(ctx, legacyCredentialsPollInterval, true, func(ctx context.Context) (bool, error) {
		for _, name := range names {
			if !pending[name] {
				continue
			}
			err := secrets.Delete(ctx, name, metav1.DeleteOptions{})
			switch {
			case err == nil:
				logger.Info("Deleted the hub credential of the previous registration driver", "secret", name)
				delete(pending, name)
			case errors.IsNotFound(err):
				delete(pending, name)
			default:
				logger.Error(err, "Failed to delete the hub credential of the previous registration driver; trying again", "secret", name)
			}
		}
		return len(pending) == 0, nil
	})
}
