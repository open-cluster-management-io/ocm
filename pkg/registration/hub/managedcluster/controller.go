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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/apply"
	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/hub/manifests"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

// this is an internal annotation to indicate a managed cluster is already accepted automatically, it is not
// expected to be changed or removed outside.
const clusterAcceptedAnnotationKey = "open-cluster-management.io/automatically-accepted-on"

var (
	workRoleBindingName = func(clusterName string) string {
		return fmt.Sprintf("open-cluster-management:managedcluster:%s:work", clusterName)
	}
	requeueError = commonhelper.NewRequeueError("cluster rbac cleanup requeue", 5*time.Second)
)

// managedClusterController reconciles instances of ManagedCluster on the hub.
type managedClusterController struct {
	kubeClient         kubernetes.Interface
	clusterClient      clientset.Interface
	roleBindingLister  rbacv1listers.RoleBindingLister
	manifestWorkLister worklister.ManifestWorkLister
	clusterLister      listerv1.ManagedClusterLister
	applier            *apply.PermissionApplier
	patcher            patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
	approver           register.Approver
	hubDriver          register.HubDriver
	eventRecorder      events.Recorder
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
	manifestWorkInformer workinformers.ManifestWorkInformer,
	approver register.Approver,
	hubDriver register.HubDriver,
	recorder events.Recorder) factory.Controller {

	c := &managedClusterController{
		kubeClient:         kubeClient,
		clusterClient:      clusterClient,
		roleBindingLister:  rolebindingInformer.Lister(),
		manifestWorkLister: manifestWorkInformer.Lister(),
		clusterLister:      clusterInformer.Lister(),
		hubDriver:          hubDriver,
		approver:           approver,
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
		return c.removeClusterRbac(ctx, managedClusterName, true)
	}
	if err != nil {
		return err
	}

	newManagedCluster := managedCluster.DeepCopy()

	if !managedCluster.DeletionTimestamp.IsZero() {
		if err = c.approver.Cleanup(ctx, managedCluster); err != nil {
			return err
		}

		err = c.removeClusterRbac(ctx, managedClusterName, true)
		if err != nil {
			return err
		}

		return c.patcher.RemoveFinalizer(ctx, newManagedCluster, v1.ManagedClusterFinalizer)
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

		meta.SetStatusCondition(&newManagedCluster.Status.Conditions, metav1.Condition{
			Type:    v1.ManagedClusterConditionHubAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  "HubClusterAdminDenied",
			Message: "Denied by hub cluster admin",
		})

		if _, err := c.patcher.PatchStatus(ctx, newManagedCluster, newManagedCluster.Status, managedCluster.Status); err != nil {
			return err
		}

		// Remove the cluster role binding files for registration-agent and work-agent.
		return c.removeClusterRbac(ctx, managedClusterName, managedCluster.Spec.HubAcceptsClient)
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

	_, err = c.patcher.AddFinalizer(ctx, newManagedCluster, v1.ManagedClusterFinalizer)
	if err != nil {
		return err
	}

	resourceResults := c.applier.Apply(ctx, syncCtx.Recorder(),
		helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, managedClusterName, managedCluster.Spec.HubAcceptsClient),
		append(manifests.ClusterSpecificRBACFiles, manifests.ClusterSpecificRoleBindings...)...)
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, result.Error)
		}
	}

	// If the resources already exist, errors are ignored
	err = c.hubDriver.CreatePermissions(ctx, managedCluster)
	if err != nil {
		errs = append(errs, err)
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

// remove the cluster rbac resources firstly.
// the work roleBinding with a finalizer remains because it is used by work agent to operator the works.
// the finalizer on work roleBinding will be removed after there is no works in the ns.
func (c *managedClusterController) removeClusterRbac(ctx context.Context, clusterName string, accepted bool) error {
	var errs []error
	assetFn := helpers.ManagedClusterAssetFnWithAccepted(manifests.RBACManifests, clusterName, accepted)
	files := manifests.ClusterSpecificRoleBindings
	if accepted {
		files = append(files, manifests.ClusterSpecificRBACFiles...)
	}

	resourceResults := resourceapply.DeleteAll(ctx, resourceapply.NewKubeClientHolder(c.kubeClient),
		c.eventRecorder, assetFn, files...)
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	works, err := c.manifestWorkLister.ManifestWorks(clusterName).List(labels.Everything())
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, err)
		return operatorhelpers.NewMultiLineAggregate(errs)
	}
	if len(works) != 0 {
		return requeueError
	}

	return c.removeFinalizerFromWorkRoleBinding(ctx, clusterName)
}

func (c *managedClusterController) removeFinalizerFromWorkRoleBinding(ctx context.Context, clusterName string) error {
	workRoleBinding, err := c.roleBindingLister.RoleBindings(clusterName).Get(workRoleBindingName(clusterName))
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	roleBindingFinalizerPatcher := patcher.NewPatcher[*rbacv1.RoleBinding, rbacv1.RoleBinding,
		rbacv1.RoleBinding](c.kubeClient.RbacV1().RoleBindings(clusterName))
	return roleBindingFinalizerPatcher.RemoveFinalizer(ctx, workRoleBinding, workv1.ManifestWorkFinalizer)
}
