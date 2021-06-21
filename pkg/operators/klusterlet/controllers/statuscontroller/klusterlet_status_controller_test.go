package statuscontroller

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	controller     *klusterletStatusController
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

func newDeployment(name, namespace string, desiredReplica, availableReplica int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desiredReplica,
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: availableReplica,
		},
	}
}

func newTestController(klusterlet *operatorapiv1.Klusterlet, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)

	klusterletController := &klusterletStatusController{
		kubeClient:       fakeKubeClient,
		klusterletClient: fakeOperatorClient.OperatorV1().Klusterlets(),
		secretLister:     kubeInformers.Core().V1().Secrets().Lister(),
		deploymentLister: kubeInformers.Apps().V1().Deployments().Lister(),
		klusterletLister: operatorInformers.Operator().V1().Klusterlets().Lister(),
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	store.Add(klusterlet)

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

		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ssar := &authorizationv1.SelfSubjectAccessReview{}
		json.Unmarshal(data, ssar)
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
		json.NewEncoder(w).Encode(ssar)
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
			name:       "No bootstrap secret",
			object:     []runtime.Object{newSecret(helpers.HubKubeConfig, "test")},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "BootstrapSecretMissing,HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "Bad bootstrap secret",
			object: []runtime.Object{
				newSecret(helpers.HubKubeConfig, "test"),
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", []byte("badsecret")),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "BootstrapSecretError,HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "Unauthorized bootstrap secret",
			object: []runtime.Object{
				newSecret(helpers.HubKubeConfig, "test"),
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "BootstrapSecretUnauthorized,HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "No hubconfig secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			klusterlet:                    newKlusterlet("testklusterlet", "test", ""),
			allowToOperateManagedClusters: true,
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "HubKubeConfigSecretMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigSecretMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "No cluster name secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			allowToOperateManagedClusters: true,
			klusterlet:                    newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "ClusterNameMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "ClusterNameMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "No kubeconfig secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecret(helpers.HubKubeConfig, "test"),
			},
			allowToOperateManagedClusters: true,
			klusterlet:                    newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigMissing,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "Bad hub config secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", []byte("badkubeconfig")),
			},
			allowToOperateManagedClusters: true,
			klusterlet:                    newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "HubKubeConfigError,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigError,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "Unauthorized hub config secret",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
			},
			allowToOperateManagedClusters: true,
			klusterlet:                    newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "HubKubeConfigUnauthorized,GetDeploymentFailed", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigUnauthorized,GetDeploymentFailed", metav1.ConditionTrue),
			},
		},
		{
			name: "Unavailable pod in deployments",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newDeployment("testklusterlet-registration-agent", "test", 3, 0),
				newDeployment("testklusterlet-work-agent", "test", 3, 0),
			},
			allowToOperateManagedClusters:      true,
			allowToOperateManagedClusterStatus: true,
			allowToOperateManifestWorks:        true,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "UnavailablePods", metav1.ConditionTrue),
			},
		},
		{
			name: "Operator functional",
			object: []runtime.Object{
				newSecretWithKubeConfig(helpers.BootstrapHubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newSecretWithKubeConfig(helpers.HubKubeConfig, "test", newKubeConfig(apiServerHost)),
				newDeployment("testklusterlet-registration-agent", "test", 3, 3),
				newDeployment("testklusterlet-work-agent", "test", 3, 3),
			},
			allowToOperateManagedClusters:      true,
			allowToOperateManagedClusterStatus: true,
			allowToOperateManifestWorks:        true,
			klusterlet:                         newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "RegistrationFunctional", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletWorKDegraded, "WorkFunctional", metav1.ConditionFalse),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(c.klusterlet, c.object...)
			syncContext := testinghelper.NewFakeSyncContext(t, c.klusterlet.Name)

			response.allowToOperateManagedClusters = c.allowToOperateManagedClusters
			response.allowToOperateManagedClusterStatus = c.allowToOperateManagedClusterStatus
			response.allowToOperateManifestWorks = c.allowToOperateManifestWorks

			err := controller.controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}
			operatorActions := controller.operatorClient.Actions()

			testinghelper.AssertEqualNumber(t, len(operatorActions), 2)
			testinghelper.AssertGet(t, operatorActions[0], "operator.open-cluster-management.io", "v1", "klusterlets")
			testinghelper.AssertAction(t, operatorActions[1], "update")
			testinghelper.AssertOnlyConditions(t, operatorActions[1].(clienttesting.UpdateActionImpl).Object, c.expectedConditions...)
		})
	}
}
