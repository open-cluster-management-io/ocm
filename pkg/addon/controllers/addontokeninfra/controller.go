package addontokeninfra

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

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

	// TokenInfrastructureResyncInterval is the interval to resync token infrastructure
	TokenInfrastructureResyncInterval = 5 * time.Minute
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

// addonFilter filters addons that use token-based kubeClient registration
// or have TokenInfrastructureReady condition (for cleanup)
func addonFilter(obj interface{}) bool {
	addon, ok := obj.(*addonapiv1alpha1.ManagedClusterAddOn)
	if !ok {
		return false
	}

	// Check if addon has a kubeClient type registration with driver="token"
	for _, reg := range addon.Status.Registrations {
		if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName && reg.Driver == "token" {
			return true
		}
	}

	// Check if addon has TokenInfrastructureReady condition (needs cleanup)
	if meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition) != nil {
		return true
	}

	return false
}

func NewTokenInfrastructureController(
	kubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
) factory.Controller {
	c := &tokenInfrastructureController{
		kubeClient:  kubeClient,
		addonClient: addonClient,
		addonLister: addonInformers.Lister(),
		cache:       resourceapply.NewResourceCache(),
		recorder:    events.NewContextualLoggingEventRecorder("addon-token-infrastructure-controller"),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			addonFilter,
			addonInformers.Informer(),
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
		// Addon is deleted, no cleanup needed (resources in the namespace will be cleaned up)
		return nil
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
			if err := c.cleanupTokenInfrastructure(ctx, clusterName, addonName); err != nil {
				return err
			}
			// Remove TokenInfrastructureReady condition
			return c.removeCondition(ctx, addon)
		}
		return nil
	}

	// Find kubeClient type registration with token driver
	var tokenReg *addonapiv1alpha1.RegistrationConfig
	for i := range addon.Status.Registrations {
		reg := &addon.Status.Registrations[i]
		if reg.SignerName == certificatesv1.KubeAPIServerClientSignerName && reg.Driver == "token" {
			tokenReg = reg
			break
		}
	}

	// If no token-based kubeClient registration found, check if we need to clean up
	if tokenReg == nil {
		// Check if TokenInfrastructureReady condition exists
		infraReady := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
		if infraReady == nil {
			// No condition, nothing to clean up
			logger.V(4).Info("No token-based kubeClient registration found and no condition exists, skipping")
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
	err = c.ensureTokenInfrastructure(ctx, addon, clusterName, addonName)
	if err == nil {
		// Queue the addon for reconciliation after TokenInfrastructureResyncInterval
		syncCtx.Queue().AddAfter(key, TokenInfrastructureResyncInterval)
	}

	return err
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
		err := utilerrors.NewAggregate(errs)
		return c.updateCondition(ctx, addon, metav1.ConditionFalse, "TokenInfrastructureApplyFailed",
			fmt.Sprintf("Failed to apply token infrastructure: %v", err))
	}

	// Set TokenInfrastructureReady condition with ServiceAccount UID
	serviceAccountName := fmt.Sprintf("%s-%s-agent", clusterName, addonName)
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
	addonCopy := addon.DeepCopy()

	condition := metav1.Condition{
		Type:    TokenInfrastructureReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	meta.SetStatusCondition(&addonCopy.Status.Conditions, condition)

	// Compare to avoid unnecessary updates
	if equality.Semantic.DeepEqual(addon.Status.Conditions, addonCopy.Status.Conditions) {
		return nil
	}

	_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace).UpdateStatus(ctx, addonCopy, metav1.UpdateOptions{})
	return err
}

// removeCondition removes the TokenInfrastructureReady condition from the addon
func (c *tokenInfrastructureController) removeCondition(ctx context.Context, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	addonCopy := addon.DeepCopy()

	// Remove the TokenInfrastructureReady condition
	meta.RemoveStatusCondition(&addonCopy.Status.Conditions, TokenInfrastructureReadyCondition)

	// Compare to avoid unnecessary updates
	if equality.Semantic.DeepEqual(addon.Status.Conditions, addonCopy.Status.Conditions) {
		return nil
	}

	_, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(addon.Namespace).UpdateStatus(ctx, addonCopy, metav1.UpdateOptions{})
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
