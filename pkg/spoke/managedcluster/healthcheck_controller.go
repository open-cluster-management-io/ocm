package managedcluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informer "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	discovery "k8s.io/client-go/discovery"
)

// managedClusterHealthCheckController checks the kube-apiserver health on managed cluster to determine it whether is available.
type managedClusterHealthCheckController struct {
	clusterName                   string
	hubClusterClient              clientset.Interface
	hubClusterLister              clusterv1listers.ManagedClusterLister
	managedClusterDiscoveryClient discovery.DiscoveryInterface
}

// NewManagedClusterHealthCheckController creates a managed cluster health check controller on managed cluster.
func NewManagedClusterHealthCheckController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterHealthCheckController{
		clusterName:                   clusterName,
		hubClusterClient:              hubClusterClient,
		hubClusterLister:              hubClusterInformer.Lister(),
		managedClusterDiscoveryClient: managedClusterDiscoveryClient,
	}

	return factory.New().
		WithInformers(hubClusterInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterHealthCheckController", recorder)
}

// sync updates managed cluster available condition by checking kube-apiserver health on managed cluster.
func (c *managedClusterHealthCheckController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	if _, err := c.hubClusterLister.Get(c.clusterName); err != nil {
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	// check the kube-apiserver health on managed cluster.
	condition := c.checkKubeAPIServerStatus(ctx)
	conditionUpdateFn := helpers.UpdateManagedClusterConditionFn(condition)
	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.hubClusterClient, c.clusterName, conditionUpdateFn)
	if err != nil {
		return fmt.Errorf("unable to update status of managed cluster %q: %w", c.clusterName, err)
	}
	if updated {
		syncCtx.Recorder().Eventf("ManagedClusterAvailableConditionUpdated", "update managed cluster %q available condition to %q, due to %q",
			c.clusterName, condition.Status, condition.Message)
	}
	return nil
}

// using readyz api to check the status of kube apiserver
func (c *managedClusterHealthCheckController) checkKubeAPIServerStatus(ctx context.Context) clusterv1.StatusCondition {
	statusCode := 0
	condition := clusterv1.StatusCondition{Type: clusterv1.ManagedClusterConditionAvailable}
	result := c.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/readyz").Do(ctx).StatusCode(&statusCode)
	if statusCode == http.StatusOK {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ManagedClusterAvailable"
		condition.Message = "Managed cluster is available"
		return condition
	}

	// for backward compatible, the readyz endpoint is supported from Kubernetes 1.16, so if the readyz is not found or
	// forbidden, the healthz endpoint will be used.
	if statusCode == http.StatusNotFound || statusCode == http.StatusForbidden {
		result = c.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/healthz").Do(ctx).StatusCode(&statusCode)
		if statusCode == http.StatusOK {
			condition.Status = metav1.ConditionTrue
			condition.Reason = "ManagedClusterAvailable"
			condition.Message = "Managed cluster is available"
			return condition
		}
	}

	condition.Status = metav1.ConditionFalse
	condition.Reason = "ManagedClusterKubeAPIServerUnavailable"
	body, err := result.Raw()
	if err == nil {
		condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, string(body))
		return condition
	}

	condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, err)
	return condition
}
