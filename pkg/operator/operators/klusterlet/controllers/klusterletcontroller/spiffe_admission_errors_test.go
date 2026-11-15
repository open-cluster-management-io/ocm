package klusterletcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var errInjected = errors.New("injected API error")

// fault fails the n-th call (1-based) with verb on resource, and records whether it fired.
type fault struct {
	verb, resource string
	n              int
	fired          bool
}

func (f *fault) String() string { return fmt.Sprintf("%s %s #%d", f.verb, f.resource, f.n) }

func (f *fault) install(c *clienttesting.Fake) {
	calls := 0
	c.PrependReactor(f.verb, f.resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == f.n {
			f.fired = true
			return true, nil, errInjected
		}
		return false, nil, nil
	})
}

// faults lists every API call the admission reconcile makes, a few times over for calls it
// repeats (one per policy, namespace or workload kind).
func faults() []*fault {
	var out []*fault
	for _, vr := range [][2]string{
		{"get", "resource"},
		{"create", "selfsubjectreviews"},
		{"get", "namespaces"}, {"create", "namespaces"}, {"list", "namespaces"}, {"patch", "namespaces"},
		{"get", "validatingadmissionpolicies"}, {"create", "validatingadmissionpolicies"},
		{"update", "validatingadmissionpolicies"}, {"delete", "validatingadmissionpolicies"},
		{"get", "validatingadmissionpolicybindings"}, {"create", "validatingadmissionpolicybindings"},
		{"update", "validatingadmissionpolicybindings"}, {"delete", "validatingadmissionpolicybindings"},
		{"delete-collection", "deployments"}, {"list", "pods"},
	} {
		for n := 1; n <= 7; n++ {
			out = append(out, &fault{verb: vr[0], resource: vr[1], n: n})
		}
	}
	return out
}

// scenario runs one admission pass on fresh fakes and returns its error.
type scenario struct {
	name string
	// setup prepares the reconcile; the faults are installed after it.
	setup func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset,
		func() error)
}

func scenarios() []scenario {
	reconcile := func(r *spiffeAdmissionReconcile, k *operatorapiv1.Klusterlet, config klusterletConfig) func() error {
		return func() error {
			_, _, err := r.reconcile(context.TODO(), k, config)
			return err
		}
	}
	fresh := func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset) {
		kube := fakekube.NewSimpleClientset(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "stray",
				Labels: map[string]string{SPIFFEAddOnNamespaceLabel: "klusterlet"}}})
		work := fakeworkclient.NewSimpleClientset()
		r := newAdmissionReconcile(kube)
		r.managedClusterClients.appliedManifestWorkClient = work.WorkV1().AppliedManifestWorks()
		return r, kube, work
	}
	return []scenario{
		{"first activation", func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset, func() error) {
			r, kube, work := fresh(t)
			k := spiffeKlusterlet()
			return r, kube, work, reconcile(r, k, spiffeConfig(k))
		}},
		{"the operator changes its own policies", func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset, func() error) {
			r, kube, work := fresh(t)
			k := spiffeKlusterlet()
			k = activate(t, r, k, spiffeConfig(k))
			// Another spiffe Klusterlet appears: the shared namespace label policy changes.
			r.spiffeWorkAgents = func() (map[string]string, error) {
				return map[string]string{"klusterlet": "x", "other": "y"}, nil
			}
			return r, kube, work, reconcile(r, k, spiffeConfig(k))
		}},
		{"the Klusterlet leaves the spiffe driver", func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset, func() error) {
			r, kube, work := fresh(t)
			k := spiffeKlusterlet()
			k = activate(t, r, k, spiffeConfig(k))
			config := spiffeConfig(k)
			config.RegistrationDriver.AuthType = operatorapiv1.CSRAuthType
			return r, kube, work, reconcile(r, k, config)
		}},
		{"the Klusterlet is deleted", func(t *testing.T) (*spiffeAdmissionReconcile, *fakekube.Clientset, *fakeworkclient.Clientset, func() error) {
			r, kube, work := fresh(t)
			k := spiffeKlusterlet()
			k = activate(t, r, k, spiffeConfig(k))
			return r, kube, work, func() error {
				_, _, err := r.clean(context.TODO(), k, spiffeConfig(k))
				return err
			}
		}},
	}
}

