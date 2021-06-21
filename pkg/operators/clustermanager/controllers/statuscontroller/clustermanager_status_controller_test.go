package statuscontroller

import (
	"context"
	"fmt"
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

const testClusterManagerName = "testclustermanager"

func newClusterManager() *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: testClusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
		},
	}
}

func newRegistrationDeployment(desiredReplica, availableReplica int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-registration-controller", testClusterManagerName),
			Namespace: "open-cluster-management-hub",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desiredReplica,
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: availableReplica,
		},
	}
}

func newPlacementDeployment(desiredReplica, availableReplica int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-placement-controller", testClusterManagerName),
			Namespace: "open-cluster-management-hub",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desiredReplica,
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: availableReplica,
		},
	}
}

func TestSyncStatus(t *testing.T) {
	cases := []struct {
		name            string
		queueKey        string
		clusterManagers []runtime.Object
		deployments     []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "empty queue key",
			queueKey:        "",
			clusterManagers: []runtime.Object{},
			deployments:     []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 0)
			},
		},
		{
			name:            "no cluster manager",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{},
			deployments:     []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 0)
			},
		},
		{
			name:            "no registration deployment and unavailable placement pods",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments: []runtime.Object{
				newPlacementDeployment(3, 0),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 4)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition1 := testinghelper.NamedCondition(registrationDegraded, "GetRegistrationDeploymentFailed", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition1)

				testinghelper.AssertGet(t, actions[2], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[3], "update")
				expectedCondition2 := testinghelper.NamedCondition(placementDegraded, "UnavailablePlacementPod", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[3].(clienttesting.UpdateActionImpl).Object, expectedCondition1, expectedCondition2)
			},
		},
		{
			name:            "unavailable registration pods and placement functional",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments: []runtime.Object{
				newRegistrationDeployment(3, 0),
				newPlacementDeployment(3, 3),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 4)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition1 := testinghelper.NamedCondition(registrationDegraded, "UnavailableRegistrationPod", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition1)

				testinghelper.AssertGet(t, actions[2], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[3], "update")
				expectedCondition2 := testinghelper.NamedCondition(placementDegraded, "PlacementFunctional", metav1.ConditionFalse)
				testinghelper.AssertOnlyConditions(t, actions[3].(clienttesting.UpdateActionImpl).Object, expectedCondition1, expectedCondition2)
			},
		},
		{
			name:            "registration functional and no placement deployment",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments:     []runtime.Object{newRegistrationDeployment(3, 3)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 4)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition1 := testinghelper.NamedCondition(registrationDegraded, "RegistrationFunctional", metav1.ConditionFalse)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition1)

				testinghelper.AssertGet(t, actions[2], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[3], "update")
				expectedCondition2 := testinghelper.NamedCondition(placementDegraded, "GetPlacementDeploymentFailed", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[3].(clienttesting.UpdateActionImpl).Object, expectedCondition1, expectedCondition2)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.deployments...)
			kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)
			deployStore := kubeInformers.Apps().V1().Deployments().Informer().GetStore()
			for _, deployment := range c.deployments {
				deployStore.Add(deployment)
			}

			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.clusterManagers...)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			for _, clusterManager := range c.clusterManagers {
				clusterManagerStore.Add(clusterManager)
			}

			controller := &clusterManagerStatusController{
				deploymentLister:     kubeInformers.Apps().V1().Deployments().Lister(),
				clusterManagerClient: fakeOperatorClient.OperatorV1().ClusterManagers(),
				clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
			}

			syncContext := testinghelper.NewFakeSyncContext(t, c.queueKey)
			err := controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}
			c.validateActions(t, fakeOperatorClient.Actions())
		})
	}

}
