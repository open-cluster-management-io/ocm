package klusterletcontroller

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	operatorv1listers "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
)

const (
	testWebhookID  = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/operator"
	testWebhookDNS = "klusterlet-spiffe-webhook.open-cluster-management.svc"
)

// fakeSource is an operator SVID from a test CA. The leaf also carries 127.0.0.1, so a local API
// server can call the webhook by IP.
type fakeSource struct {
	mu        sync.Mutex
	svid      *x509svid.SVID
	ca        *x509.Certificate
	svidErr   error
	bundleErr error
	closed    bool
}

func newFakeSource(t *testing.T, id string, dnsNames ...string) *fakeSource {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	caKey, leafKey := newKey(), newKey()
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test SPIRE CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	spiffeID := spiffeid.RequireFromString(id)
	uri, _ := url.Parse(spiffeID.String())
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), URIs: []*url.URL{uri}, DNSNames: dnsNames,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(leafDER)
	return &fakeSource{ca: ca,
		svid: &x509svid.SVID{ID: spiffeID, Certificates: []*x509.Certificate{leaf}, PrivateKey: leafKey}}
}

func (f *fakeSource) GetX509SVID() (*x509svid.SVID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.svid, f.svidErr
}

func (f *fakeSource) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return x509bundle.FromX509Authorities(td, []*x509.Certificate{f.ca}), f.bundleErr
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSource) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// testWebhookServer serves on a local port, with the given SVID source, and is reached by URL.
func testWebhookServer(t *testing.T, src x509Source) *spiffeWebhookServer {
	t.Helper()
	s := newSPIFFEWebhookServer("open-cluster-management")
	s.addr = "127.0.0.1:0"
	var addr string
	s.listen = func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err == nil {
			addr = l.Addr().String()
		}
		return l, err
	}
	s.newSource = func(context.Context, string, spiffeid.ID) (x509Source, error) { return src, nil }
	s.clientConfig = func(path string, caBundle []byte) admissionregistrationv1.WebhookClientConfig {
		return admissionregistrationv1.WebhookClientConfig{URL: ptr.To("https://" + addr + path), CABundle: caBundle}
	}
	return s
}

func testInput() spiffeAdmissionInput {
	return spiffeAdmissionInput{
		klusterletName: "klusterlet", agentNamespace: "open-cluster-management-agent",
		operatorUser: envOperator, managedOperatorUser: envOperator,
		pairs:         map[string]string{"registration": "klusterlet-registration-sa", "work": "klusterlet-work-sa"},
		workAgentUser: envWorkAgent,
		workAgents:    map[string]string{"klusterlet": envWorkAgent},
	}
}

func raw(t *testing.T, obj interface{}) runtime.RawExtension {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return runtime.RawExtension{Raw: b}
}

func podRequest(t *testing.T, user string, op admissionv1.Operation, labels map[string]string, sa, owner string) *admissionv1.AdmissionRequest {
	pod := map[string]interface{}{
		"metadata": map[string]interface{}{"name": "p", "labels": labels},
		"spec":     map[string]interface{}{"serviceAccountName": sa, "containers": []interface{}{map[string]interface{}{"image": "x"}}},
	}
	if owner != "" {
		pod["metadata"].(map[string]interface{})["ownerReferences"] = []interface{}{
			map[string]interface{}{"kind": owner, "controller": true}}
	}
	return &admissionv1.AdmissionRequest{
		UID:      "uid-1",
		Kind:     metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Resource: metav1.GroupVersionResource{Version: "v1", Resource: "pods"}, Operation: op,
		UserInfo: authenticationv1.UserInfo{Username: user}, Object: raw(t, pod),
	}
}

