package importer

import (
	"context"
	"fmt"

	"github.com/ghodss/yaml"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/utils/ptr"

	v1 "open-cluster-management.io/api/cluster/v1"
	sdkhelpers "open-cluster-management.io/sdk-go/pkg/helpers"

	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
)

const imagePullSecretName = "open-cluster-management-image-pull-credentials"

func RenderBootstrapHubKubeConfig(
	kubeClient kubernetes.Interface, apiServerURL, bootstrapSA string) KlusterletConfigRenderer {
	return func(ctx context.Context, _ *v1.ManagedCluster, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		// get bootstrap token
		bootstrapSANamespace, bootstrapSAName, err := cache.SplitMetaNamespaceKey(bootstrapSA)
		if err != nil {
			return config, err
		}
		tr, err := kubeClient.CoreV1().
			ServiceAccounts(bootstrapSANamespace).
			CreateToken(ctx, bootstrapSAName, &authv1.TokenRequest{
				Spec: authv1.TokenRequestSpec{
					// token expired in 24 hours
					ExpirationSeconds: ptr.To[int64](24 * 3600),
				},
			}, metav1.CreateOptions{})
		if err != nil {
			return config, fmt.Errorf(
				"failed to get token from sa %s/%s: %v", bootstrapSANamespace, bootstrapSAName, err)
		}

		// get apisever url
		url := apiServerURL
		if len(url) == 0 {
			url, err = sdkhelpers.GetAPIServer(kubeClient)
			if err != nil {
				return config, err
			}
		}

		// get cabundle
		ca, err := sdkhelpers.GetCACert(kubeClient)
		if err != nil {
			return config, err
		}

		clientConfig := clientcmdapiv1.Config{
			// Define a cluster stanza based on the bootstrap kubeconfig.
			Clusters: []clientcmdapiv1.NamedCluster{
				{
					Name: "hub",
					Cluster: clientcmdapiv1.Cluster{
						Server:                   url,
						CertificateAuthorityData: ca,
					},
				},
			},
			// Define auth based on the obtained client cert.
			AuthInfos: []clientcmdapiv1.NamedAuthInfo{
				{
					Name: "bootstrap",
					AuthInfo: clientcmdapiv1.AuthInfo{
						Token: tr.Status.Token,
					},
				},
			},
			// Define a context that connects the auth info and cluster, and set it as the default
			Contexts: []clientcmdapiv1.NamedContext{
				{
					Name: "bootstrap",
					Context: clientcmdapiv1.Context{
						Cluster:   "hub",
						AuthInfo:  "bootstrap",
						Namespace: "default",
					},
				},
			},
			CurrentContext: "bootstrap",
		}

		bootstrapConfigBytes, err := yaml.Marshal(clientConfig)
		if err != nil {
			return config, err
		}

		config.BootstrapHubKubeConfig = string(bootstrapConfigBytes)
		return config, nil
	}
}

func RenderImage(image string) KlusterletConfigRenderer {
	return func(ctx context.Context, _ *v1.ManagedCluster, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		if len(image) == 0 {
			return config, nil
		}
		config.Images.Overrides = chart.Overrides{
			OperatorImage: image,
		}
		return config, nil
	}
}

func RenderImagePullSecret(kubeClient kubernetes.Interface, namespace string) KlusterletConfigRenderer {
	return func(ctx context.Context, _ *v1.ManagedCluster, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, imagePullSecretName, metav1.GetOptions{})
		switch {
		case errors.IsNotFound(err):
			return config, nil
		case err != nil:
			return config, err
		}

		if len(secret.Data[corev1.DockerConfigJsonKey]) == 0 {
			return config, nil
		}

		config.Images.ImageCredentials.CreateImageCredentials = true
		config.Images.ImageCredentials.DockerConfigJson = string(secret.Data[corev1.DockerConfigJsonKey])
		return config, nil
	}
}

func RenderFromConfigSecret(kubeClient kubernetes.Interface) KlusterletConfigRenderer {
	return func(ctx context.Context, cluster *v1.ManagedCluster,
		config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		if cluster == nil {
			return nil, fmt.Errorf("cluster must not be nil")
		}

		configSecret, err := kubeClient.CoreV1().Secrets(cluster.Name).
			Get(ctx, clusterImportConfigSecret, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		valuesRaw := configSecret.Data[valuesYamlKey]
		if len(valuesRaw) == 0 {
			return nil, fmt.Errorf("no values found in secret %s/%s in namespace %s",
				clusterImportConfigSecret, valuesYamlKey, cluster.Name)
		}

		err = yaml.Unmarshal(valuesRaw, config)
		if err != nil {
			return nil, fmt.Errorf("the values in the cluster import config secret is invalid: %v", err)
		}

		return config, nil
	}
}
