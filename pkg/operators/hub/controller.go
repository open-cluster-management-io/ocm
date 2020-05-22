package hub

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

	nucleusv1client "github.com/open-cluster-management/api/client/nucleus/clientset/versioned/typed/nucleus/v1"
	nucleusinformer "github.com/open-cluster-management/api/client/nucleus/informers/externalversions/nucleus/v1"
	nucleuslister "github.com/open-cluster-management/api/client/nucleus/listers/nucleus/v1"
	nucleusapiv1 "github.com/open-cluster-management/api/nucleus/v1"
	"github.com/open-cluster-management/nucleus/pkg/helpers"
	"github.com/open-cluster-management/nucleus/pkg/operators/hub/bindata"
)

var (
	crdNames = []string{
		"manifestworks.work.open-cluster-management.io",
		"spokeclusters.cluster.open-cluster-management.io",
	}
	staticResourceFiles = []string{
		"manifests/hub/0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml",
		"manifests/hub/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"manifests/hub/hub-registration-clusterrole.yaml",
		"manifests/hub/hub-registration-clusterrolebinding.yaml",
		"manifests/hub/hub-namespace.yaml",
		"manifests/hub/hub-registration-serviceaccount.yaml",
		"manifests/hub/hub-registration-webhook-clusterrole.yaml",
		"manifests/hub/hub-registration-webhook-clusterrolebinding.yaml",
		"manifests/hub/hub-registration-webhook-service.yaml",
		"manifests/hub/hub-registration-webhook-serviceaccount.yaml",
		"manifests/hub/hub-registration-webhook-apiservice.yaml",
		"manifests/hub/hub-registration-webhook-secret.yaml",
		"manifests/hub/hub-registration-webhook-validatingconfiguration.yaml",
	}

	deploymentFiles = []string{
		"manifests/hub/hub-registration-deployment.yaml",
		"manifests/hub/hub-registration-webhook-deployment.yaml",
	}
)

const (
	nucleusHubFinalizer         = "nucleus.open-cluster-management.io/hub-core-cleanup"
	nucleusHubCoreNamespace     = "open-cluster-management-hub"
	nucleusHubCoreWebhookSecret = "webhook-serving-cert"
	hubCoreApplied              = "Applied"
	hubCoreAvailable            = "Available"
)

type nucleusHubController struct {
	nucleusClient         nucleusv1client.HubCoreInterface
	nucleusLister         nucleuslister.HubCoreLister
	kubeClient            kubernetes.Interface
	apiExtensionClient    apiextensionsclient.Interface
	apiRegistrationClient apiregistrationclient.APIServicesGetter
	currentGeneration     []int64
}

// NewNucleusHubController construct nucleus hub controller
func NewNucleusHubController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	apiRegistrationClient apiregistrationclient.APIServicesGetter,
	nucleusClient nucleusv1client.HubCoreInterface,
	nucleusInformer nucleusinformer.HubCoreInformer,
	recorder events.Recorder) factory.Controller {
	controller := &nucleusHubController{
		kubeClient:            kubeClient,
		apiExtensionClient:    apiExtensionClient,
		apiRegistrationClient: apiRegistrationClient,
		nucleusClient:         nucleusClient,
		nucleusLister:         nucleusInformer.Lister(),
		currentGeneration:     make([]int64, len(deploymentFiles)),
	}

	return factory.New().WithSync(controller.sync).
		ResyncEvery(3*time.Minute).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, nucleusInformer.Informer()).
		ToController("NucleusHubController", recorder)
}

// hubConfig is used to render the template of hub manifests
type hubConfig struct {
	HubCoreName                       string
	HubCoreNamespace                  string
	RegistrationImage                 string
	HubCoreWebhookSecret              string
	HubCoreWebhookRegistrationService string
	RegistrationAPIServiceCABundle    string
	RegistrationServingCert           string
	RegistrationServingKey            string
}

func (n *nucleusHubController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	hubCoreName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling HubCore %q", hubCoreName)

	hubCore, err := n.nucleusLister.Get(hubCoreName)
	if errors.IsNotFound(err) {
		// HubCore not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	hubCore = hubCore.DeepCopy()

	config := hubConfig{
		HubCoreName:                       hubCore.Name,
		HubCoreNamespace:                  nucleusHubCoreNamespace,
		RegistrationImage:                 hubCore.Spec.RegistrationImagePullSpec,
		HubCoreWebhookSecret:              nucleusHubCoreWebhookSecret,
		HubCoreWebhookRegistrationService: fmt.Sprintf("%s-registration-webhook", hubCore.Name),
	}

	// Update finalizer at first
	if hubCore.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range hubCore.Finalizers {
			if hubCore.Finalizers[i] == nucleusHubFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			hubCore.Finalizers = append(hubCore.Finalizers, nucleusHubFinalizer)
			_, err := n.nucleusClient.Update(ctx, hubCore, metav1.UpdateOptions{})
			return err
		}
	}

	// HubCore is deleting, we remove its related resources on hub
	if !hubCore.DeletionTimestamp.IsZero() {
		if err := n.cleanUp(ctx, controllerContext, config); err != nil {
			return err
		}
		return n.removeWorkFinalizer(ctx, hubCore)
	}

	ca, cert, key, err := n.ensureServingCertAndCA(
		ctx, config.HubCoreNamespace, config.HubCoreWebhookSecret, config.HubCoreWebhookRegistrationService)
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

	conditions := &hubCore.Status.Conditions
	if len(errs) == 0 {
		helpers.SetNucleusCondition(conditions, nucleusapiv1.StatusCondition{
			Type:    hubCoreApplied,
			Status:  metav1.ConditionTrue,
			Reason:  "HubCoreApplied",
			Message: "Components of hub core is applied",
		})
	} else {
		helpers.SetNucleusCondition(conditions, nucleusapiv1.StatusCondition{
			Type:    hubCoreApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "HubCoreApplyFailed",
			Message: "Components of hub core fail to be applied",
		})
	}

	//TODO Check if all the pods are running.
	// Update status
	_, _, updatedErr := helpers.UpdateNucleusHubStatus(
		ctx, n.nucleusClient, hubCore.Name, helpers.UpdateNucleusHubConditionFn(*conditions...))
	if updatedErr != nil {
		errs = append(errs, updatedErr)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

func (n *nucleusHubController) removeWorkFinalizer(ctx context.Context, deploy *nucleusapiv1.HubCore) error {
	copiedFinalizers := []string{}
	for i := range deploy.Finalizers {
		if deploy.Finalizers[i] == nucleusHubFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, deploy.Finalizers[i])
	}

	if len(deploy.Finalizers) != len(copiedFinalizers) {
		deploy.Finalizers = copiedFinalizers
		_, err := n.nucleusClient.Update(ctx, deploy, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// removeCRD removes crd, and check if crd resource is removed. Since the related cr is still being deleted,
// it will check the crd existence after deletion, and only return nil when crd is not found.
func (n *nucleusHubController) removeCRD(ctx context.Context, name string) error {
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
func (n *nucleusHubController) ensureServingCertAndCA(
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

	caConfig, err := crypto.MakeSelfSignedCAConfig("nucleus-webhook", 365)
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

func (n *nucleusHubController) cleanUp(
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
