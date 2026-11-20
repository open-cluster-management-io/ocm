package klusterletcontroller

import (
	"context"
	"errors"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

func TestOwnWrites(t *testing.T) {
	var none *ownWrites
	none.note("a", 1, 2)
	if none.reaches("a", 1, 2) || !none.reaches("a", 1, 1) {
		t.Error("a nil record explains only no change")
	}
	w := newOwnWrites()
	w.note("a", 1, 2)
	w.note("a", 2, 3)
	w.note("", 1, 5) // an object without a UID is never noted
	w.note("a", 7, 7)
	for _, c := range []struct {
		uid      types.UID
		from, to int64
		want     bool
	}{
		{"a", 1, 1, true}, {"a", 1, 2, true}, {"a", 1, 3, true}, {"a", 2, 3, true},
		{"a", 1, 4, false}, {"a", 3, 2, false}, {"b", 1, 2, false}, {"", 1, 5, false},
	} {
		if got := w.reaches(c.uid, c.from, c.to); got != c.want {
			t.Errorf("reaches(%s, %d, %d) = %v", c.uid, c.from, c.to, got)
		}
	}
	// A loop of noted changes ends.
	w.note("loop", 1, 2)
	w.note("loop", 2, 1)
	if w.reaches("loop", 1, 3) {
		t.Error("a loop reached a generation it doesn't contain")
	}

	policy := func(gen int64) operatorapiv1.SPIFFEAdmissionPolicyStatus {
		return operatorapiv1.SPIFFEAdmissionPolicyStatus{PolicyName: "p", PolicyUID: "a", PolicyGeneration: gen,
			BindingUID: "b", BindingGeneration: 1}
	}
	webhook := func(uid types.UID, gen int64) operatorapiv1.SPIFFEAdmissionWebhookStatus {
		return operatorapiv1.SPIFFEAdmissionWebhookStatus{Name: "c", UID: uid, Generation: gen}
	}
	w.note("c", 4, 5)
	rec := admissionRecord{policies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{policy(1)},
		webhooks: []operatorapiv1.SPIFFEAdmissionWebhookStatus{webhook("c", 4)}}
	for name, c := range map[string]struct {
		live admissionRecord
		want bool
	}{
		"unchanged": {rec, true},
		"changed by the operator": {admissionRecord{policies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{policy(3)},
			webhooks: []operatorapiv1.SPIFFEAdmissionWebhookStatus{webhook("c", 5)}}, true},
		"changed by someone else": {admissionRecord{policies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{policy(4)},
			webhooks: rec.webhooks}, false},
		"a webhook changed by someone else": {admissionRecord{policies: rec.policies,
			webhooks: []operatorapiv1.SPIFFEAdmissionWebhookStatus{webhook("c", 6)}}, false},
		"recreated": {admissionRecord{policies: rec.policies,
			webhooks: []operatorapiv1.SPIFFEAdmissionWebhookStatus{webhook("other", 4)}}, false},
		"another policy": {admissionRecord{policies: []operatorapiv1.SPIFFEAdmissionPolicyStatus{{PolicyName: "q"}},
			webhooks: rec.webhooks}, false},
		"one missing": {admissionRecord{policies: rec.policies}, false},
	} {
		if got := w.explains(rec, c.live); got != c.want {
			t.Errorf("%s: explains = %v", name, got)
		}
	}
}

// Generations and UIDs as a real API server sets them: a create gets a UID and generation 1, an
// update that changes anything raises the generation.
func countGenerations(kube *fakekube.Clientset) {
	kube.PrependReactor("create", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		o := a.(clienttesting.CreateAction).GetObject()
		if m, err := meta.Accessor(o); err == nil && m.GetUID() == "" {
			m.SetUID(types.UID(rand.String(8)))
			m.SetGeneration(1)
		}
		return false, nil, nil
	})
	kube.PrependReactor("update", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		o := a.(clienttesting.UpdateAction).GetObject()
		if m, err := meta.Accessor(o); err == nil {
			m.SetGeneration(m.GetGeneration() + 1)
		}
		return false, nil, nil
	})
}