// The webhook applies the policies' own CEL rules.
func TestCELPolicyAdmit(t *testing.T) {
	p, _ := agentAdmissionPolicy(testInput())
	compiled, err := compilePolicy(p.Spec)
	if err != nil {
		t.Fatal(err)
	}
	role := func(r string) map[string]string { return map[string]string{SPIFFERoleLabel: r} }
	cases := []struct {
		name string
		req  *admissionv1.AdmissionRequest
		want string
	}{
		{"an agent pod by the ReplicaSet controller", podRequest(t, replicaSetController, admissionv1.Create,
			role("work"), "klusterlet-work-sa", "ReplicaSet"), ""},
		{"a pod by alice", podRequest(t, "alice", admissionv1.Create, role("work"), "klusterlet-work-sa", "ReplicaSet"),
			"Pods must be created by the ReplicaSet controller"},
		{"a wrong role and ServiceAccount pair (message expression)", podRequest(t, replicaSetController,
			admissionv1.Create, role("work"), "klusterlet-registration-sa", "ReplicaSet"),
			"role work is not allowed with ServiceAccount klusterlet-registration-sa"},
		{"a Job", &admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"},
			Operation: admissionv1.Create, Object: raw(t, map[string]interface{}{"metadata": map[string]interface{}{}})},
			"Jobs and CronJobs are not allowed"},
		{"exec", &admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "PodExecOptions"},
			SubResource: "exec", Operation: admissionv1.Connect, Object: raw(t, map[string]interface{}{"command": []string{"sh"}})},
			"exec and attach are not allowed"},
		{"an object that isn't JSON", &admissionv1.AdmissionRequest{Object: runtime.RawExtension{Raw: []byte("{")}},
			"the object can't be decoded"},
		{"an old object that isn't JSON", &admissionv1.AdmissionRequest{Object: raw(t, map[string]interface{}{}),
			OldObject: runtime.RawExtension{Raw: []byte("{")}}, "the old object can't be decoded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compiled.admit(c.req)
			if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
				t.Errorf("admit: %q, want %q", got, c.want)
			}
		})
	}
}

