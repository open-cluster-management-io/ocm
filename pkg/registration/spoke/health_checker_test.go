package spoke

import (
	"os"
	"path"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

func TestHubKubeConfigHealthChecker(t *testing.T) {
	testDir, err := os.MkdirTemp("", "HubKubeConfigHealthChecker")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		err := os.RemoveAll(testDir)
		if err != nil {
			t.Fatal(err)
		}
	}()

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", nil, nil, nil)
	testinghelpers.WriteFile(path.Join(testDir, "kubeconfig"), kubeconfig)

	validCert := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 10*time.Minute)

	expiredCert := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", -1*time.Minute)

	cases := []struct {
		name      string
		tlsCert   []byte
		tlsKey    []byte
		unhealthy bool
	}{
		{
			name:    "valid client cert",
			tlsKey:  validCert.Key,
			tlsCert: validCert.Cert,
		},
		{
			name:      "expired client cert",
			tlsKey:    expiredCert.Key,
			tlsCert:   expiredCert.Cert,
			unhealthy: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(testDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(testDir, "tls.crt"), c.tlsCert)
			}

			driver := csr.NewCSRDriver()
			secretOption := register.SecretOption{
				ClusterName:       "cluster1",
				AgentName:         "agent1",
				HubKubeconfigDir:  testDir,
				HubKubeconfigFile: path.Join(testDir, "kubeconfig"),
			}

			hc := &hubKubeConfigHealthChecker{
				checkFunc:    register.IsHubKubeConfigValidFunc(driver, secretOption),
				bootstrapped: true,
			}

			err := hc.Check(nil)
			if c.unhealthy && err == nil {
				t.Errorf("expected error, but got nil")
			}
			if !c.unhealthy && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBootstrapKubeconfigHealthChecker(t *testing.T) {
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
		unhealthy                         bool
	}{
		{
			name: "add",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
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
		},
		{
			name: "spec changes",
			originalBootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("invalid-kubeconfig"),
			}),
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("another-invalid-kubeconfig"),
			}),
			update:    true,
			unhealthy: true,
		},
		{
			name:                      "invalid new object type - update",
			bootstrapKubeconfigSecret: struct{}{},
			update:                    true,
		},
		{
			name:                              "invalid old object type - update",
			originalBootstrapKubeconfigSecret: struct{}{},
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, map[string][]byte{
				"kubeconfig": []byte("another-invalid-kubeconfig"),
			}),
			update: true,
		},
		{
			name:                      "delete",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret(defaultSecretName, nil, nil),
			delete:                    true,
			unhealthy:                 true,
		},
		{
			name:                      "delete other secret",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret("other-secret", nil, nil),
			delete:                    true,
		},
		{
			name:                      "custom secret name",
			secretName:                "other-secret",
			bootstrapKubeconfigSecret: newBootstrapKubeconfigSecret("other-secret", nil, nil),
			delete:                    true,
			unhealthy:                 true,
		},
		{
			name:                      "invalid type - delete",
			secretName:                "other-secret",
			bootstrapKubeconfigSecret: struct{}{},
			delete:                    true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			secretName := defaultSecretName
			bootstrapKubeconfigSecretName := &secretName
			hc := &bootstrapKubeconfigHealthChecker{
				bootstrapKubeconfigSecretName: bootstrapKubeconfigSecretName,
			}

			if len(c.secretName) > 0 {
				*bootstrapKubeconfigSecretName = c.secretName
			}

			if c.add {
				hc.OnAdd(c.bootstrapKubeconfigSecret, false)
			}

			if c.update {
				hc.OnUpdate(c.originalBootstrapKubeconfigSecret, c.bootstrapKubeconfigSecret)
			}

			if c.delete {
				hc.OnDelete(c.bootstrapKubeconfigSecret)
			}

			err := hc.Check(nil)
			if c.unhealthy && err == nil {
				t.Errorf("expected error, but got nil")
			}
			if !c.unhealthy && err != nil {
				t.Errorf("unexpected error: %v", err)
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
