package klusterletcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"

	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const operatorUser = "system:serviceaccount:open-cluster-management:klusterlet"

func klusterletLister(t *testing.T, klusterlets ...*operatorapiv1.Klusterlet) operatorlister.KlusterletLister {
	t.Helper()
	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, k := range klusterlets {
		if err := store.Add(k); err != nil {
			t.Fatal(err)
		}
	}
	return operatorlister.NewKlusterletLister(store)
}

func newQueue() workqueue.TypedRateLimitingInterface[string] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
}

// denyAccess makes the operator lack the given "verb resource" permissions.
func denyAccess(kube *fakekube.Clientset, denied ...string) {
	kube.PrependReactor("create", "selfsubjectaccessreviews", func(a clienttesting.Action) (bool, runtime.Object, error) {
		attrs := a.(clienttesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview).Spec.ResourceAttributes
		allowed := true
		for _, d := range denied {
			if d == attrs.Verb+" "+attrs.Resource {
				allowed = false
			}
		}
		return true, &authorizationv1.SelfSubjectAccessReview{Status: authorizationv1.SubjectAccessReviewStatus{Allowed: allowed}}, nil
	})
}

func admissionActions(kube *fakekube.Clientset) []string {
	var out []string
	for _, a := range kube.Actions() {
		switch a.GetResource().Resource {
		case "validatingadmissionpolicies", "validatingadmissionpolicybindings", "validatingwebhookconfigurations",
			"selfsubjectaccessreviews", "resource":
			out = append(out, a.GetVerb()+" "+a.GetResource().Resource)
		}
	}
	return out
}

func TestMissingSPIFFEAccess(t *testing.T) {
	kube := fakekube.NewSimpleClientset()
	denyAccess(kube)
	if missing, err := missingSPIFFEAccess(context.TODO(), kube); err != nil || len(missing) != 0 {
		t.Errorf("all granted: missing %v, err %v", missing, err)
	}

	kube = fakekube.NewSimpleClientset()
	denyAccess(kube, "deletecollection pods", "watch validatingadmissionpolicies")
	missing, err := missingSPIFFEAccess(context.TODO(), kube)
	want := []string{"watch admissionregistration.k8s.io/validatingadmissionpolicies", "deletecollection pods"}
	if err != nil || !reflect.DeepEqual(missing, want) {
		t.Errorf("missing %v, err %v; want %v", missing, err, want)
	}

	kube = fakekube.NewSimpleClientset()
	kube.PrependReactor("create", "selfsubjectaccessreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected")
	})
	if _, err := missingSPIFFEAccess(context.TODO(), kube); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Errorf("err %v, want a permission check error", err)
	}
}

// The operator checks exactly what the klusterlet-spiffe ClusterRole grants, in both its
// kustomize and its Helm form.
func TestSPIFFERequiredAccessMatchesClusterRole(t *testing.T) {
	want := map[string]bool{}
	for _, a := range spiffeRequiredAccess {
		for _, v := range a.verbs {
			want[a.group+"/"+a.resource+" "+v] = true
		}
	}
	root := filepath.Join("..", "..", "..", "..", "..", "..", "deploy", "klusterlet")
	for _, f := range []string{
		filepath.Join(root, "spiffe", "cluster_role.yaml"),
		filepath.Join(root, "chart", "klusterlet", "templates", "cluster_role_spiffe.yaml"),
	} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(l, "{{") {
				lines = append(lines, l)
			}
		}
		var role rbacv1.ClusterRole
		if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &role); err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, r := range role.Rules {
			for _, g := range r.APIGroups {
				for _, res := range r.Resources {
					for _, v := range r.Verbs {
						got[g+"/"+res+" "+v] = true
					}
				}
			}
		}
		if role.Name != SPIFFEClusterRoleName || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: ClusterRole %s grants %v, the operator checks %v", f, role.Name, sortedKeys(got), sortedKeys(want))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A change to a Klusterlet's webhook configuration or policy, seen by the watch, queues that
// Klusterlet. Policies are watched only once the cluster serves them.
func TestSPIFFEAdmissionWatch(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(
		&admissionv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: agentPolicyPrefix + k.Name}},
		&admissionv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: addOnPolicyPrefix + k.Name}})
	w := newSPIFFEAdmissionWatch(kube, klusterletLister(t, k))
	queue := newQueue()
	defer queue.ShutDown()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline := time.Now().Add(10 * time.Second)
	next := func(what string) {
		t.Helper()
		for queue.Len() == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("%s didn't queue the Klusterlet", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
		key, _ := queue.Get()
		if key != k.Name {
			t.Errorf("%s queued %q, want %q", what, key, k.Name)
		}
		queue.Done(key)
	}

	if w.isStarted(false) {
		t.Fatal("started before ensure")
	}
	// A cluster without the policy API: only webhook configurations.
	w.ensure(ctx, queue, false)
	w.ensure(ctx, queue, false) // no second watch
	if !w.isStarted(false) || w.isStarted(true) {
		t.Fatalf("started without policies %v, with %v; want true, false", w.isStarted(false), w.isStarted(true))
	}
	next("the existing webhook configuration")

	// The cluster serves the policy API now: the policies are watched too.
	w.ensure(ctx, queue, true)
	if !w.isStarted(true) {
		t.Fatal("policies not watched")
	}
	next("the existing policy")

	// Someone weakens the policy.
	if _, err := kube.AdmissionregistrationV1().ValidatingAdmissionPolicies().Update(ctx, &admissionv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: addOnPolicyPrefix + k.Name, Labels: map[string]string{"changed": "true"}}},
		metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	next("the changed policy")
}

