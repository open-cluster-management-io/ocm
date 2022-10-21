package agentdeploy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/common/workbuilder"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func addonHasFinalizer(addon *addonapiv1alpha1.ManagedClusterAddOn, finalizer string) bool {
	for _, f := range addon.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func addonRemoveFinalizer(addon *addonapiv1alpha1.ManagedClusterAddOn, finalizer string) bool {
	var rst []string
	for _, f := range addon.Finalizers {
		if f != finalizer {
			rst = append(rst, f)
		}
	}
	if len(rst) != len(addon.Finalizers) {
		addon.SetFinalizers(rst)
		return true
	}
	return false
}

func addonAddFinalizer(addon *addonapiv1alpha1.ManagedClusterAddOn, finalizer string) bool {
	rst := addon.Finalizers
	if rst == nil {
		addon.SetFinalizers([]string{finalizer})
		return true
	}

	for _, f := range addon.Finalizers {
		if f == finalizer {
			return false
		}
	}

	rst = append(rst, finalizer)
	addon.SetFinalizers(rst)
	return true
}

func newManifestWork(addonNamespace, addonName, clusterName string, manifests []workapiv1.Manifest,
	manifestWorkNameFunc func(addonNamespace, addonName string) string) *workapiv1.ManifestWork {
	if len(manifests) == 0 {
		return nil
	}

	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifestWorkNameFunc(addonNamespace, addonName),
			Namespace: clusterName,
			Labels: map[string]string{
				constants.AddonLabel: addonName,
			},
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}

	// if the addon namespace is not equal with the manifestwork namespace(cluster name), add the addon namespace label
	if addonNamespace != clusterName {
		work.Labels[constants.AddonNamespaceLabel] = addonNamespace
	}
	return work
}

// isPreDeleteHookObject check the object is a pre-delete hook resources.
// currently, we only support job and pod as hook resources.
// we use WellKnownStatus here to get the job/pad status fields to check if the job/pod is completed.
func (b *addonWorksBuilder) isPreDeleteHookObject(obj runtime.Object) (bool, *workapiv1.ManifestConfigOption) {
	var resource string
	gvk := obj.GetObjectKind().GroupVersionKind()
	switch gvk.Kind {
	case "Job":
		resource = "jobs"
	case "Pod":
		resource = "pods"
	default:
		return false, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}
	labels := accessor.GetLabels()
	if _, ok := labels[constants.PreDeleteHookLabel]; !ok {
		return false, nil
	}

	return true, &workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Group:     gvk.Group,
			Resource:  resource,
			Name:      accessor.GetName(),
			Namespace: accessor.GetNamespace(),
		},
		FeedbackRules: []workapiv1.FeedbackRule{
			{
				Type: workapiv1.WellKnownStatusType,
			},
		},
	}
}
func newAddonWorksBuilder(hostedModeEnabled bool, workBuilder *workbuilder.WorkBuilder) *addonWorksBuilder {
	return &addonWorksBuilder{
		processor:         &managedManifest{},
		hostedModeEnabled: hostedModeEnabled,
		workBuilder:       workBuilder,
	}
}

func newHostingAddonWorksBuilder(hostedModeEnabled bool, workBuilder *workbuilder.WorkBuilder) *addonWorksBuilder {
	return &addonWorksBuilder{
		processor:         &hostingManifest{},
		hostedModeEnabled: hostedModeEnabled,
		workBuilder:       workBuilder,
	}
}

type addonWorksBuilder struct {
	processor         manifestProcessor
	hostedModeEnabled bool
	workBuilder       *workbuilder.WorkBuilder
}

type manifestProcessor interface {
	deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error)
	manifestWorkNamePrefix(addonNamespace, addonName string) string
	preDeleteHookManifestWorkName(addonNamespace, addonName string) string
}

// hostingManifest process manifests which will be deployed on the hosting cluster
type hostingManifest struct {
}

func (m *hostingManifest) deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error) {
	if !hostedModeEnabled {
		// hosted mode disabled, will not deploy any resource on the hosting cluster
		return false, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels())
	if err != nil {
		return false, err
	}
	if installMode != constants.InstallModeHosted {
		return false, nil
	}

	if exist && location == constants.HostedManifestLocationHostingLabelValue {
		klog.V(4).Infof("will deploy the manifest %s/%s on the hosting cluster in Hosted mode",
			accessor.GetNamespace(), accessor.GetName())
		return true, nil
	}

	return false, nil
}

func (m *hostingManifest) manifestWorkNamePrefix(addonNamespace, addonName string) string {
	return constants.DeployHostingWorkNamePrefix(addonNamespace, addonName)
}

func (m *hostingManifest) preDeleteHookManifestWorkName(addonNamespace, addonName string) string {
	return constants.PreDeleteHookHostingWorkName(addonNamespace, addonName)
}