// Klusterlet A has its enforcement on record. Then the operator, in another Klusterlet's pass,
// updates the shared namespace label policy. A's next pass sees the new generation, knows the
// operator made it, and records it without recovery. Without that knowledge (after a restart),
// the same difference runs recovery.
func TestSPIFFEAdmissionOwnSharedUpdate(t *testing.T) {
	for _, remembered := range []bool{true, false} {
		k := spiffeKlusterlet()
		kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
		countGenerations(kube)
		r := newAdmissionReconcile(kube)
		r.writes = newOwnWrites()
		k = activate(t, r, k, spiffeConfig(k))

		// The other Klusterlet's pass: its work agent joins the shared policy.
		r.spiffeWorkAgents = func() (map[string]string, error) {
			return map[string]string{"klusterlet": "system:serviceaccount:open-cluster-management-agent:klusterlet-work-sa",
				"other": "system:serviceaccount:other-agent:klusterlet-work-sa"}, nil
		}
		objects, err := r.render(context.TODO(), k, spiffeConfig(k))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range objects {
			if o.policy.Name == namespaceLabelPolicyName {
				if err := apply(context.TODO(), o, r.writes); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !remembered {
			r.writes = newOwnWrites()
		}
		kube.ClearActions()
		k, _, _ = r.reconcile(context.TODO(), k, spiffeConfig(k))
		recovered := deleteCollections(kube)["open-cluster-management-agent/deployments"]
		if remembered && recovered {
			t.Error("recovery ran for the operator's own update of the shared policy")
		}
		if !remembered && !recovered {
			t.Error("an unexplained change didn't run recovery")
		}
		if remembered {
			if c := readyCondition(k); c == nil || c.Reason != "Recorded" {
				t.Errorf("SPIFFEAdmissionReady %+v", c)
			}
			for _, p := range k.Status.SPIFFE.AdmissionPolicies {
				if p.PolicyName == namespaceLabelPolicyName && p.PolicyGeneration != 2 {
					t.Errorf("the shared policy's new generation isn't on record: %+v", p)
				}
			}
		}
	}
}

// SPIRE's CA rotates and the operator's CA bundle update reaches only some of the webhook
// configurations before it fails. The next pass finishes the update without recovery: the
// configurations it already changed differ from the record only by its own updates.
func TestSPIFFEWebhookCAUpdateInPart(t *testing.T) {
	k := spiffeKlusterlet()
	k.Spec.RegistrationConfiguration.RegistrationDriver.SPIFFE.AdmissionEnforcement = operatorapiv1.SPIFFEAdmissionValidatingWebhook
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	countGenerations(kube)
	src := newFakeSource(t, testWebhookID, testWebhookDNS)
	r := webhookReconcile(kube, src, t)
	r.writes = newOwnWrites()
	k = activate(t, r, k, testWebhookConfig(k))
	kube.ClearActions() // from here on, no workload may be deleted

	failed := false
	kube.PrependReactor("update", "validatingwebhookconfigurations", func(a clienttesting.Action) (bool, runtime.Object, error) {
		c := a.(clienttesting.UpdateAction).GetObject().(*admissionregistrationv1.ValidatingWebhookConfiguration)
		if c.Name == namespaceLabelPolicyName && !failed {
			failed = true
			return true, nil, errInjected
		}
		return false, nil, nil
	})
	*src = *newFakeSource(t, testWebhookID, testWebhookDNS)
	if _, _, err := r.reconcile(context.TODO(), k, testWebhookConfig(k)); !errors.Is(err, errInjected) {
		t.Fatalf("the first try: %v", err)
	}
	k, state, err := r.reconcile(context.TODO(), k, testWebhookConfig(k))
	if err != nil || state != reconcileContinue {
		t.Fatalf("the second try: %v, %v", state, err)
	}
	if len(deleteCollections(kube)) != 0 {
		t.Error("finishing the operator's own update ran recovery")
	}
	for _, w := range k.Status.SPIFFE.AdmissionWebhooks {
		if w.Generation != 2 {
			t.Errorf("not on record at its new generation: %+v", w)
		}
	}
}
