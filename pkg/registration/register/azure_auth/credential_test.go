package azure_auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	testClientID = "33333333-3333-3333-3333-333333333333"
	testTenantID = "44444444-4444-4444-4444-444444444444"
)

// azureEnvVars are every environment variable any credential type may read. Each test
// clears them all first, so a developer's own Azure environment can't leak in.
var azureEnvVars = []string{
	EnvClientSecret, EnvClientCertificatePath, EnvClientCertificatePassword, EnvClientSendCertificateChain,
	"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_FEDERATED_TOKEN_FILE", "AZURE_AUTHORITY_HOST",
}

// clearAzureEnv unsets (not just empties) each variable - azidentity treats a variable
// that is set to "" as present. t.Setenv first registers the restore.
func clearAzureEnv(t *testing.T) {
	for _, v := range azureEnvVars {
		t.Setenv(v, "")
		if err := os.Unsetenv(v); err != nil {
			t.Fatal(err)
		}
	}
}

func newOption(credential operatorv1.AzureCredentialType, mutate func(o *AzureOption)) *AzureOption {
	o := NewAzureOption()
	o.Credential = string(credential)
	o.ManagedClusterAzureID = testAzureID
	if mutate != nil {
		mutate(o)
	}
	return o
}

func withClient(o *AzureOption)       { o.ClientID = testClientID }
func withClientTenant(o *AzureOption) { o.ClientID, o.TenantID = testClientID, testTenantID }

func TestValidateCredential(t *testing.T) {
	cases := []struct {
		name      string
		opt       *AzureOption
		expectErr string
	}{
		{"managed identity, system-assigned", newOption(operatorv1.AzureManagedIdentityCredential, nil), ""},
		{"managed identity, user-assigned", newOption(operatorv1.AzureManagedIdentityCredential, withClient), ""},
		{"secret with client and tenant", newOption(operatorv1.AzureEnvironmentCredentialSecret, withClientTenant), ""},
		{"secret without tenant", newOption(operatorv1.AzureEnvironmentCredentialSecret, withClient),
			"azure-client-id and azure-tenant-id are required"},
		{"secret without client", newOption(operatorv1.AzureEnvironmentCredentialSecret,
			func(o *AzureOption) { o.TenantID = testTenantID }), "azure-client-id and azure-tenant-id are required"},
		{"certificate with client and tenant", newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant), ""},
		{"certificate without tenant", newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClient),
			"azure-client-id and azure-tenant-id are required"},
		{"workload identity with client", newOption(operatorv1.AzureWorkloadIdentityCredential, withClient), ""},
		{"workload identity with token file", newOption(operatorv1.AzureWorkloadIdentityCredential,
			func(o *AzureOption) { withClient(o); o.FederatedTokenFile = "/token" }), ""},
		{"workload identity without client", newOption(operatorv1.AzureWorkloadIdentityCredential, nil),
			"azure-client-id is required"},
		{"token file with another credential", newOption(operatorv1.AzureManagedIdentityCredential,
			func(o *AzureOption) { o.FederatedTokenFile = "/token" }), "applies only to azure-credential"},
		{"no credential", newOption("", nil), "azure-credential is required"},
		{"unknown credential", newOption("default-azure-credential", nil), "unsupported azure-credential"},
		{"empty token audience", newOption(operatorv1.AzureManagedIdentityCredential,
			func(o *AzureOption) { o.TokenAudience = "" }), "azure-token-audience cannot be empty"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertErr(t, c.opt.ValidateCredential(), c.expectErr)
		})
	}
}

func TestValidate(t *testing.T) {
	clearAzureEnv(t)

	t.Run("requires the managed cluster azure id", func(t *testing.T) {
		opt := newOption(operatorv1.AzureManagedIdentityCredential, func(o *AzureOption) { o.ManagedClusterAzureID = "" })
		assertErr(t, opt.Validate(), "managed-cluster-azure-id is required")
	})
	t.Run("fails at startup when the credential cannot be built", func(t *testing.T) {
		opt := newOption(operatorv1.AzureEnvironmentCredentialSecret, withClientTenant)
		assertErr(t, opt.Validate(), EnvClientSecret+" is not set")
	})
	t.Run("valid", func(t *testing.T) {
		assertErr(t, newOption(operatorv1.AzureManagedIdentityCredential, nil).Validate(), "")
	})
}

