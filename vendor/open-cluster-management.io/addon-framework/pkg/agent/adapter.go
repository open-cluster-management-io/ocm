package agent

import (
	"context"
	"errors"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	agentv1alpha1 "open-cluster-management.io/addon-framework/pkg/agent/v1alpha1"
)

// WrapV1alpha1 adapts a v1alpha1 AgentAddon so it satisfies the v1beta1 AgentAddon
// interface and all applicable optional interfaces (AddonConfigurer, Registrar,
// CSRApprover, HubPermitter).
//
// Use this to register existing addons with a v1beta1-based manager without any
// changes to the addon implementation itself.
func WrapV1alpha1(inner agentv1alpha1.AgentAddon) AgentAddon {
	return &v1alpha1Adapter{inner: inner}
}

// v1alpha1Adapter wraps a v1alpha1 AgentAddon and exposes the full v1beta1
// optional interface set.  Optional methods (RegistrationConfigs, ApproveCSR,
// SetHubPermissions) are no-ops when the corresponding v1alpha1 capability is
// not configured, so controllers that check empty returns behave identically to
// the interface not being implemented.
//
// GetAgentAddonOptions is called on each method invocation rather than cached
// at construction time, so that addons whose options change at runtime (e.g.
// integration-test stubs that mutate registrations or probers between test
// cases) always see the current state.
type v1alpha1Adapter struct {
	inner agentv1alpha1.AgentAddon
}

func (a *v1alpha1Adapter) Manifests(_ context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return a.inner.Manifests(cluster, ToV1alpha1Addon(addon))
}

// ---- AddonConfigurer ----

func (a *v1alpha1Adapter) GetAgentAddonOptions() AgentAddonOptions {
	opts := a.inner.GetAgentAddonOptions()

	// Merge Updaters into ManifestConfigs, preserving v1alpha1 priority semantics:
	// Updaters come first; ManifestConfigs entries for the same resource override the
	// Updater's UpdateStrategy (matching the old getManifestConfigOption behavior).
	manifestConfigs := make([]workapiv1.ManifestConfigOption, 0, len(opts.Updaters)+len(opts.ManifestConfigs))
	for _, u := range opts.Updaters {
		strategy := u.UpdateStrategy
		manifestConfigs = append(manifestConfigs, workapiv1.ManifestConfigOption{
			ResourceIdentifier: u.ResourceIdentifier,
			UpdateStrategy:     &strategy,
		})
	}
	manifestConfigs = append(manifestConfigs, opts.ManifestConfigs...)

	cfg := AgentAddonOptions{
		AddonName:                       opts.AddonName,
		SupportedConfigGVRs:             opts.SupportedConfigGVRs,
		ManifestConfigs:                 manifestConfigs,
		AgentDeployTriggerClusterFilter: opts.AgentDeployTriggerClusterFilter,
		ConfigCheckEnabled:              opts.ConfigCheckEnabled,
		HealthProber:                    toV1beta1HealthProber(opts.HealthProber),
		HostedModeEnabled:               opts.HostedModeEnabled,
	}

	if opts.HostedModeInfoFunc != nil {
		fn := opts.HostedModeInfoFunc
		cfg.HostedModeInfoFunc = func(addon *addonv1beta1.ManagedClusterAddOn, cluster *clusterv1.ManagedCluster) (string, string) {
			return fn(ToV1alpha1Addon(addon), cluster)
		}
	}

	if reg := opts.Registration; reg != nil {
		// Create v1beta1 Registration option with functions that delegate to adapter methods.
		// This ensures CSR approval controller can still use the old API while adapter
		// implements the new v1beta1 interfaces.
		cfg.Registration = &RegistrationOption{
			Configurations: a.RegistrationConfigs,
			CSRApproveCheck: func(ctx context.Context, cluster *clusterv1.ManagedCluster,
				addon *addonv1beta1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return a.ApproveCSR(ctx, cluster, addon, csr)
			},
			PermissionConfig: a.SetHubPermissions,
			CSRSign:          a.Sign,
		}

		switch {
		case reg.AgentInstallNamespace != nil:
			fn := reg.AgentInstallNamespace
			cfg.AgentInstallNamespace = func(_ context.Context, addon *addonv1beta1.ManagedClusterAddOn) (string, error) {
				return fn(ToV1alpha1Addon(addon))
			}
		case reg.Namespace != "":
			ns := reg.Namespace
			cfg.AgentInstallNamespace = func(_ context.Context, addon *addonv1beta1.ManagedClusterAddOn) (string, error) {
				// Preserve v1alpha1 behavior: Spec.InstallNamespace (deprecated but still
				// supported) takes priority over the static Registration.Namespace.
				// Spec.InstallNamespace is encoded in a well-known annotation by ToV1beta1Addon.
				if addon != nil {
					if v, ok := addon.Annotations[addonv1beta1.InstallNamespaceAnnotation]; ok && len(v) > 0 {
						return v, nil
					}
				}
				return ns, nil
			}
		}
	}

	return cfg
}

