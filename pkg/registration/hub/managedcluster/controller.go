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
	"open-cluster-management.io/ocm/pkg/registration/register"
)

// this is an internal annotation to indicate a managed cluster is already accepted automatically, it is not
// expected to be changed or removed outside.
const clusterAcceptedAnnotationKey = "open-cluster-management.io/automatically-accepted-on"

// managedClusterController reconciles instances of ManagedCluster on the hub.
type managedClusterController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	clusterLister listerv1.ManagedClusterLister
	applier       *apply.PermissionApplier
	patcher       patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
	approver      register.Approver
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
	approver register.Approver,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		approver:      approver,
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

	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManagedClusterAutoApproval) {
		// If the ManagedClusterAutoApproval feature is enabled, we automatically accept a cluster only
		// when it joins for the first time, afterwards users can deny it again.
		if _, ok := managedCluster.Annotations[clusterAcceptedAnnotationKey]; !ok {
			return c.acceptCluster(ctx, managedCluster)
		}
	}

	if !managedCluster.Spec.HubAcceptsClient {
		// Current spoke cluster is not accepted, do nothing.
		if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, v1.ManagedClusterConditionHubAccepted) {
			return nil
		}

		// Hub cluster-admin denies the current spoke cluster, we remove its related resources and update its condition.
		c.eventRecorder.Eventf("ManagedClusterDenied", "managed cluster %s is denied by hub cluster admin", managedClusterName)

		// Apply(Update) the cluster specific rbac resources for this spoke cluster with hubAcceptsClient=false.
		var errs []error
		applyResults := c.applier.Apply(ctx, syncCtx.Recorder(),
			helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, managedClusterName, managedCluster.Spec.HubAcceptsClient),
			manifests.ClusterSpecificRBACFiles...)
		for _, result := range applyResults {
			if result.Error != nil {
				errs = append(errs, result.Error)
			}
		}
		if aggErr := operatorhelpers.NewMultiLineAggregate(errs); aggErr != nil {
			return aggErr
		}

		// Remove the cluster role binding files for registration-agent and work-agent.
		removeResults := resourceapply.DeleteAll(ctx,
			resourceapply.NewKubeClientHolder(c.kubeClient),
			c.eventRecorder,
			helpers.ManagedClusterAssetFn(manifests.RBACManifests, managedClusterName),
			manifests.ClusterSpecificRoleBindings...)
		for _, result := range removeResults {
			if result.Error != nil {
				errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
			}
		}
		if aggErr := operatorhelpers.NewMultiLineAggregate(errs); aggErr != nil {
			return aggErr
		}

		if err = c.approver.Cleanup(ctx, managedCluster); err != nil {
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

	// Hub cluster-admin accepts the spoke cluster, we apply
	// 1. namespace for this spoke cluster.
	// 2. cluster specific rbac resources for this spoke cluster.(hubAcceptsClient=true)
	// 3. cluster specific rolebinding(registration-agent and work-agent) for this spoke cluster.
	var errs []error
	_, _, err = resourceapply.ApplyNamespace(ctx, c.kubeClient.CoreV1(), syncCtx.Recorder(), namespace)
	if err != nil {
		errs = append(errs, err)
	}

	// the ManagedClusterFinalizer is added and removed by gc controller.
	// the rbac resources are deleted by gc controller too.
	// should apply the rbac resources after the finalizer is added to the cluster, because in the case the cluster is
	// created and deleted so quickly, the rbac resources are applied but the cluster is gone, there is no event to
	// gc controller to trigger gc .
	if helpers.HasFinalizer(managedCluster.Finalizers, v1.ManagedClusterFinalizer) {
		resourceResults := c.applier.Apply(ctx, syncCtx.Recorder(),
			helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, managedClusterName, managedCluster.Spec.HubAcceptsClient),
			append(manifests.ClusterSpecificRBACFiles, manifests.ClusterSpecificRoleBindings...)...)
		for _, result := range resourceResults {
			if result.Error != nil {
				errs = append(errs, result.Error)
			}
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
		acceptedCondition.Status = metav1.ConditionFalse
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

func (c *managedClusterController) acceptCluster(ctx context.Context, managedCluster *v1.ManagedCluster) error {
	acceptedTime := time.Now()

	// If one cluster is already accepted, we only add the cluster accepted annotation, otherwise
	// we add the cluster accepted annotation and accept the cluster.
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`,
		clusterAcceptedAnnotationKey, acceptedTime.Format(time.RFC3339))
	if !managedCluster.Spec.HubAcceptsClient {
		// TODO support patching both annotations and spec simultaneously in the patcher
		patch = fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}},"spec":{"hubAcceptsClient":true}}`,
			clusterAcceptedAnnotationKey, acceptedTime.Format(time.RFC3339))
	}

	_, err := c.clusterClient.ClusterV1().ManagedClusters().Patch(ctx, managedCluster.Name,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}
