package clustermanagercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var (
	// secretNames is the slice of secrets to be synced from operator namespace to the clusterManager namespace
	secretNames = []string{helpers.ImagePullSecret, helpers.WorkDriverConfigSecret}
)

type secretReconcile struct {
	operatorKubeClient kubernetes.Interface
	hubKubeClient      kubernetes.Interface
	operatorNamespace  string
	cache              resourceapply.ResourceCache
	recorder           events.Recorder
	enableSyncLabels   bool
}

func (c *secretReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	var syncedErrs []error
	isWorkDriver := config.CloudEventsDriverEnabled && config.WorkDriver != string(operatorapiv1.WorkDriverTypeKube)

	for _, secretName := range secretNames {
		if secretName == helpers.WorkDriverConfigSecret && !isWorkDriver {
			continue
		}

		// sync the secret to target namespace
		// will delete the secret in the target ns if the secret is not found in the source ns
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
			helpers.GetClusterManagerHubLabels(cm, c.enableSyncLabels),
		); err != nil {
			syncedErrs = append(syncedErrs, fmt.Errorf("failed to sync secret %s: %v", secretName, err))
		}
	}

	if len(syncedErrs) > 0 {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type: operatorapiv1.ConditionClusterManagerApplied, Status: metav1.ConditionFalse, Reason: "HubResourceApplyFailed",
			Message: fmt.Sprintf("Failed to sync secrets to clusterManager namespace %v: %v",
				config.ClusterManagerNamespace, utilerrors.NewAggregate(syncedErrs))})

		return cm, reconcileContinue, utilerrors.NewAggregate(syncedErrs)
	}

	return cm, reconcileContinue, nil
}

func (c *secretReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	for _, secretName := range secretNames {
		if err := c.hubKubeClient.CoreV1().Secrets(config.ClusterManagerNamespace).Delete(ctx,
			secretName, metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return cm, reconcileContinue, fmt.Errorf("failed to delete secret %s: %v", secretName, err)
		}
	}

	return cm, reconcileContinue, nil
}
