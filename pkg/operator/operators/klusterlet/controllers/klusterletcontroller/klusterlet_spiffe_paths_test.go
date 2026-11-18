package klusterletcontroller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	kubeinformers "k8s.io/client-go/informers"
	coreinformer "k8s.io/client-go/informers/core/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// The constructor builds the controller, with the SPIFFE admission watch not yet started.
func TestNewKlusterletControllerWithSPIFFEWatch(t *testing.T) {
	kube := fakekube.NewSimpleClientset()
	operator := fakeoperatorclient.NewSimpleClientset()
	kubeInformers := kubeinformers.NewSharedInformerFactory(kube, time.Minute)
	operatorInformers := operatorinformers.NewSharedInformerFactory(operator, time.Minute)
	secretInformers := map[string]coreinformer.SecretInformer{
		helpers.HubKubeConfig:             kubeInformers.Core().V1().Secrets(),
		helpers.BootstrapHubKubeConfig:    kubeInformers.Core().V1().Secrets(),
		helpers.ExternalManagedKubeConfig: kubeInformers.Core().V1().Secrets(),
	}
	kubeVersion, _ := version.ParseGeneric("v1.30.0")
	c := NewKlusterletController(kube, nil, operator.OperatorV1().Klusterlets(),
		operatorInformers.Operator().V1().Klusterlets(), secretInformers,
		kubeInformers.Apps().V1().Deployments(), fakeworkclient.NewSimpleClientset().WorkV1().AppliedManifestWorks(),
		kubeVersion, "open-cluster-management", "", 1, false, false)
	if c == nil {
		t.Fatal("no controller")
	}
}

func TestFillSPIFFEConfigErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(k *operatorapiv1.Klusterlet)
		want   string
	}{
		{"no spiffe settings", func(k *operatorapiv1.Klusterlet) {
			k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE = nil
		}, "spiffe is required"},
		{"no cluster name", func(k *operatorapiv1.Klusterlet) { k.Spec.ClusterName = "" }, "clusterName is required"},
		{"bad hub ID", func(k *operatorapiv1.Klusterlet) {
			k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.HubSPIFFEID = "https://hub"
		}, "hubSPIFFEID"},
		{"bad add-on template", func(k *operatorapiv1.Klusterlet) {
			k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AddOnAgentIDTemplate = "spiffe://{trustDomain}/ocm/{cluster}/{addon}"
		}, "{agent}"},
		{"templates that overlap", func(k *operatorapiv1.Klusterlet) {
			s := k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE
			s.AgentIDTemplate = "spiffe://{trustDomain}/ocm/{cluster}/{role}/x/y"
			s.AddOnAgentIDTemplate = "spiffe://{trustDomain}/ocm/{cluster}/{addon}/{agent}/y"
		}, "can match the same SPIFFE ID"},
		{"a cluster name that is no SPIFFE ID segment", func(k *operatorapiv1.Klusterlet) {
			k.Spec.ClusterName = "bad name"
		}, "path segment"},
		{"no admission enforcement, under an older CRD", func(k *operatorapiv1.Klusterlet) {
			k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = ""
		}, "use ValidatingAdmissionPolicy (Kubernetes 1.30 or later) or ValidatingWebhook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := spiffeKlusterlet()
			c.mutate(k)
			config := spiffeConfig(k)
			_, err := fillSPIFFEConfig(&config, k)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want %q", err, c.want)
			}
		})
	}
}

// The operator's webhook ID is rendered from the agent template with the operator's namespace;
// a namespace that is no SPIFFE ID segment is an error.
func TestFillSPIFFEConfigWebhookID(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AgentIDTemplate = "spiffe://{trustDomain}/ns/{namespace}/{cluster}/{role}"
	config := spiffeConfig(k)
	config.OperatorNamespace = "open-cluster-management"
	if _, err := fillSPIFFEConfig(&config, k); err != nil ||
		config.SPIFFEWebhookID != "spiffe://ocm-hub.example.org/ns/open-cluster-management/cluster1/operator" {
		t.Errorf("webhook ID %q, %v", config.SPIFFEWebhookID, err)
	}
	config.OperatorNamespace = "bad name"
	if _, err := fillSPIFFEConfig(&config, k); err == nil {
		t.Error("no error for an operator namespace that is no path segment")
	}
}

func TestSPIFFEStatusChanged(t *testing.T) {
	old := spiffeKlusterlet()
	if spiffeStatusChanged(old, old.DeepCopy()) {
		t.Error("an unchanged status was reported as changed")
	}
	updated := old.DeepCopy()
	meta.SetStatusCondition(&updated.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionSPIFFEAdmissionReady, Status: metav1.ConditionTrue, Reason: "Recorded"})
	if !spiffeStatusChanged(old, updated) {
		t.Error("a changed SPIFFEAdmissionReady condition wasn't reported")
	}
	other := old.DeepCopy()
	meta.SetStatusCondition(&other.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionTrue, Reason: "x"})
	if spiffeStatusChanged(old, other) {
		t.Error("a change outside the SPIFFE status was reported")
	}
}

func TestStoreSPIFFEStatusGetError(t *testing.T) {
	k := spiffeKlusterlet()
	controller := newTestController(t, k, nil, nil, false)
	controller.operatorClient.PrependReactor("get", "klusterlets", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errInjected
	})
	if err := controller.controller.storeSPIFFEStatus(context.TODO(), k); !errors.Is(err, errInjected) {
		t.Errorf("storeSPIFFEStatus: %v", err)
	}
}

