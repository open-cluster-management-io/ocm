package ssarcontroller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

type testController struct {
	controller     *ssarController
	operatorClient *fakeoperatorclient.Clientset
}

type serverResponse struct {
	allowToOperateManagedClusters      bool
	allowToOperateManagedClusterStatus bool
	allowToOperateManifestWorks        bool
}

func newSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}
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

func newSecretWithKubeConfig(name, namespace string, kubeConfig []byte) *corev1.Secret {
	secret := newSecret(name, namespace)
	secret.Data["kubeconfig"] = kubeConfig
	return secret
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

func newTestController(t *testing.T, klusterlet *operatorapiv1.Klusterlet, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)

	klusterletController := &ssarController{
		kubeClient:       fakeKubeClient,
		klusterletClient: fakeOperatorClient.OperatorV1().Klusterlets(),
		secretLister:     kubeInformers.Core().V1().Secrets().Lister(),
		klusterletLister: operatorInformers.Operator().V1().Klusterlets().Lister(),
		klusterletLocker: &klusterletLocker{
			klusterletInChecking: make(map[string]struct{}),
		},
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	if err := store.Add(klusterlet); err != nil {
		t.Fatal(err)
	}

	return &testController{
		controller:     klusterletController,
		operatorClient: fakeOperatorClient,
	}
}

func TestSync(t *testing.T) {
	response := &serverResponse{}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		data, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ssar := &authorizationv1.SelfSubjectAccessReview{}
		if err := json.Unmarshal(data, ssar); err != nil {
			t.Fatal(err)
		}
		if ssar.Spec.ResourceAttributes.Resource == "managedclusters" {
			if ssar.Spec.ResourceAttributes.Subresource == "status" {
				ssar.Status.Allowed = response.allowToOperateManagedClusterStatus
			} else {
				ssar.Status.Allowed = response.allowToOperateManagedClusters
			}
		} else if ssar.Spec.ResourceAttributes.Resource == "manifestworks" {
			ssar.Status.Allowed = response.allowToOperateManifestWorks
		} else {
			ssar.Status.Allowed = true
		}

		w.Header().Set("Content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(ssar); err != nil {
			t.Fatal(err)
		}
	}))
	defer apiServer.Close()

	apiServerHost := apiServer.URL

	cases := []struct {
		name                               string
		object                             []runtime.Object
		klusterlet                         *operatorapiv1.Klusterlet
		allowToOperateManagedClusters      bool
		allowToOperateManagedClusterStatus bool
		allowToOperateManifestWorks        bool
		expectedConditions                 []metav1.Condition
	}{
		{
			name: "No bootstrap secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			allowToOperateManagedClusters:      false,
			allowToOperateManagedClusterStatus: false,
			allowToOperateManifestWorks:        false,
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "BootstrapSecretMissing,HubKubeConfigUnauthorized", metav1.ConditionTrue),
			},
		},
		{
			name: "No hubconfig secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			allowToOperateManagedClusters:      true,
			allowToOperateManagedClusterStatus: true,
			allowToOperateManifestWorks:        true,
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue),
			},
		},
		{
			name: "Bad bootstrap secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", []byte("badsecret")),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			allowToOperateManagedClusters:      false,
			allowToOperateManagedClusterStatus: false,
			allowToOperateManifestWorks:        false,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "BootstrapSecretError,HubKubeConfigUnauthorized", metav1.ConditionTrue),
			},
		},
		{
			name: "Bad hub config secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", []byte("badkubeconfig")),
			},
			allowToOperateManagedClusters:      true,
			allowToOperateManagedClusterStatus: true,
			allowToOperateManifestWorks:        true,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "BootstrapSecretFunctional,HubKubeConfigError", metav1.ConditionTrue),
			},
		},
		{
			name: "Unauthorized",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			allowToOperateManagedClusters:      false,
			allowToOperateManagedClusterStatus: false,
			allowToOperateManifestWorks:        false,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "BootstrapSecretUnauthorized,HubKubeConfigUnauthorized", metav1.ConditionTrue),
			},
		},
		{
			name: "Operator functional",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			allowToOperateManagedClusters:      true,
			allowToOperateManagedClusterStatus: true,
			allowToOperateManifestWorks:        true,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(hubConnectionDegraded, "HubConnectionFunctional", metav1.ConditionFalse),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(t, c.klusterlet, c.object...)
			syncContext := testinghelper.NewFakeSyncContext(t, c.klusterlet.Name)

			response.allowToOperateManagedClusters = c.allowToOperateManagedClusters
			response.allowToOperateManagedClusterStatus = c.allowToOperateManagedClusterStatus
			response.allowToOperateManifestWorks = c.allowToOperateManifestWorks

			err := controller.controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}

			// wait util goroutine is done
			for controller.controller.inSSARChecking(c.klusterlet.Name) {
				time.Sleep(time.Second * 1)
			}

			operatorActions := controller.operatorClient.Actions()

			testinghelper.AssertEqualNumber(t, len(operatorActions), 2)
			testinghelper.AssertGet(t, operatorActions[0], "operator.open-cluster-management.io", "v1", "klusterlets")
			testinghelper.AssertAction(t, operatorActions[1], "update")
			testinghelper.AssertOnlyConditions(t, operatorActions[1].(clienttesting.UpdateActionImpl).Object, c.expectedConditions...)
		})
	}
}

func TestLocker(t *testing.T) {
	locker := &klusterletLocker{
		klusterletInChecking: make(map[string]struct{}),
	}

	cluster1 := "cluster1"
	cluster2 := "cluster2"
	results := make(chan string, 2)

	// first we add cluster1 in processing stage
	c1status := locker.inSSARChecking(cluster1)
	if c1status {
		t.Error("c1 should not be processing yet")
	}

	locker.addSSARChecking(cluster1)

	go func() {
		defer locker.deleteSSARChecking(cluster1)
		// wait for 5 seconds
		time.Sleep(time.Second * 5)
		results <- cluster1
	}()

	// then we add cluster2 in processing stage
	go func() {
		// begin when cluster1 is in processing
		for !locker.inSSARChecking(cluster1) {
			time.Sleep(time.Millisecond * 500)
		}

		// simulate the controller part
		c2status := locker.inSSARChecking(cluster2)
		if c2status {
			t.Error("c2 should not be processing yet")
		}

		locker.addSSARChecking(cluster2)

		go func() {
			defer locker.deleteSSARChecking(cluster2)
			// wait for 2 seconds
			time.Sleep(time.Second * 2)
			results <- cluster2
		}()
	}()

	// wait for c1 and c2 done with their work
	for locker.inSSARChecking(cluster1) || locker.inSSARChecking(cluster2) {
		time.Sleep(time.Second)
	}

	// c1 works for 5 seconds
	// c2 works for 2 seconds
	// If c1 would hang c2, the c2 must return later, the results should be [cluster1, cluster2]
	// Otherwise, the results should be [cluster2, cluster1].(And this result is what we expect)
	r1 := <-results
	r2 := <-results
	if r1 != cluster2 || r2 != cluster1 {
		t.Errorf("results not as expected, [%s, %s]", r1, r2)
	}

}
