package statuscontroller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelper "open-cluster-management.io/ocm/pkg/operator/helpers/testing"
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
		Status: operatorapiv1.KlusterletStatus{
			Conditions: []metav1.Condition{
				{
					Type:   operatorapiv1.ConditionKlusterletApplied,
					Status: metav1.ConditionTrue,
				},
			},
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
		kubeClient: fakeKubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](fakeOperatorClient.OperatorV1().Klusterlets()),
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
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
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
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
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
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable, "NoAvailablePods", metav1.ConditionFalse),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded, "UnavailablePods", metav1.ConditionTrue),
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
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable, operatorapiv1.ReasonKlusterletAvailable, metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded, operatorapiv1.ReasonKlusterletUnavailablePods, metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded, operatorapiv1.ReasonKlusterletUnavailablePods, metav1.ConditionTrue),
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
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable,
					operatorapiv1.ReasonKlusterletAvailable, metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded,
					operatorapiv1.ReasonKlusterletDeploymentsFunctional, metav1.ConditionFalse),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded,
					operatorapiv1.ReasonKlusterletDeploymentsFunctional, metav1.ConditionFalse),
			},
		},
		{
			name: "Available & Desired with singleton",
			object: []runtime.Object{
				newDeployment("testklusterlet-agent", "test", 3, 3),
			},
			klusterlet: func() *operatorapiv1.Klusterlet {
				k := newKlusterlet("testklusterlet", "test", "cluster1")
				k.Spec.DeployOption.Mode = operatorapiv1.InstallModeSingleton
				return k
			}(),
			expectedConditions: []metav1.Condition{
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletAvailable,
					operatorapiv1.ReasonKlusterletAvailable, metav1.ConditionTrue),
				testinghelper.NamedCondition(operatorapiv1.ConditionRegistrationDesiredDegraded,
					operatorapiv1.ReasonKlusterletDeploymentsFunctional, metav1.ConditionFalse),
				testinghelper.NamedCondition(operatorapiv1.ConditionWorkDesiredDegraded,
					operatorapiv1.ReasonKlusterletDeploymentsFunctional, metav1.ConditionFalse),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(t, c.klusterlet, c.object...)
			syncContext := testingcommon.NewFakeSyncContext(t, c.klusterlet.Name)

			err := controller.controller.sync(context.TODO(), syncContext, c.klusterlet.Name)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}
			operatorActions := controller.operatorClient.Actions()

			testingcommon.AssertActions(t, operatorActions, "patch")
			klusterlet := &operatorapiv1.Klusterlet{}
			patchData := operatorActions[0].(clienttesting.PatchActionImpl).Patch
			err = json.Unmarshal(patchData, klusterlet)
			if err != nil {
				t.Fatal(err)
			}
			expectedConditions := c.expectedConditions
			meta.SetStatusCondition(&expectedConditions,
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "", metav1.ConditionTrue))
			c.expectedConditions = expectedConditions
			testinghelper.AssertOnlyConditions(t, klusterlet, c.expectedConditions...)
		})
	}
}
