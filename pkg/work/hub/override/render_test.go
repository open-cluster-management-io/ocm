package override

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

const deployment = `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app-ns"},
"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},
"template":{"metadata":{"labels":{"app":"web"}},
"spec":{"containers":[{"name":"app","image":"nginx","env":[{"name":"LOG","value":"info"}]},{"name":"sidecar","image":"busybox"}]}}}}`

const configMap = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cfg","namespace":"app-ns"},"data":{"a":"b"}}`

const widget = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"name":"w","namespace":"app-ns"},
"spec":{"items":[{"name":"x","size":1}]}}`

var deploymentSelector = workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "web", Namespace: "*"}

func testMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}, meta.RESTScopeNamespace)
	return m
}

func template(manifests ...string) *workapiv1.ManifestWorkSpec {
	spec := &workapiv1.ManifestWorkSpec{}
	for _, m := range manifests {
		spec.Workload.Manifests = append(spec.Workload.Manifests,
			workapiv1.Manifest{RawExtension: runtime.RawExtension{Raw: []byte(m)}})
	}
	return spec
}

func onCluster(key, value string) metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{key: value}}
}

func policy(name string, priority int32, selector workapiv1.ResourceIdentifier, overrides ...Override) Policy {
	return Policy{Namespace: "app-ns", Name: name, Priority: priority,
		ResourceSelectors: []workapiv1.ResourceIdentifier{selector}, Overrides: overrides}
}

func patch(t PatchType, body string) Patch {
	return Patch{Type: t, Patch: []byte(body)}
}

// field decodes manifest i of the result and returns the value at path.
func field(t *testing.T, r *Result, i int, path ...string) interface{} {
	t.Helper()
	var obj map[string]interface{}
	if err := json.Unmarshal(r.Spec.Workload.Manifests[i].Raw, &obj); err != nil {
		t.Fatal(err)
	}
	var cur interface{} = obj
	for _, p := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func containers(t *testing.T, r *Result) []interface{} {
	t.Helper()
	c, _ := field(t, r, 0, "spec", "template", "spec", "containers").([]interface{})
	return c
}

func TestRenderNoPolicies(t *testing.T) {
	tmpl := template(deployment)
	// A nil mapper is fine when no policy matches: existing MWRS are not affected.
	r, err := Render(tmpl, map[string]string{"env": "prod"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !equality.Semantic.DeepEqual(r.Spec, *tmpl) || len(r.AppliedPolicies) != 0 {
		t.Errorf("expected template unchanged and no applied policies, got %v", r.AppliedPolicies)
	}
}

// Scenario 1: different nodeSelector per cluster.
func TestRenderNodeSelectorPerCluster(t *testing.T) {
	p := policy("nodepool", 100, deploymentSelector,
		Override{ClusterSelector: onCluster("nodepool-profile", "a"), Patches: []Patch{
			patch(PatchTypeMerge, `{"spec":{"template":{"spec":{"nodeSelector":{"nodepool":"apps-a"}}}}}`)}},
		Override{ClusterSelector: onCluster("nodepool-profile", "b"), Patches: []Patch{
			patch(PatchTypeMerge, `{"spec":{"template":{"spec":{"nodeSelector":{"nodepool":"apps-b"}}}}}`)}},
	)
	cases := []struct {
		profile string
		want    interface{}
		applied int
	}{
		{"a", "apps-a", 1},
		{"b", "apps-b", 1},
		{"c", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.profile, func(t *testing.T) {
			r, err := Render(template(deployment), map[string]string{"nodepool-profile": c.profile}, []Policy{p}, testMapper())
			if err != nil {
				t.Fatal(err)
			}
			if got := field(t, r, 0, "spec", "template", "spec", "nodeSelector", "nodepool"); got != c.want {
				t.Errorf("nodepool = %v, want %v", got, c.want)
			}
			if len(r.AppliedPolicies) != c.applied {
				t.Errorf("applied = %v, want %d", r.AppliedPolicies, c.applied)
			}
		})
	}
}

// Scenario 2: JSON Patch adds a pod label; only selected resources change.
func TestRenderJSONPatchOnlySelectedResources(t *testing.T) {
	p := policy("region-label", 100,
		workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "*", Namespace: "*"},
		Override{ClusterSelector: onCluster("region", "east"), Patches: []Patch{
			patch(PatchTypeJSON, `[{"op":"add","path":"/spec/template/metadata/labels/region","value":"east"}]`)}},
	)
	tmpl := template(deployment, configMap)
	r, err := Render(tmpl, map[string]string{"region": "east"}, []Policy{p}, testMapper())
	if err != nil {
		t.Fatal(err)
	}
	if got := field(t, r, 0, "spec", "template", "metadata", "labels", "region"); got != "east" {
		t.Errorf("region label = %v, want east", got)
	}
	if string(r.Spec.Workload.Manifests[1].Raw) != configMap {
		t.Errorf("configmap should be unchanged, got %s", r.Spec.Workload.Manifests[1].Raw)
	}
	if want := []string{"app-ns/region-label"}; !equality.Semantic.DeepEqual(r.AppliedPolicies, want) {
		t.Errorf("applied = %v, want %v", r.AppliedPolicies, want)
	}
}

// Scenario 3: priority ascending, ties by name, so the last applied wins.
func TestRenderPriorityOrder(t *testing.T) {
	replicas := func(name string, prio int32, sel metav1.LabelSelector, n string) Policy {
		return policy(name, prio, deploymentSelector, Override{ClusterSelector: sel, Patches: []Patch{
			patch(PatchTypeMerge, `{"spec":{"replicas":`+n+`}}`)}})
	}
	// Deliberately unsorted input.
	policies := []Policy{
		replicas("prod-hotfix", 50, onCluster("env", "prod"), "6"),
		replicas("base", 10, metav1.LabelSelector{}, "2"),
		replicas("prod", 50, onCluster("env", "prod"), "5"),
	}
	cases := map[string]float64{"prod": 6, "staging": 2}
	for env, want := range cases {
		r, err := Render(template(deployment), map[string]string{"env": env}, policies, testMapper())
		if err != nil {
			t.Fatal(err)
		}
		if got := field(t, r, 0, "spec", "replicas"); got != want {
			t.Errorf("%s: replicas = %v, want %v", env, got, want)
		}
	}
}

// Scenario 5: a JSON Patch under a missing parent fails, and the error names the policy.
func TestRenderPatchFailure(t *testing.T) {
	p := policy("gpu-pool", 100, deploymentSelector,
		Override{ClusterSelector: onCluster("pool", "gpu"), Patches: []Patch{
			patch(PatchTypeJSON, `[{"op":"add","path":"/spec/template/spec/nodeSelector/pool","value":"gpu"}]`)}})

	_, err := Render(template(deployment), map[string]string{"pool": "gpu"}, []Policy{p}, testMapper())
	var renderErr *RenderError
	if !errors.As(err, &renderErr) {
		t.Fatalf("expected RenderError, got %v", err)
	}
	if renderErr.Policy != "app-ns/gpu-pool" || renderErr.ManifestIndex != 0 || renderErr.PatchIndex != 0 {
		t.Errorf("unexpected error details: %+v", renderErr)
	}

	// Clusters the policy does not select are unaffected.
	if _, err := Render(template(deployment), map[string]string{"pool": "cpu"}, []Policy{p}, testMapper()); err != nil {
		t.Errorf("unexpected error for unselected cluster: %v", err)
	}
}

// json-patch v4 silently adds a missing key on replace; v5 must return an error.
func TestRenderJSONReplaceMissingKeyFails(t *testing.T) {
	p := policy("priority", 100, deploymentSelector,
		Override{Patches: []Patch{
			patch(PatchTypeJSON, `[{"op":"replace","path":"/spec/template/spec/priorityClassName","value":"high"}]`)}})
	if _, err := Render(template(deployment), nil, []Policy{p}, testMapper()); err == nil {
		t.Error("expected error for replace on a missing key")
	}
}

// Scenario 6: strategic merge on a built-in type merges the container by name.
func TestRenderStrategicMergeBuiltin(t *testing.T) {
	p := policy("region-env", 100, deploymentSelector,
		Override{Patches: []Patch{patch(PatchTypeStrategic,
			`{"spec":{"template":{"spec":{"containers":[{"name":"app","env":[{"name":"REGION","value":"east"}]}]}}}}`)}})
	r, err := Render(template(deployment), nil, []Policy{p}, testMapper())
	if err != nil {
		t.Fatal(err)
	}
	cs := containers(t, r)
	if len(cs) != 2 {
		t.Fatalf("expected 2 containers (app and sidecar), got %v", cs)
	}
	app := cs[0].(map[string]interface{})
	if app["image"] != "nginx" {
		t.Errorf("image should be kept, got %v", app["image"])
	}
	env := app["env"].([]interface{})
	if len(env) != 2 {
		t.Errorf("expected LOG and REGION, got %v", env)
	}
}

// Scenario 6: strategic merge on a CRD is rejected instead of silently replacing lists.
func TestRenderStrategicMergeCRDRejected(t *testing.T) {
	p := policy("widget", 100,
		workapiv1.ResourceIdentifier{Group: "example.com", Resource: "widgets", Name: "w", Namespace: "app-ns"},
		Override{Patches: []Patch{patch(PatchTypeStrategic, `{"spec":{"items":[{"name":"x","size":2}]}}`)}})
	_, err := Render(template(widget), nil, []Policy{p}, testMapper())
	if err == nil || !strings.Contains(err.Error(), "no type information") {
		t.Errorf("expected a no type information error, got %v", err)
	}
}

func TestRenderRejectsIdentityChange(t *testing.T) {
	p := policy("rename", 100, deploymentSelector,
		Override{Patches: []Patch{patch(PatchTypeMerge, `{"metadata":{"name":"other"}}`)}})
	_, err := Render(template(deployment), nil, []Policy{p}, testMapper())
	if err == nil || !strings.Contains(err.Error(), "must not change") {
		t.Errorf("expected identity change error, got %v", err)
	}
}

func TestRenderResourceSelectorMismatch(t *testing.T) {
	p := policy("other", 100,
		workapiv1.ResourceIdentifier{Group: "apps", Resource: "deployments", Name: "api", Namespace: "*"},
		Override{Patches: []Patch{patch(PatchTypeMerge, `{"spec":{"replicas":9}}`)}})
	tmpl := template(deployment)
	r, err := Render(tmpl, nil, []Policy{p}, testMapper())
	if err != nil {
		t.Fatal(err)
	}
	if !equality.Semantic.DeepEqual(r.Spec, *tmpl) || len(r.AppliedPolicies) != 0 {
		t.Errorf("expected no change, applied = %v", r.AppliedPolicies)
	}
}

func TestRenderNeedsMapperWhenPolicyMatches(t *testing.T) {
	p := policy("p", 1, deploymentSelector, Override{Patches: []Patch{patch(PatchTypeMerge, `{}`)}})
	if _, err := Render(template(deployment), nil, []Policy{p}, nil); err == nil {
		t.Error("expected an error without a REST mapper")
	}
}

func TestRenderInvalidClusterSelector(t *testing.T) {
	bad := metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Bogus"}}}
	p := policy("bad", 1, deploymentSelector, Override{ClusterSelector: bad})
	var renderErr *RenderError
	if _, err := Render(template(deployment), nil, []Policy{p}, testMapper()); !errors.As(err, &renderErr) {
		t.Errorf("expected RenderError, got %v", err)
	}
}

func TestRenderDoesNotModifyTemplateAndIsDeterministic(t *testing.T) {
	policies := []Policy{
		policy("a", 1, deploymentSelector, Override{Patches: []Patch{
			patch(PatchTypeMerge, `{"spec":{"replicas":3}}`),
			patch(PatchTypeJSON, `[{"op":"add","path":"/spec/template/metadata/labels/tier","value":"web"}]`)}}),
	}
	tmpl := template(deployment)
	before := tmpl.DeepCopy()

	r1, err := Render(tmpl, nil, policies, testMapper())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Render(tmpl, nil, policies, testMapper())
	if err != nil {
		t.Fatal(err)
	}
	if !equality.Semantic.DeepEqual(tmpl, before) {
		t.Error("template was modified")
	}
	if string(r1.Spec.Workload.Manifests[0].Raw) != string(r2.Spec.Workload.Manifests[0].Raw) {
		t.Error("render is not deterministic")
	}
}
