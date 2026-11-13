package klusterletcontroller

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

func spiffeKlusterlet() *operatorapiv1.Klusterlet {
	k := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	k.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{
		AuthType: operatorapiv1.SPIFFEAuthType,
		SPIFFE:   &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "hub:443", HubSPIFFEID: testSPIFFEHubID},
	}
	return k
}

func spiffeConfig(k *operatorapiv1.Klusterlet) klusterletConfig {
	return klusterletConfig{
		KlusterletName:             k.Name,
		KlusterletNamespace:        helpers.KlusterletNamespace(k),
		AgentNamespace:             helpers.AgentNamespace(k),
		InstallMode:                k.Spec.DeployOption.Mode,
		RegistrationServiceAccount: serviceAccountName("registration-sa", k),
		WorkServiceAccount:         serviceAccountName("work-sa", k),
		RegistrationDriver:         RegistrationDriver{AuthType: operatorapiv1.SPIFFEAuthType},
	}
}

func newAdmissionReconcile(kube *fakekube.Clientset, amws ...runtime.Object) *spiffeAdmissionReconcile {
	servePolicyAPI(kube, "system:serviceaccount:open-cluster-management:klusterlet")
	work := fakeworkclient.NewSimpleClientset(amws...)
	return &spiffeAdmissionReconcile{
		kubeClient: kube,
		managedClusterClients: &managedClusterClients{
			kubeClient:                kube,
			appliedManifestWorkClient: work.WorkV1().AppliedManifestWorks(),
		},
		spiffeWorkAgents: func() (map[string]string, error) {
			return map[string]string{"klusterlet": "system:serviceaccount:open-cluster-management-agent:klusterlet-work-sa"}, nil
		},
	}
}

func deleteCollections(kube *fakekube.Clientset) map[string]bool {
	out := map[string]bool{}
	for _, a := range kube.Actions() {
		if a.GetVerb() == "delete-collection" {
			out[a.GetNamespace()+"/"+a.GetResource().Resource] = true
		}
	}
	return out
}

func readyCondition(k *operatorapiv1.Klusterlet) *metav1.Condition {
	return meta.FindStatusCondition(k.Status.Conditions, operatorapiv1.ConditionSPIFFEAdmissionReady)
}

func TestSPIFFEAdmissionPolicyAPIMissing(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := &spiffeAdmissionReconcile{kubeClient: kube, managedClusterClients: &managedClusterClients{kubeClient: kube}}
	k, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err == nil || state != reconcileStop {
		t.Fatalf("got state %v, err %v; want stop with an error", state, err)
	}
	cond := meta.FindStatusCondition(k.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
	if cond == nil || cond.Reason != ReasonSPIFFEPolicyAPIAbsent {
		t.Errorf("Applied condition %+v, want reason %s", cond, ReasonSPIFFEPolicyAPIAbsent)
	}
}

func TestSPIFFEAdmissionFirstActivation(t *testing.T) {
	k := spiffeKlusterlet()
	evil := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "evil", Namespace: "open-cluster-management-agent"}}
	kube := fakekube.NewSimpleClientset(evil)
	r := newAdmissionReconcile(kube)

	k, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("got state %v, err %v", state, err)
	}
	if !deleteCollections(kube)["open-cluster-management-agent/deployments"] {
		t.Error("recovery didn't clean the agent namespace")
	}
	if c := readyCondition(k); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("SPIFFEAdmissionReady %+v, want True", c)
	}
	if k.Status.SPIFFE == nil || len(k.Status.SPIFFE.AdmissionPolicies) != 3 {
		t.Fatalf("recorded policies %+v, want 3", k.Status.SPIFFE)
	}
	policies, _ := kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().List(context.TODO(), metav1.ListOptions{})
	bindings, _ := kube.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().List(context.TODO(), metav1.ListOptions{})
	if len(policies.Items) != 3 || len(bindings.Items) != 3 {
		t.Errorf("%d policies and %d bindings, want 3 and 3", len(policies.Items), len(bindings.Items))
	}
	ns, err := kube.CoreV1().Namespaces().Get(context.TODO(), helpers.DefaultAddonNamespace, metav1.GetOptions{})
	if err != nil || ns.Labels[SPIFFEAddOnNamespaceLabel] != "klusterlet" {
		t.Errorf("add-on namespace %+v, %v; want it created with the label", ns, err)
	}
	if k.Status.SPIFFE.AddOnNamespace == nil || k.Status.SPIFFE.AddOnNamespace.Name != helpers.DefaultAddonNamespace {
		t.Errorf("add-on namespace not recorded: %+v", k.Status.SPIFFE.AddOnNamespace)
	}

	// The next reconcile finds everything as recorded: no recovery.
	kube.ClearActions()
	_, state, err = r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("got state %v, err %v", state, err)
	}
	if len(deleteCollections(kube)) != 0 {
		t.Errorf("recovery ran although nothing changed: %v", deleteCollections(kube))
	}

	// The policy is recreated (new UID) while the operator wasn't watching: recovery runs again.
	kube.ClearActions()
	rec := k.Status.SPIFFE.AdmissionPolicies
	for i := range rec {
		if rec[i].PolicyName == agentPolicyPrefix+"klusterlet" {
			rec[i].PolicyUID = types.UID("old-uid")
		}
	}
	_, _, err = r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil {
		t.Fatal(err)
	}
	if !deleteCollections(kube)["open-cluster-management-agent/deployments"] {
		t.Error("recovery didn't run for a recreated policy")
	}
}

