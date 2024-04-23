package clustermanagercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	// workDriverConfig is the secret that contains the work driver configurarion
	workDriverConfig = "work-driver-config"
)

var (
	// secretNames is the slice of secrets to be synced from source namespace to the target namespace
	secretNames = []string{}
)

type secretReconcile struct {
	operatorKubeClient kubernetes.Interface
	hubKubeClient      kubernetes.Interface
	operatorNamespace  string
	cache              resourceapply.ResourceCache
	recorder           events.Recorder
}

func (c *secretReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// create a local slice of secrets and copy the secretNames to avoid modifying the global variable
	pendingSyncSecrets := make([]string, len(secretNames))
	copy(pendingSyncSecrets, secretNames)
	if config.CloudEventsDriverEnabled && config.WorkDriver != string(operatorapiv1.WorkDriverTypeKube) {
		pendingSyncSecrets = append(pendingSyncSecrets, workDriverConfig)
	}

	var syncedErrs []error
	for _, secretName := range pendingSyncSecrets {
		// sync the secret to target namespace
		if _, _, err := helpers.SyncSecret(
			ctx,
			c.operatorKubeClient.CoreV1(),
			c.hubKubeClient.CoreV1(),
			c.recorder,
			c.operatorNamespace,
			secretName,
			config.ClusterManagerNamespace,
			secretName,
			[]metav1.OwnerReference{},
		); err != nil {
			syncedErrs = append(syncedErrs, fmt.Errorf("failed to sync secret %s: %v", secretName, err))
		}
	}

	if len(syncedErrs) > 0 {
		// TODO: set condition to indicate the secret sync error(s)
		return cm, reconcileStop, utilerrors.NewAggregate(syncedErrs)
	}

	return cm, reconcileContinue, nil
}

func (c *secretReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// create a local slice of secrets and copy the secretNames to avoid modifying the global variable
	pendingCleanSecrets := make([]string, len(secretNames))
	copy(pendingCleanSecrets, secretNames)
	if config.CloudEventsDriverEnabled && config.WorkDriver != string(operatorapiv1.WorkDriverTypeKube) {
		pendingCleanSecrets = append(pendingCleanSecrets, workDriverConfig)
	}
	for _, secretName := range pendingCleanSecrets {
		if err := c.hubKubeClient.CoreV1().Secrets(config.ClusterManagerNamespace).Delete(ctx,
			secretName, metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				return cm, reconcileContinue, nil
			}
			return cm, reconcileStop, fmt.Errorf("failed to delete secret %s: %v", secretName, err)
		}
	}

	return cm, reconcileContinue, nil
}
