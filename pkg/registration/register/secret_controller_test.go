package register

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSync(t *testing.T) {
	commonName := "test"
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	bootstrapKubeconfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                "localhost",
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster:  "test-cluster",
			AuthInfo: "test-user",
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"test-user": {
				Token: "test-token",
			},
		},
		CurrentContext: "test-context",
	}
	err = clientcmd.WriteToFile(*bootstrapKubeconfig, path.Join(tempDir, "bootstrap-kubeconfig"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)
	testCases := []struct {
		name          string
		option        SecretOption
		secrets       []runtime.Object
		driver        *fakeDriver
		expectedCond  *metav1.Condition
		validatAction func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "create secret without additional data",
			option: SecretOption{
				SecretName:      "test",
				SecretNamespace: "test",
			},
			secrets: []runtime.Object{},
			driver: newFakeDriver(
				testinghelpers.NewHubKubeconfigSecret(
					"test", "test", "",
					testinghelpers.NewTestCert(commonName, 100*time.Second), map[string][]byte{}),
				&metav1.Condition{Type: "Created", Status: metav1.ConditionTrue}, nil,
			),
			validatAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "create")
			},
			expectedCond: &metav1.Condition{Type: "Created", Status: metav1.ConditionTrue},
		},
		{
			name: "update secret without additional data",
			option: SecretOption{
				SecretName:      "test",
				SecretNamespace: "test",
			},
			secrets: []runtime.Object{
				testinghelpers.NewHubKubeconfigSecret(
					"test", "test", "0",
					testinghelpers.NewTestCert(commonName, 100*time.Second), map[string][]byte{}),
			},
			driver: newFakeDriver(
				testinghelpers.NewHubKubeconfigSecret(
					"test", "test", "1",
					testinghelpers.NewTestCert(commonName, 200*time.Second), map[string][]byte{}),
				&metav1.Condition{Type: "Created", Status: metav1.ConditionTrue}, nil,
			),
			validatAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
			},
			expectedCond: &metav1.Condition{Type: "Created", Status: metav1.ConditionTrue},
		},
		{
			name: "nothing to create if there is no secret generated",
			option: SecretOption{
				SecretName:      "test",
				SecretNamespace: "test",
			},
			secrets: []runtime.Object{},
			driver:  newFakeDriver(nil, nil, nil),
			validatAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get")
			},
		},
		{
			name: "addition secret data",
			option: SecretOption{
				SecretName:              "test",
				SecretNamespace:         "test",
				ClusterName:             "cluster1",
				AgentName:               "agent1",
				BootStrapKubeConfigFile: path.Join(tempDir, "bootstrap-kubeconfig"),
			},
			secrets: []runtime.Object{},
			driver: newFakeDriver(testinghelpers.NewHubKubeconfigSecret(
				"test", "test", "",
				testinghelpers.NewTestCert(commonName, 100*time.Second), map[string][]byte{}), nil, nil),
			validatAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "create")
				secret := actions[1].(clienttesting.CreateActionImpl).Object.(*corev1.Secret)
				cluster, ok := secret.Data[ClusterNameFile]
				if !ok || string(cluster) != "cluster1" {
					t.Errorf("cluster name not correct")
				}
				agent, ok := secret.Data[AgentNameFile]
				if !ok || string(agent) != "agent1" {
					t.Errorf("agent name not correct")
				}
				_, ok = secret.Data[KubeconfigFile]
				if !ok {
					t.Errorf("kubeconfig file should exist")
				}
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			syncCtx := testingcommon.NewFakeSyncContext(t, "test")
			kubeClient := kubefake.NewClientset(c.secrets...)
			informerFactory := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
			updater := &fakeStatusUpdater{}
			ctrl := NewSecretController(
				c.option, c.driver, updater.update,
				kubeClient.CoreV1(),
				informerFactory.Core().V1().Secrets().Informer(),
				"test")
			err := ctrl.Sync(context.Background(), syncCtx, "test")
			if err != nil {
				t.Fatal(err)
			}
			c.validatAction(t, kubeClient.Actions())
			if !apiequality.Semantic.DeepEqual(c.expectedCond, updater.cond) {
				t.Errorf("Condition update not correct")
			}
		})
	}
}

type fakeStatusUpdater struct {
	cond *metav1.Condition
}

func (f *fakeStatusUpdater) update(_ context.Context, cond metav1.Condition) error {
	f.cond = cond.DeepCopy()
	return nil
}

type fakeDriver struct {
	secret *corev1.Secret
	err    error
	cond   *metav1.Condition
}

func newFakeDriver(secret *corev1.Secret, cond *metav1.Condition, err error) *fakeDriver {
	return &fakeDriver{
		secret: secret,
		cond:   cond,
		err:    err,
	}
}

func (f *fakeDriver) IsHubKubeConfigValid(_ context.Context, _ SecretOption) (bool, error) {
	return true, nil
}

func (f *fakeDriver) BuildKubeConfigFromTemplate(config *clientcmdapi.Config) *clientcmdapi.Config {
	return config
}

func (f *fakeDriver) BuildClients(ctx context.Context, secretOption SecretOption, bootstrap bool) (*Clients, error) {
	return &Clients{}, nil
}

func (f *fakeDriver) Process(
	_ context.Context,
	_ string,
	_ *corev1.Secret,
	_ map[string][]byte,
	_ events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	return f.secret, f.cond, f.err
}

func (f *fakeDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return nil, nil
}

func (f *fakeDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}
