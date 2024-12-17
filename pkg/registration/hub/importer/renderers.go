package importer

import (
	"context"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/utils/ptr"

	sdkhelpers "open-cluster-management.io/sdk-go/pkg/helpers"

	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
)

func RenderBootstrapHubKubeConfig(
	kubeClient kubernetes.Interface, apiServerURL string) KlusterletConfigRenderer {
	return func(ctx context.Context, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		// get bootstrap token
		tr, err := kubeClient.CoreV1().
			ServiceAccounts(operatorNamesapce).
			CreateToken(ctx, bootstrapSA, &authv1.TokenRequest{
				Spec: authv1.TokenRequestSpec{
					// token expired in 1 hour
					ExpirationSeconds: ptr.To[int64](3600),
				},
			}, metav1.CreateOptions{})
		if err != nil {
			return config, fmt.Errorf(
				"failed to get token from sa %s/%s: %v", operatorNamesapce, bootstrapSA, err)
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
	return func(ctx context.Context, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error) {
		imageArray := strings.Split(image, ":")
		if len(imageArray) != 2 {
			return config, nil
		}
		config.Images.Registry = imageArray[0]
		config.Images.Tag = imageArray[1]
		return config, nil
	}
}
