package addontokeninfra

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	rbacinformers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/common/queue"
	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
)

var (
	tokenInfraManifests = []string{
		"cluster-manager/hub/addon-manager/token-serviceaccount.yaml",
		"cluster-manager/hub/addon-manager/token-role.yaml",
		"cluster-manager/hub/addon-manager/token-rolebinding.yaml",
	}
)

const (
	// TokenInfrastructureReadyCondition is the condition type indicating token infrastructure is ready
	TokenInfrastructureReadyCondition = "TokenInfrastructureReady"
)

// TokenInfraConfig holds configuration for rendering token infrastructure manifests
type TokenInfraConfig struct {
	ClusterName string
	AddonName   string
}

// tokenInfrastructureController reconciles ManagedClusterAddOn resources
// to create token-based authentication infrastructure
type tokenInfrastructureController struct {
	kubeClient  kubernetes.Interface
	addonClient addonv1alpha1client.Interface
	addonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	cache       resourceapply.ResourceCache
	recorder    events.Recorder
}

// usesTokenAuth checks if the addon uses token-based authentication
// by checking if it has kubeClient registration and kubeClientDriver is "token"
func usesTokenAuth(addon *addonapiv1alpha1.ManagedClusterAddOn) bool {
	// First check if addon has kubeClient registration
	hasKubeClient := false
	for _, reg := range addon.Status.Registrations {
		if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName {
			hasKubeClient = true
			break
		}
	}

	if !hasKubeClient {
		return false
	}

	// Then check if kubeClientDriver is "token"
	return addon.Status.KubeClientDriver == "token"
}

// addonFilter filters addons that use token-based kubeClient registration
// or have TokenInfrastructureReady condition (for cleanup)
func addonFilter(obj interface{}) bool {
	addon, ok := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
	if !ok {
		return false
	}

	// Check if addon uses token authentication
	if usesTokenAuth(addon) {
		return true
	}

	// Check if addon has TokenInfrastructureReady condition (needs cleanup)
	if meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition) != nil {
		return true
	}

	return false
}

// tokenInfraResourceToAddonKey extracts the addon namespace/name from token infrastructure resource labels
// Returns empty string if resource doesn't have the required labels
func tokenInfraResourceToAddonKey(obj runtime.Object) string {
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return ""
	}

	labels := metaObj.GetLabels()
	if labels == nil {
		return ""
	}

	// Only process resources with token-infrastructure label
	if labels["addon.open-cluster-management.io/token-infrastructure"] != "true" {
		return ""
	}

	// Extract addon name from label
	addonName := labels["addon.open-cluster-management.io/name"]
	if addonName == "" {
		return ""
	}

	// Namespace is the cluster name
	namespace := metaObj.GetNamespace()
	if namespace == "" {
		return ""
	}

	// Return namespace/addonName as queue key
	return namespace + "/" + addonName
}

// newTokenInfraEventHandler creates an event handler for token infrastructure resources
// that enqueues the associated addon for reconciliation on Update and Delete events
func newTokenInfraEventHandler(syncCtx factory.SyncContext, queueKeyFn func(runtime.Object) string) cache.ResourceEventHandler {
	return &cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			newObj, ok := new.(runtime.Object)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
				return
			}
			if key := queueKeyFn(newObj); key != "" {
				syncCtx.Queue().Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var runtimeObj runtime.Object
			var ok bool

			if tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown); isTombstone {
				runtimeObj, ok = tombstone.Obj.(runtime.Object)
				if !ok {
					utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
					return
				}
			} else {
				runtimeObj, ok = obj.(runtime.Object)
				if !ok {
					utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
					return
				}
			}

			if key := queueKeyFn(runtimeObj); key != "" {
				syncCtx.Queue().Add(key)
			}
		},
	}
}

func NewTokenInfrastructureController(
	kubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	serviceAccountInformer coreinformers.ServiceAccountInformer,
	roleInformer rbacinformers.RoleInformer,
	roleBindingInformer rbacinformers.RoleBindingInformer,
) factory.Controller {
	c := &tokenInfrastructureController{
		kubeClient:  kubeClient,
		addonClient: addonClient,
		addonLister: addonInformers.Lister(),
		cache:       resourceapply.NewResourceCache(),
		recorder:    events.NewContextualLoggingEventRecorder("addon-token-infrastructure-controller"),
	}

	syncCtx := factory.NewSyncContext("addon-token-infrastructure-controller")

	// Register custom event handlers on infrastructure informers
	// Only handle Update and Delete, NOT Add (creation is triggered by addon watch)
	eventHandler := newTokenInfraEventHandler(syncCtx, tokenInfraResourceToAddonKey)

	// Register the same event handler for all infrastructure informers
	saInformer := serviceAccountInformer.Informer()
	_, err := saInformer.AddEventHandler(eventHandler)
	if err != nil {
		utilruntime.HandleError(err)
	}

	rInformer := roleInformer.Informer()
	_, err = rInformer.AddEventHandler(eventHandler)
	if err != nil {
		utilruntime.HandleError(err)
	}

	rbInformer := roleBindingInformer.Informer()
	_, err = rbInformer.AddEventHandler(eventHandler)
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithSyncContext(syncCtx).
		// Primary watch: ManagedClusterAddOns with token registration
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			addonFilter,
			addonInformers.Informer(),
		).
		// Bare informers with custom handlers (already registered above)
		WithBareInformers(
			saInformer,
			rInformer,
			rbInformer,
		).
		WithSync(c.sync).
		ToController("addon-token-infrastructure-controller")
}

