package klusterletcontroller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "")
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	bootstrapKubeConfigSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	bootstrapKubeConfigSecret.Data["kubeconfig"] = newKubeConfig("testhost")
	namespace := newNamespace("testns")
	appliedManifestWorks := []runtime.Object{
		newAppliedManifestWorks("testhost", nil, false),
		newAppliedManifestWorks("testhost", []string{workv1.AppliedManifestWorkFinalizer}, true),
		newAppliedManifestWorks("testhost-2", []string{workv1.AppliedManifestWorkFinalizer}, false),
	}
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), appliedManifestWorks, false,
		namespace, bootstrapKubeConfigSecret)

	err := controller.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteActions []clienttesting.DeleteActionImpl
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == deleteVerb {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	// 11 managed static manifests + 12 management static manifests + 1 hub kubeconfig + 2 namespaces + 2 deployments
	if len(deleteActions) != 28 {
		t.Errorf("Expected 28 delete actions, but got %d", len(deleteActions))
	}

	var updateWorkActions []clienttesting.PatchActionImpl
	workActions := controller.workClient.Actions()
	for _, action := range workActions {
		if action.GetVerb() == "patch" {
			updateAction := action.(clienttesting.PatchActionImpl)
			updateWorkActions = append(updateWorkActions, updateAction)
			continue
		}
	}

	// update 2 appliedminifestwork to remove appliedManifestWorkFinalizer, using agentID to filter, ignore hub host
	if len(updateWorkActions) != 2 {
		t.Errorf("Expected 2 update action, but got %d", len(updateWorkActions))
	}
}

func TestSyncDeleteHosted(t *testing.T) {
	klusterlet := newKlusterletHosted("klusterlet", "testns", "cluster1")
	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
		Message: "Klusterlet is ready to apply",
	})
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	agentNamespace := helpers.AgentNamespace(klusterlet)
	bootstrapKubeConfigSecret := newSecret(helpers.BootstrapHubKubeConfig, agentNamespace)
	bootstrapKubeConfigSecret.Data["kubeconfig"] = newKubeConfig("testhost")
	// externalManagedSecret := newSecret(helpers.ExternalManagedKubeConfig, agentNamespace)
	// externalManagedSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace(agentNamespace)
	appliedManifestWorks := []runtime.Object{
		newAppliedManifestWorks("testhost", nil, false),
		newAppliedManifestWorks("testhost", []string{workv1.AppliedManifestWorkFinalizer}, true),
		newAppliedManifestWorks("testhost-2", []string{workv1.AppliedManifestWorkFinalizer}, false),
	}
	syncContext := testingcommon.NewFakeSyncContext(t, klusterlet.Name)
	controller := newTestControllerHosted(t, klusterlet, appliedManifestWorks,
		bootstrapKubeConfigSecret, namespace /*externalManagedSecret*/)

	err := controller.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteActionsManagement []clienttesting.DeleteActionImpl
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == deleteVerb {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActionsManagement = append(deleteActionsManagement, deleteAction)
		}
	}

	// 11 static manifests + 3 secrets(hub-kubeconfig-secret, external-managed-kubeconfig-registration,external-managed-kubeconfig-work)
	// + 2 deployments(registration-agent,work-agent) + 1 namespace
	if len(deleteActionsManagement) != 17 {
		t.Errorf("Expected 17 delete actions, but got %d", len(deleteActionsManagement))
	}

	var deleteActionsManaged []clienttesting.DeleteActionImpl
	for _, action := range controller.managedKubeClient.Actions() {
		if action.GetVerb() == deleteVerb {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActionsManaged = append(deleteActionsManaged, deleteAction)
		}
	}

	// 12 static manifests + 2 namespaces
	if len(deleteActionsManaged) != 14 {
		t.Errorf("Expected 14 delete actions, but got %d", len(deleteActionsManaged))
	}

	var updateWorkActions []clienttesting.PatchActionImpl
	workActions := controller.managedWorkClient.Actions()
	for _, action := range workActions {
		if action.GetVerb() == "patch" {
			updateAction := action.(clienttesting.PatchActionImpl)
			updateWorkActions = append(updateWorkActions, updateAction)
			continue
		}
	}

	// update 2 appliedminifestwork to remove appliedManifestWorkFinalizer, using agentID to filter, ignore hub host
	if len(updateWorkActions) != 2 {
		t.Errorf("Expected 2 update action, but got %d", len(updateWorkActions))
	}
}

