package token

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	// TokenFile is the name of the token file in the secret
	TokenFile = "token"

	// TokenRefreshedCondition is the condition type for addon token refresh status
	TokenRefreshedCondition = "TokenRefreshed"

	// TokenInfrastructureReadyCondition is the condition type set by hub to indicate
	// that the ServiceAccount infrastructure is ready for token-based authentication
	TokenInfrastructureReadyCondition = "TokenInfrastructureReady"
)

// TokenControl encapsulates the operations needed for token-based authentication
type TokenControl interface {
	// CreateToken creates a ServiceAccount token
	CreateToken(ctx context.Context, serviceAccountName, namespace string, expirationSeconds int64) (string, error)
}

// tokenControl implements TokenControl interface
type tokenControl struct {
	hubCoreV1Client corev1client.CoreV1Interface
}

var _ TokenControl = &tokenControl{}

// CreateToken creates a ServiceAccount token using the TokenRequest API
func (t *tokenControl) CreateToken(ctx context.Context, serviceAccountName, namespace string, expirationSeconds int64) (string, error) {
	if t.hubCoreV1Client == nil {
		return "", fmt.Errorf("failed to create token for ServiceAccount %s/%s: hub CoreV1 client is not initialized", namespace, serviceAccountName)
	}

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}

	result, err := t.hubCoreV1Client.ServiceAccounts(namespace).CreateToken(
		ctx,
		serviceAccountName,
		tokenRequest,
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create token for ServiceAccount %s/%s: %w", namespace, serviceAccountName, err)
	}

	return result.Status.Token, nil
}

// NewTokenControl creates a new TokenControl instance
func NewTokenControl(hubCoreV1Client corev1client.CoreV1Interface) TokenControl {
	return &tokenControl{
		hubCoreV1Client: hubCoreV1Client,
	}
}

// TokenDriver implements token-based authentication for addon registration only.
// It uses ServiceAccount token projection for authentication with the hub cluster.
type TokenDriver struct {
	addonName    string
	clusterName  string
	opt          register.TokenConfiguration
	tokenControl TokenControl

	// addonClients holds the addon clients and informers
	addonClients *register.AddOnClients

	// addonPatcher for updating addon status
	addonPatcher patcher.Patcher[
		*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus]
}

var _ register.RegisterDriver = &TokenDriver{}

// NewTokenDriverForAddOn creates a new token driver instance for an addon.
// This should only be called from a cluster driver's Fork() method.
func NewTokenDriverForAddOn(addonName, clusterName string, tokenConfig register.TokenConfiguration, tokenControl TokenControl, addonClients *register.AddOnClients) *TokenDriver {
	driver := &TokenDriver{
		addonName:    addonName,
		clusterName:  clusterName,
		tokenControl: tokenControl,
		addonClients: addonClients,
		opt:          tokenConfig,
		addonPatcher: patcher.NewPatcher[
			*addonv1alpha1.ManagedClusterAddOn, addonv1alpha1.ManagedClusterAddOnSpec, addonv1alpha1.ManagedClusterAddOnStatus](
			addonClients.AddonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName)),
	}

	return driver
}

// Process updates the secret with the current token from the ServiceAccount token file
func (t *TokenDriver) Process(
	ctx context.Context,
	controllerName string,
	secret *corev1.Secret,
	additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	// Get the addon
	addon, err := t.addonClients.AddonInformer.Lister().ManagedClusterAddOns(t.clusterName).Get(t.addonName)
	if errors.IsNotFound(err) {
		// Addon not found (likely deleted), skip processing
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	// Ensure subject field is set (driver should already be set by addon registration controller)
	updated, err := t.ensureSubject(ctx, addon)
	if err != nil || updated {
		return nil, nil, err
	}

	// Wait for token infrastructure to be ready and get ServiceAccount UID
	desiredUID, ready, err := t.ensureTokenInfrastructureReady(ctx, addon)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, nil, nil
	}

	// Check if we need to refresh the token
	shouldRefresh, err := t.shouldRefreshToken(ctx, secret, desiredUID)
	if err != nil {
		return nil, nil, err
	}
	if !shouldRefresh {
		return nil, nil, nil
	}

	// Refresh the token
	return t.refreshToken(ctx, secret, recorder)
}