func TestNewAzureCredential(t *testing.T) {
	dir := t.TempDir()
	certPath := writeTestCertificate(t, dir)
	tokenPath := filepath.Join(dir, "federated-token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		opt        *AzureOption
		env        map[string]string
		expectType azcore.TokenCredential
		expectErr  string
	}{
		{
			name:       "managed identity",
			opt:        newOption(operatorv1.AzureManagedIdentityCredential, nil),
			expectType: &managedIdentityCredential{},
		},
		{
			name:       "secret",
			opt:        newOption(operatorv1.AzureEnvironmentCredentialSecret, withClientTenant),
			env:        map[string]string{EnvClientSecret: "s3cr3t"},
			expectType: &azidentity.ClientSecretCredential{},
		},
		{
			name:      "secret missing from the environment",
			opt:       newOption(operatorv1.AzureEnvironmentCredentialSecret, withClientTenant),
			expectErr: EnvClientSecret + " is not set",
		},
		{
			name:       "certificate",
			opt:        newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant),
			env:        map[string]string{EnvClientCertificatePath: certPath, EnvClientSendCertificateChain: "true"},
			expectType: &azidentity.ClientCertificateCredential{},
		},
		{
			// A stray variable belonging to another credential type must not change
			// which credential is built - unlike azidentity.EnvironmentCredential,
			// which prefers a client secret whenever one is present.
			name:       "certificate ignores a stray client secret",
			opt:        newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant),
			env:        map[string]string{EnvClientCertificatePath: certPath, EnvClientSecret: "stray"},
			expectType: &azidentity.ClientCertificateCredential{},
		},
		{
			name:      "certificate path missing from the environment",
			opt:       newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant),
			expectErr: EnvClientCertificatePath + " is not set",
		},
		{
			name:      "certificate file unreadable",
			opt:       newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant),
			env:       map[string]string{EnvClientCertificatePath: filepath.Join(dir, "missing.crt")},
			expectErr: "failed to read azure client certificate",
		},
		{
			name:      "certificate file not a certificate",
			opt:       newOption(operatorv1.AzureEnvironmentCredentialCertificate, withClientTenant),
			env:       map[string]string{EnvClientCertificatePath: tokenPath},
			expectErr: "failed to parse azure client certificate",
		},
		{
			name: "workload identity",
			opt: newOption(operatorv1.AzureWorkloadIdentityCredential, func(o *AzureOption) {
				withClientTenant(o)
				o.FederatedTokenFile = tokenPath
			}),
			expectType: &azidentity.WorkloadIdentityCredential{},
		},
		{
			name:       "workload identity from webhook-injected environment",
			opt:        newOption(operatorv1.AzureWorkloadIdentityCredential, withClient),
			env:        map[string]string{"AZURE_TENANT_ID": testTenantID, "AZURE_FEDERATED_TOKEN_FILE": tokenPath},
			expectType: &azidentity.WorkloadIdentityCredential{},
		},
		{
			name:      "workload identity without a token file",
			opt:       newOption(operatorv1.AzureWorkloadIdentityCredential, withClientTenant),
			expectErr: "token file",
		},
		{
			name:      "invalid option",
			opt:       newOption(operatorv1.AzureWorkloadIdentityCredential, nil),
			expectErr: "azure-client-id is required",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearAzureEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}

			cred, err := NewAzureCredential(c.opt)
			assertErr(t, err, c.expectErr)
			if c.expectErr != "" {
				return
			}
			if got, want := fmt.Sprintf("%T", cred), fmt.Sprintf("%T", c.expectType); got != want {
				t.Errorf("expected a %s, got a %s", want, got)
			}
		})
	}
}

type fakeCredential struct{ err error }

func (f *fakeCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "token"}, f.err
}

func TestManagedIdentityCredentialErrors(t *testing.T) {
	t.Run("success passes through", func(t *testing.T) {
		token, err := (&managedIdentityCredential{cred: &fakeCredential{}}).GetToken(context.TODO(), policy.TokenRequestOptions{})
		if err != nil || token.Token != "token" {
			t.Errorf("expected the token, got %v, %v", token, err)
		}
	})
	t.Run("authentication failure passes through", func(t *testing.T) {
		authErr := &azidentity.AuthenticationFailedError{}
		_, err := (&managedIdentityCredential{cred: &fakeCredential{err: authErr}}).GetToken(context.TODO(), policy.TokenRequestOptions{})
		if err != authErr { //nolint:errorlint // asserting the identical error is returned unwrapped
			t.Errorf("expected the authentication error unchanged, got %v", err)
		}
	})
	t.Run("unreachable endpoint is labeled", func(t *testing.T) {
		cause := errors.New("dial tcp 169.254.169.254:80: connect: no route to host")
		_, err := (&managedIdentityCredential{cred: &fakeCredential{err: cause}}).GetToken(context.TODO(), policy.TokenRequestOptions{})
		if err == nil || !strings.Contains(err.Error(), "managed identity endpoint unreachable") || !errors.Is(err, cause) {
			t.Errorf("expected a labeled error wrapping the cause, got %v", err)
		}
	})
}

func TestBuildKubeConfigFromTemplate(t *testing.T) {
	opt := newOption(operatorv1.AzureEnvironmentCredentialSecret, withClientTenant)
	driver := NewAzureAuthDriver(opt, register.SecretOption{ClusterName: "cluster1"})

	kubeConfig := driver.BuildKubeConfigFromTemplate(&clientcmdapi.Config{})
	exec := kubeConfig.AuthInfos[register.DefaultKubeConfigAuth].Exec
	if exec == nil || exec.Command != execCommand {
		t.Fatalf("expected an exec plugin running %q, got %+v", execCommand, exec)
	}

	want := []string{
		"get-azure-token",
		"--azure-credential=environment-credential-secret",
		"--azure-tenant-id=" + testTenantID,
		"--azure-client-id=" + testClientID,
		"--azure-token-audience=" + defaultTokenAudience,
	}
	if strings.Join(exec.Args, " ") != strings.Join(want, " ") {
		t.Errorf("expected args %v, got %v", want, exec.Args)
	}
	if len(exec.Env) != 0 {
		t.Errorf("expected no secret material in the persisted exec config, got env %v", exec.Env)
	}
}

func assertErr(t *testing.T, err error, expect string) {
	t.Helper()
	switch {
	case expect == "" && err != nil:
		t.Fatalf("expected no error, got %v", err)
	case expect != "" && (err == nil || !strings.Contains(err.Error(), expect)):
		t.Fatalf("expected error containing %q, got %v", expect, err)
	}
}

// writeTestCertificate writes a self-signed PEM certificate with its RSA private key
// (Azure AD only accepts RSA), the format the azure-registration-credential Secret's tls.crt key carries.
func writeTestCertificate(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "azure-auth-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	data := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...)
	path := filepath.Join(dir, ClientCertificateSecretKey)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
