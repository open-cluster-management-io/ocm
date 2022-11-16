package options

// This file exists to force the desired plugin implementations to be linked.
// This should probably be part of some configuration fed into the build for a
// given binary target.
import (
	// Admission policies

	certapproval "k8s.io/kubernetes/plugin/pkg/admission/certificates/approval"
	certsigning "k8s.io/kubernetes/plugin/pkg/admission/certificates/signing"
	certsubjectrestriction "k8s.io/kubernetes/plugin/pkg/admission/certificates/subjectrestriction"
	"k8s.io/kubernetes/plugin/pkg/admission/eventratelimit"
	"k8s.io/kubernetes/plugin/pkg/admission/gc"
	"k8s.io/kubernetes/plugin/pkg/admission/namespace/autoprovision"
	"k8s.io/kubernetes/plugin/pkg/admission/namespace/exists"
	"k8s.io/kubernetes/plugin/pkg/admission/serviceaccount"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/namespace/lifecycle"
	"k8s.io/apiserver/pkg/admission/plugin/resourcequota"
	mutatingwebhook "k8s.io/apiserver/pkg/admission/plugin/webhook/mutating"
	validatingwebhook "k8s.io/apiserver/pkg/admission/plugin/webhook/validating"

	"open-cluster-management.io/ocm-controlplane/plugin/admission/managedclustermutating"
	"open-cluster-management.io/ocm-controlplane/plugin/admission/managedclustersetbindingvalidating"
	"open-cluster-management.io/ocm-controlplane/plugin/admission/managedclustervalidating"
)

// AllOrderedPlugins is the list of all the plugins in order.
var AllOrderedPlugins = []string{
	autoprovision.PluginName,          // NamespaceAutoProvision
	lifecycle.PluginName,              // NamespaceLifecycle
	exists.PluginName,                 // NamespaceExists
	serviceaccount.PluginName,         // ServiceAccount
	eventratelimit.PluginName,         // EventRateLimit
	gc.PluginName,                     // OwnerReferencesPermissionEnforcement
	certapproval.PluginName,           // CertificateApproval
	certsigning.PluginName,            // CertificateSigning
	certsubjectrestriction.PluginName, // CertificateSubjectRestriction

	// self-defined plugins
	managedclustermutating.PluginName,             // ManagedClusterMutating
	managedclustervalidating.PluginName,           // ManagedClusterValidating
	managedclustersetbindingvalidating.PluginName, // ManagedClusterSetBindingValidating
	// new admission plugins should generally be inserted above here
	// webhook, resourcequota, and deny plugins must go at the end

	mutatingwebhook.PluginName,   // MutatingAdmissionWebhook
	validatingwebhook.PluginName, // ValidatingAdmissionWebhook
	resourcequota.PluginName,     // ResourceQuota
}

// RegisterAllAdmissionPlugins registers all admission plugins.
// The order of registration is irrelevant, see AllOrderedPlugins for execution order.
func RegisterAllAdmissionPlugins(plugins *admission.Plugins) {
	eventratelimit.Register(plugins)
	gc.Register(plugins)
	autoprovision.Register(plugins)
	exists.Register(plugins)
	resourcequota.Register(plugins)
	serviceaccount.Register(plugins)
	certapproval.Register(plugins)
	certsigning.Register(plugins)
	certsubjectrestriction.Register(plugins)

	// self-defined admission plugins
	managedclustermutating.Register(plugins)
	managedclustervalidating.Register(plugins)
	managedclustersetbindingvalidating.Register(plugins)
}

// DefaultOffAdmissionPlugins get admission plugins off by default for kube-apiserver.
func DefaultOffAdmissionPlugins() sets.String {
	defaultOnPlugins := sets.NewString(
		lifecycle.PluginName,              // NamespaceLifecycle
		serviceaccount.PluginName,         // ServiceAccount
		mutatingwebhook.PluginName,        // MutatingAdmissionWebhook
		validatingwebhook.PluginName,      // ValidatingAdmissionWebhook
		resourcequota.PluginName,          // ResourceQuota
		certapproval.PluginName,           // CertificateApproval
		certsigning.PluginName,            // CertificateSigning
		certsubjectrestriction.PluginName, // CertificateSubjectRestriction
	)

	return sets.NewString(AllOrderedPlugins...).Difference(defaultOnPlugins)
}
