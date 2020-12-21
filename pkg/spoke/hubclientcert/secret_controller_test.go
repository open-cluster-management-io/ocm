package hubclientcert

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestDumpSecret(t *testing.T) {
	testDir, err := ioutil.TempDir("", "dumpsecret")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(testDir)

	kubeConfigFile := testinghelpers.NewKubeconfig(nil, nil)

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
				files, err := ioutil.ReadDir(hubKubeconfigDir)
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
					ClusterNameFile: []byte("test"),
					AgentNameFile:   []byte("test"),
					KubeconfigFile:  testinghelpers.NewKubeconfig(nil, nil),
				},
			),
			validateFiles: func(t *testing.T, hubKubeconfigDir string) {
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, ClusterNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, AgentNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, KubeconfigFile), kubeConfigFile)
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, TLSKeyFile))
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, TLSCertFile))
			},
		},
		{
			name:     "secret is updated",
			queueKey: testSecretName,
			oldConfigData: map[string][]byte{
				ClusterNameFile: []byte("test"),
				AgentNameFile:   []byte("test"),
				KubeconfigFile:  []byte("test"),
			},
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "",
				testinghelpers.NewTestCert("test", 60*time.Second),
				map[string][]byte{
					ClusterNameFile: []byte("test1"),
					AgentNameFile:   []byte("test"),
					KubeconfigFile:  testinghelpers.NewKubeconfig(nil, nil),
				},
			),
			validateFiles: func(t *testing.T, hubKubeconfigDir string) {
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, ClusterNameFile), []byte("test1"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, AgentNameFile), []byte("test"))
				testinghelpers.AssertFileContent(t, path.Join(hubKubeconfigDir, KubeconfigFile), kubeConfigFile)
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, TLSKeyFile))
				testinghelpers.AssertFileExist(t, path.Join(hubKubeconfigDir, TLSCertFile))
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

			err = DumpSecret(kubeClient.CoreV1(), testNamespace, testSecretName, hubKubeconfigDir, context.TODO(), eventstesting.NewTestingEventRecorder(t))
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			c.validateFiles(t, hubKubeconfigDir)
		})
	}
}