// ensureSubject ensures the subject field is set correctly for token-based authentication.
// Subject.user is set to system:serviceaccount:<cluster-namespace>:<addon-name>-agent
// Returns (updated, error) where updated indicates if an update was performed
func (t *TokenDriver) ensureSubject(ctx context.Context, addon *addonv1alpha1.ManagedClusterAddOn) (bool, error) {
	logger := klog.FromContext(ctx)

	// Find the registration configuration index
	var regIndex = -1
	for i := range addon.Status.Registrations {
		if addon.Status.Registrations[i].SignerName == certificatesv1.KubeAPIServerClientSignerName {
			regIndex = i
			break
		}
	}

	if regIndex == -1 {
		return false, nil
	}

	// Set subject for token-based authentication
	expectedSubjectUser := fmt.Sprintf("system:serviceaccount:%s:%s-agent", t.clusterName, t.addonName)

	// Make a copy and update subject (create new Subject with only User specified)
	addonCopy := addon.DeepCopy()
	addonCopy.Status.Registrations[regIndex].Subject = addonv1alpha1.Subject{
		User: expectedSubjectUser,
	}

	// Update the addon status using addonPatcher
	updated, err := t.addonPatcher.PatchStatus(ctx, addonCopy, addonCopy.Status, addon.Status)
	if err != nil {
		return false, err
	}

	if updated {
		logger.Info("Updated subject field", "addon", t.addonName, "subject", expectedSubjectUser)
	}

	return updated, nil
}

// ensureTokenInfrastructureReady waits for the TokenInfrastructureReady condition and extracts the ServiceAccount UID
// Returns (uid, ready, error) where:
// - If ready is true, uid is guaranteed to be non-empty
// - If ready is false, infrastructure is not ready yet (caller should wait and retry)
// - If error is non-nil, an actual error occurred
func (t *TokenDriver) ensureTokenInfrastructureReady(ctx context.Context, addon *addonv1alpha1.ManagedClusterAddOn) (string, bool, error) {
	logger := klog.FromContext(ctx)

	infraReady := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
	if infraReady == nil {
		logger.Info("TokenInfrastructureReady condition not found, waiting for hub to set it", "addon", t.addonName)
		return "", false, nil
	}

	if infraReady.Status != metav1.ConditionTrue {
		logger.Info("TokenInfrastructureReady condition is not True, waiting",
			"addon", t.addonName,
			"status", infraReady.Status,
			"reason", infraReady.Reason)
		return "", false, nil
	}

	desiredUID, err := t.parseServiceAccountUIDFromMessage(infraReady.Message)
	if err != nil {
		logger.Error(err, "Failed to parse ServiceAccount UID from TokenInfrastructureReady condition message",
			"addon", t.addonName,
			"message", infraReady.Message)
		return "", false, err
	}

	logger.V(4).Info("Parsed ServiceAccount UID from TokenInfrastructureReady condition",
		"addon", t.addonName,
		"serviceAccountUID", desiredUID)

	return desiredUID, true, nil
}

// refreshToken creates a new token and updates the secret
func (t *TokenDriver) refreshToken(ctx context.Context, secret *corev1.Secret, recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	tokenData, expiresAt, err := t.createToken(ctx)
	if err != nil {
		return nil, &metav1.Condition{
			Type:    TokenRefreshedCondition,
			Status:  metav1.ConditionFalse,
			Reason:  "TokenCreationFailed",
			Message: fmt.Sprintf("Failed to create token: %v", err),
		}, err
	}

	secret.Data[TokenFile] = tokenData
	recorder.Eventf(ctx, "AddonTokenRefreshed", "Token refreshed for addon %s", t.addonName)

	return secret, &metav1.Condition{
		Type:    TokenRefreshedCondition,
		Status:  metav1.ConditionTrue,
		Reason:  "AddonTokenRefreshed",
		Message: fmt.Sprintf("Addon token refreshed, expires at %s", expiresAt.Format(time.RFC3339)),
	}, nil
}