// What can't be compiled is reported; what can't be evaluated refuses the request.
func TestCELPolicyErrors(t *testing.T) {
	spec := func(variables []admissionregistrationv1.Variable, validations ...admissionregistrationv1.Validation) admissionregistrationv1.ValidatingAdmissionPolicySpec {
		return admissionregistrationv1.ValidatingAdmissionPolicySpec{Variables: variables, Validations: validations}
	}
	for name, s := range map[string]admissionregistrationv1.ValidatingAdmissionPolicySpec{
		"variable":           spec([]admissionregistrationv1.Variable{{Name: "x", Expression: "1 +"}}),
		"validation":         spec(nil, admissionregistrationv1.Validation{Expression: "true &&"}),
		"message expression": spec(nil, admissionregistrationv1.Validation{Expression: "true", MessageExpression: "'a' +"}),
	} {
		if _, err := compilePolicy(s); err == nil {
			t.Errorf("%s: compiled", name)
		}
	}

	req := &admissionv1.AdmissionRequest{Object: raw(t, map[string]interface{}{"a": 1})}
	for name, c := range map[string]struct {
		spec admissionregistrationv1.ValidatingAdmissionPolicySpec
		want string
	}{
		"a variable that fails": {spec([]admissionregistrationv1.Variable{{Name: "x", Expression: "object.missing"}}),
			"variable x"},
		"a validation that fails": {spec(nil, admissionregistrationv1.Validation{Expression: "object.missing == 1"}),
			"failed to evaluate"},
		"a message expression that fails": {spec(nil, admissionregistrationv1.Validation{Expression: "false",
			Message: "fallback", MessageExpression: "object.missing"}), "fallback"},
		"a message expression that isn't a string": {spec(nil, admissionregistrationv1.Validation{Expression: "false",
			Message: "fallback", MessageExpression: "1"}), "fallback"},
	} {
		p, err := compilePolicy(c.spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.admit(req); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func review(t *testing.T, req *admissionv1.AdmissionRequest) *bytes.Buffer {
	b, err := json.Marshal(&admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Request: req})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

func TestWebhookServeHTTP(t *testing.T) {
	s := newSPIFFEWebhookServer("open-cluster-management")
	p, b := agentAdmissionPolicy(testInput())
	if err := s.setPolicies([]admissionObjects{{policy: p, binding: b}}); err != nil {
		t.Fatal(err)
	}
	serve := func(method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		if body == nil {
			body = &bytes.Buffer{}
		}
		s.ServeHTTP(w, httptest.NewRequest(method, path, body))
		return w
	}
	if w := serve(http.MethodPost, "/healthz", nil); w.Code != http.StatusNotFound {
		t.Errorf("another path: %d", w.Code)
	}
	if w := serve(http.MethodGet, spiffeWebhookPath+p.Name, nil); w.Code != http.StatusNotFound {
		t.Errorf("GET: %d", w.Code)
	}
	if w := serve(http.MethodPost, spiffeWebhookPath+p.Name, bytes.NewBufferString("{}")); w.Code != http.StatusBadRequest {
		t.Errorf("a review without a request: %d", w.Code)
	}
	decode := func(w *httptest.ResponseRecorder) *admissionv1.AdmissionResponse {
		t.Helper()
		out := &admissionv1.AdmissionReview{}
		if err := json.NewDecoder(w.Body).Decode(out); err != nil || out.Response == nil {
			t.Fatalf("response %q: %v", w.Body.String(), err)
		}
		return out.Response
	}
	allowed := decode(serve(http.MethodPost, spiffeWebhookPath+p.Name,
		review(t, podRequest(t, replicaSetController, admissionv1.Create, map[string]string{SPIFFERoleLabel: "work"}, "klusterlet-work-sa", "ReplicaSet"))))
	if !allowed.Allowed || allowed.UID != "uid-1" {
		t.Errorf("an agent pod: %+v", allowed)
	}
	denied := decode(serve(http.MethodPost, spiffeWebhookPath+p.Name,
		review(t, podRequest(t, "alice", admissionv1.Create, nil, "x", ""))))
	if denied.Allowed || denied.Result == nil || denied.Result.Code != http.StatusForbidden {
		t.Errorf("a pod by alice: %+v", denied)
	}
	unknown := decode(serve(http.MethodPost, spiffeWebhookPath+"other", review(t, podRequest(t, "alice", admissionv1.Create, nil, "x", ""))))
	if unknown.Allowed || !strings.Contains(unknown.Result.Message, "no SPIFFE admission policy other") {
		t.Errorf("an unknown policy: %+v", unknown)
	}
}

// A policy whose spec is unchanged isn't compiled again; one that can't be compiled is reported.
func TestWebhookSetPolicies(t *testing.T) {
	s := newSPIFFEWebhookServer("ns")
	p, b := agentAdmissionPolicy(testInput())
	objects := []admissionObjects{{policy: p, binding: b}}
	if err := s.setPolicies(objects); err != nil {
		t.Fatal(err)
	}
	first := s.policy(p.Name)
	if err := s.setPolicies(objects); err != nil || s.policy(p.Name) != first {
		t.Errorf("recompiled an unchanged policy: %v", err)
	}
	bad := p.DeepCopy()
	bad.Spec.Validations = append(bad.Spec.Validations, admissionregistrationv1.Validation{Expression: "("})
	if err := s.setPolicies([]admissionObjects{{policy: bad, binding: b}}); err == nil {
		t.Error("an invalid policy was accepted")
	}
}

func TestWebhookConfiguration(t *testing.T) {
	s := newSPIFFEWebhookServer("open-cluster-management")
	agent, agentBinding := agentAdmissionPolicy(testInput())
	label, labelBinding := namespaceLabelPolicy(testInput())

	c := s.webhookConfiguration(admissionObjects{policy: agent, binding: agentBinding}, []byte("ca"))
	w := c.Webhooks[0]
	if c.Name != agent.Name || w.Name != agent.Name+".spiffe.open-cluster-management.io" ||
		*w.FailurePolicy != admissionregistrationv1.Fail || *w.Rules[0].Scope != admissionregistrationv1.AllScopes {
		t.Errorf("configuration %+v", c)
	}
	svc := w.ClientConfig.Service
	if svc == nil || svc.Namespace != "open-cluster-management" || svc.Name != SPIFFEWebhookServiceName ||
		*svc.Path != spiffeWebhookPath+agent.Name || *svc.Port != 443 || string(w.ClientConfig.CABundle) != "ca" {
		t.Errorf("client config %+v", w.ClientConfig)
	}
	if w.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "open-cluster-management-agent" ||
		len(w.ObjectSelector.MatchExpressions) != 0 {
		t.Errorf("selectors %+v %+v", w.NamespaceSelector, w.ObjectSelector)
	}

	l := s.webhookConfiguration(admissionObjects{policy: label, binding: labelBinding}, nil).Webhooks[0]
	if len(l.NamespaceSelector.MatchLabels) != 0 || len(l.ObjectSelector.MatchExpressions) != 1 ||
		l.ObjectSelector.MatchExpressions[0].Key != SPIFFEAddOnNamespaceLabel {
		t.Errorf("the namespace label webhook should see only namespaces with the label: %+v %+v",
			l.NamespaceSelector, l.ObjectSelector)
	}
	if s.dnsName() != testWebhookDNS {
		t.Errorf("DNS name %s", s.dnsName())
	}
}

// The webhook starts once, with the operator's SVID, and serves TLS with it.
func TestWebhookEnsure(t *testing.T) {
	src := newFakeSource(t, testWebhookID, testWebhookDNS)
	s := testWebhookServer(t, src)
	ctx, cancel := context.WithCancel(context.Background())
	id := spiffeid.RequireFromString(testWebhookID)
	got, caBundle, err := s.ensure(ctx, "unix:///unused", id)
	if err != nil || got != id || !strings.Contains(string(caBundle), "BEGIN CERTIFICATE") {
		t.Fatalf("ensure: %s, %q, %v", got, caBundle, err)
	}
	// A second Klusterlet's ID doesn't start another server: it keeps the first one's.
	other := spiffeid.RequireFromString("spiffe://ocm-hub.example.org/ocm/cluster/cluster2/agent/operator")
	if got, _, err := s.ensure(ctx, "unix:///unused", other); err != nil || got != id {
		t.Errorf("second ensure: %s, %v", got, err)
	}

	// The server answers over TLS with the SVID, verified against the CA bundle and DNS name.
	cc := s.clientConfig("/healthz", nil)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caBundle)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig(pool, testWebhookDNS)}}
	resp, err := client.Get(strings.Replace(*cc.URL, "/healthz", "/other", 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d", resp.StatusCode)
	}

	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for !src.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("the source wasn't closed when the controller stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebhookEnsureErrors(t *testing.T) {
	id := spiffeid.RequireFromString(testWebhookID)
	ctx := context.Background()

	s := testWebhookServer(t, nil)
	s.newSource = func(context.Context, string, spiffeid.ID) (x509Source, error) { return nil, errors.New("no SPIRE") }
	if _, _, err := s.ensure(ctx, "unix:///spire.sock", id); err == nil || !strings.Contains(err.Error(), "no SVID with ID") {
		t.Errorf("no source: %v", err)
	}

	src := newFakeSource(t, testWebhookID, testWebhookDNS)
	s = testWebhookServer(t, src)
	s.listen = func(string, string) (net.Listener, error) { return nil, errors.New("port taken") }
	if _, _, err := s.ensure(ctx, "", id); err == nil || !src.isClosed() {
		t.Errorf("listen failed: %v, source closed %v", err, src.isClosed())
	}

	// The listener breaks: the server stops and logs it.
	src = newFakeSource(t, testWebhookID, testWebhookDNS)
	s = testWebhookServer(t, src)
	s.listen = func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err == nil {
			_ = l.Close()
		}
		return l, err
	}
	if _, _, err := s.ensure(ctx, "", id); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		mutate func(*fakeSource)
		want   string
	}{
		"no SVID":          {func(f *fakeSource) { f.svidErr = errors.New("rotating") }, "rotating"},
		"no trust bundle":  {func(f *fakeSource) { f.bundleErr = errors.New("no bundle") }, "no bundle"},
		"no DNS name":      {func(f *fakeSource) { *f = *newFakeSource(t, testWebhookID) }, "doesn't carry the DNS name"},
		"another DNS name": {func(f *fakeSource) { *f = *newFakeSource(t, testWebhookID, "other.svc") }, "doesn't carry"},
	} {
		src := newFakeSource(t, testWebhookID, testWebhookDNS)
		c.mutate(src)
		s := testWebhookServer(t, src)
		if _, _, err := s.ensure(ctx, "", id); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func webhookReconcile(kube *fakekube.Clientset, src x509Source, t *testing.T) *spiffeAdmissionReconcile {
	r := newAdmissionReconcile(kube)
	r.webhook = testWebhookServer(t, src)
	return r
}

func testWebhookConfig(k *operatorapiv1.Klusterlet) klusterletConfig {
	c := spiffeConfig(k)
	c.SPIFFEWebhookID = testWebhookID
	c.SPIFFEWorkloadAPISocket = "unix:///spiffe-workload-api/spire-agent.sock"
	return c
}

func webhookConfigNames(t *testing.T, kube *fakekube.Clientset) []string {
	t.Helper()
	list, err := kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range list.Items {
		out = append(out, c.Name)
	}
	return out
}

func policyNames(t *testing.T, kube *fakekube.Clientset) []string {
	t.Helper()
	list, err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range list.Items {
		out = append(out, c.Name)
	}
	return out
}

// On a cluster without the policy API, the webhook; once the cluster serves the API and the user
// asks for the policies, the operator moves to them without recovery, the webhook staying in place
// until the policies are on record; and back.
func TestSPIFFEWebhookThenPolicies(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
	kube.Resources = nil // a cluster older than 1.30

	k = activate(t, r, k, testWebhookConfig(k))
	if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingWebhook ||
		len(k.Status.SPIFFE.AdmissionWebhooks) != 3 || len(k.Status.SPIFFE.AdmissionPolicies) != 0 {
		t.Fatalf("status %+v", k.Status.SPIFFE)
	}
	if k.Status.SPIFFE.WebhookDNSName != testWebhookDNS || k.Status.SPIFFE.WebhookSPIFFEID != testWebhookID {
		t.Errorf("webhook identity in status: %+v", k.Status.SPIFFE)
	}
	if got := webhookConfigNames(t, kube); len(got) != 3 {
		t.Errorf("webhook configurations %v", got)
	}
	k, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("second pass: %v, %v", state, err)
	}

	// The cluster is upgraded to 1.30, and the user moves to the policies.
	servePolicyAPI(kube, envOperator)
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy
	cleaned := len(deleteCollections(kube))
	k, state, err = r.reconcile(context.TODO(), k, testWebhookConfig(k))
	var rqe commonhelpers.RequeueError
	if state != reconcileStop || !errors.As(err, &rqe) || !strings.Contains(err.Error(), "switched") {
		t.Fatalf("switch: %v, %v", state, err)
	}
	if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy ||
		len(k.Status.SPIFFE.AdmissionPolicies) != 3 || readyCondition(k).Reason != "Switched" {
		t.Fatalf("after the switch: %+v %+v", k.Status.SPIFFE, readyCondition(k))
	}
	if got := webhookConfigNames(t, kube); len(got) != 3 {
		t.Errorf("the webhook went before the policies were on record: %v", got)
	}
	k, state, err = r.reconcile(context.TODO(), k, testWebhookConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("after the switch: %v, %v", state, err)
	}
	if got := webhookConfigNames(t, kube); len(got) != 0 {
		t.Errorf("webhook configurations left: %v", got)
	}
	if len(deleteCollections(kube)) != cleaned {
		t.Error("the switch ran recovery")
	}

	// Back to the webhook: the same switch the other way.
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	k, _, err = r.reconcile(context.TODO(), k, testWebhookConfig(k))
	if !errors.As(err, &rqe) || k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingWebhook {
		t.Fatalf("switch to the webhook: %v, %+v", err, k.Status.SPIFFE)
	}
	if _, state, err = r.reconcile(context.TODO(), k, testWebhookConfig(k)); err != nil || state != reconcileContinue {
		t.Fatalf("after the switch to the webhook: %v, %v", state, err)
	}
	if got := policyNames(t, kube); len(got) != 0 {
		t.Errorf("policies left: %v", got)
	}
}

// The webhook's CA bundle follows SPIRE: a new CA updates the configurations, without recovery.
func TestSPIFFEWebhookCARotation(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	src := newFakeSource(t, testWebhookID, testWebhookDNS)
	r := webhookReconcile(kube, src, t)
	k = activate(t, r, k, testWebhookConfig(k))
	cleaned := len(deleteCollections(kube))

	*src = *newFakeSource(t, testWebhookID, testWebhookDNS)
	k, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("after the rotation: %v, %v", state, err)
	}
	c, err := kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(), agentPolicyPrefix+k.Name, metav1.GetOptions{})
	if err != nil || !strings.Contains(string(c.Webhooks[0].ClientConfig.CABundle), "BEGIN CERTIFICATE") {
		t.Fatalf("configuration %v, %v", c, err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(c.Webhooks[0].ClientConfig.CABundle)
	if _, err := src.svid.Certificates[0].Verify(x509.VerifyOptions{Roots: pool}); err != nil {
		t.Errorf("the configuration doesn't trust the new CA: %v", err)
	}
	if len(deleteCollections(kube)) != cleaned {
		t.Error("the rotation ran recovery")
	}
}

// A switch from an enforcement that was tampered with runs recovery instead.
func TestSPIFFESwitchFromTamperedEnforcement(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
	k = activate(t, r, k, testWebhookConfig(k)) // the policies
	if err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Delete(context.TODO(),
		agentPolicyPrefix+k.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	k = activate(t, r, k, testWebhookConfig(k))
	if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingWebhook {
		t.Errorf("status %+v", k.Status.SPIFFE)
	}

	// A record of policies on a cluster that no longer serves them can't be checked either.
	setEnforcementRecord(k, operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy,
		admissionRecord{policies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "x"}}})
	kube.Resources = nil
	k = activate(t, r, k, testWebhookConfig(k))
	if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingWebhook {
		t.Errorf("status %+v", k.Status.SPIFFE)
	}
}

