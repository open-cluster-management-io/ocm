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
	"k8s.io/apimachinery/pkg/labels"
	coordinformers "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	coordlisters "k8s.io/client-go/listers/coordination/v1"
	"k8s.io/utils/pointer"
)

const leaseDurationTimes = 5

// leaseController checks the lease of managed clusters on hub cluster to determine whether a managed cluster is available.
type leaseController struct {
	kubeClient    kubernetes.Interface
	clusterClient clientset.Interface
	clusterLister clusterv1listers.ManagedClusterLister
	leaseLister   coordlisters.LeaseLister
}

// NewClusterLeaseController creates a cluster lease controller on hub cluster.
func NewClusterLeaseController(
	kubeClient kubernetes.Interface,
	clusterClient clientset.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	leaseInformer coordinformers.LeaseInformer,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := &leaseController{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		leaseLister:   leaseInformer.Lister(),
	}
	return factory.New().
		WithInformers(clusterInformer.Informer(), leaseInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterLeaseController", recorder)
}

// sync checks the lease of each accepted cluster on hub to determine whether a managed cluster is available.
func (c *leaseController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusters, err := c.clusterLister.List(labels.Everything())
	if err != nil {
		return nil
	}
	for _, cluster := range clusters {
		// cluster is not accepted, skip it.
		if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			continue
		}

		// get the lease of a cluster, if the lease is not found, create it
		leaseName := "managed-cluster-lease"
		observedLease, err := c.leaseLister.Leases(cluster.Name).Get(leaseName)
		switch {
		case errors.IsNotFound(err):
			if !cluster.DeletionTimestamp.IsZero() {
				// the cluster is deleting, do nothing
				break
			}
			lease := &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      leaseName,
					Namespace: cluster.Name,
					Labels:    map[string]string{"open-cluster-management.io/cluster-name": cluster.Name},
				},
				Spec: coordv1.LeaseSpec{
					HolderIdentity: pointer.StringPtr(leaseName),
					RenewTime:      &metav1.MicroTime{Time: time.Now()},
				},
			}
			if _, err := c.kubeClient.CoordinationV1().Leases(cluster.Name).Create(ctx, lease, metav1.CreateOptions{}); err != nil {
				return err
			}
			continue
		case err != nil:
			return err
		case err == nil:
			gracePeriod := time.Duration(leaseDurationTimes*cluster.Spec.LeaseDurationSeconds) * time.Second
			// the lease is constantly updated, do nothing
			now := time.Now()
			if now.Before(observedLease.Spec.RenewTime.Add(gracePeriod)) {
				continue
			}
		}

		// the lease is not constantly updated, update it to unknown
		conditionUpdateFn := helpers.UpdateManagedClusterConditionFn(metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "ManagedClusterLeaseUpdateStopped",
			Message: "Registration agent stopped updating its lease.",
		})
		_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.clusterClient, cluster.Name, conditionUpdateFn)
		if err != nil {
			return err
		}
		if updated {
			syncCtx.Recorder().Eventf("ManagedClusterAvailableConditionUpdated",
				"update managed cluster %q available condition to unknown, due to its lease is not updated constantly",
				cluster.Name)
		}
	}
	return nil
}