// Every API error the admission reconcile meets is returned, so the Klusterlet is reconciled
// again, and no agent starts on a half-finished state.
func TestSPIFFEAdmissionReturnsAPIErrors(t *testing.T) {
	for _, s := range scenarios() {
		// Fresh faults per scenario: each records whether it fired.
		for _, f := range append(faults(), &fault{verb: "list", resource: "appliedmanifestworks", n: 1}) {
			t.Run(s.name+"/"+f.String(), func(t *testing.T) {
				_, kube, work, run := s.setup(t)
				if f.resource == "appliedmanifestworks" {
					f.install(&work.Fake)
				} else {
					f.install(&kube.Fake)
				}
				err := run()
				if !f.fired {
					return // this scenario makes no such call
				}
				if !errors.Is(err, errInjected) && (err == nil || !strings.Contains(err.Error(), errInjected.Error())) {
					t.Fatalf("the injected error was not returned: %v", err)
				}
			})
		}
	}
}

// The listers of the other spiffe Klusterlets fail.
func TestSPIFFEAdmissionWorkAgentsError(t *testing.T) {
	k := spiffeKlusterlet()
	r := newAdmissionReconcile(fakekube.NewSimpleClientset())
	r.spiffeWorkAgents = func() (map[string]string, error) { return nil, errInjected }
	if _, _, err := r.reconcile(context.TODO(), k, spiffeConfig(k)); !errors.Is(err, errInjected) {
		t.Errorf("reconcile: %v", err)
	}
	k.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{}
	if _, _, err := r.clean(context.TODO(), k, spiffeConfig(k)); !errors.Is(err, errInjected) {
		t.Errorf("clean: %v", err)
	}
}

// A Klusterlet deleted before it ever recorded admission state has nothing to clean.
func TestSPIFFEAdmissionCleanWithoutState(t *testing.T) {
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	k := spiffeKlusterlet()
	if _, state, err := r.clean(context.TODO(), k, spiffeConfig(k)); err != nil || state != reconcileContinue {
		t.Fatalf("state %v, err %v", state, err)
	}
	for _, a := range kube.Actions() {
		if a.GetVerb() == "delete" {
			t.Errorf("deleted %s without recorded state", a.GetResource().Resource)
		}
	}
}

func TestPolicyAPIServed(t *testing.T) {
	cases := []struct {
		name      string
		resources []metav1.APIResource
		listed    bool
		fail      bool
		want      bool
		wantErr   bool
	}{
		{name: "served", listed: true, resources: []metav1.APIResource{{Name: "validatingadmissionpolicies"}}, want: true},
		{name: "group without the policy resource", listed: true, resources: []metav1.APIResource{{Name: "validatingwebhookconfigurations"}}},
		{name: "group not served"},
		{name: "discovery fails", fail: true, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kube := fakekube.NewSimpleClientset()
			if c.listed {
				kube.Resources = append(kube.Resources, &metav1.APIResourceList{
					GroupVersion: admissionv1.SchemeGroupVersion.String(), APIResources: c.resources})
			}
			if c.fail {
				(&fault{verb: "get", resource: "resource", n: 1}).install(&kube.Fake)
			}
			got, err := PolicyAPIServed(kube)
			if got != c.want || (err != nil) != c.wantErr {
				t.Errorf("got %v, %v; want %v, error %v", got, err, c.want, c.wantErr)
			}
		})
	}
}

