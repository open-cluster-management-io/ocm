package helpers

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// EnsureSAToken get the saToken of target sa and ensure it was rendered as expected.
// A usage example combined with RenderToKubeconfigSecert would be like the following:
// ...
// saName := clusterManagerName+"-registration-controller-sa"
// secretName := saName+"-kubeconfig"
// err := helpers.EnsureSAToken(ctx, saName, clustermanagerNamespace, amdinKubeClient,
// 	helpers.RenderToKubeconfigSecret(secretName, clustermanagerNamespace, AdminKubeconfig, kubeClient, recorder)) // kubeClient used to create secret; normally recorder is from controller object.
// if err != nil {
// 	return err
// }
// ...
func EnsureSAToken(ctx context.Context, saName, saNamespace string, client kubernetes.Interface, renderSAToken func([]byte) error) error {
	// get the service account
	sa, err := client.CoreV1().ServiceAccounts(saNamespace).Get(ctx, saName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(sa.Secrets) == 0 {
		return fmt.Errorf("token secret for %s not exist yet", saName)
	}

	for _, secret := range sa.Secrets {
		// get the token secret
		tokenSecretName := secret.Name

		tokenSecret, err := client.CoreV1().Secrets(saNamespace).Get(ctx, tokenSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if tokenSecret.Type != corev1.SecretTypeServiceAccountToken {
			continue
		}

		saToken, ok := tokenSecret.Data["token"]
		if !ok {
			return fmt.Errorf("no token in data for secret %s", tokenSecretName)
		}

		return renderSAToken(saToken)
	}

	return fmt.Errorf("no token secret for this service account %s", sa.Name)
}

// RenderToKubeconfigSecret would render saToken to a secret.
func RenderToKubeconfigSecret(secretName, secretNamespace string, templateKubeconfig *rest.Config, client coreclientv1.SecretsGetter, recorder events.Recorder) func([]byte) error {
	return func(saToken []byte) error {
		kubeconfigContent, err := clientcmd.Write(clientcmdapi.Config{
			Kind:       "Config",
			APIVersion: "v1",
			Clusters: map[string]*clientcmdapi.Cluster{
				"cluster": {
					Server:                   templateKubeconfig.Host,
					CertificateAuthorityData: templateKubeconfig.CAData,
				},
			},
			Contexts: map[string]*clientcmdapi.Context{
				"context": {
					Cluster:  "cluster",
					AuthInfo: "user",
				},
			},
			AuthInfos: map[string]*clientcmdapi.AuthInfo{
				"user": {
					Token: string(saToken),
				},
			},
			CurrentContext: "context",
		})
		if err != nil {
			return err
		}
		_, _, err = resourceapply.ApplySecret(client, recorder, &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: secretNamespace,
				Name:      secretName,
			},
			Data: map[string][]byte{
				"kubeconfig": kubeconfigContent,
			},
		})
		return err
	}
}
