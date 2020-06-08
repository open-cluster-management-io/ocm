package clustermanager

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	operatorv1client "github.com/open-cluster-management/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "github.com/open-cluster-management/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "github.com/open-cluster-management/api/client/operator/listers/operator/v1"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
	"github.com/open-cluster-management/registration-operator/pkg/operators/clustermanager/bindata"
)

var (
	crdNames = []string{
		"manifestworks.work.open-cluster-management.io",
		"managedclusters.cluster.open-cluster-management.io",
	}
	staticResourceFiles = []string{
		"manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
		"manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml",
		"manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml",
		"manifests/cluster-manager/cluster-manager-namespace.yaml",
		"manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-secret.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml",
	}

	deploymentFiles = []string{
		"manifests/cluster-manager/cluster-manager-registration-deployment.yaml",
		"manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml",
	}
)

const (
	clusterManagerFinalizer     = "operator.open-cluster-management.io/cluster-manager-cleanup"
	clusterManagerNamespace     = "open-cluster-management-hub"
	clusterManagerWebhookSecret = "webhook-serving-cert"
	clusterManagerApplied       = "Applied"
	clusterManagerAvailable     = "Available"
)

type clusterManagerController struct {
	clusterManagerClient  operatorv1client.ClusterManagerInterface
	clusterManagerLister  operatorlister.ClusterManagerLister
	kubeClient            kubernetes.Interface
	apiExtensionClient    apiextensionsclient.Interface
	apiRegistrationClient apiregistrationclient.APIServicesGetter
	currentGeneration     []int64
}

// NewClusterManagerController construct cluster manager hub controller
func NewClusterManagerController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	apiRegistrationClient apiregistrationclient.APIServicesGetter,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	recorder events.Recorder) factory.Controller {
	controller := &clusterManagerController{
		kubeClient:            kubeClient,
		apiExtensionClient:    apiExtensionClient,
		apiRegistrationClient: apiRegistrationClient,
		clusterManagerClient:  clusterManagerClient,
		clusterManagerLister:  clusterManagerInformer.Lister(),
		currentGeneration:     make([]int64, len(deploymentFiles)),
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterManagerInformer.Informer()).
		ToController("ClusterManagerController", recorder)
}

// hubConfig is used to render the template of hub manifests
type hubConfig struct {
	ClusterManagerName                       string
	ClusterManagerNamespace                  string
	RegistrationImage                        string
	ClusterManagerWebhookSecret              string
	ClusterManagerWebhookRegistrationService string
	RegistrationAPIServiceCABundle           string
	RegistrationServingCert                  string
	RegistrationServingKey                   string
}

