package importer

import (
	"context"
	"testing"

	"github.com/ghodss/yaml"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
)

func TestRenderBootstrapHubKubeConfig(t *testing.T) {
	cases := []struct {
		name         string
		objects      []runtime.Object
		apiserverURL string
		expectedURL  string
	}{
		{
			name:         "render apiserver from input",
			apiserverURL: "https://127.0.0.1:6443",
			expectedURL:  "https://127.0.0.1:6443",
		},
		{
			name: "render apiserver from cluster-info",
			objects: []runtime.Object{
				func() *corev1.ConfigMap {
					config := clientcmdapiv1.Config{
						// Define a cluster stanza based on the bootstrap kubeconfig.
						Clusters: []clientcmdapiv1.NamedCluster{
							{
								Name: "hub",
								Cluster: clientcmdapiv1.Cluster{
									Server: "https://test",
								},
							},
						},
					}
					bootstrapConfigBytes, _ := yaml.Marshal(config)
					return &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-info",
							Namespace: "kube-public",
						},
						Data: map[string]string{
							"kubeconfig": string(bootstrapConfigBytes),
						},
					}
				}(),
			},
			expectedURL: "https://test",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := kubefake.NewClientset(c.objects...)
			client.PrependReactor("create", "serviceaccounts/token",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					act, ok := action.(clienttesting.CreateActionImpl)
					if !ok {
						return false, nil, nil
					}
					tokenReq, ok := act.Object.(*authenticationv1.TokenRequest)
					if !ok {
						return false, nil, nil
					}
					tokenReq.Status.Token = "token"
					return true, tokenReq, nil
				},
			)
			config := &chart.KlusterletChartConfig{}
			config, err := RenderBootstrapHubKubeConfig(client, c.apiserverURL)(context.TODO(), config)
			if err != nil {
				t.Fatalf("failed to render bootstrap hub kubeconfig: %v", err)
			}
			kConfig, err := clientcmd.NewClientConfigFromBytes([]byte(config.BootstrapHubKubeConfig))
			if err != nil {
				t.Fatalf("failed to load bootstrap hub kubeconfig: %v", err)
			}
			rawConfig, err := kConfig.RawConfig()
			if err != nil {
				t.Fatalf("failed to load bootstrap hub kubeconfig: %v", err)
			}
			cluster := rawConfig.Contexts[rawConfig.CurrentContext].Cluster
			if rawConfig.Clusters[cluster].Server != c.expectedURL {
				t.Errorf("apiserver is not rendered correctly")
			}
		})
	}
}

func TestRenderImage(t *testing.T) {
	cases := []struct {
		name          string
		image         string
		expectedImage string
	}{
		{
			name: "image is not set",
		},
		{
			name:          "image is set",
			image:         "test:latest",
			expectedImage: "test:latest",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := &chart.KlusterletChartConfig{}
			render := RenderImage(c.image)
			config, err := render(context.TODO(), config)
			if err != nil {
				t.Fatalf("failed to render image: %v", err)
			}
			if config.Images.Overrides.OperatorImage != c.expectedImage {
				t.Errorf("expected: %s, got: %s", c.expectedImage, config.Images.Overrides.OperatorImage)
			}
		})
	}
}

func TestRenderImagePullSecret(t *testing.T) {
	cases := []struct {
		name     string
		secrets  []runtime.Object
		expected string
	}{
		{
			name:    "not image secret",
			secrets: []runtime.Object{},
		},
		{
			name: "secert has not correct key",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imagePullSecretName,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"docker": []byte("test"),
					},
				},
			},
		},
		{
			name: "secert has correct key",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imagePullSecretName,
						Namespace: "test",
					},
					Data: map[string][]byte{
						corev1.DockerConfigJsonKey: []byte("test"),
					},
				},
			},
			expected: "test",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := kubefake.NewClientset(c.secrets...)
			config := &chart.KlusterletChartConfig{}
			render := RenderImagePullSecret(client, "test")
			config, err := render(context.TODO(), config)
			if err != nil {
				t.Fatalf("failed to render image: %v", err)
			}
			if config.Images.ImageCredentials.DockerConfigJson != c.expected {
				t.Errorf("expected: %s, got: %s", c.expected, config.Images.ImageCredentials.DockerConfigJson)
			}
		})
	}
}
