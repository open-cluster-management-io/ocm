package statuscontroller

import (
	"context"
	"encoding/json"
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
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelper "open-cluster-management.io/ocm/pkg/operator/helpers/testing"
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
		Status: operatorapiv1.ClusterManagerStatus{
			Conditions: []metav1.Condition{
				{
					Type:   operatorapiv1.ConditionClusterManagerApplied,
					Status: metav1.ConditionTrue,
				},
			},
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

func newRegistrationWebhookDeployment(desiredReplica, availableReplica int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-registration-webhook", testClusterManagerName),
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
	appliedCond := metav1.Condition{
		Type:   operatorapiv1.ConditionClusterManagerApplied,
		Status: metav1.ConditionTrue,
	}
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
				testingcommon.AssertEqualNumber(t, len(actions), 0)
			},
		},
		{
			name:            "no cluster manager",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{},
			deployments:     []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertEqualNumber(t, len(actions), 0)
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
				testingcommon.AssertActions(t, actions, "patch")
				klusterlet := &operatorapiv1.Klusterlet{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, klusterlet)
				if err != nil {
					t.Fatal(err)
				}
				expectedCondition1 := testinghelper.NamedCondition(operatorapiv1.ConditionHubRegistrationDegraded, "GetRegistrationDeploymentFailed", metav1.ConditionTrue)
				expectedCondition2 := testinghelper.NamedCondition(operatorapiv1.ConditionHubPlacementDegraded, "UnavailablePlacementPod", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, klusterlet, appliedCond, expectedCondition1, expectedCondition2)
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
				testingcommon.AssertActions(t, actions, "patch")
				klusterlet := &operatorapiv1.Klusterlet{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, klusterlet)
				if err != nil {
					t.Fatal(err)
				}
				expectedCondition1 := testinghelper.NamedCondition(operatorapiv1.ConditionHubRegistrationDegraded, "UnavailableRegistrationPod", metav1.ConditionTrue)
				expectedCondition2 := testinghelper.NamedCondition(operatorapiv1.ConditionHubPlacementDegraded, "PlacementFunctional", metav1.ConditionFalse)
				testinghelper.AssertOnlyConditions(t, klusterlet, appliedCond, expectedCondition1, expectedCondition2)
			},
		},
		{
			name:            "unavailable registration webhook pods and placement functional",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments: []runtime.Object{
				newRegistrationDeployment(3, 3),
				newRegistrationWebhookDeployment(3, 0),
				newPlacementDeployment(3, 3),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				klusterlet := &operatorapiv1.Klusterlet{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, klusterlet)
				if err != nil {
					t.Fatal(err)
				}
				expectedCondition1 := testinghelper.NamedCondition(operatorapiv1.ConditionHubRegistrationDegraded, "UnavailableRegistrationPod", metav1.ConditionTrue)
				expectedCondition2 := testinghelper.NamedCondition(operatorapiv1.ConditionHubPlacementDegraded, "PlacementFunctional", metav1.ConditionFalse)
				testinghelper.AssertOnlyConditions(t, klusterlet, appliedCond, expectedCondition1, expectedCondition2)
			},
		},
		{
			name:            "registration functional and no placement deployment",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments:     []runtime.Object{newRegistrationDeployment(3, 3), newRegistrationWebhookDeployment(3, 3)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				klusterlet := &operatorapiv1.Klusterlet{}
				patchData := actions[0].(clienttesting.PatchActionImpl).Patch
				err := json.Unmarshal(patchData, klusterlet)
				if err != nil {
					t.Fatal(err)
				}
				expectedCondition1 := testinghelper.NamedCondition(operatorapiv1.ConditionHubRegistrationDegraded, "RegistrationFunctional", metav1.ConditionFalse)
				expectedCondition2 := testinghelper.NamedCondition(operatorapiv1.ConditionHubPlacementDegraded, "GetPlacementDeploymentFailed", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, klusterlet, appliedCond, expectedCondition1, expectedCondition2)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.deployments...)
			kubeInformers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 5*time.Minute)
			deployStore := kubeInformers.Apps().V1().Deployments().Informer().GetStore()
			for _, deployment := range c.deployments {
				if err := deployStore.Add(deployment); err != nil {
					t.Fatal(err)
				}
			}

			fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(c.clusterManagers...)
			operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			for _, clusterManager := range c.clusterManagers {
				if err := clusterManagerStore.Add(clusterManager); err != nil {
					t.Fatal(err)
				}
			}

			controller := &clusterManagerStatusController{
				deploymentLister:     kubeInformers.Apps().V1().Deployments().Lister(),
				clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
				patcher: patcher.NewPatcher[
					*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
					fakeOperatorClient.OperatorV1().ClusterManagers()),
			}

			syncContext := testingcommon.NewFakeSyncContext(t, c.queueKey)
			err := controller.sync(context.TODO(), syncContext, c.queueKey)
			if err != nil {
				t.Errorf("Expected no error when update status: %v", err)
			}
			c.validateActions(t, fakeOperatorClient.Actions())
		})
	}

}
