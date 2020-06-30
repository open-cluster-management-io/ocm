package managedcluster

import (
	"context"
	"fmt"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	v1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"

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
	manifestDir             = "pkg/hub/managedcluster"
	clusterRolePrefix       = "system:open-cluster-management:managedcluster"
	managedClusterFinalizer = "cluster.open-cluster-management.io/api-resource-cleanup"
)

var staticFiles = []string{
	"manifests/managedcluster-clusterrole.yaml",
	"manifests/managedcluster-clusterrolebinding.yaml",
	"manifests/managedcluster-registration-role.yaml",
	"manifests/managedcluster-registration-rolebinding.yaml",
	"manifests/managedcluster-work-role.yaml",
	"manifests/managedcluster-work-rolebinding.yaml",
}

// managedClusterController reconciles instances of ManagedCluster on the hub.
type managedClusterController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	eventRecorder events.Recorder
}

// NewManagedClusterController creates a new managed cluster controller
func NewManagedClusterController(
	kubeClient kubernetes.Interface,
	clusterClient clientset.Interface,
	clusterInformer factory.Informer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		eventRecorder: recorder.WithComponentSuffix("managed-cluster-controller"),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer).
		WithSync(c.sync).
		ToController("ManagedClusterController", recorder)
}

func (c *managedClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusterName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling ManagedCluster %s", managedClusterName)
	managedCluster, err := c.clusterClient.ClusterV1().ManagedClusters().Get(ctx, managedClusterName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	if managedCluster.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range managedCluster.Finalizers {
			if managedCluster.Finalizers[i] == managedClusterFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			managedCluster.Finalizers = append(managedCluster.Finalizers, managedClusterFinalizer)
			_, err := c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
			return err
		}
	}

	// Spoke cluster is deleting, we remove its related resources
	if !managedCluster.DeletionTimestamp.IsZero() {
		if err := c.removeManagedClusterResources(ctx, managedClusterName); err != nil {
			return err
		}
		return c.removeManagedClusterFinalizer(ctx, managedCluster)
	}

	if !managedCluster.Spec.HubAcceptsClient {
		acceptedCondition := helpers.FindManagedClusterCondition(managedCluster.Status.Conditions, v1.ManagedClusterConditionHubAccepted)
		// Current spoke cluster is not accepted, do nothing.
		if !helpers.IsConditionTrue(acceptedCondition) {
			return nil
		}

		// Hub cluster-admin denies the current spoke cluster, we remove its related resources and update its condition.
		c.eventRecorder.Eventf("ManagedClusterDenied", "managed cluster %s is denied by hub cluster admin", managedClusterName)

		if err := c.removeManagedClusterResources(ctx, managedClusterName); err != nil {
			return err
		}

		_, _, err := helpers.UpdateManagedClusterStatus(
			ctx,
			c.clusterClient,
			managedClusterName,
			helpers.UpdateManagedClusterConditionFn(v1.StatusCondition{
				Type:    v1.ManagedClusterConditionHubAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  "HubClusterAdminDenied",
				Message: "Denied by hub cluster admin",
			}),
		)
		return err
	}

	// TODO: we will add the managedcluster-namespace.yaml back to staticFiles
	// in next release, currently, we need keep the namespace after the managed
	// cluster is deleted, see the issue
	// https://github.com/open-cluster-management/backlog/issues/2648
	applyFiles := []string{"manifests/managedcluster-namespace.yaml"}
	applyFiles = append(applyFiles, staticFiles...)

	// Hub cluster-admin accepts the spoke cluster, we apply
	// 1. clusterrole and clusterrolebinding for this spoke cluster.
	// 2. namespace for this spoke cluster.
	// 3. role and rolebinding for this spoke cluster on its namespace.
	resourceResults := resourceapply.ApplyDirectly(
		resourceapply.NewKubeClientHolder(c.kubeClient),
		syncCtx.Recorder(),
		helpers.ManagedClusterAssetFn(manifestDir, managedClusterName),
		applyFiles...,
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// We add the accepted condition to spoke cluster
	acceptedCondition := v1.StatusCondition{
		Type:    v1.ManagedClusterConditionHubAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  "HubClusterAdminAccepted",
		Message: "Accepted by hub cluster admin",
	}

	if len(errs) > 0 {
		applyErrors := operatorhelpers.NewMultiLineAggregate(errs)
		acceptedCondition.Reason = "Error"
		acceptedCondition.Message = applyErrors.Error()
	}

	_, updated, updatedErr := helpers.UpdateManagedClusterStatus(
		ctx,
		c.clusterClient,
		managedClusterName,
		helpers.UpdateManagedClusterConditionFn(acceptedCondition),
	)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}
	if updated {
		c.eventRecorder.Eventf("ManagedClusterAccepted", "managed cluster %s is accepted by hub cluster admin", managedClusterName)
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *managedClusterController) removeManagedClusterResources(ctx context.Context, managedClusterName string) error {
	errs := []error{}
	// Cleap up managed cluster manifests
	assetFn := helpers.ManagedClusterAssetFn(manifestDir, managedClusterName)
	if err := helpers.CleanUpManagedClusterManifests(ctx, c.kubeClient, c.eventRecorder, assetFn, staticFiles...); err != nil {
		errs = append(errs, err)
	}
	// Clean up managed cluster group from clusterrolebindings and rolebindings.
	managedClusterGroup := fmt.Sprintf("system:open-cluster-management:%s", managedClusterName)
	if err := helpers.CleanUpGroupFromClusterRoleBindings(ctx, c.kubeClient, c.eventRecorder, managedClusterGroup); err != nil {
		errs = append(errs, err)
	}
	if err := helpers.CleanUpGroupFromRoleBindings(ctx, c.kubeClient, c.eventRecorder, managedClusterGroup); err != nil {
		errs = append(errs, err)
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *managedClusterController) removeManagedClusterFinalizer(ctx context.Context, managedCluster *v1.ManagedCluster) error {
	copiedFinalizers := []string{}
	for i := range managedCluster.Finalizers {
		if managedCluster.Finalizers[i] == managedClusterFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, managedCluster.Finalizers[i])
	}

	if len(managedCluster.Finalizers) != len(copiedFinalizers) {
		managedCluster.Finalizers = copiedFinalizers
		_, err := c.clusterClient.ClusterV1().ManagedClusters().Update(ctx, managedCluster, metav1.UpdateOptions{})
		return err
	}

	return nil
}
