package spoke

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestBootstrapKubeconfigEventHandler(t *testing.T) {
	//#nosec G101
	defaultSecretName := "bootstrap-hub-kubeconfig"
	cases := []struct {
		name                              string
		secretName                        string
		originalBootstrapKubeconfigSecret interface{}
		bootstrapKubeconfigSecret         interface{}
		add                               bool
		update                            bool
		delete                            bool
		expectCancel                      bool
	}{
		{
			name: "add",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			add:          true,
			expectCancel: false,
		},
		{
			name: "label changes",
			originalBootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, map[string]string{
				"test": "true",
			}, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			update:       true,
			expectCancel: false,
		},
		{
			name: "spec changes",
			originalBootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("another-invalid-kubeconfig"),
			}),
			update:       true,
			expectCancel: true,
		},
		{
			name: "update with different secret name",
			originalBootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret("other-secret", nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			update:       true,
			expectCancel: false,
		},
		{
			name:                      "invalid new object type - update",
			bootstrapKubeconfigSecret: struct{}{},
			update:                    true,
			expectCancel:              false,
		},
		{
			name:                              "invalid old object type - update",
			originalBootstrapKubeconfigSecret: struct{}{},
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("another-invalid-kubeconfig"),
			}),
			update:       true,
			expectCancel: false,
		},
		{
			name:                      "delete",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, nil),
			delete:                    true,
			expectCancel:              true,
		},
		{
			name:                      "delete other secret",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret("other-secret", nil, nil),
			delete:                    true,
			expectCancel:              false,
		},
		{
			name:                      "delete custom secret name",
			secretName:                "other-secret",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret("other-secret", nil, nil),
			delete:                    true,
			expectCancel:              true,
		},
		{
			name:                      "invalid type - delete",
			secretName:                "other-secret",
			bootstrapKubeconfigSecret: struct{}{},
			delete:                    true,
			expectCancel:              false,
		},
		{
			name: "delete with DeletedFinalStateUnknown",
			bootstrapKubeconfigSecret: cache.DeletedFinalStateUnknown{
				Obj: newBootstrapKubeconfigSecret(defaultSecretName, nil, nil),
			},
			delete:       true,
			expectCancel: true,
		},
		{
			name: "delete with DeletedFinalStateUnknown - other secret name",
			bootstrapKubeconfigSecret: cache.DeletedFinalStateUnknown{
				Obj: newBootstrapKubeconfigSecret("other-secret", nil, nil),
			},
			delete:       true,
			expectCancel: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			secretName := defaultSecretName
			bootstrapKubeconfigSecretName := &secretName
			ctx, cancel := context.WithCancel(context.Background())

			hc := &bootstrapKubeconfigEventHandler{
				bootstrapKubeconfigSecretName: bootstrapKubeconfigSecretName,
				cancel:                        cancel,
			}

			if len(c.secretName) > 0 {
				*bootstrapKubeconfigSecretName = c.secretName
			}

			if c.add {
				hc.OnAdd(c.bootstrapKubeconfigSecret, false)
			}

			if c.update {
				hc.OnUpdate(c.originalBootstrapKubeconfigSecret, c.bootstrapKubeconfigSecret)
				select {
				case <-ctx.Done():
					// context should be cancelled
					if c.expectCancel == false {
						t.Errorf("expected context to be not cancelled, but it was not")
					}
				default:
					if c.expectCancel == true {
						t.Errorf("expected context to be cancelled, but it was not")
					}
				}
			}

			if c.delete {
				hc.OnDelete(c.bootstrapKubeconfigSecret)
				select {
				case <-ctx.Done():
					// context should be cancelled
					if c.expectCancel == false {
						t.Errorf("expected context to be not cancelled, but it was not")
					}
				default:
					if c.expectCancel == true {
						t.Errorf("expected context to be cancelled, but it was not")
					}
				}
			}

		})
	}
}

func newBootstrapKubeconfigSecret(name string, labels map[string]string, data map[string][]byte, others ...string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "open-cluster-management-agent",
			Labels:    labels,
		},
		Data: data,
	}
}