// Without an SVID, the operator waits and says which SPIRE entry it needs.
func TestSPIFFEWebhookNotReady(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := webhookReconcile(kube, nil, t)
	r.webhook.newSource = func(context.Context, string, spiffeid.ID) (x509Source, error) { return nil, errors.New("no entry") }
	k, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k))
	var rqe commonhelpers.RequeueError
	if state != reconcileStop || !errors.As(err, &rqe) {
		t.Fatalf("got %v, %v; want a requeue", state, err)
	}
	c := readyCondition(k)
	for _, want := range []string{ReasonSPIFFEWebhookNotReady, "ServiceAccount klusterlet", "namespace open-cluster-management",
		testWebhookID, testWebhookDNS} {
		if c == nil || !strings.Contains(c.Reason+" "+c.Message, want) {
			t.Errorf("SPIFFEAdmissionReady %+v doesn't mention %q", c, want)
		}
	}
	if got := webhookConfigNames(t, kube); len(got) != 0 {
		t.Errorf("webhook configurations without a webhook: %v", got)
	}

	// An invalid webhook ID is an error.
	config := testWebhookConfig(k)
	config.SPIFFEWebhookID = "not an ID"
	if _, _, err := r.reconcile(context.TODO(), k, config); err == nil {
		t.Error("no error for an invalid webhook ID")
	}

	// So is a policy the webhook can't compile.
	p, b := agentAdmissionPolicy(testInput())
	p.Spec.Validations = []admissionregistrationv1.Validation{{Expression: "("}}
	r = webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
	if ready, _, err := r.ensureWebhook(context.TODO(), k, testWebhookConfig(k), []admissionObjects{{client: kube, policy: p, binding: b}}); ready || err == nil {
		t.Errorf("ready %v, err %v for an invalid policy", ready, err)
	}
}

