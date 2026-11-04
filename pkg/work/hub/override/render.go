// Package override renders cluster-specific overrides into a ManifestWorkReplicaSet's
// ManifestWorkTemplate. See enhancements#198.
//
// The Policy types below are placeholders for the ManifestOverridePolicy API, which is
// still under review. They will be replaced by the api types once those exist.
package override

import (
	"fmt"
	"sort"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/work/helper"
)

// PatchType is the kind of patch to apply.
type PatchType string

const (
	// PatchTypeJSON is an RFC 6902 JSON Patch.
	PatchTypeJSON PatchType = "json"
	// PatchTypeMerge is an RFC 7386 JSON Merge Patch.
	PatchTypeMerge PatchType = "merge"
	// PatchTypeStrategic is a Kubernetes strategic merge patch. Only built-in types are
	// supported for now, because the merge keys come from their Go types.
	PatchTypeStrategic PatchType = "strategic"
)

// Patch is one patch to apply to a matched manifest.
type Patch struct {
	Type PatchType
	// Patch is the JSON patch body.
	Patch []byte
}

// Override applies Patches, in order, on clusters matched by ClusterSelector.
type Override struct {
	// ClusterSelector selects clusters by ManagedCluster labels. An empty selector matches all clusters.
	ClusterSelector metav1.LabelSelector
	Patches         []Patch
}

// Policy is a placeholder for ManifestOverridePolicy.
type Policy struct {
	Namespace string
	Name      string
	// Priority orders policies: lower priority is applied first, so the highest wins.
	// Ties are ordered by name.
	Priority int32
	// ResourceSelectors selects manifests. Name and namespace support "*".
	ResourceSelectors []workapiv1.ResourceIdentifier
	// Overrides are applied in list order.
	Overrides []Override
}

// Key returns the policy's namespace/name.
func (p Policy) Key() string {
	return p.Namespace + "/" + p.Name
}

// Result is the rendered ManifestWork spec for one cluster.
type Result struct {
	Spec workapiv1.ManifestWorkSpec
	// AppliedPolicies lists, sorted, the namespace/name of every policy that patched at least one manifest.
	AppliedPolicies []string
}

// RenderError reports which policy, manifest, override and patch failed to render.
// An index is -1 when it does not apply.
type RenderError struct {
	Policy        string
	ManifestIndex int
	OverrideIndex int
	PatchIndex    int
	Err           error
}

func (e *RenderError) Error() string {
	msg := "policy " + e.Policy
	if e.ManifestIndex >= 0 {
		msg += fmt.Sprintf(", manifest %d", e.ManifestIndex)
	}
	if e.OverrideIndex >= 0 {
		msg += fmt.Sprintf(", override %d", e.OverrideIndex)
	}
	if e.PatchIndex >= 0 {
		msg += fmt.Sprintf(", patch %d", e.PatchIndex)
	}
	return msg + ": " + e.Err.Error()
}

func (e *RenderError) Unwrap() error {
	return e.Err
}

// matchedPolicy is a policy with only the overrides that match the cluster.
type matchedPolicy struct {
	policy          Policy
	overrideIndexes []int
}

// Render applies policies to template for a cluster with the given labels.
//
// It never modifies template, and the same inputs always produce the same output.
// Policies are applied in ascending (priority, name) order; overrides and patches
// inside a policy are applied in list order.
//
// mapper is used to find each manifest's resource (for ResourceSelectors). It is only
// needed when at least one policy matches the cluster.
func Render(template *workapiv1.ManifestWorkSpec, clusterLabels map[string]string,
	policies []Policy, mapper meta.RESTMapper) (*Result, error) {
	result := &Result{Spec: *template.DeepCopy()}

	matched, err := matchPolicies(policies, labels.Set(clusterLabels))
	if err != nil {
		return nil, err
	}
	if len(matched) == 0 {
		return result, nil
	}

	applied := sets.New[string]()
	manifests := result.Spec.Workload.Manifests
	for i := range manifests {
		patched, policyKeys, err := renderManifest(i, manifests[i].Raw, matched, mapper)
		if err != nil {
			return nil, err
		}
		if len(policyKeys) == 0 {
			continue
		}
		manifests[i].Raw = patched
		manifests[i].Object = nil
		applied.Insert(policyKeys...)
	}

	result.AppliedPolicies = sets.List(applied)
	return result, nil
}

