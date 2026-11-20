package klusterletcontroller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
)

// spiffeAccess is one permission the spiffe driver needs, at cluster scope.
type spiffeAccess struct {
	group, resource string
	verbs           []string
}

// spiffeRequiredAccess is what the klusterlet-spiffe ClusterRole grants: the admission policies,
// bindings and webhook configurations, and the cleanup of the agent and add-on namespaces during recovery. The operator
// checks it before it uses the spiffe driver, so that a missing ClusterRole is reported instead
// of failing halfway through recovery or leaving a watch that never syncs.
var spiffeRequiredAccess = []spiffeAccess{
	{"admissionregistration.k8s.io", "validatingadmissionpolicies",
		[]string{"create", "get", "list", "update", "watch", "patch", "delete"}},
	{"admissionregistration.k8s.io", "validatingadmissionpolicybindings",
		[]string{"create", "get", "list", "update", "watch", "patch", "delete"}},
	{"admissionregistration.k8s.io", "validatingwebhookconfigurations",
		[]string{"create", "get", "list", "update", "watch", "patch", "delete"}},
	{"authentication.k8s.io", "selfsubjectreviews", []string{"create"}},
	{"apps", "deployments", []string{"list", "delete", "deletecollection"}},
	{"apps", "replicasets", []string{"list", "delete", "deletecollection"}},
	{"apps", "daemonsets", []string{"list", "delete", "deletecollection"}},
	{"apps", "statefulsets", []string{"list", "delete", "deletecollection"}},
	// create: the dry-run probe that checks a new enforcement is in effect before a switch.
	{"batch", "jobs", []string{"create", "list", "delete", "deletecollection"}},
	{"batch", "cronjobs", []string{"list", "delete", "deletecollection"}},
	{"", "pods", []string{"list", "delete", "deletecollection"}},
}

// SPIFFEClusterRoleName is the ClusterRole that grants the operator spiffeRequiredAccess. It is
// installed only where the spiffe driver is used.
const SPIFFEClusterRoleName = "klusterlet-spiffe"

// missingSPIFFEAccess returns the permissions in spiffeRequiredAccess the client's user lacks, as
// "verb group/resource".
func missingSPIFFEAccess(ctx context.Context, c kubernetes.Interface) ([]string, error) {
	var missing []string
	for _, a := range spiffeRequiredAccess {
		for _, verb := range a.verbs {
			review, err := c.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group: a.group, Resource: a.resource, Verb: verb}},
			}, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to check the operator's permissions: %w", err)
			}
			if !review.Status.Allowed {
				missing = append(missing, verb+" "+strings.TrimPrefix(a.group+"/"+a.resource, "/"))
			}
		}
	}
	return missing, nil
}

// spiffeAdmissionWatch watches the spiffe driver's admission objects, so that a change to one, for
// example its deletion, runs recovery at once rather than at the next resync. The watch starts the
// first time a Klusterlet that uses the spiffe driver is reconciled: an operator without such a
// Klusterlet never discovers, lists or watches these APIs, and needs no permission on them.
// ValidatingWebhookConfigurations are watched on every cluster, ValidatingAdmissionPolicies and
// their bindings only where the API is served.
type spiffeAdmissionWatch struct {
	kubeClient kubernetes.Interface
	lister     operatorlister.KlusterletLister

	mu       sync.Mutex
	webhooks bool
	policies bool
}

func newSPIFFEAdmissionWatch(kubeClient kubernetes.Interface, lister operatorlister.KlusterletLister) *spiffeAdmissionWatch {
	return &spiffeAdmissionWatch{kubeClient: kubeClient, lister: lister}
}

// isStarted reports whether everything there is to watch is watched; policiesServed says whether
// the cluster serves ValidatingAdmissionPolicies.
func (w *spiffeAdmissionWatch) isStarted(policiesServed bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.webhooks && (w.policies || !policiesServed)
}

// spiffeWatchResync is the resync period of the admission watch.
const spiffeWatchResync = 5 * time.Minute

// ensure starts what isn't watched yet. The informers live as long as ctx, the controller's.
// The caller has checked that the operator may list and watch these APIs.
func (w *spiffeAdmissionWatch) ensure(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string],
	policiesServed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	factory := informers.NewSharedInformerFactory(w.kubeClient, spiffeWatchResync)
	enqueue := func(obj interface{}) { w.enqueue(queue, obj) }
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    enqueue,
		UpdateFunc: func(_, obj interface{}) { enqueue(obj) },
		DeleteFunc: enqueue,
	}
	var watched []cache.SharedIndexInformer
	if !w.webhooks {
		watched = append(watched, factory.Admissionregistration().V1().ValidatingWebhookConfigurations().Informer())
		w.webhooks = true
	}
	if policiesServed && !w.policies {
		watched = append(watched,
			factory.Admissionregistration().V1().ValidatingAdmissionPolicies().Informer(),
			factory.Admissionregistration().V1().ValidatingAdmissionPolicyBindings().Informer())
		w.policies = true
	}
	for _, i := range watched {
		// AddEventHandler fails only on a stopped informer; these are new.
		_, err := i.AddEventHandler(handler)
		utilruntime.Must(err)
	}
	factory.Start(ctx.Done())
}

// enqueue queues the Klusterlets an admission policy or binding belongs to.
func (w *spiffeAdmissionWatch) enqueue(queue workqueue.TypedRateLimitingInterface[string], obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	o, ok := obj.(runtime.Object)
	if !ok {
		return
	}
	for _, k := range spiffeAdmissionQueueKeys(w.lister)(o) {
		queue.Add(k)
	}
}