func TestSPIFFEAdmissionWatchEnqueue(t *testing.T) {
	k := spiffeKlusterlet()
	w := newSPIFFEAdmissionWatch(nil, klusterletLister(t, k))
	queue := newQueue()
	defer queue.ShutDown()
	policy := &admissionv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: addOnPolicyPrefix + k.Name}}

	w.enqueue(queue, "not an object")
	w.enqueue(queue, &admissionv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: "someone-elses"}})
	if queue.Len() != 0 {
		t.Fatalf("queued %d keys for objects that aren't the Klusterlet's", queue.Len())
	}
	w.enqueue(queue, cache.DeletedFinalStateUnknown{Key: policy.Name, Obj: policy})
	if queue.Len() != 1 {
		t.Fatalf("queued %d keys for a deleted policy, want 1", queue.Len())
	}
	key, _ := queue.Get()
	queue.Done(key)
	if key != k.Name {
		t.Errorf("a deleted policy queued %q, want %q", key, k.Name)
	}
}

// Without the klusterlet-spiffe ClusterRole, the Klusterlet is refused before the operator
// installs, watches or cleans anything.
func TestSPIFFEPermissionsMissing(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := newAdmissionReconcile(kube)
	denyAccess(kube, "list validatingadmissionpolicies")
	r.watch = newSPIFFEAdmissionWatch(kube, klusterletLister(t, k))
	r.queue = newQueue()
	defer r.queue.ShutDown()

	k, state, err := r.reconcile(context.TODO(), k, spiffeConfig(k))
	if err == nil || state != reconcileStop {
		t.Fatalf("got state %v, err %v; want stop with an error", state, err)
	}
	cond := meta.FindStatusCondition(k.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
	if cond == nil || cond.Reason != ReasonSPIFFEPermissionsMissing || !strings.Contains(cond.Message, SPIFFEClusterRoleName) ||
		!strings.Contains(cond.Message, "list admissionregistration.k8s.io/validatingadmissionpolicies") {
		t.Errorf("Applied %+v, want reason %s naming the ClusterRole and the permission", cond, ReasonSPIFFEPermissionsMissing)
	}
	if r.watch.isStarted(true) {
		t.Error("the watch started without the permissions")
	}
	for _, a := range kube.Actions() {
		if a.GetResource().Resource == "validatingadmissionpolicies" || a.GetVerb() == "delete-collection" {
			t.Errorf("unexpected request %s %s", a.GetVerb(), a.GetResource().Resource)
		}
	}

	kube.PrependReactor("create", "selfsubjectaccessreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected")
	})
	if _, _, err := r.reconcile(context.TODO(), spiffeKlusterlet(), spiffeConfig(k)); err == nil ||
		!strings.Contains(err.Error(), "permissions") {
		t.Errorf("err %v, want the permission check error", err)
	}
}

// The first reconcile of a spiffe Klusterlet starts the watch; later reconciles don't check the
// permissions again.
func TestSPIFFEAdmissionStartsWatch(t *testing.T) {
	k := spiffeKlusterlet()
	kube := fakekube.NewSimpleClientset(newNamespace("open-cluster-management-agent"))
	r := newAdmissionReconcile(kube)
	r.watch = newSPIFFEAdmissionWatch(kube, klusterletLister(t, k))
	r.queue = newQueue()
	defer r.queue.ShutDown()
	// The informers the watch starts live as long as this context; stop them before the queue.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k = activateWith(ctx, t, r, k, spiffeConfig(k))
	if !r.watch.isStarted(true) {
		t.Fatal("the watch didn't start")
	}
	checks := func() int {
		n := 0
		for _, a := range kube.Actions() {
			if a.GetResource().Resource == "selfsubjectaccessreviews" {
				n++
			}
		}
		return n
	}
	before := checks()
	if _, _, err := r.reconcile(ctx, k, spiffeConfig(k)); err != nil {
		t.Fatal(err)
	}
	if after := checks(); after != before {
		t.Errorf("%d more permission checks after the watch started", after-before)
	}
}

// An operator whose Klusterlet doesn't use spiffe never discovers, checks or watches the
// admission APIs, so it needs no permission on them.
func TestKlusterletWithoutSPIFFELeavesAdmissionAPIsAlone(t *testing.T) {
	k := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	syncContext := testingcommon.NewFakeSyncContext(t, k.Name)
	controller := newTestController(t, k, syncContext.Recorder(), nil, false,
		newNamespace("open-cluster-management-agent"), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: helpers.BootstrapHubKubeConfig, Namespace: "open-cluster-management-agent"}})
	if err := controller.controller.sync(context.TODO(), syncContext, k.Name); err != nil {
		t.Fatal(err)
	}
	if got := admissionActions(controller.kubeClient); len(got) != 0 {
		t.Errorf("requests to the admission APIs: %v", got)
	}
	if controller.controller.spiffeWatch.isStarted(false) {
		t.Error("the watch started")
	}
}
