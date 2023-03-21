package klusterletcontroller

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
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
		newAppliedManifestWorks("testhost", []string{appliedManifestWorkFinalizer}, true),
		newAppliedManifestWorks("testhost-2", []string{appliedManifestWorkFinalizer}, false),
	}
	controller := newTestController(t, klusterlet, appliedManifestWorks, namespace, bootstrapKubeConfigSecret)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteActions []clienttesting.DeleteActionImpl
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			klog.Infof("kube delete name: %v\t resource:%v \t namespace:%v", deleteAction.Name, deleteAction.GetResource(), deleteAction.GetNamespace())
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	// 11 managed static manifests + 11 management static manifests + 1 hub kubeconfig + 2 namespaces + 2 deployments
	if len(deleteActions) != 27 {
		t.Errorf("Expected 27 delete actions, but got %d", len(deleteActions))
	}

	var updateWorkActions []clienttesting.UpdateActionImpl
	workActions := controller.workClient.Actions()
	for _, action := range workActions {
		if action.GetVerb() == "update" {
			updateAction := action.(clienttesting.UpdateActionImpl)
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
		Type: klusterletReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
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
		newAppliedManifestWorks("testhost", []string{appliedManifestWorkFinalizer}, true),
		newAppliedManifestWorks("testhost-2", []string{appliedManifestWorkFinalizer}, false),
	}
	controller := newTestControllerHosted(t, klusterlet, appliedManifestWorks, bootstrapKubeConfigSecret, namespace /*externalManagedSecret*/)
	syncContext := testinghelper.NewFakeSyncContext(t, klusterlet.Name)

	err := controller.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteActionsManagement []clienttesting.DeleteActionImpl
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			klog.Infof("management kube delete name: %v\t resource:%v \t namespace:%v", deleteAction.Name, deleteAction.GetResource(), deleteAction.GetNamespace())
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
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			klog.Infof("managed kube delete name: %v\t resource:%v \t namespace:%v", deleteAction.Name, deleteAction.GetResource(), deleteAction.GetNamespace())
			deleteActionsManaged = append(deleteActionsManaged, deleteAction)
		}
	}

	// 11 static manifests + 2 namespaces
	if len(deleteActionsManaged) != 13 {
		t.Errorf("Expected 13 delete actions, but got %d", len(deleteActionsManaged))
	}

	var updateWorkActions []clienttesting.UpdateActionImpl
	workActions := controller.managedWorkClient.Actions()
	for _, action := range workActions {
		if action.GetVerb() == "update" {
			updateAction := action.(clienttesting.UpdateActionImpl)
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
		Type: klusterletReadyToApply, Status: metav1.ConditionFalse, Reason: "KlusterletPrepareFailed",
		Message: fmt.Sprintf("Failed to build managed cluster clients: %v", "namespaces \"klusterlet\" not found"),
	})
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newTestControllerHosted(t, klusterlet, nil).setDefaultManagedClusterClientsBuilder()
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	kubeActions := controller.kubeClient.Actions()
	// assert there last action is deleting the klusterlet agent namespace on the management cluster
	testinghelper.AssertDelete(t, kubeActions[len(kubeActions)-1], "namespaces", "", "klusterlet")
}

func TestSyncDeleteHostedDeleteWaitKubeconfig(t *testing.T) {
	klusterlet := newKlusterletHosted("klusterlet", "testns", "cluster1")
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	controller := newTestControllerHosted(t, klusterlet, nil).setDefaultManagedClusterClientsBuilder()
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// assert no delete action on the management cluster,should wait for the kubeconfig
	for _, action := range controller.kubeClient.Actions() {
		if action.GetVerb() == "delete" {
			t.Errorf("Expected not delete the resources, should wait for the kubeconfig, but got delete actions")
		}
	}
}

func TestSyncAddHostedFinalizerWhenKubeconfigReady(t *testing.T) {
	klusterlet := removeKlusterletFinalizer(
		newKlusterletHosted("klusterlet", "testns", "cluster1"),
		klusterletHostedFinalizer)

	c := newTestControllerHosted(t, klusterlet, nil)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := c.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	klusterlet, err = c.cleanupController.klusterletClient.Get(context.TODO(), klusterlet.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Expected non error when get klusterlet, %v", err)
	}
	if hasFinalizer(klusterlet, klusterletHostedFinalizer) {
		t.Errorf("Expected no klusterlet hosted finalizer")
	}

	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: klusterletReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
		Message: "Klusterlet is ready to apply",
	})
	if err := c.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}
	err = c.cleanupController.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	klusterlet, err = c.cleanupController.klusterletClient.Get(context.TODO(), klusterlet.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Expected non error when get klusterlet, %v", err)
	}
	if !hasFinalizer(klusterlet, klusterletHostedFinalizer) {
		t.Errorf("Expected there is klusterlet hosted finalizer")
	}
}