func TestWebhookEnforcementErrors(t *testing.T) {
	s := newSPIFFEWebhookServer("ns")
	p, b := agentAdmissionPolicy(testInput())
	config := s.webhookConfiguration(admissionObjects{policy: p, binding: b}, nil)
	for _, verb := range []string{"get", "create", "update"} {
		kube := fakekube.NewSimpleClientset()
		if verb == "update" {
			old := config.DeepCopy()
			old.Webhooks[0].TimeoutSeconds = ptr.To[int32](30)
			kube = fakekube.NewSimpleClientset(old)
		}
		failOn(kube, verb, "validatingwebhookconfigurations")
		e := webhookEnforcement{configs: []webhookConfig{{client: kube, config: config}}}
		if err := e.apply(context.TODO()); err == nil {
			t.Errorf("apply with a failing %s: no error", verb)
		}
		if verb == "get" {
			if _, _, _, err := e.observe(context.TODO()); err == nil {
				t.Error("observe with a failing get: no error")
			}
		}
	}
	// Up to date, and changed.
	kube := fakekube.NewSimpleClientset()
	e := webhookEnforcement{configs: []webhookConfig{{client: kube, config: config}}}
	if err := e.apply(context.TODO()); err != nil {
		t.Fatal(err)
	}
	if err := e.apply(context.TODO()); err != nil {
		t.Fatal(err)
	}
	if _, found, upToDate, err := e.observe(context.TODO()); !found || !upToDate || err != nil {
		t.Errorf("observe: %v %v %v", found, upToDate, err)
	}
	if e.mode() != operatorapiv1.SPIFFEAdmissionValidatingWebhook || (policyEnforcement{}).mode() != operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy {
		t.Error("modes")
	}
}