// BuildKubeConfigFromTemplate builds kubeconfig with bearer token authentication
func (t *TokenDriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		register.DefaultKubeConfigAuth: {
			TokenFile: TokenFile,
		},
	}
	return kubeConfig
}

// InformerHandler returns the addon informer with a filter for the specific addon
func (t *TokenDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	if t.addonClients == nil {
		return nil, nil
	}

	// Create event filter function to only watch the specific addon
	// Note: The informer is already scoped to the cluster namespace, so we only need to filter by addon name
	eventFilterFunc := func(obj interface{}) bool {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return false
		}

		// Only enqueue the specific addon (name matches addonName)
		return accessor.GetName() == t.addonName
	}

	return t.addonClients.AddonInformer.Informer(), eventFilterFunc
}

// IsHubKubeConfigValid checks if the current token is valid
func (t *TokenDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	logger := klog.FromContext(ctx)

	tokenData, err := t.readTokenFile(secretOption.HubKubeconfigDir)
	if err != nil {
		logger.V(4).Info("Token file not found or unreadable")
		return false, nil
	}

	desiredUID := t.getDesiredUIDForValidation(ctx)
	valid, reason := isTokenValid(tokenData, desiredUID)
	if !valid {
		logger.V(4).Info("Token is invalid", "reason", reason)
	}
	return valid, nil
}

// ManagedClusterDecorator returns the cluster unchanged (no modifications needed)
func (t *TokenDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}

// BuildClients does nothing for TokenDriver
func (t *TokenDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	return nil, nil
}

// shouldRefreshToken determines if the token needs to be refreshed
func (t *TokenDriver) shouldRefreshToken(ctx context.Context, secret *corev1.Secret, desiredUID string) (bool, error) {
	logger := klog.FromContext(ctx)

	// If no token in secret, refresh is needed
	tokenData, ok := secret.Data[TokenFile]
	if !ok || len(tokenData) == 0 {
		logger.Info("Token refresh needed: no token found in secret", "addon", t.addonName)
		return true, nil
	}

	// Check if token is still valid based on expiration and UID
	valid, reason := isTokenValid(tokenData, desiredUID)
	if !valid {
		logger.Info("Token refresh needed", "addon", t.addonName, "reason", reason)
		return true, nil
	}

	logger.V(4).Info("Token is valid, no refresh needed", "addon", t.addonName)
	return false, nil
}

// readTokenFile reads the token file from the specified directory
func (t *TokenDriver) readTokenFile(hubKubeconfigDir string) ([]byte, error) {
	tokenPath := path.Join(hubKubeconfigDir, TokenFile)
	return os.ReadFile(path.Clean(tokenPath))
}

// getDesiredUIDForValidation retrieves the desired ServiceAccount UID for token validation
// Returns empty string if UID cannot be determined (validation will skip UID check)
func (t *TokenDriver) getDesiredUIDForValidation(ctx context.Context) string {
	logger := klog.FromContext(ctx)

	if t.addonClients == nil {
		return ""
	}

	addon, err := t.addonClients.AddonInformer.Lister().ManagedClusterAddOns(t.clusterName).Get(t.addonName)
	if err != nil {
		logger.V(4).Info("Failed to get addon for token validation", "addon", t.addonName, "error", err)
		return ""
	}

	infraReady := meta.FindStatusCondition(addon.Status.Conditions, TokenInfrastructureReadyCondition)
	if infraReady == nil || infraReady.Status != metav1.ConditionTrue {
		return ""
	}

	uid, err := t.parseServiceAccountUIDFromMessage(infraReady.Message)
	if err != nil {
		logger.Info("Failed to parse ServiceAccount UID for token validation", "error", err)
		return ""
	}

	return uid
}