func (n *clusterManagerController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	clusterManager, err := n.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		// ClusterManager not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	clusterManager = clusterManager.DeepCopy()

	config := hubConfig{
		ClusterManagerName:                       clusterManager.Name,
		ClusterManagerNamespace:                  clusterManagerNamespace,
		RegistrationImage:                        clusterManager.Spec.RegistrationImagePullSpec,
		ClusterManagerWebhookSecret:              clusterManagerWebhookSecret,
		ClusterManagerWebhookRegistrationService: fmt.Sprintf("%s-registration-webhook", clusterManager.Name),
	}

	// Update finalizer at first
	if clusterManager.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range clusterManager.Finalizers {
			if clusterManager.Finalizers[i] == clusterManagerFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			clusterManager.Finalizers = append(clusterManager.Finalizers, clusterManagerFinalizer)
			_, err := n.clusterManagerClient.Update(ctx, clusterManager, metav1.UpdateOptions{})
			return err
		}
	}

	// ClusterManager is deleting, we remove its related resources on hub
	if !clusterManager.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, config); err != nil {
			return err
		}
		return n.removeClusterManagerFinalizer(ctx, clusterManager)
	}

	ca, cert, key, err := n.ensureServingCertAndCA(
		ctx, config.ClusterManagerNamespace, config.ClusterManagerWebhookSecret, config.ClusterManagerWebhookRegistrationService)
	if err != nil {
		return err
	}
	config.RegistrationAPIServiceCABundle = base64.StdEncoding.EncodeToString(ca)
	config.RegistrationServingCert = base64.StdEncoding.EncodeToString(cert)
	config.RegistrationServingKey = base64.StdEncoding.EncodeToString(key)

	// Apply static files
	resourceResults := helpers.ApplyDirectly(
		n.kubeClient,
		n.apiExtensionClient,
		n.apiRegistrationClient,
		controllerContext.Recorder(),
		func(name string) ([]byte, error) {
			return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join("", name)), config).Data, nil
		},
		staticResourceFiles...,
	)
	errs := []error{}
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// Render deployment manifest and apply
	for index, file := range deploymentFiles {
		currentGeneration, err := helpers.ApplyDeployment(
			n.kubeClient,
			n.currentGeneration[index],
			func(name string) ([]byte, error) {
				return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join("", name)), config).Data, nil
			},
			controllerContext.Recorder(),
			file)
		if err != nil {
			errs = append(errs, err)
		}
		n.currentGeneration[index] = currentGeneration
	}

	conditions := &clusterManager.Status.Conditions
	if len(errs) == 0 {
		helpers.SetOperatorCondition(conditions, operatorapiv1.StatusCondition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "ClusterManagerApplied",
			Message: "Components of cluster manager is applied",
		})
	} else {
		helpers.SetOperatorCondition(conditions, operatorapiv1.StatusCondition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterManagerApplyFailed",
			Message: "Components of cluster manager fail to be applied",
		})
	}

	//TODO Check if all the pods are running.
	// Update status
	_, _, updatedErr := helpers.UpdateClusterManagerStatus(
		ctx, n.clusterManagerClient, clusterManager.Name, helpers.UpdateClusterManagerConditionFn(*conditions...))
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (n *clusterManagerController) removeClusterManagerFinalizer(ctx context.Context, deploy *operatorapiv1.ClusterManager) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == clusterManagerFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}

	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := n.clusterManagerClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// removeCRD removes crd, and check if crd resource is removed. Since the related cr is still being deleted,
// it will check the crd existence after deletion, and only return nil when crd is not found.
func (n *clusterManagerController) removeCRD(ctx context.Context, name string) error {
	err := n.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Delete(
		ctx, name, metav1.DeleteOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	_, err = n.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	return fmt.Errorf("CRD %s is still being deleted", name)
}

// ensureServingCertAndCA generates self signed CA and server key/cert for webhook server.
// TODO consider ca/cert renewal
func (n *clusterManagerController) ensureServingCertAndCA(
	ctx context.Context, namespace, secretName, svcName string) ([]byte, []byte, []byte, error) {
	secret, err := n.kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	switch {
	case err != nil && !errors.IsNotFound(err):
		return nil, nil, nil, err
	case err == nil:
		if secret.Data["ca.crt"] != nil && secret.Data["tls.crt"] != nil && secret.Data["tls.key"] != nil {
			return secret.Data["ca.crt"], secret.Data["tls.crt"], secret.Data["tls.key"], nil
		}
	}

	caConfig, err := crypto.MakeSelfSignedCAConfig("cluster-manager-webhook", 365)
	if err != nil {
		return nil, nil, nil, err
	}

	ca := &crypto.CA{
		SerialGenerator: &crypto.RandomSerialGenerator{},
		Config:          caConfig,
	}

	hostName := fmt.Sprintf("%s.%s.svc", svcName, namespace)
	server, err := ca.MakeServerCert(sets.NewString(hostName), 365)
	if err != nil {
		return nil, nil, nil, err
	}

	caData, _, err := caConfig.GetPEMBytes()
	if err != nil {
		return nil, nil, nil, err
	}
	certData, keyData, err := server.GetPEMBytes()
	if err != nil {
		return nil, nil, nil, err
	}

	return caData, certData, keyData, nil
}

func (n *clusterManagerController) cleanUp(
	ctx context.Context, controllerContext factory.SyncContext, config hubConfig) error {
	// Remove crd
	for _, name := range crdNames {
		err := n.removeCRD(ctx, name)
		if err != nil {
			return err
		}
		controllerContext.Recorder().Eventf("CRDDeleted", "crd %s is deleted", name)
	}

	// Remove Static files
	for _, file := range staticResourceFiles {
		err := helpers.CleanUpStaticObject(
			ctx,
			n.kubeClient,
			n.apiExtensionClient,
			n.apiRegistrationClient,
			func(name string) ([]byte, error) {
				return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join("", name)), config).Data, nil
			},
			file,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
