package klusterletcontroller

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

const (
	envOperator     = "system:serviceaccount:open-cluster-management:klusterlet"
	envWorkAgent    = "system:serviceaccount:open-cluster-management-agent:klusterlet-work-sa"
	envAgentNS      = "open-cluster-management-agent"
	envAddOnNS      = "addon-a"
	envAlice        = "alice"
	envClusterAdmin = "admin-user"
)

// TestSPIFFEAdmissionPoliciesInAPIServer applies the rendered policies to a real kube-apiserver
// and checks each rule by impersonating the operator, the built-in controllers, the work agent and
// a random user. Everyone is cluster-admin by RBAC, so only the policies decide.
func TestSPIFFEAdmissionPoliciesInAPIServer(t *testing.T) {
	checkAdmissionRules(t, operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy)
}

// TestSPIFFEAdmissionWebhookInAPIServer checks the same rules enforced by the operator's webhook,
// which the API server calls over TLS with the webhook's SVID. It runs on every version.
func TestSPIFFEAdmissionWebhookInAPIServer(t *testing.T) {
	checkAdmissionRules(t, operatorapiv1.SPIFFEAdmissionValidatingWebhook)
}

func checkAdmissionRules(t *testing.T, mode operatorapiv1.SPIFFEAdmissionEnforcement) {
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
	admin := kubernetes.NewForConfigOrDie(cfg)
	if served, err := policyAPIServed(admin); mode == operatorapiv1.SPIFFEAdmissionValidatingAdmissionPolicy && (err != nil || !served) {
		t.Skipf("the API server doesn't serve ValidatingAdmissionPolicy (Kubernetes older than 1.30): %v", err)
	}
	as := func(user string) kubernetes.Interface {
		c := rest.CopyConfig(cfg)
		c.Impersonate = rest.ImpersonationConfig{UserName: user}
		return kubernetes.NewForConfigOrDie(c)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = admin.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "everyone-admin"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "system:authenticated"}},
	}, metav1.CreateOptions{})
	must(err)
	for _, ns := range []string{envAgentNS, "default-ish"} {
		_, err = admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
		must(err)
	}
	_, err = admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: envAddOnNS, Labels: map[string]string{SPIFFEAddOnNamespaceLabel: "klusterlet"}}}, metav1.CreateOptions{})
	must(err)
	for _, sa := range []struct{ ns, name string }{{envAgentNS, "klusterlet-work-sa"}, {envAgentNS, "klusterlet-registration-sa"},
		{envAddOnNS, "my-addon-agent-sa"}, {envAddOnNS, "klusterlet-work-sa"}} {
		_, err = admin.CoreV1().ServiceAccounts(sa.ns).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: sa.name}}, metav1.CreateOptions{})
		must(err)
	}

	in := spiffeAdmissionInput{
		klusterletName: "klusterlet", agentNamespace: envAgentNS,
		operatorUser: envOperator, managedOperatorUser: envOperator,
		pairs:         map[string]string{"registration": "klusterlet-registration-sa", "work": "klusterlet-work-sa"},
		workAgentUser: envWorkAgent,
		workAgents:    map[string]string{"klusterlet": envWorkAgent},
	}
	var objects []admissionObjects
	for _, render := range []func(spiffeAdmissionInput) (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding){
		agentAdmissionPolicy, addOnAdmissionPolicy, namespaceLabelPolicy} {
		p, b := render(in)
		objects = append(objects, admissionObjects{client: admin, policy: p, binding: b})
	}
	var enf enforcement = policyEnforcement{objects}
	if mode == operatorapiv1.SPIFFEAdmissionValidatingWebhook {
		webhook := testWebhookServer(t, newFakeSource(t, testWebhookID, "klusterlet-spiffe-webhook.open-cluster-management.svc"))
		_, caBundle, err := webhook.ensure(ctx, "unix:///unused", spiffeid.RequireFromString(testWebhookID))
		must(err)
		must(webhook.setPolicies(objects))
		r := &spiffeAdmissionReconcile{webhook: webhook}
		enf = r.webhookEnforcement(objects, caBundle)
	}
	must(enf.apply(ctx))

	deployment := func(ns, name string) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: podTemplate(name, "busybox:1"),
			},
		}
	}

	// The policies take a moment to become active.
	waitDenied(t, func() error {
		_, err := as(envAlice).AppsV1().Deployments(envAgentNS).Create(ctx, deployment(envAgentNS, "probe"), metav1.CreateOptions{})
		if err == nil {
			_ = admin.AppsV1().Deployments(envAgentNS).Delete(ctx, "probe", metav1.DeleteOptions{})
		}
		return err
	})

	// Each policy becomes active on its own.
	waitDenied(t, func() error {
		_, err := as(envAlice).AppsV1().Deployments(envAddOnNS).Create(ctx, deployment(envAddOnNS, "probe"), metav1.CreateOptions{})
		if err == nil {
			_ = admin.AppsV1().Deployments(envAddOnNS).Delete(ctx, "probe", metav1.DeleteOptions{})
		}
		return err
	})
	waitDenied(t, func() error {
		_, err := as(envAlice).CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "probe-ns", Labels: map[string]string{SPIFFEAddOnNamespaceLabel: "klusterlet"}}}, metav1.CreateOptions{})
		if err == nil {
			_ = admin.CoreV1().Namespaces().Delete(ctx, "probe-ns", metav1.DeleteOptions{})
		}
		return err
	})

	// Agent namespace: Deployments, ReplicaSets, Pods.
	x, err := as(envOperator).AppsV1().Deployments(envAgentNS).Create(ctx, deployment(envAgentNS, "x"), metav1.CreateOptions{})
	must(err)
	expectDenied(t, "Deployment by alice", "only the Klusterlet operator", func() error {
		_, err := as(envAlice).AppsV1().Deployments(envAgentNS).Create(ctx, deployment(envAgentNS, "y"), metav1.CreateOptions{})
		return err
	})
	x.Annotations = map[string]string{"deployment.kubernetes.io/revision": "1"}
	x, err = as(deploymentController).AppsV1().Deployments(envAgentNS).Update(ctx, x, metav1.UpdateOptions{})
	must(err) // a rollout's metadata update by the Deployment controller
	expectDenied(t, "Deployment spec change by the Deployment controller", "only the Klusterlet operator", func() error {
		y := x.DeepCopy()
		y.Spec.Template.Spec.Containers[0].Image = "evil:1"
		_, err := as(deploymentController).AppsV1().Deployments(envAgentNS).Update(ctx, y, metav1.UpdateOptions{})
		return err
	})

	owner := func(kind, name string, uid types.UID) []metav1.OwnerReference {
		return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid, Controller: ptr.To(true)}}
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "x-1", Namespace: envAgentNS, OwnerReferences: owner("Deployment", "x", x.UID)},
		Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}, Template: podTemplate("x", "busybox:1")},
	}
	rs, err = as(deploymentController).AppsV1().ReplicaSets(envAgentNS).Create(ctx, rs, metav1.CreateOptions{})
	must(err)
	expectDenied(t, "ReplicaSet without owner", "ReplicaSets must be created by the Deployment controller", func() error {
		orphan := rs.DeepCopy()
		orphan.ResourceVersion, orphan.UID, orphan.Name, orphan.OwnerReferences = "", "", "orphan", nil
		_, err := as(deploymentController).AppsV1().ReplicaSets(envAgentNS).Create(ctx, orphan, metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "ReplicaSet image change", "Pod template can't change", func() error {
		y := rs.DeepCopy()
		y.Spec.Template.Spec.Containers[0].Image = "evil:1"
		_, err := as(deploymentController).AppsV1().ReplicaSets(envAgentNS).Update(ctx, y, metav1.UpdateOptions{})
		return err
	})
	expectDenied(t, "ReplicaSet scale by alice", "only the Deployment controller", func() error {
		y := rs.DeepCopy()
		y.Spec.Replicas = ptr.To(int32(3))
		_, err := as(envAlice).AppsV1().ReplicaSets(envAgentNS).Update(ctx, y, metav1.UpdateOptions{})
		return err
	})
	rs.Spec.Replicas = ptr.To(int32(2))
	rs, err = as(deploymentController).AppsV1().ReplicaSets(envAgentNS).Update(ctx, rs, metav1.UpdateOptions{})
	must(err) // scaling by the Deployment controller

	pod := func(ns, name, role, sa string, owners []metav1.OwnerReference) *corev1.Pod {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, OwnerReferences: owners,
			Labels: map[string]string{SPIFFERoleLabel: role}},
			Spec: corev1.PodSpec{ServiceAccountName: sa, Containers: []corev1.Container{{Name: "c", Image: "busybox:1"}}}}
		return p
	}
	agentPod, err := as(replicaSetController).CoreV1().Pods(envAgentNS).Create(ctx,
		pod(envAgentNS, "agent", "work", "klusterlet-work-sa", owner("ReplicaSet", rs.Name, rs.UID)), metav1.CreateOptions{})
	must(err)
	expectDenied(t, "Pod with a role/ServiceAccount pair of another mode", "role klusterlet is not allowed with ServiceAccount klusterlet-work-sa", func() error {
		_, err := as(replicaSetController).CoreV1().Pods(envAgentNS).Create(ctx,
			pod(envAgentNS, "p2", "klusterlet", "klusterlet-work-sa", owner("ReplicaSet", rs.Name, rs.UID)), metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "Pod by alice", "Pods must be created by the ReplicaSet controller", func() error {
		_, err := as(envAlice).CoreV1().Pods(envAgentNS).Create(ctx,
			pod(envAgentNS, "p3", "work", "klusterlet-work-sa", owner("ReplicaSet", rs.Name, rs.UID)), metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "role label change", "spiffe-role label can't change", func() error {
		y := agentPod.DeepCopy()
		y.Labels[SPIFFERoleLabel] = "registration"
		_, err := as(envOperator).CoreV1().Pods(envAgentNS).Update(ctx, y, metav1.UpdateOptions{})
		return err
	})
	expectDenied(t, "image change", "container images of an agent pod can't change", func() error {
		y := agentPod.DeepCopy()
		y.Spec.Containers[0].Image = "evil:1"
		_, err := as(envClusterAdmin).CoreV1().Pods(envAgentNS).Update(ctx, y, metav1.UpdateOptions{})
		return err
	})
	expectDenied(t, "ephemeral container", "ephemeral containers are not allowed", func() error {
		y := agentPod.DeepCopy()
		y.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name: "debug", Image: "busybox:1"}}}
		_, err := as(envClusterAdmin).CoreV1().Pods(envAgentNS).UpdateEphemeralContainers(ctx, agentPod.Name, y, metav1.UpdateOptions{})
		return err
	})
	expectDenied(t, "exec", "exec and attach are not allowed", func() error {
		return as(envClusterAdmin).CoreV1().RESTClient().Post().Namespace(envAgentNS).Resource("pods").Name(agentPod.Name).
			SubResource("exec").Param("command", "true").Param("stdout", "true").Do(ctx).Error()
	})
	expectDenied(t, "attach", "exec and attach are not allowed", func() error {
		return as(envAlice).CoreV1().RESTClient().Post().Namespace(envAgentNS).Resource("pods").Name(agentPod.Name).
			SubResource("attach").Param("stdout", "true").Do(ctx).Error()
	})
	expectDenied(t, "Job in the agent namespace", "not allowed in OCM agent namespaces", func() error {
		_, err := as(envOperator).BatchV1().Jobs(envAgentNS).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "j"},
			Spec: batchv1.JobSpec{Template: podTemplateNever("j")}}, metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "DaemonSet in the agent namespace", "not allowed in OCM agent namespaces", func() error {
		_, err := as(envOperator).AppsV1().DaemonSets(envAgentNS).Create(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "d"},
			Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "d"}},
				Template: podTemplate("d", "busybox:1")}}, metav1.CreateOptions{})
		return err
	})

	// Add-on namespace.
	_, err = as(envWorkAgent).AppsV1().Deployments(envAddOnNS).Create(ctx, deployment(envAddOnNS, "my-addon"), metav1.CreateOptions{})
	must(err)
	ds, err := as(envWorkAgent).AppsV1().DaemonSets(envAddOnNS).Create(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "my-addon-ds"},
		Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-addon-ds"}},
			Template: podTemplate("my-addon-ds", "busybox:1")}}, metav1.CreateOptions{})
	must(err)
	expectDenied(t, "add-on DaemonSet by alice", "only this Klusterlet's work agent", func() error {
		_, err := as(envAlice).AppsV1().DaemonSets(envAddOnNS).Create(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "evil"},
			Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "evil"}},
				Template: podTemplate("evil", "busybox:1")}}, metav1.CreateOptions{})
		return err
	})
	dsOwner := []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: ptr.To(true)}}
	_, err = as(daemonSetController).CoreV1().Pods(envAddOnNS).Create(ctx,
		pod(envAddOnNS, "addon-pod", "addon", "my-addon-agent-sa", dsOwner), metav1.CreateOptions{})
	must(err)
	expectDenied(t, "add-on pod with a Klusterlet ServiceAccount", "add-on pods must have spiffe-role addon", func() error {
		_, err := as(daemonSetController).CoreV1().Pods(envAddOnNS).Create(ctx,
			pod(envAddOnNS, "evil-pod", "addon", "klusterlet-work-sa", dsOwner), metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "Job in the add-on namespace", "Jobs and CronJobs are not allowed", func() error {
		_, err := as(envWorkAgent).BatchV1().Jobs(envAddOnNS).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "j"},
			Spec: batchv1.JobSpec{Template: podTemplateNever("j")}}, metav1.CreateOptions{})
		return err
	})

	// Namespace labels.
	labeledNS := func(name, value string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{SPIFFEAddOnNamespaceLabel: value}}}
	}
	_, err = as(envWorkAgent).CoreV1().Namespaces().Create(ctx, labeledNS("addon-b", "klusterlet"), metav1.CreateOptions{})
	must(err)
	expectDenied(t, "add-on namespace by alice", "only the Klusterlet operator or that Klusterlet's work agent", func() error {
		_, err := as(envAlice).CoreV1().Namespaces().Create(ctx, labeledNS("addon-c", "klusterlet"), metav1.CreateOptions{})
		return err
	})
	expectDenied(t, "label on an existing namespace", "can't be added or changed", func() error {
		ns, err := admin.CoreV1().Namespaces().Get(ctx, "default-ish", metav1.GetOptions{})
		must(err)
		ns.Labels[SPIFFEAddOnNamespaceLabel] = "klusterlet"
		_, err = as(envClusterAdmin).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
		return err
	})
	removeLabel := []byte(`{"metadata":{"labels":{"` + SPIFFEAddOnNamespaceLabel + `":null}}}`)
	expectDenied(t, "label removed by alice", "only the operator may remove it", func() error {
		_, err := as(envAlice).CoreV1().Namespaces().Patch(ctx, "addon-b", types.MergePatchType, removeLabel, metav1.PatchOptions{})
		return err
	})
	expectDenied(t, "label changed by the work agent", "can't be added or changed", func() error {
		_, err := as(envWorkAgent).CoreV1().Namespaces().Patch(ctx, "addon-b", types.MergePatchType,
			[]byte(`{"metadata":{"labels":{"`+SPIFFEAddOnNamespaceLabel+`":"cluster4"}}}`), metav1.PatchOptions{})
		return err
	})
	_, err = as(envOperator).CoreV1().Namespaces().Patch(ctx, "addon-b", types.MergePatchType, removeLabel, metav1.PatchOptions{})
	must(err) // the operator may remove it

	// Not covered: an unlabeled namespace.
	_, err = as(envAlice).AppsV1().Deployments("default-ish").Create(ctx, deployment("default-ish", "free"), metav1.CreateOptions{})
	must(err)
}

func waitDenied(t *testing.T, f func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if f() != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the admission policies never became active")
}

func expectDenied(t *testing.T, name, message string, f func() error) {
	t.Helper()
	err := f()
	if err == nil {
		t.Errorf("%s: allowed, want denied with %q", name, message)
		return
	}
	if !strings.Contains(err.Error(), message) {
		t.Errorf("%s: %v, want an error containing %q", name, err, message)
	}
}

func podTemplate(app, image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": app}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: image}}},
	}
}

func podTemplateNever(app string) corev1.PodTemplateSpec {
	t := podTemplate(app, "busybox:1")
	t.Spec.RestartPolicy = corev1.RestartPolicyNever
	return t
}
