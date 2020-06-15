package statuscontroller

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeoperatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
	testinghelper "github.com/open-cluster-management/registration-operator/pkg/helpers/testing"
)

type testController struct {
	controller     *klusterletStatusController
	operatorClient *fakeoperatorclient.Clientset
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

func newSecretWithKubeConfig(name, namespace string) *corev1.Secret {
	secret := newSecret(name, namespace)
	secret.Data["kubeconfig"] = []byte("kubeconfig")
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
	cases := []struct {
		name               string
		object             []runtime.Object
		klusterlet         *operatorapiv1.Klusterlet
		expectedConditions []operatorapiv1.StatusCondition
	}{
		{
			name:       "No bootstrap secret",
			object:     []runtime.Object{},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "BootStrapSecretMissing", metav1.ConditionTrue),
			},
		},
		{
			name:       "No hubconfig secret",
			object:     []runtime.Object{newSecret(helpers.BootstrapHubKubeConfigSecret, "test")},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "HubKubeConfigSecretMissing", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "HubKubeConfigSecretMissing", metav1.ConditionTrue),
			},
		},
		{
			name:       "No cluster name secret",
			object:     []runtime.Object{newSecret(helpers.BootstrapHubKubeConfigSecret, "test"), newSecret(helpers.HubKubeConfigSecret, "test")},
			klusterlet: newKlusterlet("testklusterlet", "test", ""),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "ClusterNameMissing", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "ClusterNameMissing", metav1.ConditionTrue),
			},
		},
		{
			name:       "No kubeconfig secret",
			object:     []runtime.Object{newSecret(helpers.BootstrapHubKubeConfigSecret, "test"), newSecret(helpers.HubKubeConfigSecret, "test")},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "KubeConfigMissing", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "KubeConfigMissing", metav1.ConditionTrue),
			},
		},
		{
			name: "Unavailable pod in deployments",
			object: []runtime.Object{
				newSecret(helpers.BootstrapHubKubeConfigSecret, "test"),
				newSecretWithKubeConfig(helpers.HubKubeConfigSecret, "test"),
				newDeployment("testklusterlet-registration-agent", "test", 3, 0),
				newDeployment("testklusterlet-work-agent", "test", 3, 0),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "UnavailableRegistrationPod", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorKDegraded, "UnavailableWorkPod", metav1.ConditionTrue),
			},
		},
		{
			name: "Operator functional",
			object: []runtime.Object{
				newSecret(helpers.BootstrapHubKubeConfigSecret, "test"),
				newSecretWithKubeConfig(helpers.HubKubeConfigSecret, "test"),
				newDeployment("testklusterlet-registration-agent", "test", 3, 3),
				newDeployment("testklusterlet-work-agent", "test", 3, 3),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []operatorapiv1.StatusCondition{
				testinghelper.NamedCondition(klusterletRegistrationDegraded, "RegistrationFunctional", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletWorKDegraded, "WorkFunctional", metav1.ConditionFalse),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(c.klusterlet, c.object...)
			syncContext := testinghelper.NewFakeSyncContext(t, c.klusterlet.Name)
			err := controller.controller.sync(nil, syncContext)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}

			operatorActions := controller.operatorClient.Actions()

			testinghelper.AssertEqualNumber(t, len(operatorActions), 2)
			testinghelper.AssertGet(t, operatorActions[0], "operator.open-cluster-management.io", "v1", "klusterlets")
			testinghelper.AssertAction(t, operatorActions[1], "update")
			testinghelper.AssertOnlyConditions(
				t, operatorActions[1].(clienttesting.UpdateActionImpl).Object, c.expectedConditions...)
		})
	}
}