func (c *tokenInfrastructureController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	logger := klog.FromContext(ctx).WithValues("addon", key)
	logger.V(4).Info("Reconciling addon token authentication")

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is invalid
		return nil
	}

	addon, err := c.addonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		// Addon is deleted, attempt to clean up any remaining token infrastructure
		logger.Info("Addon not found, cleaning up any remaining token infrastructure")
		return c.cleanupTokenInfrastructure(ctx, clusterName, addonName)
	}
	if err != nil {
		return err
	}

	// If addon is being deleted, clean up token infrastructure
	if addon.DeletionTimestamp != nil {
		logger.Info("Addon is being deleted, cleaning up token infrastructure")
		// Check if TokenInfrastructureReady condition exists
		infraReady := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
		if infraReady != nil {
			// Clean up token infrastructure resources
			// No need to remove the condition - the addon is being deleted anyway
			return c.cleanupTokenInfrastructure(ctx, clusterName, addonName)
		}
		return nil
	}

	// Check if addon uses token authentication
	if !usesTokenAuth(addon) {
		// Check if TokenInfrastructureReady condition exists
		infraReady := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
		if infraReady == nil {
			// No condition, nothing to clean up
			logger.V(4).Info("No token-based kubeClient authentication found and no condition exists, skipping")
			return nil
		}

		// Clean up token infrastructure resources
		logger.Info("Addon no longer uses token-based authentication, cleaning up token infrastructure")
		if err := c.cleanupTokenInfrastructure(ctx, clusterName, addonName); err != nil {
			return err
		}

		// Remove TokenInfrastructureReady condition
		return c.removeCondition(ctx, addon)
	}

	// Ensure token infrastructure is created and ready
	return c.ensureTokenInfrastructure(ctx, addon, clusterName, addonName)
}

// ensureTokenInfrastructure creates and maintains token authentication infrastructure
func (c *tokenInfrastructureController) ensureTokenInfrastructure(
	ctx context.Context,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	clusterName, addonName string) error {

	config := TokenInfraConfig{
		ClusterName: clusterName,
		AddonName:   addonName,
	}

	// Apply manifests
	resourceResults := c.applyManifests(ctx, config)

	var saUID string
	var errs []error
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
		// Extract ServiceAccount UID
		if sa, ok := result.Result.(*corev1.ServiceAccount); ok {
			saUID = string(sa.UID)
		}
	}

	if len(errs) > 0 {
		updateErr := c.updateCondition(ctx, addon, metav1.ConditionFalse, "TokenInfrastructureApplyFailed",
			fmt.Sprintf("Failed to apply token infrastructure: %v", utilerrors.NewAggregate(errs)))
		// Append updateErr to errs and return aggregate (NewAggregate filters out nil errors)
		return utilerrors.NewAggregate(append(errs, updateErr))
	}

	// Set TokenInfrastructureReady condition with ServiceAccount UID
	serviceAccountName := fmt.Sprintf("%s-agent", addonName)
	message := fmt.Sprintf("ServiceAccount %s/%s (UID: %s) is ready", clusterName, serviceAccountName, saUID)
	return c.updateCondition(ctx, addon, metav1.ConditionTrue, "TokenInfrastructureReady", message)
}

// applyManifests applies token infrastructure manifests
func (c *tokenInfrastructureController) applyManifests(ctx context.Context, config TokenInfraConfig) []resourceapply.ApplyResult {
	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, c.recorder)
	return resourceapply.ApplyDirectly(
		ctx,
		resourceapply.NewKubeClientHolder(c.kubeClient),
		recorderWrapper,
		c.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		tokenInfraManifests...,
	)
}

// updateCondition updates the TokenInfrastructureReady condition on the addon
func (c *tokenInfrastructureController) updateCondition(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn,
	status metav1.ConditionStatus, reason, message string) error {
	addonPatcher := patcher.NewPatcher[
		*addonapiv1alpha1.ManagedClusterAddOn,
		addonapiv1alpha1.ManagedClusterAddOnSpec,
		addonapiv1alpha1.ManagedClusterAddOnStatus](
		c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace))

	addonCopy := addon.DeepCopy()

	condition := metav1.Condition{
		Type:    TokenInfrastructureReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	meta.SetStatusCondition(&addonCopy.Status.Conditions, condition)

	_, err := addonPatcher.PatchStatus(ctx, addonCopy, addonCopy.Status, addon.Status)
	return err
}

// removeCondition removes the TokenInfrastructureReady condition from the addon
func (c *tokenInfrastructureController) removeCondition(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	addonPatcher := patcher.NewPatcher[
		*addonapiv1alpha1.ManagedClusterAddOn,
		addonapiv1alpha1.ManagedClusterAddOnSpec,
		addonapiv1alpha1.ManagedClusterAddOnStatus](
		c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace))

	addonCopy := addon.DeepCopy()

	// Remove the TokenInfrastructureReady condition
	meta.RemoveStatusCondition(&addonCopy.Status.Conditions, TokenInfrastructureReadyCondition)

	_, err := addonPatcher.PatchStatus(ctx, addonCopy, addonCopy.Status, addon.Status)
	return err
}

// cleanupTokenInfrastructure removes token authentication infrastructure resources
func (c *tokenInfrastructureController) cleanupTokenInfrastructure(ctx context.Context, clusterName, addonName string) error {
	logger := klog.FromContext(ctx)
	config := TokenInfraConfig{
		ClusterName: clusterName,
		AddonName:   addonName,
	}

	recorderWrapper := commonrecorder.NewEventsRecorderWrapper(ctx, c.recorder)
	resourceResults := resourceapply.DeleteAll(
		ctx,
		resourceapply.NewKubeClientHolder(c.kubeClient),
		recorderWrapper,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		tokenInfraManifests...,
	)

	var errs []error
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	logger.Info("Successfully cleaned up token infrastructure", "addon", addonName)
	return nil
}
