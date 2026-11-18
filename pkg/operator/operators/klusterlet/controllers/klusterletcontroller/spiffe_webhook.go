package klusterletcontroller

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"
)

const (
	// SPIFFEWebhookServiceName is the Service in the operator namespace that fronts the
	// operator's webhook, with ValidatingWebhook enforcement.
	SPIFFEWebhookServiceName = "klusterlet-spiffe-webhook"
	// SPIFFEWebhookPort is the port the operator serves the webhook on; the Service maps 443 to it.
	SPIFFEWebhookPort = 9443
	spiffeWebhookPath = "/validate/"
	// spiffeWebhookRole is the role in the operator's SPIFFE ID; it is no agent role.
	spiffeWebhookRole = "operator"
	// svidWait is how long a reconcile waits for the operator's SVID.
	svidWait = 10 * time.Second
	// webhookRequeue is how soon the operator tries again when its SVID isn't there yet.
	webhookRequeue = 15 * time.Second
)

// x509Source is the operator's Workload API source: its SVID and the trust bundles.
type x509Source interface {
	x509svid.Source
	x509bundle.Source
	Close() error
}

// spiffeWebhookServer serves the ValidatingWebhook enforcement: it evaluates the same policies
// the ValidatingAdmissionPolicy enforcement installs. It starts with the operator's SVID the first
// time a Klusterlet uses that enforcement, and serves every such Klusterlet's policies.
type spiffeWebhookServer struct {
	namespace string
	addr      string
	// newSource and listen are replaceable in tests.
	newSource func(ctx context.Context, socket string, id spiffeid.ID) (x509Source, error)
	listen    func(network, address string) (net.Listener, error)
	// clientConfig is how the API server reaches the webhook; replaceable in tests.
	clientConfig func(path string, caBundle []byte) admissionregistrationv1.WebhookClientConfig

	mu       sync.Mutex
	source   x509Source
	id       spiffeid.ID
	specs    map[string]admissionregistrationv1.ValidatingAdmissionPolicySpec
	policies map[string]*celPolicy
}

func newSPIFFEWebhookServer(namespace string) *spiffeWebhookServer {
	s := &spiffeWebhookServer{
		namespace: namespace,
		addr:      fmt.Sprintf(":%d", SPIFFEWebhookPort),
		newSource: func(ctx context.Context, socket string, id spiffeid.ID) (x509Source, error) {
			return ocmspiffe.NewX509Source(ctx, socket, id)
		},
		listen:   net.Listen,
		specs:    map[string]admissionregistrationv1.ValidatingAdmissionPolicySpec{},
		policies: map[string]*celPolicy{},
	}
	s.clientConfig = func(path string, caBundle []byte) admissionregistrationv1.WebhookClientConfig {
		return admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{Namespace: s.namespace, Name: SPIFFEWebhookServiceName,
				Path: ptr.To(path), Port: ptr.To[int32](443)},
			CABundle: caBundle,
		}
	}
	return s
}

// dnsName is the name the API server calls the webhook by; the operator's SVID must carry it.
func (s *spiffeWebhookServer) dnsName() string {
	return SPIFFEWebhookServiceName + "." + s.namespace + ".svc"
}

// ensure starts the webhook with the operator's SVID if it doesn't run yet, and returns the
// SPIFFE ID it serves with and the CA bundle the API server must trust: the trust bundle of the
// SVID's trust domain. The SVID and its key stay in memory, and rotate.
func (s *spiffeWebhookServer) ensure(ctx context.Context, socket string, id spiffeid.ID) (spiffeid.ID, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source == nil {
		waitCtx, cancel := context.WithTimeout(ctx, svidWait)
		defer cancel()
		source, err := s.newSource(waitCtx, socket, id)
		if err != nil {
			return id, nil, fmt.Errorf("no SVID with ID %s from the Workload API at %s: %w", id, socket, err)
		}
		listener, err := s.listen("tcp", s.addr)
		if err != nil {
			_ = source.Close()
			return id, nil, fmt.Errorf("the webhook can't listen on %s: %w", s.addr, err)
		}
		server := &http.Server{Handler: s, TLSConfig: tlsconfig.TLSServerConfig(source), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := server.ServeTLS(listener, "", ""); err != http.ErrServerClosed {
				klog.FromContext(ctx).Error(err, "the SPIFFE admission webhook stopped")
			}
		}()
		go func() {
			<-ctx.Done()
			_ = server.Close()
			_ = source.Close()
		}()
		s.source, s.id = source, id
	}
	svid, err := s.source.GetX509SVID()
	if err != nil {
		return s.id, nil, err
	}
	if !slices.Contains(svid.Certificates[0].DNSNames, s.dnsName()) {
		return s.id, nil, fmt.Errorf("the SVID %s doesn't carry the DNS name %s", svid.ID, s.dnsName())
	}
	bundle, err := s.source.GetX509BundleForTrustDomain(svid.ID.TrustDomain())
	if err != nil {
		return s.id, nil, err
	}
	var caBundle []byte
	for _, ca := range bundle.X509Authorities() {
		caBundle = append(caBundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})...)
	}
	return s.id, caBundle, nil
}

