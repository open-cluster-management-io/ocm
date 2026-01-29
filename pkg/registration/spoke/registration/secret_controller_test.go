package registration

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	kubefake "k8s.io/client-go/kubernetes/fake"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

const (
	testNamespace  = "testns"
	testSecretName = "testsecret"
)

func TestDumpSecret(t *testing.T) {
	testDir, err := os.MkdirTemp("", "dumpsecret")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		err := os.RemoveAll(testDir)
		if err != nil {
			t.Fatal(err)
		}
	}()

	kubeConfigFile := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil)

	cases := []struct {
		name          string
		queueKey      string
		secret        *corev1.Secret
		oldConfigData map[string][]byte
		validateFiles func(t *testing.T, fileDir string)
	}{
		{
			name:     "no secret",
			queueKey: "",
			secret:   testinghelpers.NewHubKubeconfigSecret("irrelevant", "irrelevant", "", nil, map[string][]byte{}),
			validateFiles: func(t *testing.T, hubKubeconfigDir string) {
				files, err := os.ReadDir(hubKubeconfigDir)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(files) != 0 {
					t.Errorf("expect no files, but get %d files", len(files))
				}
			},
		},
		{
			name:     "secret is created",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "",
				testinghelpers.NewTestCert("test", 60*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte("test"),
					register.AgentNameFile:   []byte("test"),
					register.KubeconfigFile:  testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				},
			),
			validateFiles: func(t *testing.T, hubKubeconfigDir string) {
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.ClusterNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.AgentNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.KubeconfigFile), kubeConfigFile)
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, csr.TLSKeyFile))
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, csr.TLSCertFile))
			},
		},
		{
			name:     "secret is updated",
			queueKey: testSecretName,
			oldConfigData: map[string][]byte{
				register.ClusterNameFile: []byte("test"),
				register.AgentNameFile:   []byte("test"),
				register.KubeconfigFile:  []byte("test"),
			},
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "",
				testinghelpers.NewTestCert("test", 60*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte("test1"),
					register.AgentNameFile:   []byte("test"),
					register.KubeconfigFile:  testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				},
			),
			validateFiles: func(t *testing.T, hubKubeconfigDir string) {
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.ClusterNameFile), []byte("test1"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.AgentNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, register.KubeconfigFile), kubeConfigFile)
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, csr.TLSKeyFile))
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, csr.TLSCertFile))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.secret)

			hubKubeconfigDir := path.Join(testDir, fmt.Sprintf("/%s/hub-kubeconfig", rand.String(6)))
			if err := os.MkdirAll(hubKubeconfigDir, 0755); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			for k, v := range c.oldConfigData {
				testinghelpers.WriteFile(path.Join(hubKubeconfigDir, k), v)
			}

			err = DumpSecret(
				context.TODO(), kubeClient.CoreV1(), testNamespace, testSecretName, hubKubeconfigDir)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			c.validateFiles(t, hubKubeconfigDir)
		})
	}
}