// managedManifest process manifests which will be deployed on the managed cluster
type managedManifest struct {
}

func (m *managedManifest) deployable(hostedModeEnabled bool, installMode string, obj runtime.Object) (bool, error) {
	if !hostedModeEnabled {
		// hosted mode disabled, will deploy all resources on the managed cluster
		return true, nil
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, nil
	}

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels())
	if err != nil {
		return false, err
	}

	if installMode != constants.InstallModeHosted {
		return true, nil
	}

	if !exist || location == constants.HostedManifestLocationManagedLabelValue {
		klog.V(4).Infof("will deploy the manifest %s/%s on the managed cluster in Hosted mode",
			accessor.GetNamespace(), accessor.GetName())
		return true, nil
	}

	return false, nil
}

func (m *managedManifest) manifestWorkNamePrefix(addonNamespace, addonName string) string {
	return constants.DeployWorkNamePrefix(addonName)
}

func (m *managedManifest) preDeleteHookManifestWorkName(addonNamespace, addonName string) string {
	return constants.PreDeleteHookWorkName(addonName)
}

// BuildDeployWorks returns the deploy manifestWorks. if there is no manifest need
// to deploy, will return nil.
func (b *addonWorksBuilder) BuildDeployWorks(addonWorkNamespace string,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	existingWorks []workapiv1.ManifestWork,
	objects []runtime.Object,
	manifestOptions []workapiv1.ManifestConfigOption) (deployWorks, deleteWorks []*workapiv1.ManifestWork, err error) {
	var deployObjects []runtime.Object
	var owner *metav1.OwnerReference
	installMode, _ := constants.GetHostedModeInfo(addon.GetAnnotations())

	// only set addon as the owner of works in default mode. should not set owner in hosted mode.
	if installMode == constants.InstallModeDefault {
		owner = metav1.NewControllerRef(addon, addonapiv1alpha1.GroupVersion.WithKind("ManagedClusterAddOn"))
	}

	var deletionOrphaningRules []workapiv1.OrphaningRule
	for _, object := range objects {
		deployable, err := b.processor.deployable(b.hostedModeEnabled, installMode, object)
		if err != nil {
			return nil, nil, err
		}
		if !deployable {
			continue
		}

		isHookObject, _ := b.isPreDeleteHookObject(object)
		if isHookObject {
			continue
		}

		rule, err := getDeletionOrphaningRule(object)
		if err != nil {
			return nil, nil, err
		}
		if rule != nil {
			deletionOrphaningRules = append(deletionOrphaningRules, *rule)
		}

		deployObjects = append(deployObjects, object)
	}
	if len(deployObjects) == 0 {
		return nil, nil, nil
	}

	var deletionOption *workapiv1.DeleteOption
	if len(deletionOrphaningRules) != 0 {
		deletionOption = &workapiv1.DeleteOption{
			PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
			SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
				OrphaningRules: deletionOrphaningRules,
			},
		}
	}

	return b.workBuilder.Build(deployObjects,
		newAddonWorkObjectMeta(b.processor.manifestWorkNamePrefix(addon.Namespace, addon.Name), addon.Name, addon.Namespace, addonWorkNamespace, owner),
		workbuilder.ExistingManifestWorksOption(existingWorks),
		workbuilder.ManifestConfigOption(manifestOptions),
		workbuilder.DeletionOption(deletionOption))
}