// matchPolicies sorts policies and keeps those with at least one override matching the cluster.
func matchPolicies(policies []Policy, clusterLabels labels.Set) ([]matchedPolicy, error) {
	sorted := make([]Policy, len(policies))
	copy(sorted, policies)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Key() < sorted[j].Key()
	})

	var matched []matchedPolicy
	for _, p := range sorted {
		var indexes []int
		for i := range p.Overrides {
			selector, err := metav1.LabelSelectorAsSelector(&p.Overrides[i].ClusterSelector)
			if err != nil {
				return nil, &RenderError{Policy: p.Key(), ManifestIndex: -1, OverrideIndex: i, PatchIndex: -1,
					Err: fmt.Errorf("invalid cluster selector: %w", err)}
			}
			if selector.Matches(clusterLabels) {
				indexes = append(indexes, i)
			}
		}
		if len(indexes) > 0 {
			matched = append(matched, matchedPolicy{policy: p, overrideIndexes: indexes})
		}
	}
	return matched, nil
}

// renderManifest applies matching policies to one manifest. It returns the patched manifest
// and the keys of the policies that were applied; no keys means the manifest is unchanged.
func renderManifest(index int, raw []byte, matched []matchedPolicy,
	mapper meta.RESTMapper) ([]byte, []string, error) {
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(raw); err != nil {
		return nil, nil, fmt.Errorf("manifest %d: failed to decode: %w", index, err)
	}
	if mapper == nil {
		return nil, nil, fmt.Errorf("manifest %d: no REST mapper to resolve %s", index, obj.GroupVersionKind())
	}
	resourceMeta, _, err := helper.BuildResourceMeta(index, obj, mapper)
	if err != nil {
		return nil, nil, fmt.Errorf("manifest %d: %w", index, err)
	}

	patched := raw
	var policyKeys []string
	for _, m := range matched {
		if !selectsResource(m.policy.ResourceSelectors, resourceMeta) {
			continue
		}
		for _, oi := range m.overrideIndexes {
			for pi, patch := range m.policy.Overrides[oi].Patches {
				patched, err = applyPatch(patched, patch, obj.GroupVersionKind())
				if err != nil {
					return nil, nil, &RenderError{Policy: m.policy.Key(), ManifestIndex: index,
						OverrideIndex: oi, PatchIndex: pi, Err: err}
				}
			}
		}
		if err := checkIdentity(obj, patched); err != nil {
			return nil, nil, &RenderError{Policy: m.policy.Key(), ManifestIndex: index,
				OverrideIndex: -1, PatchIndex: -1, Err: err}
		}
		policyKeys = append(policyKeys, m.policy.Key())
	}
	return patched, policyKeys, nil
}

func selectsResource(selectors []workapiv1.ResourceIdentifier, resourceMeta workapiv1.ManifestResourceMeta) bool {
	for _, s := range selectors {
		if helper.ResourceMatch(resourceMeta, s) {
			return true
		}
	}
	return false
}

func applyPatch(doc []byte, patch Patch, gvk schema.GroupVersionKind) ([]byte, error) {
	switch patch.Type {
	case PatchTypeJSON:
		p, err := jsonpatch.DecodePatch(patch.Patch)
		if err != nil {
			return nil, fmt.Errorf("invalid json patch: %w", err)
		}
		return p.Apply(doc)
	case PatchTypeMerge:
		return jsonpatch.MergePatch(doc, patch.Patch)
	case PatchTypeStrategic:
		dataStruct, err := scheme.Scheme.New(gvk)
		if err != nil {
			return nil, fmt.Errorf("strategic merge patch is not supported for %s: no type information", gvk)
		}
		return strategicpatch.StrategicMergePatch(doc, patch.Patch, dataStruct)
	default:
		return nil, fmt.Errorf("unknown patch type %q", patch.Type)
	}
}

// checkIdentity rejects patches that change which resource a manifest refers to.
func checkIdentity(original *unstructured.Unstructured, patched []byte) error {
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(patched); err != nil {
		return fmt.Errorf("patched manifest is invalid: %w", err)
	}
	if obj.GetAPIVersion() != original.GetAPIVersion() || obj.GetKind() != original.GetKind() ||
		obj.GetName() != original.GetName() || obj.GetNamespace() != original.GetNamespace() {
		return fmt.Errorf("patches must not change apiVersion, kind, name or namespace")
	}
	return nil
}
