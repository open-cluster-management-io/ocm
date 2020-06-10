package lease

import (
	"context"
	"fmt"
	"sync"
	"time"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informer "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	coordv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	kubeClient      kubernetes.Interface
	clusterClient   clientset.Interface
	clusterLister   clusterv1listers.ManagedClusterLister
	leaseLister     coordlisters.LeaseLister
	clusterLeaseMap *clusterLeaseMap
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
		kubeClient:      kubeClient,
		clusterClient:   clusterClient,
		clusterLister:   clusterInformer.Lister(),
		leaseLister:     leaseInformer.Lister(),
		clusterLeaseMap: newClusterLeaseMap(),
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
		acceptedCondition := helpers.FindManagedClusterCondition(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
		if !helpers.IsConditionTrue(acceptedCondition) {
			continue
		}

		// get the lease of a cluster, if the lease is not found, create it
		leaseName := fmt.Sprintf("cluster-%s-lease", cluster.Name)
		observedLease, err := c.leaseLister.Leases(cluster.Name).Get(leaseName)
		switch {
		case errors.IsNotFound(err):
			lease := &coordv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      leaseName,
					Namespace: cluster.Name,
					Labels:    map[string]string{"open-cluster-management.io/cluster-name": cluster.Name},
				},
				Spec: coordv1.LeaseSpec{
					HolderIdentity: pointer.StringPtr(leaseName),
				},
			}
			if _, err := c.kubeClient.CoordinationV1().Leases(cluster.Name).Create(ctx, lease, metav1.CreateOptions{}); err != nil {
				return err
			}
			continue
		case err != nil:
			return err
		}

		// update the lease probe time and last lease on hub cluster if the managed cluster lease is constantly
		// updated, in next, we will use hub probe time to determine whether the managed cluster lease is constantly
		// updated in a grace period to avoid the problem of time synchronization between hub and managed
		// cluster.
		lastClusterLease := c.clusterLeaseMap.get(observedLease.Name)
		if lastClusterLease.lease == nil || (lastClusterLease.lease.Spec.RenewTime == nil ||
			lastClusterLease.lease.Spec.RenewTime.Before(observedLease.Spec.RenewTime)) {
			lastClusterLease.lease = observedLease.DeepCopy()
			lastClusterLease.probeTimestamp = metav1.Now()
			c.clusterLeaseMap.set(observedLease.Name, lastClusterLease)
		}

		leaseDurationSeconds := cluster.Spec.LeaseDurationSeconds
		// TODO: use CRDs defaulting or mutating admission webhook to eliminate this code.
		if leaseDurationSeconds == 0 {
			leaseDurationSeconds = 60
		}

		gracePeriod := time.Duration(leaseDurationTimes*leaseDurationSeconds) * time.Second
		// the lease is constantly updated, do nothing
		now := metav1.Now()
		if now.Time.Before(lastClusterLease.probeTimestamp.Add(gracePeriod)) {
			continue
		}

		// the lease is not constantly updated, update it to unknown
		conditionUpdateFn := helpers.UpdateManagedClusterConditionFn(clusterv1.StatusCondition{
			Type:   clusterv1.ManagedClusterConditionAvailable,
			Status: metav1.ConditionUnknown,
			Reason: "ManagedClusterLeaseUpdateStopped",
			Message: fmt.Sprintf("Registration agent stopped updating its lease within %.0f minutes.",
				now.Sub(lastClusterLease.probeTimestamp.Time).Minutes()),
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

type clusterLease struct {
	probeTimestamp metav1.Time
	lease          *coordv1.Lease
}

type clusterLeaseMap struct {
	lock          sync.RWMutex
	clusterLeases map[string]*clusterLease
}

func newClusterLeaseMap() *clusterLeaseMap {
	return &clusterLeaseMap{
		clusterLeases: make(map[string]*clusterLease),
	}
}

func (n *clusterLeaseMap) get(name string) *clusterLease {
	n.lock.RLock()
	defer n.lock.RUnlock()
	if leaseData, ok := n.clusterLeases[name]; ok {
		return leaseData
	}
	return &clusterLease{}
}

func (n *clusterLeaseMap) set(name string, data *clusterLease) {
	n.lock.Lock()
	defer n.lock.Unlock()
	n.clusterLeases[name] = data
}