// setPolicies makes the webhook serve these policies, compiling the ones that changed.
func (s *spiffeWebhookServer) setPolicies(objects []admissionObjects) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range objects {
		if p, ok := s.specs[o.policy.Name]; ok && equality.Semantic.DeepEqual(p, o.policy.Spec) {
			continue
		}
		compiled, err := compilePolicy(o.policy.Spec)
		if err != nil {
			return fmt.Errorf("policy %s: %w", o.policy.Name, err)
		}
		s.specs[o.policy.Name], s.policies[o.policy.Name] = o.policy.Spec, compiled
	}
	return nil
}

func (s *spiffeWebhookServer) policy(name string) *celPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policies[name]
}

// ServeHTTP answers an AdmissionReview for the policy named in the path. Anything it can't
// evaluate is refused.
func (s *spiffeWebhookServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutPrefix(r.URL.Path, spiffeWebhookPath)
	if !ok || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	review := &admissionv1.AdmissionReview{}
	if err := json.NewDecoder(r.Body).Decode(review); err != nil || review.Request == nil {
		http.Error(w, "not an AdmissionReview", http.StatusBadRequest)
		return
	}
	message := "no SPIFFE admission policy " + name
	if p := s.policy(name); p != nil {
		message = p.admit(review.Request)
	}
	response := &admissionv1.AdmissionResponse{UID: review.Request.UID, Allowed: message == ""}
	if !response.Allowed {
		response.Result = &metav1.Status{Status: metav1.StatusFailure, Code: http.StatusForbidden,
			Reason: metav1.StatusReasonForbidden, Message: message}
	}
	review.Request, review.Response = nil, response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(review)
}

// webhookConfiguration renders the ValidatingWebhookConfiguration that enforces a policy and its
// binding through the operator's webhook. Every field the API server would default is set, so the
// live object compares equal to the rendered one.
func (s *spiffeWebhookServer) webhookConfiguration(o admissionObjects, caBundle []byte) *admissionregistrationv1.ValidatingWebhookConfiguration {
	var rules []admissionregistrationv1.RuleWithOperations
	for _, r := range o.policy.Spec.MatchConstraints.ResourceRules {
		rule := r.RuleWithOperations
		rule.Scope = ptr.To(admissionregistrationv1.AllScopes)
		rules = append(rules, rule)
	}
	namespaceSelector := &metav1.LabelSelector{}
	if o.binding.Spec.MatchResources != nil && o.binding.Spec.MatchResources.NamespaceSelector != nil {
		namespaceSelector = o.binding.Spec.MatchResources.NamespaceSelector
	}
	objectSelector := &metav1.LabelSelector{}
	if o.policy.Name == namespaceLabelPolicyName {
		// Only namespaces that have or had the add-on namespace label reach the webhook, so a
		// webhook that is down can't block every namespace on the cluster.
		objectSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: SPIFFEAddOnNamespaceLabel, Operator: metav1.LabelSelectorOpExists}}}
	}
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: o.policy.Name, Labels: o.policy.Labels},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name:                    o.policy.Name + ".spiffe.open-cluster-management.io",
			ClientConfig:            s.clientConfig(spiffeWebhookPath+o.policy.Name, caBundle),
			Rules:                   rules,
			FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
			MatchPolicy:             ptr.To(admissionregistrationv1.Equivalent),
			NamespaceSelector:       namespaceSelector,
			ObjectSelector:          objectSelector,
			SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
			TimeoutSeconds:          ptr.To[int32](10),
			AdmissionReviewVersions: []string{"v1"},
		}},
	}
}

// admissionRecord is what the operator records of an enforcement: its live objects' UIDs and
// generations.
type admissionRecord struct {
	policies []operatorapiv1.SPIFFEAdmissionPolicyStatus
	webhooks []operatorapiv1.SPIFFEAdmissionWebhookStatus
}