func TestSyncDeleteHostedDeleteAgentNamespace(t *testing.T) {
	klusterlet := removeKlusterletFinalizer(
		newKlusterletHosted("klusterlet", "testns", "cluster1"),
		klusterletHostedFinalizer)
	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionFalse, Reason: "KlusterletPrepareFailed",
		Message: fmt.Sprintf("Failed to build managed cluster clients: %v", "namespaces \"klusterlet\" not found"),
	})
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestControllerHosted(t, klusterlet, nil).setDefaultManagedClusterClientsBuilder()

	err := controller.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	kubeActions := controller.kubeClient.Actions()
	// assert there last action is deleting the klusterlet agent namespace on the management cluster
	testingcommon.AssertDelete(t, kubeActions[len(kubeActions)-1], "namespaces", "", "klusterlet")
}

func TestSyncDeleteHostedDeleteWaitKubeconfig(t *testing.T) {
	klusterlet := newKlusterletHosted("klusterlet", "testns", "cluster1")
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestControllerHosted(t, klusterlet, nil).setDefaultManagedClusterClientsBuilder()

	err := controller.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// assert no delete action on the management cluster,should wait for the kubeconfig
	for _, action := range controller.kubeClient.Actions() {
		if action.GetVerb() == deleteVerb {
			t.Errorf("Expected not delete the resources, should wait for the kubeconfig, but got delete actions")
		}
	}
}

func TestSyncAddHostedFinalizerWhenKubeconfigReady(t *testing.T) {
	klusterlet := removeKlusterletFinalizer(
		newKlusterletHosted("klusterlet", "testns", "cluster1"),
		klusterletHostedFinalizer)

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	c := newTestControllerHosted(t, klusterlet, nil)

	err := c.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	klusterlet, err = c.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterlet.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Expected non error when get klusterlet, %v", err)
	}
	if commonhelper.HasFinalizer(klusterlet.Finalizers, klusterletHostedFinalizer) {
		t.Errorf("Expected no klusterlet hosted finalizer")
	}

	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
		Message: "Klusterlet is ready to apply",
	})
	if err := c.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}
	err = c.cleanupController.sync(context.TODO(), syncContext, "klusterlet")
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	klusterlet, err = c.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterlet.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Expected non error when get klusterlet, %v", err)
	}
	if !commonhelper.HasFinalizer(klusterlet.Finalizers, klusterletHostedFinalizer) {
		t.Errorf("Expected there is klusterlet hosted finalizer")
	}
}

func TestConnectivityError(t *testing.T) {
	cases := []struct {
		name                        string
		err                         error
		isTCPTimeOutError           bool
		isTCPNoSuchHostError        bool
		isTCPConnectionRefusedError bool
	}{
		{
			name:              "TCPTimeOutError",
			err:               fmt.Errorf("dial tcp 172.0.0.1:443: connect: i/o timeout"),
			isTCPTimeOutError: true,
		},
		{
			name:                 "TCPNoSuchHostError",
			err:                  fmt.Errorf("dial tcp: lookup foo.bar.com: connect: no such host"),
			isTCPNoSuchHostError: true,
		},
		{
			name:                        "TCPConnectionRefusedError",
			err:                         fmt.Errorf("dial tcp 172.0.0.1:443: connect: connection refused"),
			isTCPConnectionRefusedError: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.isTCPTimeOutError, isTCPTimeOutError(c.err), c.name)
			assert.Equal(t, c.isTCPNoSuchHostError, isTCPNoSuchHostError(c.err), c.name)
			assert.Equal(t, c.isTCPConnectionRefusedError, isTCPConnectionRefusedError(c.err), c.name)
		})
	}
}
