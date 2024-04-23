package clustermanagercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
	// secretNames is the set of secrets to be synced from source namespace to the target namespace
	secretNames = sets.New[string]()
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
	if config.CloudEventsDriverEnabled && config.WorkDriver != string(operatorapiv1.WorkDriverTypeKube) {
		secretNames = secretNames.Insert(workDriverConfig)
	} else {
		secretNames = secretNames.Delete(workDriverConfig)
	}

	for _, secretName := range secretNames.UnsortedList() {
		// check the source secret explicitly as the 'helpers.SyncSecret' doesn't return an error
		// when the source secret is not found.
		if _, err := c.operatorKubeClient.CoreV1().Secrets(c.operatorNamespace).Get(ctx,
			secretName, metav1.GetOptions{}); errors.IsNotFound(err) {
			// TODO: set condition if the source secret doesn't exist,
			return cm, reconcileStop,
				fmt.Errorf("failed to sync secret as the source secret %s not found", secretName)
		}

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
			return cm, reconcileStop, fmt.Errorf("failed to sync secret %s: %v", secretName, err)
		}
		// TODO: set condition to indicate if the secret is synced successfully
	}

	return cm, reconcileContinue, nil
}

func (c *secretReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	for _, secretName := range secretNames.UnsortedList() {
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
