package klusterletcontroller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	// SPIFFERoleLabel tells the platform's SPIRE entries which agent a pod is. The admission policy
	// pairs it with the pod's ServiceAccount.
	SPIFFERoleLabel = "open-cluster-management.io/spiffe-role"
	// SPIFFEAddOnNamespaceLabel marks a namespace as a dedicated add-on namespace of the
	// Klusterlet named in its value.
	SPIFFEAddOnNamespaceLabel = "open-cluster-management.io/spiffe-addon-namespace"

	agentPolicyPrefix           = "ocm-spiffe-agents-"
	addOnPolicyPrefix           = "ocm-spiffe-addons-"
	namespaceLabelPolicyName    = "ocm-spiffe-addon-namespace-label"
	deploymentController        = "system:serviceaccount:kube-system:deployment-controller"
	replicaSetController        = "system:serviceaccount:kube-system:replicaset-controller"
	daemonSetController         = "system:serviceaccount:kube-system:daemon-set-controller"
	statefulSetController       = "system:serviceaccount:kube-system:statefulset-controller"
	recoveryRequeue             = 5 * time.Second
	ReasonSPIFFEPolicyAPIAbsent = "SPIFFEAdmissionPolicyUnavailable"
	// ConditionAddOnNamespaceNotDedicated is True when the add-on namespace exists without the
	// add-on namespace label: the operator never labels an existing namespace.
	ConditionAddOnNamespaceNotDedicated = "AddOnNamespaceNotDedicated"
)

