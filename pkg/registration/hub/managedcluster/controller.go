package managedcluster

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/apply"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/manifests"
)

// this is an internal annotation to indicate a managed cluster is already accepted automatically, it is not
// expected to be changed or removed outside.
const clusterAcceptedAnnotationKey = "open-cluster-management.io/automatically-accepted-on"

var staticFiles = []string{
	"rbac/managedcluster-clusterrole.yaml",
	"rbac/managedcluster-clusterrolebinding.yaml",
	"rbac/managedcluster-registration-rolebinding.yaml",
	"rbac/managedcluster-work-rolebinding.yaml",
}

// managedClusterController reconciles instances of ManagedCluster on the hub.
type managedClusterController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	clusterLister listerv1.ManagedClusterLister
	applier       *apply.PermissionApplier
	patcher       patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
	eventRecorder events.Recorder
}

// NewManagedClusterController creates a new managed cluster controller
func NewManagedClusterController(
	kubeClient kubernetes.Interface,
	clusterClient clientset.Interface,
	clusterInformer informerv1.ManagedClusterInformer,
	roleInformer rbacv1informers.RoleInformer,
	clusterRoleInformer rbacv1informers.ClusterRoleInformer,
	rolebindingInformer rbacv1informers.RoleBindingInformer,
	clusterRoleBindingInformer rbacv1informers.ClusterRoleBindingInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		applier: apply.NewPermissionApplier(
			kubeClient,
			roleInformer.Lister(),
			rolebindingInformer.Lister(),
			clusterRoleInformer.Lister(),
			clusterRoleBindingInformer.Lister(),
		),
		patcher: patcher.NewPatcher[
			*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		eventRecorder: recorder.WithComponentSuffix("managed-cluster-controller"),
	}
	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByLabel(v1.ClusterNameLabelKey),
			queue.FileterByLabel(v1.ClusterNameLabelKey),
			roleInformer.Informer(),
			rolebindingInformer.Informer(),
			clusterRoleInformer.Informer(),
			clusterRoleBindingInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterController", recorder)
}

func (c *managedClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusterName := syncCtx.QueueKey()
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling ManagedCluster", "managedClusterName", managedClusterName)
	managedCluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	newManagedCluster := managedCluster.DeepCopy()

	if !managedCluster.DeletionTimestamp.IsZero() {
		// the cleanup job is moved to gc controller
		return nil
	}

	if !managedCluster.Spec.HubAcceptsClient {
		// If the ManagedClusterAutoApproval feature is enabled, we automatically accept a cluster only
		// when it joins for the first time, afterwards users can deny it again.
		if features.HubMutableFeatureGate.Enabled(ocmfeature.ManagedClusterAutoApproval) {
			if _, ok := managedCluster.Annotations[clusterAcceptedAnnotationKey]; !ok {
				return c.acceptCluster(ctx, managedClusterName)
			}
		}

		// Current spoke cluster is not accepted, do nothing.
		if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, v1.ManagedClusterConditionHubAccepted) {
			return nil
		}

		// Hub cluster-admin denies the current spoke cluster, we remove its related resources and update its condition.
		c.eventRecorder.Eventf("ManagedClusterDenied", "managed cluster %s is denied by hub cluster admin", managedClusterName)

		if err := c.removeManagedClusterResources(ctx, managedClusterName); err != nil {
			return err
		}

		meta.SetStatusCondition(&newManagedCluster.Status.Conditions, metav1.Condition{
			Type:    v1.ManagedClusterConditionHubAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  "HubClusterAdminDenied",
			Message: "Denied by hub cluster admin",
		})

		if _, err := c.patcher.PatchStatus(ctx, newManagedCluster, newManagedCluster.Status, managedCluster.Status); err != nil {
			return err
		}
		return nil
	}

	// TODO consider to add the managedcluster-namespace.yaml back to staticFiles,
	// currently, we keep the namespace after the managed cluster is deleted.
	// apply namespace at first
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterName,
			Labels: map[string]string{
				v1.ClusterNameLabelKey: managedClusterName,
			},
		},
	}

	var errs []error
	_, _, err = resourceapply.ApplyNamespace(ctx, c.kubeClient.CoreV1(), syncCtx.Recorder(), namespace)
	if err != nil {
		errs = append(errs, err)
	}

	// Hub cluster-admin accepts the spoke cluster, we apply
	// 1. clusterrole and clusterrolebinding for this spoke cluster.
	// 2. namespace for this spoke cluster.
	// 3. role and rolebinding for this spoke cluster on its namespace.
	resourceResults := c.applier.Apply(
		ctx,
		syncCtx.Recorder(),
		helpers.ManagedClusterAssetFn(manifests.RBACManifests, managedClusterName),
		staticFiles...,
	)
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// We add the accepted condition to spoke cluster
	acceptedCondition := metav1.Condition{
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

	meta.SetStatusCondition(&newManagedCluster.Status.Conditions, acceptedCondition)
	updated, updatedErr := c.patcher.PatchStatus(ctx, newManagedCluster, newManagedCluster.Status, managedCluster.Status)
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}
	if updated {
		c.eventRecorder.Eventf("ManagedClusterAccepted", "managed cluster %s is accepted by hub cluster admin", managedClusterName)
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *managedClusterController) removeManagedClusterResources(ctx context.Context, managedClusterName string) error {
	var errs []error
	// Clean up managed cluster manifests
	assetFn := helpers.ManagedClusterAssetFn(manifests.RBACManifests, managedClusterName)
	resourceResults := resourceapply.DeleteAll(ctx, resourceapply.NewKubeClientHolder(c.kubeClient), c.eventRecorder, assetFn, staticFiles...)
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (c *managedClusterController) acceptCluster(ctx context.Context, managedClusterName string) error {
	// TODO support patching both annotations and spec simultaneously in the patcher
	acceptedTime := time.Now()
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}},"spec":{"hubAcceptsClient":true}}`,
		clusterAcceptedAnnotationKey, acceptedTime.Format(time.RFC3339))
	_, err := c.clusterClient.ClusterV1().ManagedClusters().Patch(ctx, managedClusterName,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}