// BuildHookWork returns the preDelete manifestWork, if there is no manifest need
// to deploy, will return nil.
func (b *addonWorksBuilder) BuildHookWork(addonWorkNamespace string,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	objects []runtime.Object) (hookWork *workapiv1.ManifestWork, err error) {
	var hookManifests []workapiv1.Manifest
	var hookManifestConfigs []workapiv1.ManifestConfigOption
	var owner *metav1.OwnerReference
	installMode, _ := constants.GetHostedModeInfo(addon.GetAnnotations())

	// only set addon as the owner of works in default mode. should not set owner in hosted mode.
	if installMode == constants.InstallModeDefault {
		owner = metav1.NewControllerRef(addon, addonapiv1alpha1.GroupVersion.WithKind("ManagedClusterAddOn"))
	}

	for _, object := range objects {
		deployable, err := b.processor.deployable(b.hostedModeEnabled, installMode, object)
		if err != nil {
			return nil, err
		}
		if !deployable {
			continue
		}

		isHookObject, manifestConfig := b.isPreDeleteHookObject(object)
		if !isHookObject {
			continue
		}
		rawObject, err := runtime.Encode(unstructured.UnstructuredJSONScheme, object)
		if err != nil {
			return nil, err
		}

		hookManifests = append(hookManifests, workapiv1.Manifest{RawExtension: runtime.RawExtension{Raw: rawObject}})
		hookManifestConfigs = append(hookManifestConfigs, *manifestConfig)
	}
	if len(hookManifests) == 0 {
		return nil, nil
	}

	hookWork = newManifestWork(addon.Namespace, addon.Name, addonWorkNamespace, hookManifests, b.processor.preDeleteHookManifestWorkName)
	if owner != nil {
		hookWork.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	hookWork.Spec.ManifestConfigs = hookManifestConfigs
	if addon.Namespace != addonWorkNamespace {
		hookWork.Labels[constants.AddonNamespaceLabel] = addon.Namespace
	}
	return hookWork, nil
}

func FindManifestValue(
	resourceStatus workapiv1.ManifestResourceStatus,
	identifier workapiv1.ResourceIdentifier,
	valueName string) workapiv1.FieldValue {
	for _, manifest := range resourceStatus.Manifests {
		values := manifest.StatusFeedbacks.Values
		if len(values) == 0 {
			return workapiv1.FieldValue{}
		}
		resourceMeta := manifest.ResourceMeta
		if identifier.Group == resourceMeta.Group &&
			identifier.Resource == resourceMeta.Resource &&
			identifier.Name == resourceMeta.Name &&
			identifier.Namespace == resourceMeta.Namespace {
			for _, v := range values {
				if v.Name == valueName {
					return v.Value
				}
			}
		}
	}
	return workapiv1.FieldValue{}
}

// hookWorkIsCompleted checks the hook resources are completed.
// hookManifestWork is completed if all resources are completed.
// currently, we only support job and pod as hook manifest.
// job is completed if the Completed condition of status is true.
// pod is completed if the phase of status is Succeeded.
func hookWorkIsCompleted(hookWork *workapiv1.ManifestWork) bool {
	if hookWork == nil {
		return false
	}
	if !meta.IsStatusConditionTrue(hookWork.Status.Conditions, workapiv1.WorkAvailable) {
		return false
	}

	if len(hookWork.Spec.ManifestConfigs) == 0 {
		klog.Errorf("the hook manifestWork should have manifest configs,but got 0.")
		return false
	}
	for _, manifestConfig := range hookWork.Spec.ManifestConfigs {
		switch manifestConfig.ResourceIdentifier.Resource {
		case "jobs":
			value := FindManifestValue(hookWork.Status.ResourceStatus, manifestConfig.ResourceIdentifier, "JobComplete")
			if value.Type == "" {
				return false
			}
			if value.String == nil {
				return false
			}
			if *value.String != "True" {
				return false
			}

		case "pods":
			value := FindManifestValue(hookWork.Status.ResourceStatus, manifestConfig.ResourceIdentifier, "PodPhase")
			if value.Type == "" {
				return false
			}
			if value.String == nil {
				return false
			}
			if *value.String != "Succeeded" {
				return false
			}
		default:
			return false
		}
	}

	return true
}

func newAddonWorkObjectMeta(namePrefix, addonName, addonNamespace, workNamespace string, owner *metav1.OwnerReference) workbuilder.GenerateManifestWorkObjectMeta {
	return func(index int) metav1.ObjectMeta {
		objectMeta := metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", namePrefix, index),
			Namespace: workNamespace,
			Labels: map[string]string{
				constants.AddonLabel: addonName,
			},
		}
		if owner != nil {
			objectMeta.OwnerReferences = []metav1.OwnerReference{*owner}
		}
		// if the addon namespace is not equal with the manifestwork namespace(cluster name), add the addon namespace label
		if addonNamespace != workNamespace {
			objectMeta.Labels[constants.AddonNamespaceLabel] = addonNamespace
		}
		return objectMeta
	}
}

func getManifestConfigOption(agentAddon agent.AgentAddon) []workapiv1.ManifestConfigOption {
	if agentAddon.GetAgentAddonOptions().HealthProber == nil {
		return nil
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.Type != agent.HealthProberTypeWork {
		return nil
	}

	if agentAddon.GetAgentAddonOptions().HealthProber.WorkProber == nil {
		return nil
	}

	manifestConfigs := []workapiv1.ManifestConfigOption{}
	probeRules := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.ProbeFields
	for _, rule := range probeRules {
		manifestConfigs = append(manifestConfigs, workapiv1.ManifestConfigOption{
			ResourceIdentifier: rule.ResourceIdentifier,
			FeedbackRules:      rule.ProbeRules,
		})
	}
	return manifestConfigs
}

func getDeletionOrphaningRule(obj runtime.Object) (*workapiv1.OrphaningRule, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	annotations := accessor.GetAnnotations()
	if _, ok := annotations[constants.AnnotationDeletionOrphan]; !ok {
		return nil, nil
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	plural, _ := meta.UnsafeGuessKindToResource(gvk)

	rule := &workapiv1.OrphaningRule{
		Group:     plural.Group,
		Resource:  plural.Resource,
		Name:      accessor.GetName(),
		Namespace: accessor.GetNamespace(),
	}
	return rule, nil
}
