package helpers

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

func newSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}
}

func newKlusterlet(name, namespace, clustername string) *operatorapiv1.Klusterlet {
	return &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "testregistration",
			WorkImagePullSpec:         "testwork",
			ClusterName:               clustername,
			Namespace:                 namespace,
			ExternalServerURLs:        []operatorapiv1.ServerURL{},
		},
	}
}

func newClusterManager(name string, mode operatorapiv1.InstallMode) *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: mode,
			},
		},
	}
}

func TestKlusterletSecretQueueKeyFunc(t *testing.T) {
	cases := []struct {
		name        string
		object      runtime.Object
		klusterlet  *operatorapiv1.Klusterlet
		expectedKey string
	}{
		{
			name:        "key by hub config secret",
			object:      newSecret(HubKubeConfig, "test"),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "testklusterlet",
		},
		{
			name:        "key by bootstrap secret",
			object:      newSecret(BootstrapHubKubeConfig, "test"),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "testklusterlet",
		},
		{
			name:        "key by wrong secret",
			object:      newSecret("dummy", "test"),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "",
		},
		{
			name:        "key by klusterlet with empty namespace",
			object:      newSecret(BootstrapHubKubeConfig, KlusterletDefaultNamespace),
			klusterlet:  newKlusterlet("testklusterlet", "", ""),
			expectedKey: "testklusterlet",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.klusterlet)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
			if err := store.Add(c.klusterlet); err != nil {
				t.Fatal(err)
			}
			keyFunc := KlusterletSecretQueueKeyFunc(operatorInformers.Operator().V1().Klusterlets().Lister())
			actualKey := keyFunc(c.object)
			if actualKey != c.expectedKey {
				t.Errorf("Queued key is not correct: actual %s, expected %s", actualKey, c.expectedKey)
			}
		})
	}
}

func TestKlusterletDeploymentQueueKeyFunc(t *testing.T) {
	cases := []struct {
		name        string
		object      runtime.Object
		klusterlet  *operatorapiv1.Klusterlet
		expectedKey string
	}{
		{
			name:        "key by work agent",
			object:      newDeployment("testklusterlet-work-agent", "test", 0),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "testklusterlet",
		},
		{
			name:        "key by registrartion agent",
			object:      newDeployment("testklusterlet-registration-agent", "test", 0),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "testklusterlet",
		},
		{
			name:        "key by wrong deployment",
			object:      newDeployment("dummy", "test", 0),
			klusterlet:  newKlusterlet("testklusterlet", "test", ""),
			expectedKey: "",
		},
		{
			name:        "key by klusterlet with empty namespace",
			object:      newDeployment("testklusterlet-work-agent", KlusterletDefaultNamespace, 0),
			klusterlet:  newKlusterlet("testklusterlet", "", ""),
			expectedKey: "testklusterlet",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.klusterlet)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
			if err := store.Add(c.klusterlet); err != nil {
				t.Fatal(err)
			}
			keyFunc := KlusterletDeploymentQueueKeyFunc(operatorInformers.Operator().V1().Klusterlets().Lister())
			actualKey := keyFunc(c.object)
			if actualKey != c.expectedKey {
				t.Errorf("Queued key is not correct: actual %s, expected %s", actualKey, c.expectedKey)
			}
		})
	}
}

func TestClusterManagerDeploymentQueueKeyFunc(t *testing.T) {
	cases := []struct {
		name           string
		object         runtime.Object
		clusterManager *operatorapiv1.ClusterManager
		expectedKey    string
	}{
		{
			name:           "key by registrartion controller",
			object:         newDeployment("testhub-registration-controller", ClusterManagerDefaultNamespace, 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeDefault),
			expectedKey:    "testhub",
		},
		{
			name:           "key by registrartion webhook",
			object:         newDeployment("testhub-registration-webhook", ClusterManagerDefaultNamespace, 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeDefault),
			expectedKey:    "testhub",
		},
		{
			name:           "key by work webhook",
			object:         newDeployment("testhub-work-webhook", ClusterManagerDefaultNamespace, 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeDefault),
			expectedKey:    "testhub",
		},
		{
			name:           "key by placement controller",
			object:         newDeployment("testhub-placement-controller", ClusterManagerDefaultNamespace, 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeDefault),
			expectedKey:    "testhub",
		},
		{
			name:           "key by wrong deployment",
			object:         newDeployment("dummy", "test", 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeDefault),
			expectedKey:    "",
		},
		{
			name:           "key by registrartion controller in hosted mode, namespace not match",
			object:         newDeployment("testhub-registration-controller", ClusterManagerDefaultNamespace, 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeHosted),
			expectedKey:    "",
		},
		{
			name:           "key by registrartion controller in hosted mode, namespace match",
			object:         newDeployment("testhub-registration-controller", "testhub", 0),
			clusterManager: newClusterManager("testhub", operatorapiv1.InstallModeHosted),
			expectedKey:    "testhub",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.clusterManager)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			if err := store.Add(c.clusterManager); err != nil {
				t.Fatal(err)
			}
			keyFunc := ClusterManagerDeploymentQueueKeyFunc(operatorInformers.Operator().V1().ClusterManagers().Lister())
			actualKey := keyFunc(c.object)
			if actualKey != c.expectedKey {
				t.Errorf("Queued key is not correct: actual %s, expected %s; test name:%s", actualKey, c.expectedKey, c.name)
			}
		})
	}
}
