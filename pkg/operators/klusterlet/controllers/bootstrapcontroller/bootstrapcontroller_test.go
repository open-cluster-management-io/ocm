package bootstrapcontroller

import (
	"context"
	"testing"
	"time"

	fakeoperatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	testinghelper "github.com/open-cluster-management/registration-operator/pkg/helpers/testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		queueKey        string
		objects         []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:    "the changed secret is not bootstrap secret",
			objects: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no actions happens, but got %#v", actions)
				}
			},
		},
		{
			name:     "the bootstrap is not started",
			queueKey: "test/test",
			objects:  []runtime.Object{newSecret("bootstrap-hub-kubeconfig", "test", newKubeConfig("https://10.0.118.47:6443"))},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no actions happens, but got %#v", actions)
				}
			},
		},
		{
			name:     "the bootstrap secret is not changed",
			queueKey: "test/test",
			objects: []runtime.Object{
				newSecret("bootstrap-hub-kubeconfig", "test", newKubeConfig("https://10.0.118.47:6443")),
				newSecret("hub-kubeconfig-secret", "test", newKubeConfig("https://10.0.118.47:6443")),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no actions happens, but got %#v", actions)
				}
			},
		},
		{
			name:     "the bootstrap secret is changed",
			queueKey: "test/test",
			objects: []runtime.Object{
				newSecret("bootstrap-hub-kubeconfig", "test", newKubeConfig("https://10.0.118.47:6443")),
				newSecret("hub-kubeconfig-secret", "test", newKubeConfig("https://10.0.118.48:6443")),
				newDeployment("test-registration-agent", "test"),
				newDeployment("test-work-agent", "test"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertDelete(t, actions[0], "secrets", "test", "hub-kubeconfig-secret")
				testinghelper.AssertDelete(t, actions[1], "deployments", "test", "test-registration-agent")
				testinghelper.AssertDelete(t, actions[2], "deployments", "test", "test-work-agent")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.objects...)
			kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)
			secretStore := kubeInformers.Core().V1().Secrets().Informer().GetStore()
			for _, object := range c.objects {
				switch object.(type) {
				case *corev1.Secret:
					secretStore.Add(object)
				}
			}

			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset()
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			operatorStore := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
			operatorStore.Add(newKlusterlet("test", "test"))

			controller := &bootstrapController{
				kubeClient:       fakeKubeClient,
				klusterletLister: operatorInformers.Operator().V1().Klusterlets().Lister(),
				secretLister:     kubeInformers.Core().V1().Secrets().Lister(),
			}

			syncContext := testinghelper.NewFakeSyncContext(t, c.queueKey)
			if err := controller.sync(context.TODO(), syncContext); err != nil {
				t.Errorf("Expected no errors, but got %v", err)
			}

			c.validateActions(t, fakeKubeClient.Actions())
		})
	}
}

func TestBootstrapSecretQueueKeyFunc(t *testing.T) {
	cases := []struct {
		name        string
		object      runtime.Object
		klusterlet  *operatorapiv1.Klusterlet
		expectedKey string
	}{
		{
			name:        "key by bootstrap secret",
			object:      newSecret("bootstrap-hub-kubeconfig", "test", []byte{}),
			klusterlet:  newKlusterlet("testklusterlet", "test"),
			expectedKey: "test/testklusterlet",
		},
		{
			name:        "key by wrong secret",
			object:      newSecret("dummy", "test", []byte{}),
			klusterlet:  newKlusterlet("testklusterlet", "test"),
			expectedKey: "",
		},
		{
			name:        "key by klusterlet with empty namespace",
			object:      newSecret("bootstrap-hub-kubeconfig", "open-cluster-management-agent", []byte{}),
			klusterlet:  newKlusterlet("testklusterlet", ""),
			expectedKey: "open-cluster-management-agent/testklusterlet",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.klusterlet)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
			store.Add(c.klusterlet)
			keyFunc := bootstrapSecretQueueKeyFunc(operatorInformers.Operator().V1().Klusterlets().Lister())
			actualKey := keyFunc(c.object)
			if actualKey != c.expectedKey {
				t.Errorf("Queued key is not correct: actual %s, expected %s", actualKey, c.expectedKey)
			}
		})
	}
}

func newKlusterlet(name, namespace string) *operatorapiv1.Klusterlet {
	return &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			Namespace: namespace,
		},
	}
}

func newSecret(name, namespace string, kubeConfig []byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}
	secret.Data["kubeconfig"] = kubeConfig
	return secret
}

func newKubeConfig(host string) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                host,
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster: "default-cluster",
		}},
		CurrentContext: "default-context",
	})
	return configData
}

func newDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{},
	}
}
