package helpers

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
)

type TokenGetterFunc func() ([]byte, []byte, error)

// SATokenGetter get the saToken of target sa. If there is not secrets in the sa, use the tokenrequest to get a token.
func SATokenGetter(ctx context.Context, saName, saNamespace string, saClient kubernetes.Interface) TokenGetterFunc {
	return func() ([]byte, []byte, error) {
		// get the service account
		sa, err := saClient.CoreV1().ServiceAccounts(saNamespace).Get(ctx, saName, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}

		for _, secret := range sa.Secrets {
			// get the token secret
			tokenSecretName := secret.Name

			// get the token secret
			tokenSecret, err := saClient.CoreV1().Secrets(saNamespace).Get(ctx, tokenSecretName, metav1.GetOptions{})
			if err != nil {
				return nil, nil, err
			}

			if tokenSecret.Type != corev1.SecretTypeServiceAccountToken {
				continue
			}

			saToken, ok := tokenSecret.Data["token"]
			if !ok {
				continue
			}

			return saToken, nil, nil
		}

		// 8640 hour
		tr, err := saClient.CoreV1().ServiceAccounts(saNamespace).
			CreateToken(ctx, saName, &authv1.TokenRequest{
				Spec: authv1.TokenRequestSpec{
					ExpirationSeconds: pointer.Int64Ptr(8640 * 3600),
				},
			}, metav1.CreateOptions{})
		if err != nil {
			return nil, nil, err
		}
		expiration, err := tr.Status.ExpirationTimestamp.MarshalText()
		if err != nil {
			return nil, nil, nil
		}
		return []byte(tr.Status.Token), expiration, nil
	}
}

func SyncKubeConfigSecret(ctx context.Context, secretName, secretNamespace, kubeconfigPath string, templateKubeconfig *rest.Config, secretClient coreclientv1.SecretsGetter, tokenGetter TokenGetterFunc, recorder events.Recorder) error {
	secret, err := secretClient.Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return applyKubeconfigSecret(ctx, templateKubeconfig, secretName, secretNamespace, kubeconfigPath, secretClient, tokenGetter, recorder)
	case err != nil:
		return err
	}

	if tokenValid(secret) && clusterInfoNotChanged(secret, templateKubeconfig) {
		return nil
	}

	return applyKubeconfigSecret(ctx, templateKubeconfig, secretName, secretNamespace, kubeconfigPath, secretClient, tokenGetter, recorder)
}

func tokenValid(secret *corev1.Secret) bool {
	_, tokenFound := secret.Data["token"]
	expiration, expirationFound := secret.Data["expiration"]

	if !tokenFound {
		return false
	}

	if expirationFound {
		expirationTime, err := time.Parse(time.RFC3339, string(expiration))
		if err != nil {
			return false
		}

		now := metav1.Now()
		refreshThreshold := 8640 * time.Hour / 5
		lifetime := expirationTime.Sub(now.Time)
		if lifetime < refreshThreshold {
			return false
		}
	}

	return true
}

func clusterInfoNotChanged(secret *corev1.Secret, templateKubeconfig *rest.Config) bool {
	// check if the templateKubeconfig is changed
	templateCluster, err := assembleClusterConfig(templateKubeconfig)
	if err != nil {
		klog.Infof("Assemble template cluster config error: %s", err)
		return false
	}

	saKubeconfig, kubeconfigFound := secret.Data["kubeconfig"]
	if !kubeconfigFound {
		return false
	}
	kubeconfig, err := clientcmd.Load(saKubeconfig)
	if err != nil {
		klog.Infof("Load kubeconfig error: %s", err)
		return false
	}
	cluster, ok := kubeconfig.Clusters["cluster"]
	if !ok {
		klog.Infof("Cluster not found")
		return false
	}

	if cluster.Server != templateCluster.Server {
		klog.Infof("Cluster host changed from %s to %s", cluster.Server, templateCluster.Server)
		return false
	}
	if bytes.Compare(cluster.CertificateAuthorityData, templateCluster.CertificateAuthorityData) != 0 {
		klog.Infof("Cluster certificate authority data changed")
		return false
	}
	if cluster.InsecureSkipTLSVerify != templateCluster.InsecureSkipTLSVerify {
		klog.Infof("Cluster insecureSkipTLSVerify changed from %v to %v",
			cluster.InsecureSkipTLSVerify, templateCluster.InsecureSkipTLSVerify)
		return false
	}

	return true
}

// applyKubeconfigSecret would render saToken to a secret.
func applyKubeconfigSecret(ctx context.Context, templateKubeconfig *rest.Config, secretName, secretNamespace, kubeconfigPath string, secretClient coreclientv1.SecretsGetter, tokenGetter TokenGetterFunc, recorder events.Recorder) error {

	token, expiration, err := tokenGetter()
	if err != nil {
		return err
	}

	c, err := assembleClusterConfig(templateKubeconfig)
	if err != nil {
		return err
	}

	kubeconfigContent, err := clientcmd.Write(clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster": c,
		},
		Contexts: map[string]*clientcmdapi.Context{
			"context": {
				Cluster:  "cluster",
				AuthInfo: "user",
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"user": {
				TokenFile: filepath.Join(filepath.Dir(kubeconfigPath), "token"),
			},
		},
		CurrentContext: "context",
	})
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			"kubeconfig": kubeconfigContent,
			"token":      token,
		},
	}

	if expiration != nil {
		secret.Data["expiration"] = expiration
	}

	_, _, err = resourceapply.ApplySecret(ctx, secretClient, recorder, secret)
	return err
}

func assembleClusterConfig(templateKubeconfig *rest.Config) (*clientcmdapi.Cluster, error) {
	var c *clientcmdapi.Cluster
	if len(templateKubeconfig.CAData) != 0 {
		c = &clientcmdapi.Cluster{
			Server:                   templateKubeconfig.Host,
			CertificateAuthorityData: templateKubeconfig.CAData,
		}
	} else if len(templateKubeconfig.CAFile) != 0 {
		caData, err := os.ReadFile(templateKubeconfig.CAFile)
		if err != nil {
			return nil, err
		}
		c = &clientcmdapi.Cluster{
			Server:                   templateKubeconfig.Host,
			CertificateAuthorityData: caData,
		}
	} else {
		c = &clientcmdapi.Cluster{
			Server:                templateKubeconfig.Host,
			InsecureSkipTLSVerify: true,
		}
	}
	return c, nil
}
