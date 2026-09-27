package azure_auth

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

// defaultTokenAudience is the well-known application ID of the "Azure Kubernetes
// Service AAD Server" application. It is the audience kubelogin/az aks
// get-credentials request by default when authenticating to an AKS apiserver via
// Azure AD. Hubs that trust Azure AD through a different app registration (e.g. a
// custom OIDC-issuer configuration on a self-managed apiserver) must override this
// with --azure-token-audience.
const defaultTokenAudience = "6dae42f8-4368-4678-94ff-3960e28e3630/.default"

// ManagedClusterAzureIdentityAnnotation is the (unqualified) annotation key set on a
// ManagedCluster to record the Azure AD object ID (principal ID) of the identity used
// by that managed cluster. It is applied under operatorv1.ClusterAnnotationsKeyPrefix,
// e.g. "agent.open-cluster-management.io/managed-cluster-azure-identity".
const ManagedClusterAzureIdentityAnnotation = "managed-cluster-azure-identity"

// The environment-credential-* credential types read their secret material from the
// agent container's environment and filesystem, never from flags or the Klusterlet
// spec. The klusterlet operator wires these up from a Secret named CredentialSecretName
// in the agent namespace (see manifests/klusterlet/management/*-deployment.yaml).
const (
	// CredentialSecretName is the Secret, in the agent namespace, holding the
	// environment-credential-* secret material.
	CredentialSecretName = "azure-registration-credential"

	// EnvClientSecret holds the service principal client secret
	// (environment-credential-secret). It is both the Secret key and the container
	// environment variable name.
	EnvClientSecret = "AZURE_CLIENT_SECRET"
	// EnvClientCertificatePassword holds the password of the client certificate
	// (environment-credential-certificate). Optional: an unencrypted certificate has none.
	EnvClientCertificatePassword = "AZURE_CLIENT_CERTIFICATE_PASSWORD"
	// EnvClientSendCertificateChain is "true" or "1" to send the certificate chain (x5c)
	// with each token request, needed for subject name/issuer authentication.
	EnvClientSendCertificateChain = "AZURE_CLIENT_SEND_CERTIFICATE_CHAIN"
	// EnvClientCertificatePath is set by the operator to ClientCertificatePath, as a
	// plain environment variable - the certificate itself is a mounted file.
	EnvClientCertificatePath = "AZURE_CLIENT_CERTIFICATE_PATH"

	// ClientCertificateSecretKey is the Secret key holding the PEM or PKCS#12 client
	// certificate (including its private key).
	ClientCertificateSecretKey = "tls.crt"
	// ClientCertificateMountDir is where the certificate key of CredentialSecretName is
	// mounted inside the agent containers.
	ClientCertificateMountDir = "/var/run/secrets/ocm/azure"
	// ClientCertificatePath is the fixed in-container path of the client certificate.
	ClientCertificatePath = ClientCertificateMountDir + "/" + ClientCertificateSecretKey
)

// AzureOption holds the configuration needed for the spoke agent to authenticate to
// the hub apiserver using an Azure AD (Entra ID) access token instead of a client
// certificate.
//
// Credential selects exactly one credential mechanism; there is no fallback between
// mechanisms. Only non-secret, identifying values are flags - secret material is read
// from the environment (see the Env* constants).
type AzureOption struct {
	// Credential selects which Azure credential mechanism is used to obtain a token.
	Credential string

	// TenantID is the Entra ID tenant used to request tokens. Required for the
	// environment-credential-* types; optional for workload-identity-credential, where
	// it falls back to the webhook-injected AZURE_TENANT_ID.
	TenantID string

	// ClientID is the client ID of the identity used to request tokens. Optional for
	// managed-identity-credential (empty selects the system-assigned identity),
	// required for every other credential type.
	ClientID string

	// FederatedTokenFile overrides the projected service account token path used by
	// workload-identity-credential. Falls back to the webhook-injected
	// AZURE_FEDERATED_TOKEN_FILE when empty.
	FederatedTokenFile string

	// TokenAudience is the OAuth2 scope requested for the hub apiserver, for example
	// "<server-app-id>/.default". Defaults to the well-known AKS AAD Server
	// application.
	TokenAudience string

	// ManagedClusterAzureID is the Azure AD object ID (principal ID) of the identity
	// used by this managed cluster to authenticate. It is recorded as an annotation
	// on the ManagedCluster on the hub, so the hub-side driver knows which principal
	// to grant permissions to. Not needed by the get-azure-token exec plugin.
	ManagedClusterAzureID string
}

