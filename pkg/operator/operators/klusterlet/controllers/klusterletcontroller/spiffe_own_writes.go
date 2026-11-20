package klusterletcontroller

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

// ownWrites remembers the generation changes the operator itself made to its admission objects:
// each update it sends is based on the object it just read, so it moves the object from one known
// generation to the next. A record that differs from the live objects only by such changes still
// describes objects nobody else touched, so it is updated without recovery. Examples: another
// Klusterlet's pass updating the shared namespace label objects, or a CA bundle update that
// reached only some of the webhook configurations before it failed.
//
// It is kept in memory, shared by all the Klusterlets of the operator. After a restart it is
// empty, and a difference runs recovery, as it would without it.
type ownWrites struct {
	mu sync.Mutex
	// next maps an object's UID and a generation to the generation the operator's update made.
	next map[types.UID]map[int64]int64
}

func newOwnWrites() *ownWrites {
	return &ownWrites{next: map[types.UID]map[int64]int64{}}
}

// note records that the operator's update moved the object uid from generation from to to.
func (w *ownWrites) note(uid types.UID, from, to int64) {
	if w == nil || uid == "" || from == to {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.next[uid] == nil {
		w.next[uid] = map[int64]int64{}
	}
	w.next[uid][from] = to
}

// reaches reports whether the operator's own updates lead object uid from generation from to to.
func (w *ownWrites) reaches(uid types.UID, from, to int64) bool {
	if from == to {
		return true
	}
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for steps := 0; steps < 1000 && from != to; steps++ {
		n, ok := w.next[uid][from]
		if !ok {
			return false
		}
		from = n
	}
	return from == to
}

// explains reports whether the live objects are the recorded ones, changed only by the
// operator's own updates: the same objects (by name and UID), each at a generation the operator's
// updates lead to from the recorded one.
func (w *ownWrites) explains(recorded, live admissionRecord) bool {
	if len(recorded.policies) != len(live.policies) || len(recorded.webhooks) != len(live.webhooks) {
		return false
	}
	policies := map[string]operatorapiv1.SPIFFEAdmissionPolicyStatus{}
	for _, p := range recorded.policies {
		policies[p.PolicyName] = p
	}
	for _, l := range live.policies {
		r, ok := policies[l.PolicyName]
		if !ok || r.PolicyUID != l.PolicyUID || r.BindingUID != l.BindingUID ||
			!w.reaches(types.UID(l.PolicyUID), r.PolicyGeneration, l.PolicyGeneration) ||
			!w.reaches(types.UID(l.BindingUID), r.BindingGeneration, l.BindingGeneration) {
			return false
		}
	}
	webhooks := map[string]operatorapiv1.SPIFFEAdmissionWebhookStatus{}
	for _, c := range recorded.webhooks {
		webhooks[c.Name] = c
	}
	for _, l := range live.webhooks {
		r, ok := webhooks[l.Name]
		if !ok || r.UID != l.UID || !w.reaches(types.UID(l.UID), r.Generation, l.Generation) {
			return false
		}
	}
	return true
}
