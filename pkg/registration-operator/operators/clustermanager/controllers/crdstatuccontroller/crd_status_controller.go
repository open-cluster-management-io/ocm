/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package crdstatuccontroller

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/ocm/pkg/registration-operator/helpers"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/migrationcontroller"
)

var (

	//  CRD StoredVersions
	desiredCRDStoredVersions = map[string][]string{
		"managedclustersets.cluster.open-cluster-management.io":        {"v1beta2"},
		"managedclustersetbindings.cluster.open-cluster-management.io": {"v1beta2"},
	}
)

type crdStatusController struct {
	kubeconfig                *rest.Config
	kubeClient                kubernetes.Interface
	clusterManagerLister      operatorlister.ClusterManagerLister
	generateHubClusterClients func(hubConfig *rest.Config) (apiextensionsclient.Interface, error)
}

// NewClusterManagerController construct cluster manager hub controller
func NewCRDStatusController(
	kubeconfig *rest.Config,
	kubeClient kubernetes.Interface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	recorder events.Recorder) factory.Controller {
	controller := &crdStatusController{
		kubeconfig:                kubeconfig,
		kubeClient:                kubeClient,
		clusterManagerLister:      clusterManagerInformer.Lister(),
		generateHubClusterClients: generateHubClients,
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterManagerInformer.Informer()).
		ToController("CRDStatusController", recorder)
}

func (c *crdStatusController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	clusterManager, err := c.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// ClusterManager is deleting
	if !clusterManager.DeletionTimestamp.IsZero() {
		return nil
	}

	// need to wait storage version migrations succeed.
	if succeeded := meta.IsStatusConditionTrue(clusterManager.Status.Conditions, migrationcontroller.MigrationSucceeded); !succeeded {
		controllerContext.Queue().AddRateLimited(clusterManagerName)
		klog.V(4).Info("Wait storage version migration succeed.")
		return nil
	}

	// If mode is default, then config is management kubeconfig, else it would use management kubeconfig to find the hub
	hubKubeconfig, err := helpers.GetHubKubeconfig(ctx, c.kubeconfig, c.kubeClient, clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
	if err != nil {
		return err
	}

	apiExtensionClient, err := c.generateHubClusterClients(hubKubeconfig)
	if err != nil {
		return err
	}

	err = updateStoredVersion(ctx, apiExtensionClient)
	if err != nil {
		return err
	}
	return nil
}

// updateStoredVersion update(remove) deleted api version from CRD status.StoredVersions
func updateStoredVersion(ctx context.Context, hubAPIExtensionClient apiextensionsclient.Interface) error {
	for name, desiredVersions := range desiredCRDStoredVersions {
		// retrieve CRD
		crd, err := hubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(crd.Status.StoredVersions, desiredVersions) {
			crd.Status.StoredVersions = desiredVersions
			// update the status sub-resource
			klog.V(4).Infof("Need update stored versions. oldStoredVersions: %v, newStoredVersions: %v", crd.Status.StoredVersions, desiredVersions)
			crd, err = hubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().UpdateStatus(ctx, crd, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("Failed to update storedversion:%v", err)
				return err
			}
			klog.V(4).Infof("updated CRD %v status storedVersions: %v", crd.Name, crd.Status.StoredVersions)
		}
	}

	return nil
}

func generateHubClients(hubKubeConfig *rest.Config) (apiextensionsclient.Interface, error) {
	hubApiExtensionClient, err := apiextensionsclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, err
	}

	return hubApiExtensionClient, nil
}