func NewAzureOption() *AzureOption {
	return &AzureOption{
		TokenAudience: defaultTokenAudience,
	}
}

// AddCredentialFlags adds the flags needed to obtain a token. These are shared by the
// registration agent and the get-azure-token exec plugin.
func (o *AzureOption) AddCredentialFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Credential, "azure-credential", o.Credential,
		fmt.Sprintf("The Azure credential mechanism used to request tokens for the hub apiserver: %q, %q, %q or %q.",
			operatorv1.AzureManagedIdentityCredential, operatorv1.AzureEnvironmentCredentialSecret,
			operatorv1.AzureEnvironmentCredentialCertificate, operatorv1.AzureWorkloadIdentityCredential))
	fs.StringVar(&o.TenantID, "azure-tenant-id", o.TenantID,
		"The Entra ID (Azure AD) tenant ID used to request tokens for the hub apiserver.")
	fs.StringVar(&o.ClientID, "azure-client-id", o.ClientID,
		"The client ID of the identity used to request tokens. Omit with managed-identity-credential to use the system-assigned identity.")
	fs.StringVar(&o.FederatedTokenFile, "azure-federated-token-file", o.FederatedTokenFile,
		"Path to the projected service account token file used by workload-identity-credential. "+
			"Defaults to the AZURE_FEDERATED_TOKEN_FILE environment variable.")
	fs.StringVar(&o.TokenAudience, "azure-token-audience", o.TokenAudience,
		"The OAuth2 scope requested for the hub apiserver, e.g. '<server-app-id>/.default'.")
}

// AddFlags adds every flag the registration agent needs when registration-auth is azure.
func (o *AzureOption) AddFlags(fs *pflag.FlagSet) {
	o.AddCredentialFlags(fs)
	fs.StringVar(&o.ManagedClusterAzureID, "managed-cluster-azure-id", o.ManagedClusterAzureID,
		"The Azure AD object ID (principal ID) of the identity used by this managed cluster.")
}

// ValidateCredential checks that the flags required by the selected credential type are
// set. It does not inspect the environment; see NewAzureCredential.
func (o *AzureOption) ValidateCredential() error {
	if o.TokenAudience == "" {
		return errors.New("azure-token-audience cannot be empty if registration-auth is azure")
	}

	switch operatorv1.AzureCredentialType(o.Credential) {
	case operatorv1.AzureManagedIdentityCredential:
		// ClientID is optional: empty selects the system-assigned identity.
	case operatorv1.AzureEnvironmentCredentialSecret, operatorv1.AzureEnvironmentCredentialCertificate:
		if o.ClientID == "" || o.TenantID == "" {
			return fmt.Errorf("azure-client-id and azure-tenant-id are required for azure-credential %q", o.Credential)
		}
	case operatorv1.AzureWorkloadIdentityCredential:
		if o.ClientID == "" {
			return fmt.Errorf("azure-client-id is required for azure-credential %q", o.Credential)
		}
	case "":
		return errors.New("azure-credential is required if registration-auth is azure")
	default:
		return fmt.Errorf("unsupported azure-credential %q", o.Credential)
	}

	if o.FederatedTokenFile != "" && operatorv1.AzureCredentialType(o.Credential) != operatorv1.AzureWorkloadIdentityCredential {
		return fmt.Errorf("azure-federated-token-file applies only to azure-credential %q", operatorv1.AzureWorkloadIdentityCredential)
	}
	return nil
}

// Validate checks the registration agent's configuration. Beyond the flags, it also
// builds the selected credential once (without requesting a token), so a missing
// Secret-backed environment variable, an unreadable certificate or a missing federated
// token file fails the agent at startup rather than at its first token request.
func (o *AzureOption) Validate() error {
	if o.ManagedClusterAzureID == "" {
		return errors.New("managed-cluster-azure-id is required if registration-auth is azure")
	}
	if err := o.ValidateCredential(); err != nil {
		return err
	}
	if _, err := NewAzureCredential(o); err != nil {
		return err
	}
	return nil
}