// celString quotes s for a CEL expression. Names here are DNS names and user names, which hold no
// quote or backslash, but quote properly anyway.
func celString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`) + "'"
}

func celMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, celString(k)+": "+celString(m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func celList(l []string) string {
	parts := make([]string, 0, len(l))
	for _, s := range l {
		parts = append(parts, celString(s))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

var (
	workloadRules = []admissionv1.NamedRuleWithOperations{
		rule("apps", []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, admissionv1.Create, admissionv1.Update),
		rule("batch", []string{"jobs", "cronjobs"}, admissionv1.Create, admissionv1.Update),
		rule("", []string{"pods"}, admissionv1.Create, admissionv1.Update),
		rule("", []string{"pods/ephemeralcontainers"}, admissionv1.Update),
		rule("", []string{"pods/exec", "pods/attach"}, admissionv1.Connect),
	}
	commonVariables = []admissionv1.Variable{
		// request.subResource is absent, not empty, on a request without a subresource.
		{Name: "sub", Expression: "has(request.subResource) ? request.subResource : ''"},
		{Name: "user", Expression: "request.userInfo.username"},
		{Name: "kind", Expression: "request.kind.kind"},
		{Name: "role", Expression: "has(object.metadata) && has(object.metadata.labels) && " +
			celString(SPIFFERoleLabel) + " in object.metadata.labels ? object.metadata.labels[" + celString(SPIFFERoleLabel) + "] : ''"},
		{Name: "ownedBy", Expression: "has(object.metadata) && has(object.metadata.ownerReferences) ? " +
			"object.metadata.ownerReferences.filter(o, has(o.controller) && o.controller).map(o, o.kind) : []"},
	}
	// podHardening refuses exec, attach, ephemeral containers and image changes on any pod the
	// policy covers. Logs stay readable: GET pods/log never reaches admission.
	podHardening = []admissionv1.Validation{
		{Expression: "!(variables.sub in ['exec', 'attach'])",
			Message: "exec and attach are not allowed on OCM agent pods; read the logs instead"},
		{Expression: "variables.sub != 'ephemeralcontainers'",
			Message: "ephemeral containers are not allowed on OCM agent pods"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'UPDATE' || variables.sub != '' || " +
			"(object.spec.containers.map(c, c.image) == oldObject.spec.containers.map(c, c.image) && " +
			"(has(object.spec.initContainers) ? object.spec.initContainers.map(c, c.image) : []) == " +
			"(has(oldObject.spec.initContainers) ? oldObject.spec.initContainers.map(c, c.image) : []))",
			Message: "container images of an agent pod can't change"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'UPDATE' || variables.sub != '' || " +
			"variables.role == (has(oldObject.metadata.labels) && " + celString(SPIFFERoleLabel) +
			" in oldObject.metadata.labels ? oldObject.metadata.labels[" + celString(SPIFFERoleLabel) + "] : '')",
			Message: "the spiffe-role label can't change after creation"},
		{Expression: "variables.kind != 'ReplicaSet' || request.operation != 'CREATE' || " +
			"(variables.user == " + celString(deploymentController) + " && variables.ownedBy == ['Deployment'])",
			Message: "ReplicaSets must be created by the Deployment controller for a Deployment"},
		{Expression: "variables.kind != 'ReplicaSet' || request.operation != 'UPDATE' || variables.sub != '' || " +
			"(object.spec.template == oldObject.spec.template && " +
			"(object.spec == oldObject.spec || variables.user == " + celString(deploymentController) + "))",
			Message: "a ReplicaSet's Pod template can't change, and only the Deployment controller may change its spec"},
	}
)

func rule(group string, resources []string, ops ...admissionv1.OperationType) admissionv1.NamedRuleWithOperations {
	return admissionv1.NamedRuleWithOperations{RuleWithOperations: admissionv1.RuleWithOperations{
		Operations: ops,
		Rule:       admissionv1.Rule{APIGroups: []string{group}, APIVersions: []string{"v1"}, Resources: resources},
	}}
}

func policyWithBinding(name string, rules []admissionv1.NamedRuleWithOperations, variables []admissionv1.Variable,
	validations []admissionv1.Validation, selector *metav1.LabelSelector, labels map[string]string) (
	*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {
	fail := admissionv1.Fail
	policy := &admissionv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: admissionv1.ValidatingAdmissionPolicySpec{
			FailurePolicy:    &fail,
			MatchConstraints: &admissionv1.MatchResources{ResourceRules: rules},
			Variables:        variables,
			Validations:      validations,
		},
	}
	binding := &admissionv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: admissionv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        name,
			ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny},
		},
	}
	if selector != nil {
		binding.Spec.MatchResources = &admissionv1.MatchResources{NamespaceSelector: selector}
	}
	return policy, binding
}

// spiffeAdmissionInput is what the policies are rendered from.
type spiffeAdmissionInput struct {
	klusterletName string
	agentNamespace string
	// operatorUser is the operator's user on the cluster where the agents run.
	operatorUser string
	// managedOperatorUser is the operator's user on the managed cluster (the same cluster and
	// user in Default and Singleton modes).
	managedOperatorUser string
	// pairs maps each spiffe-role of the install mode to its ServiceAccount.
	pairs map[string]string
	// workAgentUser is this Klusterlet's work agent on the managed cluster.
	workAgentUser string
	// workAgents maps every SPIFFE Klusterlet name to its work agent's user.
	workAgents map[string]string
	labels     map[string]string
}

// agentAdmissionPolicy covers the agent namespace: only operator-owned Deployments can start pods
// with an agent's ServiceAccount and spiffe-role, so nobody can borrow an agent's SVID.
func agentAdmissionPolicy(in spiffeAdmissionInput) (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {
	variables := append(append([]admissionv1.Variable{}, commonVariables...),
		admissionv1.Variable{Name: "operator", Expression: celString(in.operatorUser)},
		admissionv1.Variable{Name: "pairs", Expression: celMap(in.pairs)})
	validations := append([]admissionv1.Validation{
		{Expression: "!(variables.kind in ['StatefulSet', 'DaemonSet', 'Job', 'CronJob'])",
			Message: "StatefulSets, DaemonSets, Jobs and CronJobs are not allowed in OCM agent namespaces"},
		// The Deployment controller and the garbage collector update Deployments' metadata during
		// rollouts and deletion; only the spec is the operator's alone.
		{Expression: "variables.kind != 'Deployment' || variables.user == variables.operator || " +
			"(request.operation == 'UPDATE' && object.spec == oldObject.spec)",
			Message: "only the Klusterlet operator may create or change Deployments here"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'CREATE' || " +
			"(variables.user == " + celString(replicaSetController) + " && variables.ownedBy == ['ReplicaSet'])",
			Message: "Pods must be created by the ReplicaSet controller for a ReplicaSet"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'CREATE' || " +
			"(variables.role in variables.pairs && variables.pairs[variables.role] == object.spec.serviceAccountName)",
			MessageExpression: "'role ' + variables.role + ' is not allowed with ServiceAccount ' + object.spec.serviceAccountName"},
	}, podHardening...)
	return policyWithBinding(agentPolicyPrefix+in.klusterletName, workloadRules, variables, validations,
		&metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: in.agentNamespace}}, in.labels)
}

// addOnAdmissionPolicy covers this Klusterlet's add-on namespaces: only its work agent creates
// workloads there, add-on pods carry spiffe-role addon and their own ServiceAccount.
func addOnAdmissionPolicy(in spiffeAdmissionInput) (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {
	sas := make([]string, 0, len(in.pairs))
	for _, sa := range in.pairs {
		sas = append(sas, sa)
	}
	sort.Strings(sas)
	variables := append(append([]admissionv1.Variable{}, commonVariables...),
		admissionv1.Variable{Name: "workAgent", Expression: celString(in.workAgentUser)},
		admissionv1.Variable{Name: "klusterletSAs", Expression: celList(sas)},
		admissionv1.Variable{Name: "controllerFor", Expression: celMap(map[string]string{
			"ReplicaSet": replicaSetController, "DaemonSet": daemonSetController, "StatefulSet": statefulSetController})})
	validations := append([]admissionv1.Validation{
		{Expression: "!(variables.kind in ['Job', 'CronJob'])",
			Message: "Jobs and CronJobs are not allowed in add-on namespaces"},
		{Expression: "!(variables.kind in ['Deployment', 'DaemonSet', 'StatefulSet']) || variables.user == variables.workAgent || " +
			"(request.operation == 'UPDATE' && object.spec == oldObject.spec)",
			Message: "only this Klusterlet's work agent may create or change add-on workloads"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'CREATE' || " +
			"(size(variables.ownedBy) == 1 && variables.ownedBy[0] in variables.controllerFor && " +
			"variables.user == variables.controllerFor[variables.ownedBy[0]])",
			Message: "Pods must be created by the matching built-in controller"},
		{Expression: "variables.kind != 'Pod' || request.operation != 'CREATE' || " +
			"(variables.role == 'addon' && !(object.spec.serviceAccountName in variables.klusterletSAs))",
			Message: "add-on pods must have spiffe-role addon and their own ServiceAccount"},
	}, podHardening...)
	return policyWithBinding(addOnPolicyPrefix+in.klusterletName, workloadRules, variables, validations,
		&metav1.LabelSelector{MatchLabels: map[string]string{SPIFFEAddOnNamespaceLabel: in.klusterletName}}, in.labels)
}

// namespaceLabelPolicy covers every namespace on the managed cluster: the add-on namespace label
// can only be set when a namespace is created, by the operator or the named Klusterlet's work
// agent, can't be added or changed later, and only the operator may remove it.
func namespaceLabelPolicy(in spiffeAdmissionInput) (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {
	key := celString(SPIFFEAddOnNamespaceLabel)
	variables := []admissionv1.Variable{
		{Name: "newValue", Expression: "has(object.metadata.labels) && " + key + " in object.metadata.labels ? object.metadata.labels[" + key + "] : ''"},
		{Name: "oldValue", Expression: "request.operation == 'UPDATE' && has(oldObject.metadata.labels) && " + key +
			" in oldObject.metadata.labels ? oldObject.metadata.labels[" + key + "] : ''"},
		{Name: "operator", Expression: celString(in.managedOperatorUser)},
		{Name: "workAgents", Expression: celMap(in.workAgents)},
	}
	validations := []admissionv1.Validation{
		{Expression: "request.operation != 'CREATE' || variables.newValue == '' || request.userInfo.username == variables.operator || " +
			"(variables.newValue in variables.workAgents && request.userInfo.username == variables.workAgents[variables.newValue])",
			Message: "only the Klusterlet operator or that Klusterlet's work agent may create an add-on namespace"},
		{Expression: "request.operation != 'UPDATE' || variables.newValue == variables.oldValue || " +
			"(variables.newValue == '' && request.userInfo.username == variables.operator)",
			Message: "the spiffe-addon-namespace label can't be added or changed on an existing namespace; only the operator may remove it"},
	}
	return policyWithBinding(namespaceLabelPolicyName,
		[]admissionv1.NamedRuleWithOperations{rule("", []string{"namespaces"}, admissionv1.Create, admissionv1.Update)},
		variables, validations, nil, in.labels)
}

// admissionObjects is one policy and its binding, and the cluster they belong on.
type admissionObjects struct {
	client  kubernetes.Interface
	policy  *admissionv1.ValidatingAdmissionPolicy
	binding *admissionv1.ValidatingAdmissionPolicyBinding
}

// observe returns the live policy and binding's UIDs and generations, whether both exist, and
// whether their specs are the rendered ones.
func observe(ctx context.Context, o admissionObjects) (rec operatorapiv1.SPIFFEAdmissionPolicyStatus, found, upToDate bool, err error) {
	rec = operatorapiv1.SPIFFEAdmissionPolicyStatus{PolicyName: o.policy.Name}
	p, err := o.client.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(ctx, o.policy.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return rec, false, false, nil
	}
	if err != nil {
		return rec, false, false, err
	}
	b, err := o.client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(ctx, o.binding.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return rec, false, false, nil
	}
	if err != nil {
		return rec, false, false, err
	}
	rec.PolicyUID, rec.PolicyGeneration = p.UID, p.Generation
	rec.BindingUID, rec.BindingGeneration = b.UID, b.Generation
	upToDate = equality.Semantic.DeepEqual(p.Spec, o.policy.Spec) && equality.Semantic.DeepEqual(b.Spec, o.binding.Spec)
	return rec, true, upToDate, nil
}

func apply(ctx context.Context, o admissionObjects) error {
	policies := o.client.AdmissionregistrationV1().ValidatingAdmissionPolicies()
	p, err := policies.Get(ctx, o.policy.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		if _, err := policies.Create(ctx, o.policy, metav1.CreateOptions{}); err != nil {
			return err
		}
	case err != nil:
		return err
	case !equality.Semantic.DeepEqual(p.Spec, o.policy.Spec):
		p.Spec = o.policy.Spec
		if _, err := policies.Update(ctx, p, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	bindings := o.client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings()
	b, err := bindings.Get(ctx, o.binding.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = bindings.Create(ctx, o.binding, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	case !equality.Semantic.DeepEqual(b.Spec, o.binding.Spec):
		b.Spec = o.binding.Spec
		_, err = bindings.Update(ctx, b, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func policyAPIServed(c kubernetes.Interface) (bool, error) {
	resources, err := c.Discovery().ServerResourcesForGroupVersion(admissionv1.SchemeGroupVersion.String())
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, r := range resources.APIResources {
		if r.Name == "validatingadmissionpolicies" {
			return true, nil
		}
	}
	return false, nil
}

// whoAmI returns the user a client authenticates as.
func whoAmI(ctx context.Context, c kubernetes.Interface) (string, error) {
	review, err := c.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to find the operator's user: %w", err)
	}
	return review.Status.UserInfo.Username, nil
}

// spiffeAdmissionReconcile installs the admission policies of the spiffe driver and makes sure
// the operator can trust the agent and add-on namespaces before any agent runs. The Klusterlet
// status is the only record: the live policies and bindings must still be the ones recorded
// there, or the operator runs recovery, which cleans the namespaces again. Recovery is done one
// reconcile at a time and is safe to repeat.
type spiffeAdmissionReconcile struct {
	managedClusterClients *managedClusterClients
	kubeClient            kubernetes.Interface
	// spiffeWorkAgents lists every SPIFFE Klusterlet's work agent user, by Klusterlet name.
	spiffeWorkAgents func() (map[string]string, error)
}

func (r *spiffeAdmissionReconcile) reconcile(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	if config.RegistrationDriver.AuthType != operatorapiv1.SPIFFEAuthType {
		return klusterlet, reconcileContinue, nil
	}
	managed := r.managedClusterClients.kubeClient
	for _, c := range []kubernetes.Interface{r.kubeClient, managed} {
		served, err := policyAPIServed(c)
		if err != nil {
			return klusterlet, reconcileStop, err
		}
		if !served {
			meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
				Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionFalse, Reason: ReasonSPIFFEPolicyAPIAbsent,
				Message: "the spiffe driver needs the admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy API (Kubernetes 1.30 or later)"})
			return klusterlet, reconcileStop, fmt.Errorf("the ValidatingAdmissionPolicy API is not served")
		}
	}

	objects, err := r.render(ctx, klusterlet, config)
	if err != nil {
		return klusterlet, reconcileStop, err
	}
	if err := r.ensureAddOnNamespace(ctx, klusterlet, config); err != nil {
		return klusterlet, reconcileStop, err
	}

	var recorded []operatorapiv1.SPIFFEAdmissionPolicyStatus
	if klusterlet.Status.SPIFFE != nil {
		recorded = klusterlet.Status.SPIFFE.AdmissionPolicies
	}
	live, upToDate, err := observeAll(ctx, objects)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	if matchesRecord(recorded, live) {
		if !upToDate {
			// The operator changes its own policies (for example after an upgrade). The live state
			// was the recorded one before the change, so record the new generations without
			// recovery.
			for _, o := range objects {
				if err := apply(ctx, o); err != nil {
					return klusterlet, reconcileStop, err
				}
			}
			if live, _, err = observeAll(ctx, objects); err != nil {
				return klusterlet, reconcileStop, err
			}
			setAdmissionRecord(klusterlet, live)
		}
		setAdmissionReady(klusterlet, metav1.ConditionTrue, "Recorded", "the admission state is recorded")
		return klusterlet, reconcileContinue, nil
	}

	return r.recover(ctx, klusterlet, config, objects)
}

// recover runs the recovery steps: restore the policies, unlabel add-on namespaces that aren't on
// record, delete every workload in the agent namespace and the recorded add-on namespaces, and
// record the policies once no pod is left and they didn't change meanwhile. No agent runs until
// then.
func (r *spiffeAdmissionReconcile) recover(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig, objects []admissionObjects) (*operatorapiv1.Klusterlet, reconcileState, error) {
	logger := klog.FromContext(ctx)
	setAdmissionReady(klusterlet, metav1.ConditionFalse, "Recovering", "cleaning the agent and add-on namespaces")

	for _, o := range objects {
		if err := apply(ctx, o); err != nil {
			return klusterlet, reconcileStop, err
		}
	}
	noted, _, err := observeAll(ctx, objects)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	trusted, err := r.trustedAddOnNamespaces(ctx, klusterlet)
	if err != nil {
		return klusterlet, reconcileStop, err
	}
	if err := r.unlabelUntrusted(ctx, klusterlet.Name, trusted); err != nil {
		return klusterlet, reconcileStop, err
	}

	targets := []nsTarget{{r.kubeClient, config.AgentNamespace}}
	for _, ns := range trusted {
		targets = append(targets, nsTarget{r.managedClusterClients.kubeClient, ns})
	}
	remaining := 0
	for _, t := range targets {
		n, err := deleteWorkloads(ctx, t.client, t.namespace)
		if err != nil {
			return klusterlet, reconcileStop, err
		}
		remaining += n
	}
	if remaining > 0 {
		logger.Info("waiting for pods to go before recording the admission state", "pods", remaining)
		return klusterlet, reconcileStop, commonhelpers.NewRequeueError(
			fmt.Sprintf("SPIFFE recovery: %d pod(s) left", remaining), recoveryRequeue)
	}

	after, _, err := observeAll(ctx, objects)
	if err != nil {
		return klusterlet, reconcileStop, err
	}
	if !sameRecords(noted, after) {
		return klusterlet, reconcileStop, commonhelpers.NewRequeueError(
			"SPIFFE recovery: the admission policies changed during cleanup; starting over", recoveryRequeue)
	}
	setAdmissionRecord(klusterlet, noted)
	setAdmissionReady(klusterlet, metav1.ConditionTrue, "Recorded", "recovery finished and the admission state is recorded")
	return klusterlet, reconcileContinue, nil
}

type nsTarget struct {
	client    kubernetes.Interface
	namespace string
}

func (r *spiffeAdmissionReconcile) render(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) ([]admissionObjects, error) {
	operatorUser, err := whoAmI(ctx, r.kubeClient)
	if err != nil {
		return nil, err
	}
	managedOperatorUser := operatorUser
	if helpers.IsHosted(config.InstallMode) {
		if managedOperatorUser, err = whoAmI(ctx, r.managedClusterClients.kubeClient); err != nil {
			return nil, err
		}
	}
	workAgents, err := r.spiffeWorkAgents()
	if err != nil {
		return nil, err
	}
	in := spiffeAdmissionInput{
		klusterletName:      klusterlet.Name,
		agentNamespace:      config.AgentNamespace,
		operatorUser:        operatorUser,
		managedOperatorUser: managedOperatorUser,
		pairs:               spiffeRolePairs(config),
		workAgentUser:       workAgentUser(config.KlusterletNamespace, config.WorkServiceAccount),
		workAgents:          workAgents,
	}
	agentPolicy, agentBinding := agentAdmissionPolicy(in)
	addOnPolicy, addOnBinding := addOnAdmissionPolicy(in)
	labelPolicy, labelBinding := namespaceLabelPolicy(in)
	return []admissionObjects{
		{r.kubeClient, agentPolicy, agentBinding},
		{r.managedClusterClients.kubeClient, addOnPolicy, addOnBinding},
		{r.managedClusterClients.kubeClient, labelPolicy, labelBinding},
	}, nil
}

// spiffeRolePairs maps each spiffe-role of the install mode to its ServiceAccount.
func spiffeRolePairs(config klusterletConfig) map[string]string {
	if helpers.IsSingleton(config.InstallMode) {
		return map[string]string{"klusterlet": config.WorkServiceAccount}
	}
	return map[string]string{"registration": config.RegistrationServiceAccount, "work": config.WorkServiceAccount}
}

// workAgentUser is the work agent's user on the managed cluster: its ServiceAccount in the
// Klusterlet namespace there (also in hosted modes, where its kubeconfig carries that
// ServiceAccount's token).
func workAgentUser(klusterletNamespace, workServiceAccount string) string {
	return "system:serviceaccount:" + klusterletNamespace + ":" + workServiceAccount
}

func observeAll(ctx context.Context, objects []admissionObjects) ([]operatorapiv1.SPIFFEAdmissionPolicyStatus, bool, error) {
	var out []operatorapiv1.SPIFFEAdmissionPolicyStatus
	all := true
	for _, o := range objects {
		rec, found, upToDate, err := observe(ctx, o)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil // missing: not trusted
		}
		all = all && upToDate
		out = append(out, rec)
	}
	return out, all, nil
}

func matchesRecord(recorded, live []operatorapiv1.SPIFFEAdmissionPolicyStatus) bool {
	return live != nil && sameRecords(recorded, live)
}

func sameRecords(a, b []operatorapiv1.SPIFFEAdmissionPolicyStatus) bool {
	if len(a) != len(b) {
		return false
	}
	byName := map[string]operatorapiv1.SPIFFEAdmissionPolicyStatus{}
	for _, r := range a {
		byName[r.PolicyName] = r
	}
	for _, r := range b {
		if got, ok := byName[r.PolicyName]; !ok || got != r {
			return false
		}
	}
	return true
}

func setAdmissionRecord(k *operatorapiv1.Klusterlet, rec []operatorapiv1.SPIFFEAdmissionPolicyStatus) {
	if k.Status.SPIFFE == nil {
		k.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{}
	}
	k.Status.SPIFFE.AdmissionPolicies = rec
}

func setAdmissionReady(k *operatorapiv1.Klusterlet, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&k.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionSPIFFEAdmissionReady, Status: status, Reason: reason, Message: message})
}

// trustedAddOnNamespaces returns the namespaces labeled for this Klusterlet whose UID is on record:
// the add-on namespace the operator created (status.spiffe.addOnNamespace), or one the work agent
// created, listed in an AppliedManifestWork on the managed cluster.
func (r *spiffeAdmissionReconcile) trustedAddOnNamespaces(ctx context.Context, klusterlet *operatorapiv1.Klusterlet) ([]string, error) {
	recorded := map[types.UID]bool{}
	if klusterlet.Status.SPIFFE != nil && klusterlet.Status.SPIFFE.AddOnNamespace != nil {
		recorded[klusterlet.Status.SPIFFE.AddOnNamespace.UID] = true
	}
	amws, err := r.managedClusterClients.appliedManifestWorkClient.List(ctx, metav1.ListOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if amws != nil {
		for _, amw := range amws.Items {
			for _, res := range amw.Status.AppliedResources {
				if res.Group == "" && res.Resource == "namespaces" {
					recorded[types.UID(res.UID)] = true
				}
			}
		}
	}
	nss, err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: SPIFFEAddOnNamespaceLabel + "=" + klusterlet.Name})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ns := range nss.Items {
		if recorded[ns.UID] {
			out = append(out, ns.Name)
		}
	}
	return out, nil
}

// unlabelUntrusted removes this Klusterlet's add-on namespace label from every namespace that
// isn't on record. Nothing else in such a namespace is touched.
func (r *spiffeAdmissionReconcile) unlabelUntrusted(ctx context.Context, klusterletName string, trusted []string) error {
	nss, err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: SPIFFEAddOnNamespaceLabel + "=" + klusterletName})
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, ns := range trusted {
		keep[ns] = true
	}
	patch := []byte(`{"metadata":{"labels":{"` + SPIFFEAddOnNamespaceLabel + `":null}}}`)
	for _, ns := range nss.Items {
		if keep[ns.Name] {
			continue
		}
		if _, err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().Patch(ctx, ns.Name,
			types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return err
		}
		klog.FromContext(ctx).Info("removed the add-on namespace label from a namespace that isn't on record", "namespace", ns.Name)
	}
	return nil
}

// deleteWorkloads deletes every workload and pod in namespace and returns how many pods are left.
func deleteWorkloads(ctx context.Context, c kubernetes.Interface, namespace string) (int, error) {
	fg := metav1.DeletePropagationForeground
	opts := metav1.DeleteOptions{PropagationPolicy: &fg}
	all := metav1.ListOptions{}
	deletes := []func() error{
		func() error { return c.AppsV1().Deployments(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.AppsV1().DaemonSets(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.AppsV1().StatefulSets(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.AppsV1().ReplicaSets(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.BatchV1().Jobs(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.BatchV1().CronJobs(namespace).DeleteCollection(ctx, opts, all) },
		func() error { return c.CoreV1().Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, all) },
	}
	for _, d := range deletes {
		if err := d(); err != nil && !apierrors.IsNotFound(err) {
			return 0, err
		}
	}
	pods, err := c.CoreV1().Pods(namespace).List(ctx, all)
	if err != nil {
		return 0, err
	}
	return len(pods.Items), nil
}

// ensureAddOnNamespace creates the default add-on namespace with this Klusterlet's label when it
// doesn't exist, and records its UID. It never labels an existing namespace: one without the label
// is reported with the AddOnNamespaceNotDedicated condition and left alone.
func (r *spiffeAdmissionReconcile) ensureAddOnNamespace(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) error {
	if config.DisableAddonNamespace {
		return nil
	}
	namespaces := r.managedClusterClients.kubeClient.CoreV1().Namespaces()
	ns, err := namespaces.Get(ctx, helpers.DefaultAddonNamespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		ns, err = namespaces.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:        helpers.DefaultAddonNamespace,
			Annotations: map[string]string{"workload.openshift.io/allowed": "management"},
			Labels:      map[string]string{SPIFFEAddOnNamespaceLabel: klusterlet.Name},
		}}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		if klusterlet.Status.SPIFFE == nil {
			klusterlet.Status.SPIFFE = &operatorapiv1.KlusterletSPIFFEStatus{}
		}
		klusterlet.Status.SPIFFE.AddOnNamespace = &operatorapiv1.SPIFFENamespaceRef{Name: ns.Name, UID: ns.UID}
	} else if err != nil {
		return err
	}
	if ns.Labels[SPIFFEAddOnNamespaceLabel] != klusterlet.Name {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: ConditionAddOnNamespaceNotDedicated, Status: metav1.ConditionTrue, Reason: "ExistingNamespace",
			Message: fmt.Sprintf("namespace %s existed before without the %s label; the operator doesn't label an existing namespace",
				ns.Name, SPIFFEAddOnNamespaceLabel)})
		return nil
	}
	meta.RemoveStatusCondition(&klusterlet.Status.Conditions, ConditionAddOnNamespaceNotDedicated)
	return nil
}

func (r *spiffeAdmissionReconcile) clean(_ context.Context, klusterlet *operatorapiv1.Klusterlet,
	_ klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	return klusterlet, reconcileContinue, nil
}
