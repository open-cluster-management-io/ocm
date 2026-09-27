package azure_auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

// NewAzureCredential builds the single azcore.TokenCredential selected by
// opt.Credential. There is deliberately no chain or fallback between credential
// types: a misconfigured credential must fail loudly instead of silently
// authenticating as a different identity.
//
//   - managed-identity-credential: the node's managed identity, user-assigned when
//     opt.ClientID is set and system-assigned otherwise.
//   - environment-credential-secret: a service principal client secret read from
//     AZURE_CLIENT_SECRET.
//   - environment-credential-certificate: a service principal certificate read from the
//     file at AZURE_CLIENT_CERTIFICATE_PATH, decrypted with the optional
//     AZURE_CLIENT_CERTIFICATE_PASSWORD, sending the x5c chain when
//     AZURE_CLIENT_SEND_CERTIFICATE_CHAIN is true.
//   - workload-identity-credential: Workload Identity Federation using the projected
//     service account token.
//
// The environment-credential-* types read the same environment variables
// azidentity.EnvironmentCredential does, but only the ones belonging to the selected
// type, so a stray variable for another type can never change which credential is
// used. Tenant and client IDs always come from opt, not the environment.
func NewAzureCredential(opt *AzureOption) (azcore.TokenCredential, error) {
	if err := opt.ValidateCredential(); err != nil {
		return nil, err
	}

	switch operatorv1.AzureCredentialType(opt.Credential) {
	case operatorv1.AzureManagedIdentityCredential:
		miOptions := &azidentity.ManagedIdentityCredentialOptions{}
		if opt.ClientID != "" {
			miOptions.ID = azidentity.ClientID(opt.ClientID)
		}
		cred, err := azidentity.NewManagedIdentityCredential(miOptions)
		if err != nil {
			return nil, err
		}
		return &managedIdentityCredential{cred: cred}, nil

	case operatorv1.AzureEnvironmentCredentialSecret:
		secret := os.Getenv(EnvClientSecret)
		if secret == "" {
			return nil, fmt.Errorf("%s is not set; for azure-credential %q it must be provided from key %q of Secret %q",
				EnvClientSecret, opt.Credential, EnvClientSecret, CredentialSecretName)
		}
		return azidentity.NewClientSecretCredential(opt.TenantID, opt.ClientID, secret, nil)

	case operatorv1.AzureEnvironmentCredentialCertificate:
		certPath := os.Getenv(EnvClientCertificatePath)
		if certPath == "" {
			return nil, fmt.Errorf("%s is not set; for azure-credential %q it must point at the certificate mounted from Secret %q",
				EnvClientCertificatePath, opt.Credential, CredentialSecretName)
		}
		certData, err := os.ReadFile(certPath) //#nosec G304 -- path is the operator-configured certificate mount
		if err != nil {
			return nil, fmt.Errorf("failed to read azure client certificate %q: %w", certPath, err)
		}
		var password []byte
		if v := os.Getenv(EnvClientCertificatePassword); v != "" {
			password = []byte(v)
		}
		certs, key, err := azidentity.ParseCertificates(certData, password)
		if err != nil {
			return nil, fmt.Errorf("failed to parse azure client certificate %q: %w", certPath, err)
		}
		v := strings.ToLower(os.Getenv(EnvClientSendCertificateChain))
		return azidentity.NewClientCertificateCredential(opt.TenantID, opt.ClientID, certs, key,
			&azidentity.ClientCertificateCredentialOptions{SendCertificateChain: v == "1" || v == "true"})

	case operatorv1.AzureWorkloadIdentityCredential:
		return azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
			ClientID:      opt.ClientID,
			TenantID:      opt.TenantID,
			TokenFilePath: opt.FederatedTokenFile,
		})
	}

	// Unreachable: ValidateCredential rejects any other value.
	return nil, fmt.Errorf("unsupported azure-credential %q", opt.Credential)
}

// managedIdentityCredential wraps a ManagedIdentityCredential so that failing to reach
// the managed identity endpoint at all is reported distinctly from Azure AD rejecting
// the identity. The endpoint is the Azure Instance Metadata Service
// (169.254.169.254), which only exists inside Azure's network; outside of it this
// credential can never work, regardless of any other configuration.
type managedIdentityCredential struct {
	cred azcore.TokenCredential
}

func (m *managedIdentityCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	token, err := m.cred.GetToken(ctx, opts)
	if err == nil {
		return token, nil
	}
	var authErr *azidentity.AuthenticationFailedError
	if errors.As(err, &authErr) {
		// The endpoint responded; Azure AD rejected the request.
		return token, err
	}
	return token, fmt.Errorf("managed identity endpoint unreachable: managed-identity-credential requires the "+
		"Azure Instance Metadata Service (169.254.169.254), which is only available on Azure compute such as an AKS node: %w", err)
}
