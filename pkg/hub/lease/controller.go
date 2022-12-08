package lease

import (
	"context"
	"time"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	coordv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coordinformers "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	coordlisters "k8s.io/client-go/listers/coordination/v1"
	"k8s.io/utils/pointer"
)

const leaseDurationTimes = 5
const leaseName = "managed-cluster-lease"
const clusterNameLabel = "open-cluster-management.io/cluster-name"

var (
	// LeaseDurationSeconds is lease update time interval
	LeaseDurationSeconds = 60
)

// leaseController checks the lease of managed clusters on hub cluster to determine whether a managed cluster is available.
type leaseController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	clusterLister clusterv1listers.ManagedClusterLister
	leaseLister   coordlisters.LeaseLister
	eventRecorder events.Recorder
}

// NewClusterLeaseController creates a cluster lease controller on hub cluster.
func NewClusterLeaseController(
	kubeClient kubernetes.Interface,
	clusterClient clientset.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	leaseInformer coordinformers.LeaseInformer,
	recorder events.Recorder) factory.Controller {
	c := &leaseController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		leaseLister:   leaseInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("managed-cluster-lease-controller"),
	}
	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetLabels()[clusterNameLabel]
			},
			func(obj interface{}) bool {
				metaObj, ok := obj.(metav1.ObjectMetaAccessor)
				if !ok {
					return false
				}

				// only handle the managed cluster lease
				// TODO instead of this by adding label filter in the SharedInformerFactory
				// see https://github.com/open-cluster-management-io/registration/issues/225
				if _, ok := metaObj.GetObjectMeta().GetLabels()[clusterNameLabel]; !ok {
					return false
				}

				return metaObj.GetObjectMeta().GetName() == leaseName
			},
			leaseInformer.Informer(),
		).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterLeaseController", recorder)
}

// sync checks the lease of each accepted cluster on hub to determine whether a managed cluster is available.
func (c *leaseController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusterName := syncCtx.QueueKey()

	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		// the cluster is not found, do nothing
		return nil
	}
	if err != nil {
		return err
	}

	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		// cluster is not accepted, skip it.
		return nil
	}

	observedLease, err := c.leaseLister.Leases(cluster.Name).Get(leaseName)
	if errors.IsNotFound(err) {
		if !cluster.DeletionTimestamp.IsZero() {
			// the lease is not found and the cluster is deleting, update the cluster to unknown immediately
			return c.updateClusterStatus(ctx, cluster)
		}

		// the lease is not found, try to create it
		lease := &coordv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: cluster.Name,
				Labels:    map[string]string{clusterNameLabel: cluster.Name},
			},
			Spec: coordv1.LeaseSpec{
				HolderIdentity: pointer.StringPtr(leaseName),
				RenewTime:      &metav1.MicroTime{Time: time.Now()},
			},
		}
		_, err := c.kubeClient.CoordinationV1().Leases(cluster.Name).Create(ctx, lease, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	gracePeriod := time.Duration(leaseDurationTimes*cluster.Spec.LeaseDurationSeconds) * time.Second
	if gracePeriod == 0 {
		// FIX: #183 avoid gracePeriod is zero, will non-stop update ManagedClusterLeaseUpdateStopped condition.
		gracePeriod = time.Duration(leaseDurationTimes*LeaseDurationSeconds) * time.Second
	}

	now := time.Now()
	if !now.Before(observedLease.Spec.RenewTime.Add(gracePeriod)) {
		// the lease is not updated constantly, change the cluster available condition to unknown
		if err := c.updateClusterStatus(ctx, cluster); err != nil {
			return err
		}
	}

	// always requeue this cluster to check its lease constantly
	syncCtx.Queue().AddAfter(clusterName, gracePeriod)
	return nil
}

func (c *leaseController) updateClusterStatus(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	if meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable, metav1.ConditionUnknown) {
		// the managed cluster available condition alreay is unknown, do nothing
		return nil
	}

	// the lease is not constantly updated, update it to unknown
	conditionUpdateFn := helpers.UpdateManagedClusterConditionFn(metav1.Condition{
		Type:    clusterv1.ManagedClusterConditionAvailable,
		Status:  metav1.ConditionUnknown,
		Reason:  "ManagedClusterLeaseUpdateStopped",
		Message: "Registration agent stopped updating its lease.",
	})

	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.clusterClient, cluster.Name, conditionUpdateFn)
	if updated {
		c.eventRecorder.Eventf("ManagedClusterAvailableConditionUpdated",
			"update managed cluster %q available condition to unknown, due to its lease is not updated constantly",
			cluster.Name)
	}

	return err
}
