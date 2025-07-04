package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	admissionclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

const (
	defaultReplica = 3
	singleReplica  = 1

	FeatureGatesTypeValid             = "ValidFeatureGates"
	FeatureGatesReasonAllValid        = "FeatureGatesAllValid"
	FeatureGatesReasonInvalidExisting = "InvalidFeatureGatesExisting"

	// DefaultAddonNamespace is the default namespace for agent addon
	DefaultAddonNamespace = "open-cluster-management-agent-addon"

	// The labels with LabelPrefix are reserved, and will not be synced to the resources created by the operators.
	LabelPrefix = "open-cluster-management.io"

	// HubLabelKey is used to filter resources in informers
	HubLabelKey = LabelPrefix + "/created-by-clustermanager"

	// AgentLabelKey is used to filter resources in informers
	AgentLabelKey = LabelPrefix + "/created-by-klusterlet"

	// AppLabelKey is the label key for all deployments
	AppLabelKey = "app"
)

const (
	// ImagePullSecret is the image pull secret for operator components, which is synced from the operator ns to hub/spoke/addon ns.
	ImagePullSecret = "open-cluster-management-image-pull-credentials"

	// WorkDriverConfigSecret is the secret that contains the work driver configuration
	WorkDriverConfigSecret = "work-driver-config"

	// DefaultComponentNamespace is the default namespace in which the operator is deployed
	DefaultComponentNamespace = "open-cluster-management"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
	utilruntime.Must(apiextensionsv1beta1.AddToScheme(genericScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
	utilruntime.Must(apiregistrationv1.AddToScheme(genericScheme))
	utilruntime.Must(admissionv1.AddToScheme(genericScheme))
}

func CleanUpStaticObject(
	ctx context.Context,
	client kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	apiRegistrationClient apiregistrationclient.APIServicesGetter,
	manifests resourceapply.AssetFunc,
	file string) error {
	objectRaw, err := manifests(file)
	if err != nil {
		return err
	}
	object, _, err := genericCodec.Decode(objectRaw, nil, nil)
	if err != nil {
		return err
	}
	switch t := object.(type) {
	case *corev1.Namespace:
		err = client.CoreV1().Namespaces().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *appsv1.Deployment:
		err = client.AppsV1().Deployments(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.Endpoints:
		err = client.CoreV1().Endpoints(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.Service:
		err = client.CoreV1().Services(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.ServiceAccount:
		err = client.CoreV1().ServiceAccounts(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.ConfigMap:
		err = client.CoreV1().ConfigMaps(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *corev1.Secret:
		err = client.CoreV1().Secrets(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.ClusterRole:
		err = client.RbacV1().ClusterRoles().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.ClusterRoleBinding:
		err = client.RbacV1().ClusterRoleBindings().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.Role:
		err = client.RbacV1().Roles(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *rbacv1.RoleBinding:
		err = client.RbacV1().RoleBindings(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *apiextensionsv1.CustomResourceDefinition:
		if apiExtensionClient == nil {
			err = fmt.Errorf("apiExtensionClient is nil")
		} else {
			err = apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, t.Name, metav1.DeleteOptions{})
		}
	case *apiextensionsv1beta1.CustomResourceDefinition:
		if apiExtensionClient == nil {
			err = fmt.Errorf("apiExtensionClient is nil")
		} else {
			err = apiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(ctx, t.Name, metav1.DeleteOptions{})
		}
	case *apiregistrationv1.APIService:
		if apiRegistrationClient == nil {
			err = fmt.Errorf("apiRegistrationClient is nil")
		} else {
			err = apiRegistrationClient.APIServices().Delete(ctx, t.Name, metav1.DeleteOptions{})
		}
	case *admissionv1.ValidatingWebhookConfiguration:
		err = client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, t.Name, metav1.DeleteOptions{})
	case *admissionv1.MutatingWebhookConfiguration:
		err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, t.Name, metav1.DeleteOptions{})
	default:
		err = fmt.Errorf("unhandled type %T", object)
	}
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

func ApplyValidatingWebhookConfiguration(
	client admissionclient.ValidatingWebhookConfigurationsGetter,
	required *admissionv1.ValidatingWebhookConfiguration) (*admissionv1.ValidatingWebhookConfiguration, bool, error) {
	existing, err := client.ValidatingWebhookConfigurations().Get(context.TODO(), required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := client.ValidatingWebhookConfigurations().Create(context.TODO(), required, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	if !equality.Semantic.DeepEqual(existingCopy.Webhooks, required.Webhooks) {
		*modified = true
		existing.Webhooks = required.Webhooks
	}
	if !*modified {
		return existing, false, nil
	}
	actual, err := client.ValidatingWebhookConfigurations().Update(context.TODO(), existing, metav1.UpdateOptions{})
	return actual, true, err
}

func ApplyMutatingWebhookConfiguration(
	client admissionclient.MutatingWebhookConfigurationsGetter,
	required *admissionv1.MutatingWebhookConfiguration) (*admissionv1.MutatingWebhookConfiguration, bool, error) {
	existing, err := client.MutatingWebhookConfigurations().Get(context.TODO(), required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := client.MutatingWebhookConfigurations().Create(context.TODO(), required, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	if !equality.Semantic.DeepEqual(existingCopy.Webhooks, required.Webhooks) {
		*modified = true
		existing.Webhooks = required.Webhooks
	}
	if !*modified {
		return existing, false, nil
	}

	actual, err := client.MutatingWebhookConfigurations().Update(context.TODO(), existing, metav1.UpdateOptions{})
	return actual, true, err
}

func ApplyDeployment(
	ctx context.Context,
	client kubernetes.Interface,
	generationStatuses []operatorapiv1.GenerationStatus,
	nodePlacement operatorapiv1.NodePlacement,
	manifests resourceapply.AssetFunc,
	recorder events.Recorder, file string) (*appsv1.Deployment, operatorapiv1.GenerationStatus, error) {
	deploymentBytes, err := manifests(file)
	if err != nil {
		return nil, operatorapiv1.GenerationStatus{}, err
	}
	deployment, _, err := genericCodec.Decode(deploymentBytes, nil, nil)
	if err != nil {
		return nil, operatorapiv1.GenerationStatus{}, fmt.Errorf("%q: %v", file, err)
	}
	generationStatus := NewGenerationStatus(appsv1.SchemeGroupVersion.WithResource("deployments"), deployment)
	currentGenerationStatus := FindGenerationStatus(generationStatuses, generationStatus)

	if currentGenerationStatus != nil {
		generationStatus.LastGeneration = currentGenerationStatus.LastGeneration
	}

	deployment.(*appsv1.Deployment).Spec.Template.Spec.NodeSelector = nodePlacement.NodeSelector
	deployment.(*appsv1.Deployment).Spec.Template.Spec.Tolerations = nodePlacement.Tolerations

	updatedDeployment, updated, err := resourceapply.ApplyDeployment(
		ctx,
		client.AppsV1(),
		recorder,
		deployment.(*appsv1.Deployment), generationStatus.LastGeneration)
	if err != nil {
		return updatedDeployment, generationStatus, fmt.Errorf("%q (%T): %v", file, deployment, err)
	}

	if updated {
		generationStatus.LastGeneration = updatedDeployment.ObjectMeta.Generation
	}

	return updatedDeployment, generationStatus, nil
}

func ApplyEndpoints(ctx context.Context, client coreclientv1.EndpointsGetter, required *corev1.Endpoints) (*corev1.Endpoints, bool, error) {
	existing, err := client.Endpoints(required.Namespace).Get(ctx, required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Endpoints(required.Namespace).Create(ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)

	if !*modified && equality.Semantic.DeepEqual(existingCopy.Subsets, required.Subsets) {
		return existingCopy, false, nil
	}

	existingCopy.Subsets = required.Subsets
	actual, err := client.Endpoints(required.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})
	return actual, true, err
}

func ApplyDirectly(
	ctx context.Context,
	client kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	recorder events.Recorder,
	cache resourceapply.ResourceCache,
	manifests resourceapply.AssetFunc,
	files ...string) []resourceapply.ApplyResult {
	var ret []resourceapply.ApplyResult

	var genericApplyFiles []string
	for _, file := range files {
		result := resourceapply.ApplyResult{File: file}
		objBytes, err := manifests(file)
		if err != nil {
			result.Error = fmt.Errorf("missing %q: %v", file, err)
			ret = append(ret, result)
			continue
		}

		requiredObj, _, err := genericCodec.Decode(objBytes, nil, nil)
		if err != nil {
			result.Error = fmt.Errorf("cannot decode %q: %v", file, err)
			ret = append(ret, result)
			continue
		}

		// Special treatment on webhook, apiservices, and endpoints.
		result.Type = fmt.Sprintf("%T", requiredObj)
		switch t := requiredObj.(type) {
		case *admissionv1.ValidatingWebhookConfiguration:
			result.Result, result.Changed, result.Error = ApplyValidatingWebhookConfiguration(
				client.AdmissionregistrationV1(), t)
		case *admissionv1.MutatingWebhookConfiguration:
			result.Result, result.Changed, result.Error = ApplyMutatingWebhookConfiguration(
				client.AdmissionregistrationV1(), t)
		case *corev1.Endpoints:
			result.Result, result.Changed, result.Error = ApplyEndpoints(context.TODO(), client.CoreV1(), t)
		default:
			genericApplyFiles = append(genericApplyFiles, file)
		}

		ret = append(ret, result)
	}
	clientHolder := resourceapply.NewKubeClientHolder(client).WithAPIExtensionsClient(apiExtensionClient)
	applyResults := resourceapply.ApplyDirectly(
		ctx,
		clientHolder,
		recorder,
		cache,
		manifests,
		genericApplyFiles...,
	)

	ret = append(ret, applyResults...)
	return ret
}

// NumOfUnavailablePod is to check if a deployment is in degraded state.
func NumOfUnavailablePod(deployment *appsv1.Deployment) int32 {
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *(deployment.Spec.Replicas)
	}

	if desiredReplicas <= deployment.Status.AvailableReplicas {
		return 0
	}

	return desiredReplicas - deployment.Status.AvailableReplicas
}

func NewGenerationStatus(gvr schema.GroupVersionResource, object runtime.Object) operatorapiv1.GenerationStatus {
	accessor, _ := meta.Accessor(object)
	return operatorapiv1.GenerationStatus{
		Group:          gvr.Group,
		Version:        gvr.Version,
		Resource:       gvr.Resource,
		Namespace:      accessor.GetNamespace(),
		Name:           accessor.GetName(),
		LastGeneration: accessor.GetGeneration(),
	}
}

func FindGenerationStatus(generationStatuses []operatorapiv1.GenerationStatus, generation operatorapiv1.GenerationStatus) *operatorapiv1.GenerationStatus {
	for i := range generationStatuses {
		if generationStatuses[i].Group != generation.Group {
			continue
		}
		if generationStatuses[i].Resource != generation.Resource {
			continue
		}
		if generationStatuses[i].Version != generation.Version {
			continue
		}
		if generationStatuses[i].Name != generation.Name {
			continue
		}
		if generationStatuses[i].Namespace != generation.Namespace {
			continue
		}
		return &generationStatuses[i]
	}
	return nil
}

func SetGenerationStatuses(generationStatuses *[]operatorapiv1.GenerationStatus, newGenerationStatus operatorapiv1.GenerationStatus) {
	if generationStatuses == nil {
		generationStatuses = &[]operatorapiv1.GenerationStatus{}
	}

	existingGeneration := FindGenerationStatus(*generationStatuses, newGenerationStatus)
	if existingGeneration == nil {
		*generationStatuses = append(*generationStatuses, newGenerationStatus)
		return
	}

	existingGeneration.LastGeneration = newGenerationStatus.LastGeneration
}

// LoadClientConfigFromSecret returns a client config loaded from the given secret
func LoadClientConfigFromSecret(secret *corev1.Secret) (*rest.Config, error) {
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("unable to find kubeconfig in secret %q %q",
			secret.Namespace, secret.Name)
	}

	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, err
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("unable to find the current context %q from the kubeconfig in secret %q %q",
			config.CurrentContext, secret.Namespace, secret.Name)
	}

	if authInfo, ok := config.AuthInfos[context.AuthInfo]; ok {
		// use embedded cert/key data instead of references to external cert/key files
		if certData, ok := secret.Data["tls.crt"]; ok && len(authInfo.ClientCertificateData) == 0 {
			authInfo.ClientCertificateData = certData
			authInfo.ClientCertificate = ""
		}
		if keyData, ok := secret.Data["tls.key"]; ok && len(authInfo.ClientKeyData) == 0 {
			authInfo.ClientKeyData = keyData
			authInfo.ClientKey = ""
		}
	}

	return clientcmd.NewDefaultClientConfig(*config, nil).ClientConfig()
}

// DetermineReplica determines the replica of deployment based on:
// - mode: if it is Hosted mode will return 1
// - kube version: if the kube version is less than v1.14 reutn 1
// - node: list master nodes in the cluster and return 1 if the
// number of master nodes is equal or less than 1. Return 3 otherwise.
func DetermineReplica(ctx context.Context, kubeClient kubernetes.Interface, mode operatorapiv1.InstallMode, kubeVersion *version.Version,
	controlPlaneNodeLabelSelector string) int32 {
	// For hosted mode, there may be many cluster-manager/klusterlet running on the management cluster,
	// set the replica to 1 to reduce the footprint of the management cluster.
	if IsHosted(mode) {
		return singleReplica
	}

	if kubeVersion != nil {
		// If the cluster does not support lease.coordination.k8s.io/v1, set the replica to 1.
		// And then the leader election of agent running on this cluster should be disabled, because
		// it leverages the lease API. Kubernetes starts support lease/v1 from v1.14.
		if cnt, err := kubeVersion.Compare("v1.14.0"); err != nil {
			klog.Warningf("set replica to %d because it's failed to check whether the cluster supports lease/v1 or not: %v", singleReplica, err)
			return singleReplica
		} else if cnt == -1 {
			return singleReplica
		}
	}

	return DetermineReplicaByNodes(ctx, kubeClient, controlPlaneNodeLabelSelector)
}

// DetermineReplicaByNodes determines the replica of deployment based on:
// list master nodes in the cluster and return 1 if
// the number of master nodes is equal or less than 1. Return 3 otherwise.
func DetermineReplicaByNodes(ctx context.Context, kubeClient kubernetes.Interface, controlPlaneNodeLabelSelector string) int32 {
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: controlPlaneNodeLabelSelector})
	if err != nil {
		return defaultReplica
	}

	if len(nodes.Items) <= 1 {
		return singleReplica
	}

	return defaultReplica
}

func GenerateRelatedResource(objBytes []byte) (operatorapiv1.RelatedResourceMeta, error) {
	var relatedResource operatorapiv1.RelatedResourceMeta
	requiredObj, _, err := genericCodec.Decode(objBytes, nil, nil)
	if err != nil {
		return relatedResource, err
	}

	switch requiredObj.(type) {
	case *admissionv1.ValidatingWebhookConfiguration:
		relatedResource = newRelatedResource(admissionv1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations"), requiredObj)
	case *admissionv1.MutatingWebhookConfiguration:
		relatedResource = newRelatedResource(admissionv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations"), requiredObj)
	case *apiregistrationv1.APIService:
		relatedResource = newRelatedResource(apiregistrationv1.SchemeGroupVersion.WithResource("apiservices"), requiredObj)
	case *appsv1.Deployment:
		relatedResource = newRelatedResource(appsv1.SchemeGroupVersion.WithResource("deployments"), requiredObj)
	case *corev1.Namespace:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("namespaces"), requiredObj)
	case *corev1.Endpoints:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("endpoints"), requiredObj)
	case *corev1.Service:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("services"), requiredObj)
	case *corev1.Pod:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("pods"), requiredObj)
	case *corev1.ServiceAccount:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("serviceaccounts"), requiredObj)
	case *corev1.ConfigMap:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("configmaps"), requiredObj)
	case *corev1.Secret:
		relatedResource = newRelatedResource(corev1.SchemeGroupVersion.WithResource("secrets"), requiredObj)
	case *rbacv1.ClusterRole:
		relatedResource = newRelatedResource(rbacv1.SchemeGroupVersion.WithResource("clusterroles"), requiredObj)
	case *rbacv1.ClusterRoleBinding:
		relatedResource = newRelatedResource(rbacv1.SchemeGroupVersion.WithResource("clusterrolebindings"), requiredObj)
	case *rbacv1.Role:
		relatedResource = newRelatedResource(rbacv1.SchemeGroupVersion.WithResource("roles"), requiredObj)
	case *rbacv1.RoleBinding:
		relatedResource = newRelatedResource(rbacv1.SchemeGroupVersion.WithResource("rolebindings"), requiredObj)
	case *apiextensionsv1beta1.CustomResourceDefinition:
		relatedResource = newRelatedResource(apiextensionsv1beta1.SchemeGroupVersion.WithResource("customresourcedefinitions"), requiredObj)
	case *apiextensionsv1.CustomResourceDefinition:
		relatedResource = newRelatedResource(apiextensionsv1.SchemeGroupVersion.WithResource("customresourcedefinitions"), requiredObj)
	default:
		return relatedResource, fmt.Errorf("unhandled type %T", requiredObj)
	}

	return relatedResource, nil
}

func newRelatedResource(gvr schema.GroupVersionResource, obj runtime.Object) operatorapiv1.RelatedResourceMeta {
	accessor, _ := meta.Accessor(obj)
	return operatorapiv1.RelatedResourceMeta{
		Group:     gvr.Group,
		Version:   gvr.Version,
		Resource:  gvr.Resource,
		Namespace: accessor.GetNamespace(),
		Name:      accessor.GetName(),
	}
}

func SetRelatedResourcesStatuses(
	relatedResourcesStatuses *[]operatorapiv1.RelatedResourceMeta,
	newRelatedResourcesStatus operatorapiv1.RelatedResourceMeta) {
	if relatedResourcesStatuses == nil {
		relatedResourcesStatuses = &[]operatorapiv1.RelatedResourceMeta{}
	}

	existingRelatedResource := FindRelatedResourcesStatus(*relatedResourcesStatuses, newRelatedResourcesStatus)
	if existingRelatedResource == nil {
		*relatedResourcesStatuses = append(*relatedResourcesStatuses, newRelatedResourcesStatus)
		return
	}
}

func RemoveRelatedResourcesStatuses(
	relatedResourcesStatuses *[]operatorapiv1.RelatedResourceMeta,
	rmRelatedResourcesStatus operatorapiv1.RelatedResourceMeta) {
	if relatedResourcesStatuses == nil {
		return
	}

	existingRelatedResource := FindRelatedResourcesStatus(*relatedResourcesStatuses, rmRelatedResourcesStatus)
	if existingRelatedResource != nil {
		RemoveRelatedResourcesStatus(relatedResourcesStatuses, rmRelatedResourcesStatus)
		return
	}
}

func FindRelatedResourcesStatus(
	relatedResourcesStatuses []operatorapiv1.RelatedResourceMeta,
	relatedResource operatorapiv1.RelatedResourceMeta) *operatorapiv1.RelatedResourceMeta {
	for i := range relatedResourcesStatuses {
		if reflect.DeepEqual(relatedResourcesStatuses[i], relatedResource) {
			return &relatedResourcesStatuses[i]
		}
	}
	return nil
}

func RemoveRelatedResourcesStatus(
	relatedResourcesStatuses *[]operatorapiv1.RelatedResourceMeta,
	relatedResource operatorapiv1.RelatedResourceMeta) {
	var result []operatorapiv1.RelatedResourceMeta
	for _, v := range *relatedResourcesStatuses {
		if v != relatedResource {
			result = append(result, v)
		}
	}
	*relatedResourcesStatuses = result
}

func SetRelatedResourcesStatusesWithObj(
	relatedResourcesStatuses *[]operatorapiv1.RelatedResourceMeta, objData []byte) {
	res, err := GenerateRelatedResource(objData)
	if err != nil {
		klog.Errorf("failed to generate relatedResource %v, and skip to set into status. %v", objData, err)
		return
	}
	SetRelatedResourcesStatuses(relatedResourcesStatuses, res)
}

func RemoveRelatedResourcesStatusesWithObj(
	relatedResourcesStatuses *[]operatorapiv1.RelatedResourceMeta, objData []byte) {
	res, err := GenerateRelatedResource(objData)
	if err != nil {
		klog.Errorf("failed to generate relatedResource %v, and skip to set into status. %v", objData, err)
		return
	}
	RemoveRelatedResourcesStatuses(relatedResourcesStatuses, res)
}

// KlusterletNamespace returns the klusterlet namespace on the managed cluster.
func KlusterletNamespace(klusterlet *operatorapiv1.Klusterlet) string {
	if len(klusterlet.Spec.Namespace) == 0 {
		// If namespace is not set, use the default namespace
		return KlusterletDefaultNamespace
	}

	return klusterlet.Spec.Namespace
}

// AgentNamespace returns the namespace to deploy the agents.
// It is on the managed cluster in the Default mode, and on the management cluster in the Hosted mode.
func AgentNamespace(klusterlet *operatorapiv1.Klusterlet) string {
	if IsHosted(klusterlet.Spec.DeployOption.Mode) {
		return klusterlet.GetName()
	}

	return KlusterletNamespace(klusterlet)
}

// ResourceType set default and return resource requirements override by user
func ResourceType(resourceRequirementAcquirer operatorapiv1.ResourceRequirementAcquirer) operatorapiv1.ResourceQosClass {
	r := resourceRequirementAcquirer.GetResourceRequirement()
	if r == nil {
		return operatorapiv1.ResourceQosClassDefault
	}
	return r.Type
}

// ResourceRequirements get resource requirements overridden by user for ResourceQosClassResourceRequirement type
func ResourceRequirements(resourceRequirementAcquirer operatorapiv1.ResourceRequirementAcquirer) ([]byte, error) {
	r := resourceRequirementAcquirer.GetResourceRequirement()
	if r == nil || r.Type == operatorapiv1.ResourceQosClassBestEffort {
		return nil, nil
	}
	marshal, err := yaml.Marshal(r.ResourceRequirements)
	if err != nil {
		klog.Errorf("failed to marshal resource requirement: %v", err)
		return nil, err
	}
	return marshal, nil
}

// AgentPriorityClassName return the name of the PriorityClass that should be used for the klusterlet agents
func AgentPriorityClassName(klusterlet *operatorapiv1.Klusterlet, kubeVersion *version.Version) string {
	if kubeVersion == nil || klusterlet == nil {
		return ""
	}

	// priorityclass.scheduling.k8s.io/v1 is supported since v1.14.
	if cnt, err := kubeVersion.Compare("v1.14.0"); err != nil {
		klog.Warningf("Ignore PriorityClass because it's failed to check whether the cluster supports PriorityClass/v1 or not: %v", err)
		return ""
	} else if cnt == -1 {
		return ""
	}

	return klusterlet.Spec.PriorityClassName
}

// SyncSecret forked from:
// https://github.com/openshift/library-go/blob/d9cdfbd844ea08465b938c46a16bed2ea23207e4/pkg/operator/resource/resourceapply/core.go#L357,
// add an addition targetClient parameter to support sync secret to another cluster.
func SyncSecret(ctx context.Context, client, targetClient coreclientv1.SecretsGetter, recorder events.Recorder,
	sourceNamespace, sourceName, targetNamespace, targetName string, ownerRefs []metav1.OwnerReference, labels map[string]string) (*corev1.Secret, bool, error) {
	source, err := client.Secrets(sourceNamespace).Get(ctx, sourceName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		if _, getErr := targetClient.Secrets(targetNamespace).Get(ctx, targetName, metav1.GetOptions{}); getErr != nil && errors.IsNotFound(getErr) {
			return nil, true, nil
		}
		deleteErr := targetClient.Secrets(targetNamespace).Delete(ctx, targetName, metav1.DeleteOptions{})
		if errors.IsNotFound(deleteErr) {
			return nil, false, nil
		}
		if deleteErr == nil {
			recorder.Eventf("TargetSecretDeleted", "Deleted target secret %s/%s because source config does not exist", targetNamespace, targetName)
			return nil, true, nil
		}
		return nil, false, deleteErr
	case err != nil:
		return nil, false, err
	default:
		if source.Type == corev1.SecretTypeServiceAccountToken {

			// Make sure the token is already present, otherwise we have to wait before creating the target
			if len(source.Data[corev1.ServiceAccountTokenKey]) == 0 {
				return nil, false, fmt.Errorf("secret %s/%s doesn't have a token yet", source.Namespace, source.Name)
			}

			if source.Annotations != nil {
				// When syncing a service account token we have to remove the SA annotation to disable injection into copies
				delete(source.Annotations, corev1.ServiceAccountNameKey)
				// To make it clean, remove the dormant annotations as well
				delete(source.Annotations, corev1.ServiceAccountUIDKey)
			}

			// SecretTypeServiceAccountToken implies required fields and injection which we do not want in copies
			source.Type = corev1.SecretTypeOpaque
		}

		source.Namespace = targetNamespace
		source.Name = targetName
		source.ResourceVersion = ""
		source.OwnerReferences = ownerRefs
		source.Labels = labels
		return resourceapply.ApplySecret(ctx, targetClient, recorder, source)
	}
}

// GetHubKubeconfig is used to get the kubeconfig of the hub cluster.
// If it's Default mode, the kubeconfig of the hub cluster should equal to the operator cluster's kubeconfig and mostly, it's the InClusterConfig.
// If it's Hosted mode, the kubeconfig of the hub cluster is stored as a secret under clustermanager namespace.
func GetHubKubeconfig(ctx context.Context,
	operatorKubeconfig *rest.Config, // this is the kubeconfig of the cluster which controller is running on now.
	operatorClient kubernetes.Interface,
	clustermamagerName string,
	clustermanagerMode operatorapiv1.InstallMode) (*rest.Config, error) {
	switch clustermanagerMode {
	case operatorapiv1.InstallModeHosted:
		clustermanagerNamespace := ClusterManagerNamespace(clustermamagerName, clustermanagerMode)

		// get secret of external kubeconfig
		secret, err := operatorClient.CoreV1().Secrets(clustermanagerNamespace).Get(ctx, ExternalHubKubeConfig, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		config, err := LoadClientConfigFromSecret(secret)
		if err != nil {
			return nil, err
		}

		return config, nil
	default:
		// backward compatible with previous crd.
		return operatorKubeconfig, nil
	}
}

func BuildFeatureCondition(invalidMsgs ...string) metav1.Condition {
	if len(strings.Join(invalidMsgs, "")) == 0 {
		return metav1.Condition{
			Type:    FeatureGatesTypeValid,
			Status:  metav1.ConditionTrue,
			Reason:  FeatureGatesReasonAllValid,
			Message: "Feature gates are all valid",
		}
	}

	return metav1.Condition{
		Type:   FeatureGatesTypeValid,
		Status: metav1.ConditionFalse,
		Reason: FeatureGatesReasonInvalidExisting,
		Message: fmt.Sprintf("There are some invalid feature gates of %v, will process them with default values",
			invalidMsgs),
	}
}

func ConvertToFeatureGateFlags(component string, features []operatorapiv1.FeatureGate,
	defaultFeatureGates map[featuregate.Feature]featuregate.FeatureSpec) ([]string, string) {
	var flags, invalidFeatures []string

	for _, feature := range features {
		defaultFeature, ok := defaultFeatureGates[featuregate.Feature(feature.Feature)]
		if !ok {
			invalidFeatures = append(invalidFeatures, feature.Feature)
			continue
		}

		if feature.Mode == operatorapiv1.FeatureGateModeTypeDisable && defaultFeature.Default {
			flags = append(flags, fmt.Sprintf("--feature-gates=%s=false", feature.Feature))
		}

		if feature.Mode == operatorapiv1.FeatureGateModeTypeEnable && !defaultFeature.Default {
			flags = append(flags, fmt.Sprintf("--feature-gates=%s=true", feature.Feature))
		}
	}

	if len(invalidFeatures) > 0 {
		return flags, fmt.Sprintf("%s: %v", component, invalidFeatures)
	}

	return flags, ""
}

// FeatureGateEnabled checks if a feature is enabled or disabled in operator API, or fallback to use the
// the default setting
func FeatureGateEnabled(features []operatorapiv1.FeatureGate,
	defaultFeatures map[featuregate.Feature]featuregate.FeatureSpec, featureName featuregate.Feature) bool {
	for _, feature := range features {
		if feature.Feature == string(featureName) {
			return feature.Mode == operatorapiv1.FeatureGateModeTypeEnable
		}
	}

	defaultFeature, ok := defaultFeatures[featureName]
	if !ok {
		return false
	}

	return defaultFeature.Default
}

// IsSingleton returns if agent is deployed in singleton mode either hosted or not
func IsSingleton(mode operatorapiv1.InstallMode) bool {
	return mode == operatorapiv1.InstallModeSingleton || mode == operatorapiv1.InstallModeSingletonHosted
}

func IsHosted(mode operatorapiv1.InstallMode) bool {
	return mode == operatorapiv1.InstallModeHosted || mode == operatorapiv1.InstallModeSingletonHosted
}

func GetOperatorNamespace() string {
	operatorNamespace := DefaultComponentNamespace
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		operatorNamespace = string(nsBytes)
	}
	return operatorNamespace
}

// filterLabels removes reserved label keys from the input map
func filterLabels(labels map[string]string) map[string]string {
	filtered := map[string]string{}
	for k, v := range labels {
		if k == AppLabelKey || strings.HasPrefix(k, LabelPrefix) {
			continue
		}
		filtered[k] = v
	}
	return filtered
}

func GetRegistrationLabelString(clusterManagerLabels map[string]string) string {
	return ConvertLabelsMapToString(filterLabels(clusterManagerLabels))
}

func GetClusterManagerHubLabels(clusterManager *operatorapiv1.ClusterManager, enableSyncLabels bool) map[string]string {
	labels := map[string]string{}
	if enableSyncLabels {
		labels = filterLabels(clusterManager.Labels)
	}

	// This label key is used to filter resources in deployment informer
	labels[HubLabelKey] = clusterManager.GetName()

	return labels
}

func GetKlusterletAgentLabels(klusterlet *operatorapiv1.Klusterlet, enableSyncLabels bool) map[string]string {
	labels := map[string]string{}
	if enableSyncLabels {
		labels = filterLabels(klusterlet.Labels)
	}

	// This label key is used to filter resources in deployment informer
	labels[AgentLabelKey] = klusterlet.GetName()

	return labels
}

func ConvertLabelsMapToString(labels map[string]string) string {
	var labelList []string
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		labelList = append(labelList, fmt.Sprintf("%s=%s", key, labels[key]))
	}
	return strings.Join(labelList, ",")
}

func MapCompare(required, existing map[string]string) bool {
	for k, v := range required {
		if existing[k] != v {
			return false
		}
	}
	return true
}

func AddLabelsToYaml(objData []byte, cmLabels map[string]string) ([]byte, error) {
	jsonData, err := yaml.YAMLToJSON(objData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(jsonData, u); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Add or update labels
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range cmLabels {
		labels[k] = v
	}
	u.SetLabels(labels)

	// Marshal back to JSON
	modifiedJSON, err := json.Marshal(u)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated object: %w", err)
	}

	// Convert back to YAML (optional, if needed downstream)
	modifiedYAML, err := yaml.JSONToYAML(modifiedJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON to YAML: %w", err)
	}

	return modifiedYAML, nil
}
