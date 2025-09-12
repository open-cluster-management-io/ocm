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

	v1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
	reghelpers "open-cluster-management.io/ocm/pkg/registration/helpers"
)

func TestRenderBootstrapHubKubeConfig(t *testing.T) {
	cases := []struct {
		name         string
		objects      []runtime.Object
		apiserverURL string
		bootstrapSA  string
		expectedURL  string
	}{
		{
			name:         "render apiserver from input",
			apiserverURL: "https://127.0.0.1:6443",
			expectedURL:  "https://127.0.0.1:6443",
			bootstrapSA:  "open-cluster-management/bootstrap-sa",
		},
		{
			name:        "render apiserver from cluster-info",
			bootstrapSA: "open-cluster-management/bootstrap-sa",
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
			config, err := RenderBootstrapHubKubeConfig(client, c.apiserverURL, c.bootstrapSA)(context.TODO(), nil, config)
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
			config, err := render(context.TODO(), nil, config)
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
			config, err := render(context.TODO(), nil, config)
			if err != nil {
				t.Fatalf("failed to render image: %v", err)
			}
			if config.Images.ImageCredentials.DockerConfigJson != c.expected {
				t.Errorf("expected: %s, got: %s", c.expected, config.Images.ImageCredentials.DockerConfigJson)
			}
		})
	}
}

func TestRenderFromConfigSecret(t *testing.T) {
	cases := []struct {
		name         string
		clusterName  string
		secret       *corev1.Secret
		expectErr    bool
		expectConfig *chart.KlusterletChartConfig
	}{
		{
			name:        "secret not found",
			clusterName: "test-cluster",
			secret:      nil,
			expectErr:   true,
		},
		{
			name:        "secret found but no values key",
			clusterName: "test-cluster",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reghelpers.ClusterImportConfigSecret,
					Namespace: "test-cluster",
				},
				Data: map[string][]byte{},
			},
			expectErr: true,
		},
		{
			name:        "secret found with invalid yaml",
			clusterName: "test-cluster",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reghelpers.ClusterImportConfigSecret,
					Namespace: "test-cluster",
				},
				Data: map[string][]byte{
					reghelpers.ValuesYamlKey: []byte("invalid: ["),
				},
			},
			expectErr: true,
		},
		{
			name:        "secret found with valid yaml",
			clusterName: "test-cluster",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reghelpers.ClusterImportConfigSecret,
					Namespace: "test-cluster",
				},
				Data: map[string][]byte{
					reghelpers.ValuesYamlKey: func() []byte {
						cfg := &chart.KlusterletChartConfig{
							BootstrapHubKubeConfig: "test-kubeconfig",
						}
						b, _ := yaml.Marshal(cfg)
						return b
					}(),
				},
			},
			expectErr: false,
			expectConfig: &chart.KlusterletChartConfig{
				BootstrapHubKubeConfig: "test-kubeconfig",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.secret != nil {
				objects = append(objects, c.secret)
			}
			client := kubefake.NewClientset(objects...)
			render := RenderFromConfigSecret(client)
			cluster := &v1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: c.clusterName,
				},
			}
			config := &chart.KlusterletChartConfig{}
			result, err := render(context.TODO(), cluster, config)
			if c.expectErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.expectConfig != nil && result.BootstrapHubKubeConfig != c.expectConfig.BootstrapHubKubeConfig {
				t.Errorf("expected BootstrapHubKubeConfig: %s, got: %s", c.expectConfig.BootstrapHubKubeConfig, result.BootstrapHubKubeConfig)
			}
		})
	}
}
