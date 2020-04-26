package spokecluster

import (
	"context"
	"fmt"
	"path/filepath"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	v1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"
	"github.com/open-cluster-management/registration/pkg/hub/spokecluster/bindata"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	manifestDir           = "pkg/hub/spokecluster"
	clusterRolePrefix     = "system:open-cluster-management:spokecluster"
	spokeClusterFinalizer = "cluster.open-cluster-management.io/api-resource-cleanup"
)

// spokeClusterController reconciles instances of SpokeCluster on the hub.
type spokeClusterController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	eventRecorder events.Recorder
}

// NewSpokeClusterController creates a new spoke cluster controller
func NewSpokeClusterController(
	kubeClient kubernetes.Interface,
	clusterClient clientset.Interface,
	clusterInformer factory.Informer,
	recorder events.Recorder) factory.Controller {
	c := &spokeClusterController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		eventRecorder: recorder.WithComponentSuffix("spoke-cluster-controller"),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer).
		WithSync(c.sync).
		ToController("SpokeClusterController", recorder)
}

func (c *spokeClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	spokeClusterName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling SpokeCluster %s", spokeClusterName)
	spokeCluster, err := c.clusterClient.ClusterV1().SpokeClusters().Get(ctx, spokeClusterName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	if spokeCluster.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range spokeCluster.Finalizers {
			if spokeCluster.Finalizers[i] == spokeClusterFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			spokeCluster.Finalizers = append(spokeCluster.Finalizers, spokeClusterFinalizer)
			_, err := c.clusterClient.ClusterV1().SpokeClusters().Update(ctx, spokeCluster, metav1.UpdateOptions{})
			return err
		}
	}

	// Spoke cluster is deleting, we remove its related resources
	if !spokeCluster.DeletionTimestamp.IsZero() {
		if err := c.removeSpokeClusterResources(ctx, spokeClusterName); err != nil {
			return err
		}
		return c.removeSpokeClusterFinalizer(ctx, spokeCluster)
	}

	if !spokeCluster.Spec.HubAcceptsClient {
		acceptedCondition := helpers.FindSpokeClusterCondition(spokeCluster.Status.Conditions, v1.SpokeClusterConditionHubAccepted)
		// Current spoke cluster is not accepted, do nothing.
		if !helpers.IsConditionTrue(acceptedCondition) {
			return nil
		}

		// Hub cluster-admin denies the current spoke cluster, we remove its related resources and update its condition.
		c.eventRecorder.Eventf("SpokeClusterDenied", "spoke cluster %s is denied by hub cluster admin", spokeClusterName)

		if err := c.removeSpokeClusterResources(ctx, spokeClusterName); err != nil {
			return err
		}

		_, _, err := helpers.UpdateSpokeClusterStatus(
			ctx,
			c.clusterClient,
			spokeClusterName,
			helpers.UpdateSpokeClusterConditionFn(v1.StatusCondition{
				Type:    v1.SpokeClusterConditionHubAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  "HubClusterAdminDenied",
				Message: "Denied by hub cluster admin",
			}),
		)
		return err
	}

	// Hub cluster-admin accepts the spoke cluster, we apply
	// 1. clusterrole and clusterrolebinding for this spoke cluster.
	// 2. namespace for this spoke cluster.
	// 3. role and rolebinding for this spoke cluster on its namespace.
	resourceResults := resourceapply.ApplyDirectly(
		resourceapply.NewKubeClientHolder(c.kubeClient),
		syncCtx.Recorder(),
		func(name string) ([]byte, error) {
			config := struct {
				SpokeClusterName string
			}{
				SpokeClusterName: spokeClusterName,
			}
			return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join(manifestDir, name)), config).Data, nil
		},
		"manifests/spokecluster-clusterrole.yaml",
		"manifests/spokecluster-clusterrolebinding.yaml",
		"manifests/spokecluster-namespace.yaml",
		"manifests/spokecluster-role.yaml",
		"manifests/spokecluster-rolebinding.yaml",
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// We add the accepted condition to spoke cluster
	acceptedCondition := v1.StatusCondition{
		Type:    v1.SpokeClusterConditionHubAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  "HubClusterAdminAccepted",
		Message: "Accepted by hub cluster admin",
	}

	if len(errs) > 0 {
		applyErrors := operatorhelpers.NewMultiLineAggregate(errs)
		acceptedCondition.Reason = "Error"
		acceptedCondition.Message = applyErrors.Error()
	}

	_, updated, updatedErr := helpers.UpdateSpokeClusterStatus(
		ctx,
		c.clusterClient,
		spokeClusterName,
		helpers.UpdateSpokeClusterConditionFn(acceptedCondition),
	)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}
	if updated {
		c.eventRecorder.Eventf("SpokeClusterAccepted", "spoke cluster %s is accepted by hub cluster admin", spokeClusterName)
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *spokeClusterController) removeSpokeClusterResources(ctx context.Context, spokeClusterName string) error {
	err := c.kubeClient.CoreV1().Namespaces().Delete(ctx, spokeClusterName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	c.eventRecorder.Eventf("SpokeClusterNamespaceDeleted", "namespace %s is deleted", spokeClusterName)

	clusterRoleName := fmt.Sprintf("%s:%s", clusterRolePrefix, spokeClusterName)
	err = c.kubeClient.RbacV1().ClusterRoles().Delete(ctx, clusterRoleName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	c.eventRecorder.Eventf("SpokeClusterClusterRoleDeleted", "clusterrole %s is deleted", clusterRoleName)

	//TODO search all clusterroles and roles for this group and remove the entry or delete the clusterrolebinding if it's the only subject.
	clusterRoleBindingName := fmt.Sprintf("%s:%s", clusterRolePrefix, spokeClusterName)
	err = c.kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, clusterRoleBindingName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	c.eventRecorder.Eventf("SpokeClusterClusterRoleBindingDeleted", "clusterrolebinding %s is deleted", clusterRoleBindingName)

	return nil
}

func (c *spokeClusterController) removeSpokeClusterFinalizer(ctx context.Context, spokeCluster *v1.SpokeCluster) error {
	copiedFinalizers := []string{}
	for i := range spokeCluster.Finalizers {
		if spokeCluster.Finalizers[i] == spokeClusterFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, spokeCluster.Finalizers[i])
	}

	if len(spokeCluster.Finalizers) != len(copiedFinalizers) {
		spokeCluster.Finalizers = copiedFinalizers
		_, err := c.clusterClient.ClusterV1().SpokeClusters().Update(ctx, spokeCluster, metav1.UpdateOptions{})
		return err
	}

	return nil
}