func failOn(kube *fakekube.Clientset, verb, resource string) {
	kube.PrependReactor(verb, resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected")
	})
}

// A switch fails safely: errors are returned, and the old enforcement stays on record.
func TestSPIFFESwitchErrors(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(kube *fakekube.Clientset)
	}{
		{"the old enforcement can't be read", func(kube *fakekube.Clientset) { failOn(kube, "get", "validatingadmissionpolicies") }},
		{"the new one can't be created", func(kube *fakekube.Clientset) { failOn(kube, "create", "validatingwebhookconfigurations") }},
		{"the new one can't be read back", func(kube *fakekube.Clientset) {
			created := 0
			kube.PrependReactor("create", "validatingwebhookconfigurations", func(clienttesting.Action) (bool, runtime.Object, error) {
				created++
				return false, nil, nil
			})
			kube.PrependReactor("get", "validatingwebhookconfigurations", func(clienttesting.Action) (bool, runtime.Object, error) {
				if created == 3 {
					return true, nil, errors.New("injected")
				}
				return false, nil, nil
			})
		}},
		{"the new one is gone at once", func(kube *fakekube.Clientset) {
			kube.PrependReactor("create", "validatingwebhookconfigurations", func(a clienttesting.Action) (bool, runtime.Object, error) {
				return true, a.(clienttesting.CreateAction).GetObject(), nil // not stored
			})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			k := spiffeKlusterlet()
			kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
			r := webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
			k = activate(t, r, k, testWebhookConfig(k))
			c.setup(kube)
			k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
			k, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k))
			var rqe commonhelpers.RequeueError
			if state != reconcileStop || err == nil || errors.As(err, &rqe) {
				t.Errorf("got %v, %v; want an error", state, err)
			}
			if k.Status.SPIFFE.AdmissionEnforcement != operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy {
				t.Errorf("the record moved: %+v", k.Status.SPIFFE)
			}
		})
	}
}

