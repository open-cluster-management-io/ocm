package azure_auth

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

// execCommand is the path of the "get-azure-token" exec credential plugin inside
// the pod. The exec config built here is persisted into the hub-kubeconfig secret
// by the registration agent (see the HubKubeconfigSecretController / BootstrapController
// wiring in pkg/registration/register), and that same persisted secret is later
// reused verbatim by whichever *other* process reads it to reach the hub - in
// Default mode that's a separate work-agent container (built from cmd/work), a
// completely different binary/image than the registration container that wrote
// the secret. So this can't resolve to "whatever binary is currently running"
// (os.Executable()) - that only happens to work in Singleton mode, where
// registration and work share one process. Instead every image that might need to
// exec this plugin (cmd/registration, cmd/registration-operator, cmd/work) copies
// its built binary to this second, fixed path in its Dockerfile
// (build/Dockerfile.registration, .registration-operator, .work) *in addition to*
// its normal entrypoint name, and each of those binaries also registers the
// "get-azure-token" subcommand - so the same exec command is valid regardless of
// which container ends up reading the persisted secret.
const execCommand = "/ocm-agent"

// AzureAuthDriver is a register.RegisterDriver that authenticates the spoke agent to
// the hub apiserver using an Azure AD access token (obtained via
// NewAzureCredential/GetTokenExecCredential) instead of a hub-signed client
// certificate. The persisted exec config carries only non-secret arguments: any
// secret material the selected credential needs is read from the environment of
// whichever container runs the plugin, which inherits the agent's environment. There is no CSR to create or rotate: the token is fetched fresh by the
// kubeconfig's exec credential plugin on every request whose cached token has expired.
type AzureAuthDriver struct {
	name string
	opt  *AzureOption

	azureAuthControl AzureAuthControl

	// addonClients holds the addon clients and informers (for addon driver only)
	addonClients *register.AddOnClients

	// tokenControl is used for token-based addon authentication
	tokenControl token.TokenControl

	// csrControl is used for CSR-based addon authentication
	csrControl csr.CSRControl
}

func (a *AzureAuthDriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {

	isApproved, err := a.azureAuthControl.isApproved(a.name)
	if err != nil {
		return nil, nil, err
	}
	if !isApproved {
		return nil, nil, nil
	}

	recorder.Eventf(ctx, "AzureRegistrationRequestApproved", "An azure-auth registration request is approved for %s", controllerName)
	return secret, nil, nil
}

func (a *AzureAuthDriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	args := []string{"get-azure-token", fmt.Sprintf("--azure-credential=%s", a.opt.Credential)}
	if a.opt.TenantID != "" {
		args = append(args, fmt.Sprintf("--azure-tenant-id=%s", a.opt.TenantID))
	}
	if a.opt.ClientID != "" {
		args = append(args, fmt.Sprintf("--azure-client-id=%s", a.opt.ClientID))
	}
	if a.opt.FederatedTokenFile != "" {
		args = append(args, fmt.Sprintf("--azure-federated-token-file=%s", a.opt.FederatedTokenFile))
	}
	args = append(args, fmt.Sprintf("--azure-token-audience=%s", a.opt.TokenAudience))

	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		Exec: &clientcmdapi.ExecConfig{
			APIVersion: execCredentialAPIVersion,
			Command:    execCommand,
			Args:       args,
		},
	}}
	return kubeConfig
}

func (a *AzureAuthDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return a.azureAuthControl.Informer(), nil
}

// IsHubKubeConfigValid always returns true once bootstrapped: there is no client
// certificate to expire, so as long as a bootstrap kubeconfig was supplied there is
// nothing further to validate here - each request re-fetches a fresh Azure AD token
// through the exec credential plugin.
func (a *AzureAuthDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	if secretOption.BootStrapKubeConfigFile == "" {
		return false, nil
	}
	return true, nil
}

func (a *AzureAuthDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}
	cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterAzureIdentityAnnotation] = a.opt.ManagedClusterAzureID
	return cluster
}

func (a *AzureAuthDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	clients, err := register.BuildClientsFromSecretOption(secretOption, bootstrap)
	if err != nil {
		return nil, err
	}
	a.azureAuthControl, err = NewAzureAuthControl(clients.ClusterInformer, clients.ClusterClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create azure auth control: %w", err)
	}

	// Store addon clients and initialize controls for addon authentication after bootstrap
	if !bootstrap {
		a.addonClients = &register.AddOnClients{
			AddonClient:   clients.AddonClient,
			AddonInformer: clients.AddonInformer,
		}

		kubeConfig, err := register.KubeConfigFromSecretOption(secretOption, bootstrap)
		if err != nil {
			return nil, err
		}
		kubeClient, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			return nil, err
		}
		a.tokenControl = token.NewTokenControl(kubeClient.CoreV1())

		// Initialize CSR control for CSR-based addon authentication
		logger := klog.FromContext(ctx)
		kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
			kubeClient,
			10*time.Minute,
			informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
				listOptions.LabelSelector = fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, secretOption.ClusterName)
			}),
		)
		csrControl, err := csr.NewCSRControl(logger, kubeInformerFactory.Certificates(), kubeClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create CSR control: %w", err)
		}
		a.csrControl = csrControl

		// Consumed by drivers forked for addon registration; this driver's own
		// InformerHandler only exposes the azureAuthControl informer.
		go kubeInformerFactory.Start(ctx.Done())
	}

	return clients, nil
}

// Fork creates a RegisterDriver for addon registration. Addon registration is
// independent of how the managed cluster itself authenticates to the hub, so it
// reuses the same token/CSR addon drivers as the other RegisterDriver implementations.
func (a *AzureAuthDriver) Fork(addonName string, authConfig register.AddonAuthConfig, secretOption register.SecretOption) (register.RegisterDriver, error) {
	tokenDriver, err := token.TryForkTokenDriver(addonName, authConfig, secretOption, a.tokenControl, a.addonClients)
	if err != nil {
		return nil, err
	}
	if tokenDriver != nil {
		return tokenDriver, nil
	}

	csrConfig := authConfig.GetCSRConfiguration()
	if csrConfig == nil {
		return nil, fmt.Errorf("CSR configuration is nil for addon %s", addonName)
	}

	return csr.NewCSRDriverForAddOn(addonName, csrConfig, secretOption, a.csrControl), nil
}

func NewAzureAuthDriver(opt *AzureOption, secretOption register.SecretOption) register.RegisterDriver {
	return &AzureAuthDriver{
		opt:  opt,
		name: secretOption.ClusterName,
	}
}

var _ register.RegisterDriver = &AzureAuthDriver{}
var _ register.AddonDriverFactory = &AzureAuthDriver{}