// A policy whose binding was deleted is not trusted: recovery runs again.
func TestSPIFFEAdmissionBindingMissing(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	k = activate(t, r, k, spiffeConfig(k))
	if err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Delete(context.TODO(),
		agentPolicyPrefix+k.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	k, _, _ = r.reconcile(context.TODO(), k, spiffeConfig(k))
	if c := readyCondition(k); c == nil || c.Reason != "Recorded" {
		t.Errorf("SPIFFEAdmissionReady %+v; want recovery to run and record again", c)
	}
	if _, err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(context.TODO(),
		agentPolicyPrefix+k.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("the binding wasn't recreated: %v", err)
	}
}

// Someone changes a policy while recovery deletes the workloads: recovery starts over instead of
// recording the changed policy.
func TestSPIFFEAdmissionPolicyChangedDuringCleanup(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	changed := false
	kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		if !changed {
			changed = true
			// The fake client holds its lock while reactors run: change the object in the tracker.
			gvr := admissionv1.SchemeGroupVersion.WithResource("validatingadmissionpolicies")
			obj, err := kube.Tracker().Get(gvr, "", agentPolicyPrefix+k.Name)
			if err != nil {
				return true, nil, err
			}
			p := obj.(*admissionv1.ValidatingAdmissionPolicy).DeepCopy()
			p.Generation++
			if err := kube.Tracker().Update(gvr, p, ""); err != nil {
				return true, nil, err
			}
		}
		return false, nil, nil
	})
	k, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	var rqe commonhelpers.RequeueError
	if state != reconcileStop || !errors.As(err, &rqe) || !strings.Contains(err.Error(), "starting over") {
		t.Fatalf("state %v, err %v; want recovery to start over", state, err)
	}
	if c := readyCondition(k); c == nil || c.Status != metav1.ConditionFalse {
		t.Errorf("SPIFFEAdmissionReady %+v, want False", c)
	}
	if k.Status.SPIFFE != nil && len(k.Status.SPIFFE.AdmissionPolicies) != 0 {
		t.Errorf("recorded %v although the policy changed during cleanup", k.Status.SPIFFE.AdmissionPolicies)
	}
}

// With add-on namespaces turned off, the operator creates none.
func TestSPIFFEAdmissionAddOnNamespaceDisabled(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	config := spiffeConfig(k)
	config.DisableAddonNamespace = true
	activate(t, r, k, config)
	if _, err := kube.CoreV1().Namespaces().Get(context.TODO(), helpers.DefaultAddonNamespace, metav1.GetOptions{}); err == nil {
		t.Error("created the add-on namespace although it is disabled")
	}
}

func TestSameRecords(t *testing.T) {
	a := []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "p", PolicyGeneration: 1}}
	b := []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "p", PolicyGeneration: 2}}
	for _, c := range []struct {
		x, y []operatorapiv1.SPIFFEAdmissionPolicyStatus
		want bool
	}{
		{a, a, true}, {a, b, false}, {a, nil, false}, {a, append(a, b...), false},
		{a, []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "q", PolicyGeneration: 1}}, false},
	} {
		if got := sameRecords(c.x, c.y); got != c.want {
			t.Errorf("sameRecords(%v, %v) = %v", c.x, c.y, got)
		}
	}
}

// A binding whose spec no longer matches the rendered one is put back. (The fake client keeps the
// generation, so the record still matches and the operator applies its binding without recovery.)
func TestSPIFFEAdmissionRestoresBindingSpec(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset()
	r := newAdmissionReconcile(kube)
	k = activate(t, r, k, spiffeConfig(k))
	bindings := kube.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings()
	b, err := bindings.Get(context.TODO(), agentPolicyPrefix+k.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b.Spec.ValidationActions = []admissionv1.ValidationAction{admissionv1.Warn}
	if _, err := bindings.Update(context.TODO(), b, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.reconcile(context.TODO(), k, spiffeConfig(k)); err != nil {
		t.Fatal(err)
	}
	b, err = bindings.Get(context.TODO(), agentPolicyPrefix+k.Name, metav1.GetOptions{})
	if err != nil || len(b.Spec.ValidationActions) != 1 || b.Spec.ValidationActions[0] != admissionv1.Deny {
		t.Errorf("binding %+v, %v; want Deny restored", b.Spec.ValidationActions, err)
	}
}