// Removing an enforcement keeps the shared namespace label object while another Klusterlet has
// that enforcement, and reports API errors.
func TestRemoveEnforcement(t *testing.T) {
	kube := fakekube.NewSimpleClientset(
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: namespaceLabelPolicyName}},
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: agentPolicyPrefix + "klusterlet"}})
	r := newAdmissionReconcile(kube)
	r.spiffeEnforcements = func() (map[string]operatorapiv1.SPIFFEAdmissionEnforcement, error) {
		return map[string]operatorapiv1.SPIFFEAdmissionEnforcement{
			"klusterlet": operatorapiv1.SPIFFEAdmissionValidatingWebhook,
			"other":      operatorapiv1.SPIFFEAdmissionValidatingWebhook}, nil
	}
	if err := r.removeEnforcement(context.TODO(), "klusterlet", operatorapiv1.SPIFFEAdmissionValidatingWebhook, true); err != nil {
		t.Fatal(err)
	}
	if got := webhookConfigNames(t, kube); len(got) != 1 || got[0] != namespaceLabelPolicyName {
		t.Errorf("left %v, want only the shared one", got)
	}

	r.spiffeEnforcements = func() (map[string]operatorapiv1.SPIFFEAdmissionEnforcement, error) {
		return nil, errors.New("injected")
	}
	if err := r.removeEnforcement(context.TODO(), "klusterlet", operatorapiv1.SPIFFEAdmissionValidatingWebhook, true); err == nil {
		t.Error("no error from the Klusterlet list")
	}
	r.spiffeEnforcements = nil
	for _, c := range []struct{ mode, resource string }{
		{string(operatorapiv1.SPIFFEAdmissionValidatingWebhook), "validatingwebhookconfigurations"},
		{string(operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy), "validatingadmissionpolicybindings"},
		{string(operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy), "validatingadmissionpolicies"},
	} {
		kube := fakekube.NewSimpleClientset()
		failOn(kube, "delete", c.resource)
		r := newAdmissionReconcile(kube)
		if err := r.removeEnforcement(context.TODO(), "klusterlet", operatorapiv1.SPIFFEAdmissionEnforcement(c.mode), true); err == nil {
			t.Errorf("no error deleting %s", c.resource)
		}
	}
	// Policies aren't touched where the API isn't served.
	kube = fakekube.NewSimpleClientset()
	failOn(kube, "delete", "validatingadmissionpolicies")
	if err := newAdmissionReconcile(kube).removeEnforcement(context.TODO(), "klusterlet",
		operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy, false); err != nil {
		t.Error(err)
	}
}