// createToken creates a new ServiceAccount token using the TokenRequest API
// ServiceAccount name format: <addon-name>-agent
// Returns the token data and expiration time
func (t *TokenDriver) createToken(ctx context.Context) ([]byte, time.Time, error) {
	if t.tokenControl == nil {
		return nil, time.Time{}, fmt.Errorf("token control not initialized")
	}

	// ServiceAccount naming convention: <addon-name>-agent
	// The ServiceAccount is created by the hub addon manager for token-based registration
	serviceAccountName := fmt.Sprintf("%s-agent", t.addonName)

	// Create token using TokenRequest API
	token, err := t.tokenControl.CreateToken(ctx, serviceAccountName, t.clusterName, t.opt.GetExpirationSeconds())
	if err != nil {
		return nil, time.Time{}, err
	}

	tokenData := []byte(token)

	// Parse token to get expiration time
	_, expiresAt, _, err := parseToken(tokenData)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse created token: %w", err)
	}

	return tokenData, expiresAt, nil
}

// parseServiceAccountUIDFromMessage parses the ServiceAccount UID from the TokenInfrastructureReady condition message
// Expected message format: "ServiceAccount <namespace>/<name> (UID: <uid>) is ready"
func (t *TokenDriver) parseServiceAccountUIDFromMessage(message string) (string, error) {
	// Look for "UID: <uid>" pattern in the message
	const uidPrefix = "UID: "
	const uidSuffix = ")"

	startIdx := strings.Index(message, uidPrefix)
	if startIdx == -1 {
		return "", fmt.Errorf("ServiceAccount UID not found in message: %s", message)
	}

	startIdx += len(uidPrefix)
	endIdx := strings.Index(message[startIdx:], uidSuffix)
	if endIdx == -1 {
		return "", fmt.Errorf("malformed ServiceAccount UID in message: %s", message)
	}

	uid := message[startIdx : startIdx+endIdx]
	if uid == "" {
		return "", fmt.Errorf("empty ServiceAccount UID in message: %s", message)
	}

	return uid, nil
}

// TryForkTokenDriver checks if token-based authentication should be used for the addon,
// and if so, creates and returns a TokenDriver. Returns nil if token auth is not needed.
// This helper is shared by all cluster drivers (CSR, AWS IRSA, gRPC) to avoid code duplication.
func TryForkTokenDriver(
	addonName string,
	authConfig register.AddonAuthConfig,
	secretOption register.SecretOption,
	tokenControl TokenControl,
	addonClients *register.AddOnClients,
) (register.RegisterDriver, error) {
	// Determine registration type from signer name
	isKubeClientType := secretOption.Signer == certificatesv1.KubeAPIServerClientSignerName

	// Only use token auth for KubeClient type with token authentication
	if !isKubeClientType || authConfig.GetKubeClientAuth() != "token" {
		return nil, nil // Not using token auth
	}

	// Get token configuration from AddonAuthConfig (type-safe interface)
	tokenConfig := authConfig.GetTokenConfiguration()
	if tokenConfig == nil {
		return nil, fmt.Errorf("token authentication requested but TokenConfiguration is nil for addon %s", addonName)
	}

	if tokenControl == nil {
		return nil, fmt.Errorf("token authentication requested but tokenControl is not initialized for addon %s", addonName)
	}

	if addonClients == nil {
		return nil, fmt.Errorf("token authentication requested but addonClients is not initialized for addon %s", addonName)
	}

	if addonClients.AddonClient == nil {
		return nil, fmt.Errorf("token authentication requested but addonClients.AddonClient is nil for addon %s", addonName)
	}

	if addonClients.AddonInformer == nil {
		return nil, fmt.Errorf("token authentication requested but addonClients.AddonInformer is nil for addon %s", addonName)
	}

	return NewTokenDriverForAddOn(addonName, secretOption.ClusterName, tokenConfig, tokenControl, addonClients), nil
}
