package statuscontroller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

type testController struct {
	controller     *klusterletStatusController
	operatorClient *fakeoperatorclient.Clientset
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

func newTestController(t *testing.T, klusterlet *operatorapiv1.Klusterlet, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)

	klusterletController := &klusterletStatusController{
		kubeClient:       fakeKubeClient,
		klusterletClient: fakeOperatorClient.OperatorV1().Klusterlets(),
		deploymentLister: kubeInformers.Apps().V1().Deployments().Lister(),
		klusterletLister: operatorInformers.Operator().V1().Klusterlets().Lister(),
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
	cases := []struct {
		name       string
		object     []runtime.Object
		klusterlet *operatorapiv1.Klusterlet

		expectedConditions []metav1.Condition
	}{
		{
			name: "Unavailable & Undesired",
			object: []runtime.Object{
				newDeployment("testklusterlet-registration-agent", "test", 3, 0),
				newDeployment("testklusterlet-work-agent", "test", 3, 0),
			},

			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
			},
		},
		{
			name: "Unavailable(by registration) & Undesired",
			object: []runtime.Object{
				newDeployment("testklusterlet-registration-agent", "test", 3, 0),
				newDeployment("testklusterlet-work-agent", "test", 3, 1),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
			},
		},
		{
			name: "Unavailable(by work) & Undesired",
			object: []runtime.Object{
				newDeployment("testklusterlet-registration-agent", "test", 3, 1),
				newDeployment("testklusterlet-work-agent", "test", 3, 0),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
			},
		},
		{
			name: "Available & Undesired",
			object: []runtime.Object{
				newDeployment("testklusterlet-registration-agent", "test", 3, 1),
				newDeployment("testklusterlet-work-agent", "test", 3, 1),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletAvailable, "klusterletAvailable", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
			},
		},
		{
			name: "Available & Desired",
			object: []runtime.Object{
				newDeployment("testklusterlet-registration-agent", "test", 3, 3),
				newDeployment("testklusterlet-work-agent", "test", 3, 3),
			},
			klusterlet: newKlusterlet("testklusterlet", "test", "cluster1"),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(klusterletAvailable, "klusterletAvailable", metav1.ConditionTrue),
				testinghelper.NamedCondition(klusterletRegistrationDesiredDegraded, "DeploymentsFunctional", metav1.ConditionFalse),
				testinghelper.NamedCondition(klusterletWorkDesiredDegraded, "DeploymentsFunctional", metav1.ConditionFalse),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(t, c.klusterlet, c.object...)
			syncContext := testinghelper.NewFakeSyncContext(t, c.klusterlet.Name)

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
