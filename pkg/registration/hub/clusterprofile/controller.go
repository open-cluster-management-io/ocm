package clusterprofile

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions/apis/v1alpha1"
	cplisterv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/listers/apis/v1alpha1"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	ClusterProfileManagerName = "open-cluster-management"
	ClusterProfileNamespace   = "open-cluster-management"
)

// clusterProfileController reconciles instances of ClusterProfile on the hub.
type clusterProfileController struct {
	clusterLister        listerv1.ManagedClusterLister
	clusterProfileClient cpclientset.Interface
	clusterProfileLister cplisterv1alpha1.ClusterProfileLister
	patcher              patcher.Patcher[*cpv1alpha1.ClusterProfile, cpv1alpha1.ClusterProfileSpec, cpv1alpha1.ClusterProfileStatus]
}

// NewClusterProfileController creates a new managed cluster controller
func NewClusterProfileController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha1.ClusterProfileInformer) factory.Controller {
	c := &clusterProfileController{
		clusterLister:        clusterInformer.Lister(),
		clusterProfileClient: clusterProfileClient,
		clusterProfileLister: clusterProfileInformer.Lister(),
		patcher: patcher.NewPatcher[
			*cpv1alpha1.ClusterProfile, cpv1alpha1.ClusterProfileSpec, cpv1alpha1.ClusterProfileStatus](
			clusterProfileClient.ApisV1alpha1().ClusterProfiles(ClusterProfileNamespace)),
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer(), clusterProfileInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileController")
}

func (c *clusterProfileController) sync(ctx context.Context, syncCtx factory.SyncContext, managedClusterName string) error {
	logger := klog.FromContext(ctx).WithValues("managedClusterName", managedClusterName)
	logger.V(4).Info("Reconciling Cluster")

	managedCluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	clusterProfile, err := c.clusterProfileLister.ClusterProfiles(ClusterProfileNamespace).Get(managedClusterName)

	// if the managed cluster is deleting, delete the clusterprofile as well.
	if !managedCluster.DeletionTimestamp.IsZero() {
		if errors.IsNotFound(err) {
			return nil
		}
		err = c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(ClusterProfileNamespace).Delete(ctx, managedClusterName, metav1.DeleteOptions{})
		return err
	}

	// create cluster profile if not found
	if errors.IsNotFound(err) {
		clusterProfile = &cpv1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:   managedClusterName,
				Labels: map[string]string{cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName},
			},
			Spec: cpv1alpha1.ClusterProfileSpec{
				DisplayName: managedClusterName,
				ClusterManager: cpv1alpha1.ClusterManager{
					Name: ClusterProfileManagerName,
				},
			},
		}
		_, err = c.clusterProfileClient.ApisV1alpha1().ClusterProfiles(ClusterProfileNamespace).Create(ctx, clusterProfile, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	// return if not managed by ocm
	if clusterProfile.Spec.ClusterManager.Name != ClusterProfileManagerName {
		logger.Info("Not managed by open-cluster-management, skipping", "ClusterName", managedClusterName)
		return nil
	}

	newClusterProfile := clusterProfile.DeepCopy()

	// sync required labels
	mclLabels := managedCluster.GetLabels()
	mclSetLabel := mclLabels[v1beta2.ClusterSetLabel]
	// The value of label "x-k8s.io/cluster-manager" MUST be the same as the name of the cluster manager.
	requiredLabels := map[string]string{
		cpv1alpha1.LabelClusterManagerKey: newClusterProfile.Spec.ClusterManager.Name,
		cpv1alpha1.LabelClusterSetKey:     mclSetLabel,
	}
	modified := false
	resourcemerge.MergeMap(&modified, &newClusterProfile.Labels, requiredLabels)

	// patch labels
	if modified {
		_, err := c.patcher.PatchLabelAnnotations(ctx, newClusterProfile, newClusterProfile.ObjectMeta, clusterProfile.ObjectMeta)
		return err
	}

	// sync status.version.kubernetes
	newClusterProfile.Status.Version.Kubernetes = managedCluster.Status.Version.Kubernetes

	// sync status.properties
	cpProperties := []cpv1alpha1.Property{}
	for _, v := range managedCluster.Status.ClusterClaims {
		cpProperties = append(cpProperties, cpv1alpha1.Property{Name: v.Name, Value: v.Value})
	}
	newClusterProfile.Status.Properties = cpProperties

	// sync status.conditions
	managedClusterAvailableCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, v1.ManagedClusterConditionAvailable)
	if managedClusterAvailableCondition != nil {
		c := metav1.Condition{
			Type:    cpv1alpha1.ClusterConditionControlPlaneHealthy,
			Status:  managedClusterAvailableCondition.Status,
			Reason:  managedClusterAvailableCondition.Reason,
			Message: managedClusterAvailableCondition.Message,
		}
		meta.SetStatusCondition(&newClusterProfile.Status.Conditions, c)
	}
	managedClusterJoinedCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, v1.ManagedClusterConditionJoined)
	if managedClusterJoinedCondition != nil {
		c := metav1.Condition{
			Type:    "Joined",
			Status:  managedClusterJoinedCondition.Status,
			Reason:  managedClusterJoinedCondition.Reason,
			Message: managedClusterJoinedCondition.Message,
		}
		meta.SetStatusCondition(&newClusterProfile.Status.Conditions, c)
	}

	// patch status
	updated, err := c.patcher.PatchStatus(ctx, newClusterProfile, newClusterProfile.Status, clusterProfile.Status)
	if err != nil {
		return err
	}
	if updated {
		syncCtx.Recorder().Eventf(ctx, "ClusterProfileSynced", "cluster profile %s is synced from open cluster management", managedClusterName)
	}
	return nil
}