func TestSPIFFEAdmissionOperatorsOwnChange(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	k, _, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil {
		t.Fatal(err)
	}

	// A new operator version renders a different policy; the live one is still the recorded one.
	p, _ := kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(context.TODO(), agentPolicyPrefix+"klusterlet", metav1.GetOptions{})
	p.Spec.Validations = p.Spec.Validations[:1]
	if _, err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().Update(context.TODO(), p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	kube.ClearActions()
	_, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("got state %v, err %v", state, err)
	}
	if len(deleteCollections(kube)) != 0 {
		t.Errorf("recovery ran for the operator's own change: %v", deleteCollections(kube))
	}
	p, _ = kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(context.TODO(), agentPolicyPrefix+"klusterlet", metav1.GetOptions{})
	if len(p.Spec.Validations) == 1 {
		t.Error("the rendered policy wasn't applied")
	}
}

func TestSPIFFEAdmissionUntrustedNamespaces(t *testing.T) {
	k := spiffeKlusterlet()
	labeled := func(name, uid string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid),
			Labels: map[string]string{SPIFFEAddOnNamespaceLabel: "klusterlet"}}}
	}
	// default is labeled but not on record; addon-a was created by the work agent.
	amw := &workv1.AppliedManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "hash-addon-a"},
		Status: workv1.AppliedManifestWorkStatus{AppliedResources: []workv1.AppliedManifestResourceMeta{{
			ResourceIdentifier: workv1.ResourceIdentifier{Resource: "namespaces", Name: "addon-a"}, UID: "uid-a"}}}}
	kube := fakekube.NewSimpleClientset(labeled("default", "uid-default"), labeled("addon-a", "uid-a"))
	r := newAdmissionReconcile(kube, amw)

	if _, _, err := r.reconcile(context.TODO(), k, spiffeConfig(k)); err != nil {
		t.Fatal(err)
	}
	cleaned := deleteCollections(kube)
	if cleaned["default/deployments"] || cleaned["default/pods"] {
		t.Error("recovery cleaned default, which isn't on record")
	}
	if !cleaned["addon-a/deployments"] {
		t.Error("recovery didn't clean the recorded add-on namespace")
	}
	def, _ := kube.CoreV1().Namespaces().Get(context.TODO(), "default", metav1.GetOptions{})
	if _, ok := def.Labels[SPIFFEAddOnNamespaceLabel]; ok {
		t.Error("the label wasn't removed from default")
	}
}

func TestSPIFFEAdmissionExistingAddOnNamespace(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: helpers.DefaultAddonNamespace}})
	r := newAdmissionReconcile(kube)
	k, _, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err != nil {
		t.Fatal(err)
	}
	ns, _ := kube.CoreV1().Namespaces().Get(context.TODO(), helpers.DefaultAddonNamespace, metav1.GetOptions{})
	if _, ok := ns.Labels[SPIFFEAddOnNamespaceLabel]; ok {
		t.Error("an existing namespace was labeled")
	}
	if c := meta.FindStatusCondition(k.Status.Conditions, ConditionAddOnNamespaceNotDedicated); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("condition %+v, want AddOnNamespaceNotDedicated True", c)
	}
}

func TestSPIFFEAdmissionWaitsForPods(t *testing.T) {
	k := spiffeKlusterlet()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "open-cluster-management-agent"}}
	kube := fakekube.NewSimpleClientset(pod)
	// Pods take time to go: delete-collection leaves them in place.
	kube.PrependReactor("delete-collection", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil
	})
	r := newAdmissionReconcile(kube)
	k, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	var rqe commonhelpers.RequeueError
	if !errors.As(err, &rqe) || state != reconcileStop {
		t.Fatalf("got state %v, err %v; want a requeue", state, err)
	}
	if k.Status.SPIFFE != nil && len(k.Status.SPIFFE.AdmissionPolicies) != 0 {
		t.Error("recorded the admission state while pods were left")
	}
	if c := readyCondition(k); c == nil || c.Status != metav1.ConditionFalse || c.Reason != "Recovering" {
		t.Errorf("SPIFFEAdmissionReady %+v, want False/Recovering", c)
	}
}

func TestSPIFFERolePairs(t *testing.T) {
	k := spiffeKlusterlet()
	if got := spiffeRolePairs(spiffeConfig(k)); got["registration"] != "klusterlet-registration-sa" || got["work"] != "klusterlet-work-sa" {
		t.Errorf("Default pairs %v", got)
	}
	k.Spec.DeployOption.Mode = operatorapiv1.InstallModeSingleton
	if got := spiffeRolePairs(spiffeConfig(k)); len(got) != 1 || got["klusterlet"] != "klusterlet-work-sa" {
		t.Errorf("Singleton pairs %v", got)
	}
}