// ---- Registrar ----

// RegistrationConfigs returns nil when no registration is configured in the
// wrapped v1alpha1 addon.
func (a *v1alpha1Adapter) RegistrationConfigs(_ context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonv1beta1.ManagedClusterAddOn) ([]RegistrationConfig, error) {
	reg := a.inner.GetAgentAddonOptions().Registration
	if reg == nil || reg.CSRConfigurations == nil {
		return nil, nil
	}

	v1alpha1Configs, err := reg.CSRConfigurations(cluster, ToV1alpha1Addon(addon))
	if err != nil {
		return nil, err
	}

	return toV1beta1RegistrationConfigs(v1alpha1Configs), nil
}

// ---- CSRApprover ----

// ApproveCSR returns false when no CSRApproveCheck is configured in the
// wrapped v1alpha1 addon, leaving CSR approval to manual hub action.
func (a *v1alpha1Adapter) ApproveCSR(_ context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonv1beta1.ManagedClusterAddOn,
	csr *certificatesv1.CertificateSigningRequest) bool {
	reg := a.inner.GetAgentAddonOptions().Registration
	if reg == nil || reg.CSRApproveCheck == nil {
		return false
	}
	return reg.CSRApproveCheck(cluster, ToV1alpha1Addon(addon), csr)
}

// ---- CSRSigner ----
func (a *v1alpha1Adapter) Sign(_ context.Context, cluster *clusterv1.ManagedCluster, addon *addonv1beta1.ManagedClusterAddOn,
	csr *certificatesv1.CertificateSigningRequest) ([]byte, error) {
	reg := a.inner.GetAgentAddonOptions().Registration
	if reg == nil || reg.CSRSign == nil {
		return nil, nil
	}
	return reg.CSRSign(cluster, ToV1alpha1Addon(addon), csr)
}

// ---- HubPermitter ----

// SetHubPermissions is a no-op when no PermissionConfig is set in the wrapped
// v1alpha1 addon.
func (a *v1alpha1Adapter) SetHubPermissions(_ context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonv1beta1.ManagedClusterAddOn) error {
	reg := a.inner.GetAgentAddonOptions().Registration
	if reg == nil || reg.PermissionConfig == nil {
		return nil
	}
	err := reg.PermissionConfig(cluster, ToV1alpha1Addon(addon))
	if err != nil {
		// Convert v1alpha1 SubjectNotReadyError to v1beta1 SubjectNotReadyError so
		// controllers that check for the v1beta1 type work correctly.
		var v1alpha1SubjectErr *agentv1alpha1.SubjectNotReadyError
		if errors.As(err, &v1alpha1SubjectErr) {
			return &SubjectNotReadyError{}
		}
	}
	return err
}

// -------------------------------------------------------------------------
// Conversion helpers
// -------------------------------------------------------------------------

// ToV1alpha1Addon converts a v1beta1 ManagedClusterAddOn to the v1alpha1 type
// so it can be forwarded to existing v1alpha1 addon implementations.
//
// The Spec and Status conversions are delegated to the generated conversion
// functions in the API package, which handle the RegistrationConfig discriminated
// union and the KubeClientDriver ↔ Registrations[].KubeClient.Driver promotion.
//
//nolint:staticcheck
func ToV1alpha1Addon(in *addonv1beta1.ManagedClusterAddOn) *addonv1alpha1.ManagedClusterAddOn {
	if in == nil {
		return nil
	}
	out := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: in.ObjectMeta,
	}
	// nil scope is safe: none of the reachable Convert_* functions dereference it.
	_ = addonv1beta1.Convert_v1beta1_ManagedClusterAddOnSpec_To_v1alpha1_ManagedClusterAddOnSpec(&in.Spec, &out.Spec, nil)
	_ = addonv1beta1.Convert_v1beta1_ManagedClusterAddOnStatus_To_v1alpha1_ManagedClusterAddOnStatus(&in.Status, &out.Status, nil)
	return out
}

