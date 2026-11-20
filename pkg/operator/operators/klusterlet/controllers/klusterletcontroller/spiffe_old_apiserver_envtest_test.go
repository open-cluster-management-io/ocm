package klusterletcontroller

import (
	"context"
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

// TestSPIFFEOnAPIServerWithoutPolicies runs the spiffe admission reconcile against a real
// kube-apiserver older than 1.30, which has no ValidatingAdmissionPolicy API: a Klusterlet that
// asks for ValidatingAdmissionPolicy enforcement is refused as an unsupported Kubernetes version;
// with ValidatingWebhook the operator puts the webhook configurations in place.
func TestSPIFFEOnAPIServerWithoutPolicies(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("needs envtest: set KUBEBUILDER_ASSETS")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = env.Stop() }()
	ctx := context.Background()
	kube := kubernetes.NewForConfigOrDie(cfg)
	served, err := policyAPIServed(kube)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if served {
		t.Skip("needs an API server without ValidatingAdmissionPolicy (Kubernetes older than 1.30, such as 1.29)")
	}
	if _, err := kube.CoreV1().Namespaces().Create(ctx, newNamespace("open-cluster-management-agent"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	r := &spiffeAdmissionReconcile{
		kubeClient: kube,
		managedClusterClients: &managedClusterClients{kubeClient: kube,
			appliedManifestWorkClient: fakeworkclient.NewSimpleClientset().WorkV1().AppliedManifestWorks()},
		spiffeWorkAgents: func() (map[string]string, error) { return map[string]string{"klusterlet": envWorkAgent}, nil },
		watch:            startedWatch(),
		webhook:          testWebhookServer(t, newFakeSource(t, testWebhookID, testWebhookDNS)),
	}

	// ValidatingAdmissionPolicy: refused as an unsupported Kubernetes version.
	k := spiffeKlusterlet()
	k, state, err := r.reconcile(ctx, k, testWebhookConfig(k))
	if err == nil || state != reconcileStop {
		t.Fatalf("ValidatingAdmissionPolicy: state %v, err %v; want the Klusterlet refused", state, err)
	}
	if c := meta.FindStatusCondition(k.Status.Conditions, operatorapiv1.ConditionKlusterletApplied); c == nil ||
		c.Reason != ReasonSPIFFEUnsupportedKubernetesVersion || !strings.HasPrefix(c.Message, "unsupported Kubernetes version v1.") {
		t.Errorf("ValidatingAdmissionPolicy: Applied %+v, want reason %s", c, ReasonSPIFFEUnsupportedKubernetesVersion)
	}

	// ValidatingWebhook: in place and recorded.
	k = spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	k = activate(t, r, k, testWebhookConfig(k))
	if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingWebhook || len(k.Status.SPIFFE.AdmissionWebhooks) != 3 {
		t.Errorf("status %+v", k.Status.SPIFFE)
	}
	for _, name := range []string{agentPolicyPrefix + k.Name, addOnPolicyPrefix + k.Name, namespaceLabelPolicyName} {
		if _, err := kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("webhook configuration %s: %v", name, err)
		}
	}
}