func (a admissionRecord) same(b admissionRecord) bool {
	return sameRecords(a.policies, b.policies) && sameWebhookRecords(a.webhooks, b.webhooks)
}

func sameWebhookRecords(a, b []operatorapiv1.SPIFFEAdmissionWebhookStatus) bool {
	if len(a) != len(b) {
		return false
	}
	byName := map[string]operatorapiv1.SPIFFEAdmissionWebhookStatus{}
	for _, r := range a {
		byName[r.Name] = r
	}
	for _, r := range b {
		if got, ok := byName[r.Name]; !ok || got != r {
			return false
		}
	}
	return true
}

// recordedEnforcement returns the enforcement on record and its record. A record from before
// admissionEnforcement existed has only policies.
func recordedEnforcement(k *operatorapiv1.Klusterlet) (operatorapiv1.SPIFFEAdmissionEnforcement, admissionRecord) {
	s := k.Status.SPIFFE
	if s == nil {
		return "", admissionRecord{}
	}
	mode := s.AdmissionEnforcement
	if mode == "" && len(s.AdmissionPolicies) > 0 {
		mode = operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy
	}
	return mode, admissionRecord{policies: s.AdmissionPolicies, webhooks: s.AdmissionWebhooks}
}

func setEnforcementRecord(k *operatorapiv1.Klusterlet, mode operatorapiv1.SPIFFEAdmissionEnforcement, rec admissionRecord) {
	if k.Status.SPIFFE == nil {
		k.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{}
	}
	k.Status.SPIFFE.AdmissionEnforcement = mode
	k.Status.SPIFFE.AdmissionPolicies = rec.policies
	k.Status.SPIFFE.AdmissionWebhooks = rec.webhooks
}

// enforcement puts one kind of admission enforcement in place.
type enforcement interface {
	mode() operatorapiv1.SPIFFEAdmissionEnforcement
	apply(ctx context.Context) error
	// observe returns the live objects' record, whether they all exist, and whether they are all as
	// rendered.
	observe(ctx context.Context) (rec admissionRecord, found, upToDate bool, err error)
}

// policyEnforcement is the ValidatingAdmissionPolicy enforcement.
type policyEnforcement struct{ objects []admissionObjects }

func (e policyEnforcement) mode() operatorapiv1.SPIFFEAdmissionEnforcement {
	return operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy
}

func (e policyEnforcement) apply(ctx context.Context) error {
	for _, o := range e.objects {
		if err := apply(ctx, o); err != nil {
			return err
		}
	}
	return nil
}

func (e policyEnforcement) observe(ctx context.Context) (admissionRecord, bool, bool, error) {
	live, upToDate, err := observeAll(ctx, e.objects)
	return admissionRecord{policies: live}, live != nil, upToDate, err
}

// webhookConfig is one ValidatingWebhookConfiguration and the cluster it belongs on.
type webhookConfig struct {
	client kubernetes.Interface
	config *admissionregistrationv1.ValidatingWebhookConfiguration
}

// webhookEnforcement is the ValidatingWebhook enforcement.
type webhookEnforcement struct{ configs []webhookConfig }

func (e webhookEnforcement) mode() operatorapiv1.SPIFFEAdmissionEnforcement {
	return operatorapiv1.SPIFFEAdmissionValidatingWebhook
}

func (e webhookEnforcement) apply(ctx context.Context) error {
	for _, c := range e.configs {
		configs := c.client.AdmissionregistrationV1().ValidatingWebhookConfigurations()
		live, err := configs.Get(ctx, c.config.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			if _, err := configs.Create(ctx, c.config, metav1.CreateOptions{}); err != nil {
				return err
			}
		case err != nil:
			return err
		case !equality.Semantic.DeepEqual(live.Webhooks, c.config.Webhooks):
			live.Webhooks = c.config.Webhooks
			if _, err := configs.Update(ctx, live, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e webhookEnforcement) observe(ctx context.Context) (admissionRecord, bool, bool, error) {
	var rec []operatorapiv1.SPIFFEAdmissionWebhookStatus
	upToDate := true
	for _, c := range e.configs {
		live, err := c.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, c.config.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return admissionRecord{}, false, false, nil
		}
		if err != nil {
			return admissionRecord{}, false, false, err
		}
		rec = append(rec, operatorapiv1.SPIFFEAdmissionWebhookStatus{Name: live.Name, UID: live.UID, Generation: live.Generation})
		upToDate = upToDate && equality.Semantic.DeepEqual(live.Webhooks, c.config.Webhooks)
	}
	return admissionRecord{webhooks: rec}, true, upToDate, nil
}