// A reconciler that fails with an ordinary error (here: ValidatingAdmissionPolicy enforcement
// without the API) makes the sync fail and leaves Applied false, rather than requeueing.
func TestSPIFFESyncReconcilerError(t *testing.T) {
	k := spiffeKlusterlet()
	syncContext := testingcommon.NewFakeSyncContext(t, k.Name)
	controller := newTestController(t, k, syncContext.Recorder(), nil, false, newNamespace("open-cluster-management-agent"))
	err := controller.controller.sync(context.TODO(), syncContext, k.Name)
	if err == nil || !strings.Contains(err.Error(), "ValidatingAdmissionPolicy") {
		t.Fatalf("sync: %v", err)
	}
	got, _ := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), k.Name, metav1.GetOptions{})
	cond := meta.FindStatusCondition(got.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonSPIFFEUnsupportedKubernetesVersion {
		t.Errorf("Applied %+v", cond)
	}
}

// failingLister is a Klusterlet lister whose List fails.
type failingLister struct {
	operatorlister.KlusterletLister
}

func (failingLister) List(labels.Selector) ([]*operatorapiv1.Klusterlet, error) {
	return nil, errInjected
}

func TestSPIFFEWorkAgentsListError(t *testing.T) {
	if _, err := spiffeWorkAgentsFrom(failingLister{}); !errors.Is(err, errInjected) {
		t.Errorf("spiffeWorkAgentsFrom: %v", err)
	}
	keys := spiffeAdmissionQueueKeys(failingLister{})
	if got := keys(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: namespaceLabelPolicyName}}); got != nil {
		t.Errorf("queue keys %v, want none when the Klusterlets can't be listed", got)
	}
	// An object without metadata maps to no Klusterlet.
	if got := keys(&runtime.Unknown{}); got != nil {
		t.Errorf("queue keys %v for an object without metadata", got)
	}
}

// Deleting a spiffe Klusterlet removes its admission policies, through the cleanup controller.
func TestSPIFFECleanupRemovesPolicies(t *testing.T) {
	k := spiffeKlusterlet()
	syncContext := testingcommon.NewFakeSyncContext(t, k.Name)
	controller := newTestController(t, k, syncContext.Recorder(), nil, false, newNamespace("open-cluster-management-agent"))
	servePolicyAPI(controller.kubeClient, "system:serviceaccount:open-cluster-management:klusterlet")
	r := &spiffeAdmissionReconcile{
		kubeClient: controller.kubeClient,
		managedClusterClients: &managedClusterClients{kubeClient: controller.kubeClient,
			appliedManifestWorkClient: controller.workClient.WorkV1().AppliedManifestWorks()},
		spiffeWorkAgents: func() (map[string]string, error) { return map[string]string{k.Name: "x"}, nil },
		watch:            startedWatch(),
	}
	k = activate(t, r, k, spiffeConfig(k))
	now := metav1.Now()
	k.DeletionTimestamp = &now
	if err := controller.operatorStore.Update(k); err != nil {
		t.Fatal(err)
	}
	if err := controller.cleanupController.sync(context.TODO(), syncContext, k.Name); err != nil {
		t.Fatal(err)
	}
	policies, _ := controller.kubeClient.AdmissionregistrationV1().ValidatingAdmissionPolicies().List(context.TODO(), metav1.ListOptions{})
	if len(policies.Items) != 0 {
		t.Errorf("%d admission policies left after the Klusterlet was deleted", len(policies.Items))
	}
}

// Without spiffe, an error creating the add-on namespace stops the managed reconcile.
func TestManagedReconcileAddOnNamespaceError(t *testing.T) {
	k := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	syncContext := testingcommon.NewFakeSyncContext(t, k.Name)
	controller := newTestController(t, k, syncContext.Recorder(), nil, false,
		newNamespace("open-cluster-management-agent"), newSecret(helpers.BootstrapHubKubeConfig, "open-cluster-management-agent"))
	controller.kubeClient.PrependReactor("create", "namespaces", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.(clienttesting.CreateAction).GetObject().(metav1.Object).GetName() == helpers.DefaultAddonNamespace {
			return true, nil, errInjected
		}
		return false, nil, nil
	})
	if err := controller.controller.sync(context.TODO(), syncContext, k.Name); err == nil ||
		!strings.Contains(err.Error(), errInjected.Error()) {
		t.Errorf("sync: %v", err)
	}
}

// Without spiffe, a missing hub kubeconfig Secret keeps the work agent at zero replicas.
func TestWorkAgentWaitsForHubKubeconfigSecret(t *testing.T) {
	k := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	meta.SetStatusCondition(&k.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionHubConnectionDegraded, Status: metav1.ConditionTrue,
		Reason: operatorapiv1.ReasonHubKubeConfigSecretMissing})
	syncContext := testingcommon.NewFakeSyncContext(t, k.Name)
	controller := newTestController(t, k, syncContext.Recorder(), nil, false,
		newNamespace("open-cluster-management-agent"), newSecret(helpers.BootstrapHubKubeConfig, "open-cluster-management-agent"))
	if err := controller.controller.sync(context.TODO(), syncContext, k.Name); err != nil {
		t.Fatal(err)
	}
	d := getDeployments(controller.kubeClient.Actions(), createVerb, "work-agent")
	if d == nil || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
		t.Errorf("work agent %+v, want 0 replicas", d)
	}
}
