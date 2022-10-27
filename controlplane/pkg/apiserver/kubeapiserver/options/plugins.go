package options

// This file exists to force the desired plugin implementations to be linked.
// This should probably be part of some configuration fed into the build for a
// given binary target.
import (
	// Admission policies
	"k8s.io/kubernetes/plugin/pkg/admission/admit"
	"k8s.io/kubernetes/plugin/pkg/admission/alwayspullimages"
	"k8s.io/kubernetes/plugin/pkg/admission/antiaffinity"
	certapproval "k8s.io/kubernetes/plugin/pkg/admission/certificates/approval"
	certsigning "k8s.io/kubernetes/plugin/pkg/admission/certificates/signing"
	certsubjectrestriction "k8s.io/kubernetes/plugin/pkg/admission/certificates/subjectrestriction"
	"k8s.io/kubernetes/plugin/pkg/admission/defaulttolerationseconds"
	"k8s.io/kubernetes/plugin/pkg/admission/deny"
	"k8s.io/kubernetes/plugin/pkg/admission/eventratelimit"
	"k8s.io/kubernetes/plugin/pkg/admission/extendedresourcetoleration"
	"k8s.io/kubernetes/plugin/pkg/admission/gc"
	"k8s.io/kubernetes/plugin/pkg/admission/imagepolicy"
	"k8s.io/kubernetes/plugin/pkg/admission/limitranger"
	"k8s.io/kubernetes/plugin/pkg/admission/namespace/autoprovision"
	"k8s.io/kubernetes/plugin/pkg/admission/namespace/exists"
	"k8s.io/kubernetes/plugin/pkg/admission/securitycontext/scdeny"
	"k8s.io/kubernetes/plugin/pkg/admission/serviceaccount"
	"k8s.io/kubernetes/plugin/pkg/admission/storage/persistentvolume/label"
	"k8s.io/kubernetes/plugin/pkg/admission/storage/persistentvolume/resize"
	"k8s.io/kubernetes/plugin/pkg/admission/storage/storageclass/setdefault"
	"k8s.io/kubernetes/plugin/pkg/admission/storage/storageobjectinuseprotection"

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
	admit.PluginName,                        // AlwaysAdmit
	autoprovision.PluginName,                // NamespaceAutoProvision
	lifecycle.PluginName,                    // NamespaceLifecycle
	exists.PluginName,                       // NamespaceExists
	scdeny.PluginName,                       // SecurityContextDeny
	antiaffinity.PluginName,                 // LimitPodHardAntiAffinityTopology
	limitranger.PluginName,                  // LimitRanger
	serviceaccount.PluginName,               // ServiceAccount
	alwayspullimages.PluginName,             // AlwaysPullImages
	imagepolicy.PluginName,                  // ImagePolicyWebhook
	defaulttolerationseconds.PluginName,     // DefaultTolerationSeconds
	eventratelimit.PluginName,               // EventRateLimit
	extendedresourcetoleration.PluginName,   // ExtendedResourceToleration
	label.PluginName,                        // PersistentVolumeLabel
	setdefault.PluginName,                   // DefaultStorageClass
	storageobjectinuseprotection.PluginName, // StorageObjectInUseProtection
	gc.PluginName,                           // OwnerReferencesPermissionEnforcement
	resize.PluginName,                       // PersistentVolumeClaimResize
	certapproval.PluginName,                 // CertificateApproval
	certsigning.PluginName,                  // CertificateSigning
	certsubjectrestriction.PluginName,       // CertificateSubjectRestriction

	// self-defined plugins
	managedclustermutating.PluginName,
	managedclustervalidating.PluginName,
	managedclustersetbindingvalidating.PluginName,
	// new admission plugins should generally be inserted above here
	// webhook, resourcequota, and deny plugins must go at the end

	mutatingwebhook.PluginName,   // MutatingAdmissionWebhook
	validatingwebhook.PluginName, // ValidatingAdmissionWebhook
	resourcequota.PluginName,     // ResourceQuota
	deny.PluginName,              // AlwaysDeny
}

// RegisterAllAdmissionPlugins registers all admission plugins.
// The order of registration is irrelevant, see AllOrderedPlugins for execution order.
func RegisterAllAdmissionPlugins(plugins *admission.Plugins) {
	admit.Register(plugins) // DEPRECATED as no real meaning
	alwayspullimages.Register(plugins)
	antiaffinity.Register(plugins)
	defaulttolerationseconds.Register(plugins)
	deny.Register(plugins) // DEPRECATED as no real meaning
	eventratelimit.Register(plugins)
	extendedresourcetoleration.Register(plugins)
	gc.Register(plugins)
	imagepolicy.Register(plugins)
	limitranger.Register(plugins)
	autoprovision.Register(plugins)
	exists.Register(plugins)
	label.Register(plugins) // DEPRECATED, future PVs should not rely on labels for zone topology
	resourcequota.Register(plugins)
	scdeny.Register(plugins)
	serviceaccount.Register(plugins)
	setdefault.Register(plugins)
	resize.Register(plugins)
	storageobjectinuseprotection.Register(plugins)
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
		lifecycle.PluginName,                    // NamespaceLifecycle
		limitranger.PluginName,                  // LimitRanger
		serviceaccount.PluginName,               // ServiceAccount
		setdefault.PluginName,                   // DefaultStorageClass
		resize.PluginName,                       // PersistentVolumeClaimResize
		defaulttolerationseconds.PluginName,     // DefaultTolerationSeconds
		mutatingwebhook.PluginName,              // MutatingAdmissionWebhook
		validatingwebhook.PluginName,            // ValidatingAdmissionWebhook
		resourcequota.PluginName,                // ResourceQuota
		storageobjectinuseprotection.PluginName, // StorageObjectInUseProtection
		certapproval.PluginName,                 // CertificateApproval
		certsigning.PluginName,                  // CertificateSigning
		certsubjectrestriction.PluginName,       // CertificateSubjectRestriction
	)

	return sets.NewString(AllOrderedPlugins...).Difference(defaultOnPlugins)
}