// ToV1beta1Addon converts a v1alpha1 ManagedClusterAddOn to the v1beta1 type
// so it can be passed to v1beta1 AgentAddon implementations from controllers
// that still hold v1alpha1 objects from their informer caches.
//
// The Spec and Status conversions are delegated to the generated conversion
// functions in the API package, which handle the RegistrationConfig discriminated
// union and the KubeClientDriver ↔ Registrations[].KubeClient.Driver promotion.
// The one exception is Spec.InstallNamespace (deprecated, v1alpha1-only): it is
// encoded into a well-known annotation so the v1alpha1 adapter's InstallNamespace
// function can honor v1alpha1 priority semantics (Spec.InstallNamespace >
// Registration.Namespace).
//
//nolint:staticcheck
func ToV1beta1Addon(in *addonv1alpha1.ManagedClusterAddOn) *addonv1beta1.ManagedClusterAddOn {
	if in == nil {
		return nil
	}
	out := &addonv1beta1.ManagedClusterAddOn{
		ObjectMeta: in.ObjectMeta,
	}

	// Encode deprecated Spec.InstallNamespace into an annotation so the v1alpha1
	// adapter's static-namespace InstallNamespace function can give it priority over
	// the addon's Registration.Namespace default.
	//nolint:staticcheck
	if len(in.Spec.InstallNamespace) > 0 {
		if out.Annotations == nil {
			out.Annotations = make(map[string]string)
		}
		//nolint:staticcheck
		out.Annotations[addonv1beta1.InstallNamespaceAnnotation] = in.Spec.InstallNamespace
	}

	// nil scope is safe: none of the reachable Convert_* functions dereference it.
	_ = addonv1beta1.Convert_v1alpha1_ManagedClusterAddOnSpec_To_v1beta1_ManagedClusterAddOnSpec(&in.Spec, &out.Spec, nil)
	_ = addonv1beta1.Convert_v1alpha1_ManagedClusterAddOnStatus_To_v1beta1_ManagedClusterAddOnStatus(&in.Status, &out.Status, nil)
	return out
}

// toV1beta1RegistrationConfigs converts v1alpha1 flat RegistrationConfigs to
// the framework-owned intent types used by the v1beta1 Registrar interface.
//
// The v1alpha1 CSRSign function (a single function shared across all custom
// signers) is attached to every CustomSignerRegistration entry.  This is safe
// because in v1alpha1 an addon can only have one custom signer in practice.
//
//nolint:staticcheck
func toV1beta1RegistrationConfigs(
	in []addonv1alpha1.RegistrationConfig,
) []RegistrationConfig {
	out := make([]RegistrationConfig, 0, len(in))
	for _, cfg := range in {
		if cfg.SignerName == certificatesv1.KubeAPIServerClientSignerName {
			out = append(out, &KubeClientRegistration{
				User:   cfg.Subject.User,
				Groups: cfg.Subject.Groups,
			})
			continue
		}

		out = append(out, &CustomSignerRegistration{
			SignerName:        cfg.SignerName,
			User:              cfg.Subject.User,
			Groups:            cfg.Subject.Groups,
			OrganizationUnits: cfg.Subject.OrganizationUnits,
		})
	}
	return out
}

// toV1beta1HealthProber converts a v1alpha1 HealthProber to the v1beta1 type.
// The HealthProberType string constants have the same values in both versions.
// The deprecated single-field HealthCheck function is dropped; only HealthChecker
// is carried forward, wrapped to perform the necessary addon type conversion.
func toV1beta1HealthProber(in *agentv1alpha1.HealthProber) *HealthProber {
	if in == nil {
		return nil
	}
	out := &HealthProber{
		Type: HealthProberType(in.Type),
	}
	if in.WorkProber == nil {
		return out
	}
	out.WorkProber = &WorkHealthProber{
		ProbeFields: toV1beta1ProbeFields(in.WorkProber.ProbeFields),
	}
	if in.WorkProber.HealthChecker != nil {
		fn := in.WorkProber.HealthChecker
		out.WorkProber.HealthChecker = func(
			results []FieldResult,
			cluster *clusterv1.ManagedCluster,
			addon *addonv1beta1.ManagedClusterAddOn,
		) error {
			return fn(toV1alpha1FieldResults(results), cluster, ToV1alpha1Addon(addon))
		}
	}
	return out
}

func toV1beta1ProbeFields(in []agentv1alpha1.ProbeField) []ProbeField {
	out := make([]ProbeField, len(in))
	for i, pf := range in {
		out[i] = ProbeField{
			ResourceIdentifier: pf.ResourceIdentifier,
			ProbeRules:         pf.ProbeRules,
		}
	}
	return out
}

func toV1alpha1FieldResults(in []FieldResult) []agentv1alpha1.FieldResult {
	out := make([]agentv1alpha1.FieldResult, len(in))
	for i, fr := range in {
		out[i] = agentv1alpha1.FieldResult{
			ResourceIdentifier: fr.ResourceIdentifier,
			FeedbackResult:     fr.FeedbackResult,
		}
	}
	return out
}
