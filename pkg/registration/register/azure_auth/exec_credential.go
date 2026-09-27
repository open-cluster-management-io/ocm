package azure_auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/apis/clientauthentication/v1beta1"
)

// execCredentialAPIVersion must match the APIVersion set on the "exec" AuthInfo
// built by AzureAuthDriver.BuildKubeConfigFromTemplate.
const execCredentialAPIVersion = "client.authentication.k8s.io/v1beta1"

// GetTokenExecCredential fetches an Azure AD access token for opt.TokenAudience using
// the credential NewAzureCredential selects, and returns it wrapped as a client-go ExecCredential (see
// https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins),
// the format client-go expects an "exec" kubeconfig AuthInfo's command to print.
func GetTokenExecCredential(ctx context.Context, opt *AzureOption) (*v1beta1.ExecCredential, error) {
	cred, err := NewAzureCredential(opt)
	if err != nil {
		return nil, fmt.Errorf("failed to build azure credential: %w", err)
	}

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{opt.TokenAudience}})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire an azure ad token for scope %q: %w", opt.TokenAudience, err)
	}

	expiry := metav1.NewTime(token.ExpiresOn)
	return &v1beta1.ExecCredential{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExecCredential",
			APIVersion: execCredentialAPIVersion,
		},
		Status: &v1beta1.ExecCredentialStatus{
			Token:               token.Token,
			ExpirationTimestamp: &expiry,
		},
	}, nil
}

// NewGetTokenCommand returns a cobra command that fetches an Azure AD access token and
// prints it to stdout as a client-go ExecCredential. It is registered as a subcommand
// of the registration, registration-operator and work binaries and invoked as an "exec" credential plugin from the
// kubeconfig AzureAuthDriver.BuildKubeConfigFromTemplate builds - client-go re-runs it
// on every request whose cached token has expired.
func NewGetTokenCommand() *cobra.Command {
	opt := NewAzureOption()
	cmd := &cobra.Command{
		Use:   "get-azure-token",
		Short: "Print an Azure AD access token as a client-go ExecCredential",
		Long: "Fetches an Azure AD access token using the credential selected by --azure-credential " +
			"and prints it to stdout as a client-go ExecCredential. Intended to be invoked as a " +
			"kubeconfig exec credential plugin, not run directly.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			execCredential, err := GetTokenExecCredential(context.Background(), opt)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(os.Stdout)
			return encoder.Encode(execCredential)
		},
	}

	fs := pflag.NewFlagSet("get-azure-token", pflag.ExitOnError)
	opt.AddCredentialFlags(fs)
	cmd.Flags().AddFlagSet(fs)

	return cmd
}