type errLister struct {
	operatorv1listers.KlusterletLister
}

func (errLister) List(labels.Selector) ([]*operatorapiv1.Klusterlet, error) {
	return nil, errors.New("injected")
}

func TestRecordedEnforcement(t *testing.T) {
	k := spiffeKlusterlet()
	if mode, _ := recordedEnforcement(k); mode != "" {
		t.Errorf("no status: %s", mode)
	}
	// A record from before admissionEnforcement existed.
	k.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{AdmissionPolicies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "p"}}}
	if mode, rec := recordedEnforcement(k); mode != operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy || len(rec.policies) != 1 {
		t.Errorf("legacy record: %s %+v", mode, rec)
	}

	webhookK := newKlusterlet("other", "open-cluster-management-agent", "cluster2")
	webhookK.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{AdmissionEnforcement: operatorapiv1.SPIFFEAdmissionValidatingWebhook}
	got, err := spiffeEnforcementsFrom(klusterletLister(t, k, webhookK, newKlusterlet("csr", "x", "c3")))()
	if err != nil || len(got) != 2 || got["other"] != operatorapiv1.SPIFFEAdmissionValidatingWebhook {
		t.Errorf("enforcements %v, %v", got, err)
	}
	if _, err := spiffeEnforcementsFrom(errLister{})(); err == nil {
		t.Error("no error from the lister")
	}
	if !sameWebhookRecords(nil, nil) || sameWebhookRecords(nil, []operatorapiv1.SPIFFEAdmissionWebhookStatus{{Name: "a"}}) ||
		sameWebhookRecords([]operatorapiv1.SPIFFEAdmissionWebhookStatus{{Name: "a"}}, []operatorapiv1.SPIFFEAdmissionWebhookStatus{{Name: "b"}}) {
		t.Error("sameWebhookRecords")
	}
}

// Leaving the spiffe driver removes the objects of both enforcements.
func TestRemoveSPIFFEBothEnforcements(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
	k = activate(t, r, k, testWebhookConfig(k))
	if err := r.removeSPIFFE(context.TODO(), k); err != nil {
		t.Fatal(err)
	}
	if got := webhookConfigNames(t, kube); len(got) != 0 {
		t.Errorf("left %v", got)
	}
	kube.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected")
	})
	if err := r.removeSPIFFE(context.TODO(), spiffeKlusterlet()); err == nil {
		t.Error("no error from discovery")
	}
	kube = fakekube.NewSimpleClientset()
	failOn(kube, "delete", "validatingwebhookconfigurations")
	if err := newAdmissionReconcile(kube).removeSPIFFE(context.TODO(), spiffeKlusterlet()); err == nil {
		t.Error("no error from the delete")
	}
}

func tlsConfig(roots *x509.CertPool, serverName string) *tls.Config {
	return &tls.Config{RootCAs: roots, ServerName: serverName, MinVersion: tls.VersionTLS12}
}

// The old enforcement can't be removed after a switch: the error is returned, and the next pass
// tries again.
func TestSPIFFERemoveOldEnforcementError(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := webhookReconcile(kube, newFakeSource(t, testWebhookID, testWebhookDNS), t)
	k = activate(t, r, k, testWebhookConfig(k))
	failOn(kube, "delete", "validatingwebhookconfigurations")
	if _, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k)); err == nil || state != reconcileStop {
		t.Errorf("got %v, %v; want the delete error", state, err)
	}
}

// The real source dials the Workload API; an address that isn't one fails at once.
func TestWebhookDefaultSource(t *testing.T) {
	s := newSPIFFEWebhookServer("ns")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.newSource(ctx, "not a socket address", spiffeid.RequireFromString(testWebhookID)); err == nil {
		t.Error("no error for an invalid Workload API address")
	}
}
