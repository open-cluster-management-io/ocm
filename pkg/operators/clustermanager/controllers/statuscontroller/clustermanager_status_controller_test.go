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

	fakeoperatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	testinghelper "github.com/open-cluster-management/registration-operator/pkg/helpers/testing"
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

func newDeployment(desiredReplica, availableReplica int32) *appsv1.Deployment {
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
			name:            "failed to get registration deployment",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments:     []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 2)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition := testinghelper.NamedCondition(registrationDegraded, "GetRegistrationDeploymentFailed", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition)
			},
		},
		{
			name:            "unavailable registration pods",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments:     []runtime.Object{newDeployment(3, 0)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 2)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition := testinghelper.NamedCondition(registrationDegraded, "UnavailableRegistrationPod", metav1.ConditionTrue)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition)
			},
		},
		{
			name:            "registration functional",
			queueKey:        testClusterManagerName,
			clusterManagers: []runtime.Object{newClusterManager()},
			deployments:     []runtime.Object{newDeployment(3, 3)},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelper.AssertEqualNumber(t, len(actions), 2)
				testinghelper.AssertGet(t, actions[0], "operator.open-cluster-management.io", "v1", "clustermanagers")
				testinghelper.AssertAction(t, actions[1], "update")
				expectedCondition := testinghelper.NamedCondition(registrationDegraded, "RegistrationFunctional", metav1.ConditionFalse)
				testinghelper.AssertOnlyConditions(t, actions[1].(clienttesting.UpdateActionImpl).Object, expectedCondition)
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
