package apply

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/pkg/work/helper"
)

// Prototype for enhancement 192 (hosted addon cluster-scoped resources). The KEP adds a per-manifest
// ownership field to the Work API; this prototype carries the expected owner in a ManifestWork label
// instead, because labels are already copied to the AppliedManifestWork and so are still available
// during finalization. Not for merge.

// HostingOwnerLabel holds the expected owner as "<cluster>_<addon>". "_" can't appear in either name,
// and label values can't contain "/".
const HostingOwnerLabel = "proto.work.open-cluster-management.io/hosting-owner"

// HostingAddonAnnotation is the ownership annotation the KEP stamps on every claimed resource.
const HostingAddonAnnotation = "addon.open-cluster-management.io/hosting-addon"

// ExpectedHostingOwner returns "<cluster>/<addon>" from the Work labels, or "" when ownership is not enforced.
func ExpectedHostingOwner(labels map[string]string) string {
	return strings.Replace(labels[HostingOwnerLabel], "_", "/", 1)
}

// HostingOwnerConflictError reports a live resource that is unannotated or owned by another addon instance.
type HostingOwnerConflictError struct {
	Expected, Observed string
}

func (e *HostingOwnerConflictError) Error() string {
	observed := e.Observed
	if observed == "" {
		observed = "<none>"
	}
	return fmt.Sprintf("HostingResourceConflict: expected owner %q, live resource is owned by %s", e.Expected, observed)
}

// HostingOwnerApply creates an absent resource, applies an owned one with forced server-side apply, and
// leaves a foreign one unchanged. Every write is bound to the object it checked.
type HostingOwnerApply struct {
	client   dynamic.Interface
	expected string
}

func NewHostingOwnerApply(client dynamic.Interface, expected string) *HostingOwnerApply {
	return &HostingOwnerApply{client: client, expected: expected}
}

func (h *HostingOwnerApply) Apply(ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	option *workapiv1.ManifestConfigOption,
	_ events.Recorder) (runtime.Object, error) {
	if got := required.GetAnnotations()[HostingAddonAnnotation]; got != h.expected {
		return nil, fmt.Errorf("manifest %s annotation is %q, want the Work's expected owner %q",
			HostingAddonAnnotation, got, h.expected)
	}
	fieldManager := workapiv1.DefaultFieldManager
	if option != nil && option.UpdateStrategy != nil && option.UpdateStrategy.ServerSideApply != nil &&
		option.UpdateStrategy.ServerSideApply.FieldManager != "" {
		fieldManager = option.UpdateStrategy.ServerSideApply.FieldManager
	}
	resource := h.client.Resource(gvr).Namespace(required.GetNamespace())

	live, err := resource.Get(ctx, required.GetName(), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// A create request can't overwrite an object that appears at the same name in the meantime;
		// AlreadyExists sends the next reconcile through the ownership check again.
		obj := required.DeepCopy()
		if !isRemovalOwner(owner) {
			obj.SetOwnerReferences([]metav1.OwnerReference{owner})
		}
		return resource.Create(ctx, obj, metav1.CreateOptions{FieldManager: fieldManager})
	case err != nil:
		return nil, err
	}

	if observed := live.GetAnnotations()[HostingAddonAnnotation]; observed != h.expected {
		return nil, &HostingOwnerConflictError{Expected: h.expected, Observed: observed}
	}

	// UID and resourceVersion from the read above: if the object was replaced or changed since, the
	// apply fails with a conflict instead of writing to an object that wasn't checked.
	obj := required.DeepCopy()
	obj.SetUID(live.GetUID())
	obj.SetResourceVersion(live.GetResourceVersion())
	applied, err := resource.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: fieldManager, Force: true})
	if err != nil {
		return nil, err
	}
	return applied, helper.ApplyOwnerReferences(ctx, h.client, gvr, applied, owner)
}

// isRemovalOwner reports the owner that manageOwnerRef builds for an orphaned resource.
func isRemovalOwner(owner metav1.OwnerReference) bool {
	return strings.HasSuffix(string(owner.UID), "-")
}
