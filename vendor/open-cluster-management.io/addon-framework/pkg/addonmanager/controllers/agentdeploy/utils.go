package agentdeploy

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workbuilder "open-cluster-management.io/sdk-go/pkg/apis/work/v1/builder"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
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
		if f == finalizer {
			continue
		}
		// remove deperecated finalizers also
		if f == addonapiv1alpha1.AddonDeprecatedHostingManifestFinalizer ||
			f == addonapiv1alpha1.AddonDeprecatedPreDeleteHookFinalizer ||
			f == addonapiv1alpha1.AddonDeprecatedHostingPreDeleteHookFinalizer {
			continue
		}
		rst = append(rst, f)
	}
	if len(rst) != len(addon.Finalizers) {
		addon.SetFinalizers(rst)
		return true
	}
	return false
}

func addonAddFinalizer(addon *addonapiv1alpha1.ManagedClusterAddOn, finalizer string) bool {
	if addon.Finalizers == nil {
		addon.SetFinalizers([]string{finalizer})
		return true
	}

	var rst []string
	for _, f := range addon.Finalizers {
		// remove deperecated finalizers also
		if f == addonapiv1alpha1.AddonDeprecatedHostingManifestFinalizer ||
			f == addonapiv1alpha1.AddonDeprecatedPreDeleteHookFinalizer ||
			f == addonapiv1alpha1.AddonDeprecatedHostingPreDeleteHookFinalizer {
			continue
		}
		rst = append(rst, f)
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
				addonapiv1alpha1.AddonLabelKey: addonName,
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
		work.Labels[addonapiv1alpha1.AddonNamespaceLabelKey] = addonNamespace
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
	annotations := accessor.GetAnnotations()

	// TODO: deprecate PreDeleteHookLabel in the future release.
	_, hasPreDeleteLabel := labels[addonapiv1alpha1.AddonPreDeleteHookLabelKey]
	_, hasPreDeleteAnnotation := annotations[addonapiv1alpha1.AddonPreDeleteHookAnnotationKey]
	if !hasPreDeleteLabel && !hasPreDeleteAnnotation {
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

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels(), accessor.GetAnnotations())
	if err != nil {
		return false, err
	}
	if installMode != constants.InstallModeHosted {
		return false, nil
	}

	if exist && location == addonapiv1alpha1.HostedManifestLocationHostingValue {
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

	location, exist, err := constants.GetHostedManifestLocation(accessor.GetLabels(), accessor.GetAnnotations())
	if err != nil {
		return false, err
	}

	if installMode != constants.InstallModeHosted {
		return true, nil
	}

	if !exist || location == addonapiv1alpha1.HostedManifestLocationManagedValue {
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
func (b *addonWorksBuilder) BuildDeployWorks(installMode, addonWorkNamespace string,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	existingWorks []workapiv1.ManifestWork,
	objects []runtime.Object,
	manifestOptions []workapiv1.ManifestConfigOption) (deployWorks, deleteWorks []*workapiv1.ManifestWork, err error) {
	var deployObjects []runtime.Object
	// This owner is only added to the manifestWork deployed in managed cluster ns.
	// the manifestWork in managed cluster ns is cleaned up via the addon ownerRef, so need to add the owner.
	// the manifestWork in hosting cluster ns is cleaned up by its controller since it and its addon cross ns.
	owner := metav1.NewControllerRef(addon, schema.GroupVersionKind{
		Group:   addonapiv1alpha1.GroupName,
		Version: addonapiv1alpha1.GroupVersion.Version,
		Kind:    "ManagedClusterAddOn",
	})

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

	annotations, err := configsToAnnotations(addon.Status.ConfigReferences)
	if err != nil {
		return nil, nil, err
	}

	return b.workBuilder.Build(deployObjects,
		newAddonWorkObjectMeta(b.processor.manifestWorkNamePrefix(addon.Namespace, addon.Name), addon.Name, addon.Namespace, addonWorkNamespace, owner),
		workbuilder.ExistingManifestWorksOption(existingWorks),
		workbuilder.ManifestConfigOption(manifestOptions),
		workbuilder.ManifestAnnotations(annotations),
		workbuilder.DeletionOption(deletionOption))
}

// BuildHookWork returns the preDelete manifestWork, if there is no manifest need
// to deploy, will return nil.
func (b *addonWorksBuilder) BuildHookWork(installMode, addonWorkNamespace string,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	objects []runtime.Object) (hookWork *workapiv1.ManifestWork, err error) {
	var hookManifests []workapiv1.Manifest
	var hookManifestConfigs []workapiv1.ManifestConfigOption

	owner := metav1.NewControllerRef(addon, schema.GroupVersionKind{
		Group:   addonapiv1alpha1.GroupName,
		Version: addonapiv1alpha1.GroupVersion.Version,
		Kind:    "ManagedClusterAddOn",
	})

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

	// This owner is only added to the manifestWork deployed in managed cluster ns.
	if addon.Namespace == addonWorkNamespace {
		hookWork.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	hookWork.Spec.ManifestConfigs = hookManifestConfigs
	if addon.Namespace != addonWorkNamespace {
		hookWork.Labels[addonapiv1alpha1.AddonNamespaceLabelKey] = addon.Namespace
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

func newAddonWorkObjectMeta(namePrefix, addonName, addonNamespace, workNamespace string,
	owner *metav1.OwnerReference) workbuilder.GenerateManifestWorkObjectMeta {
	return func(index int) metav1.ObjectMeta {
		objectMeta := metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", namePrefix, index),
			Namespace: workNamespace,
			Labels: map[string]string{
				addonapiv1alpha1.AddonLabelKey: addonName,
			},
		}
		// This owner is only added to the manifestWork deployed in managed cluster ns.
		// the manifestWork in managed cluster ns is cleaned up via the addon ownerRef, so need to add the owner.
		// the manifestWork in hosting cluster ns is cleaned up by its controller since it and its addon cross ns.
		if addonNamespace == workNamespace && owner != nil {
			objectMeta.OwnerReferences = []metav1.OwnerReference{*owner}
		}
		// if the addon namespace is not equal with the manifestwork namespace(cluster name), add the addon namespace label
		if addonNamespace != workNamespace {
			objectMeta.Labels[addonapiv1alpha1.AddonNamespaceLabelKey] = addonNamespace
		}
		return objectMeta
	}
}

func getManifestConfigOption(agentAddon agent.AgentAddon,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]workapiv1.ManifestConfigOption, error) {
	manifestConfigs := []workapiv1.ManifestConfigOption{}

	if agentAddon.GetAgentAddonOptions().HealthProber != nil &&
		agentAddon.GetAgentAddonOptions().HealthProber.Type == agent.HealthProberTypeWork &&
		agentAddon.GetAgentAddonOptions().HealthProber.WorkProber != nil {
		probeRules := agentAddon.GetAgentAddonOptions().HealthProber.WorkProber.ProbeFields
		for _, rule := range probeRules {
			manifestConfigs = append(manifestConfigs, workapiv1.ManifestConfigOption{
				ResourceIdentifier: rule.ResourceIdentifier,
				FeedbackRules:      rule.ProbeRules,
			})
		}
	}

	if agentAddon.GetAgentAddonOptions().HealthProber != nil &&
		agentAddon.GetAgentAddonOptions().HealthProber.Type == agent.HealthProberTypeDeploymentAvailability {

		manifests, err := agentAddon.Manifests(cluster, addon)
		if err != nil {
			return manifestConfigs, fmt.Errorf("get all deployments error: %v", err)
		}

		deployments := utils.FilterDeployments(manifests)
		for _, deployment := range deployments {
			manifestConfig := utils.DeploymentWellKnowManifestConfig(deployment.Namespace, deployment.Name)
			manifestConfigs = append(manifestConfigs, manifestConfig)
		}
	}

	if agentAddon.GetAgentAddonOptions().HealthProber != nil &&
		agentAddon.GetAgentAddonOptions().HealthProber.Type == agent.HealthProberTypeWorkloadAvailability {

		manifests, err := agentAddon.Manifests(cluster, addon)
		if err != nil {
			return manifestConfigs, fmt.Errorf("get all workloads error: %v", err)
		}
		workloads := utils.FilterWorkloads(manifests)
		for _, workload := range workloads {
			manifestConfig := utils.WellKnowManifestConfig(workload.Group, workload.Resource,
				workload.Namespace, workload.Name)
			manifestConfigs = append(manifestConfigs, manifestConfig)
		}
	}

	if updaters := agentAddon.GetAgentAddonOptions().Updaters; updaters != nil {
		for _, updater := range updaters {
			strategy := updater.UpdateStrategy
			manifestConfigs = append(manifestConfigs, workapiv1.ManifestConfigOption{
				ResourceIdentifier: updater.ResourceIdentifier,
				UpdateStrategy:     &strategy,
			})
		}
	}

	userConfiguredManifestConfigs := agentAddon.GetAgentAddonOptions().ManifestConfigs
	for _, mc := range userConfiguredManifestConfigs {
		index := containsResourceIdentifier(manifestConfigs, mc.ResourceIdentifier)
		if index == -1 {
			manifestConfigs = append(manifestConfigs, mc)
			continue
		}

		// merge the feedback rules generated by the addon manager and the feedback rules configured by users
		for _, rule := range mc.FeedbackRules {
			manifestConfigs[index].FeedbackRules = mergeFeedbackRule(manifestConfigs[index].FeedbackRules, rule)
		}

		if mc.UpdateStrategy != nil {
			manifestConfigs[index].UpdateStrategy = mc.UpdateStrategy
		}
	}
	return manifestConfigs, nil
}

func containsResourceIdentifier(mcs []workapiv1.ManifestConfigOption, ri workapiv1.ResourceIdentifier) int {
	for index, mc := range mcs {
		if mc.ResourceIdentifier == ri {
			return index
		}
	}
	return -1
}

// mergeFeedbackRule merges the new rule into the existing rules.
// If the new rule is json path type, will ignore the json path which
// is already in the existing rules, compare by the path name.
func mergeFeedbackRule(rules []workapiv1.FeedbackRule, rule workapiv1.FeedbackRule) []workapiv1.FeedbackRule {
	rrules := rules
	var existJsonPaths []workapiv1.JsonPath
	var existWellKnownStatus bool = false
	for _, rule := range rules {
		if rule.Type == workapiv1.WellKnownStatusType {
			existWellKnownStatus = true
			continue
		}
		if rule.Type == workapiv1.JSONPathsType {
			existJsonPaths = append(existJsonPaths, rule.JsonPaths...)
		}
	}

	if rule.Type == workapiv1.WellKnownStatusType {
		if !existWellKnownStatus {
			rrules = append(rrules, rule)
		}
		return rrules
	}

	if rule.Type == workapiv1.JSONPathsType {
		var jsonPaths []workapiv1.JsonPath
		for _, path := range rule.JsonPaths {
			found := false
			for _, rpath := range existJsonPaths {
				if path.Name == rpath.Name {
					found = true
					break
				}
			}
			if !found {
				jsonPaths = append(jsonPaths, path)
			}
		}
		if len(jsonPaths) != 0 {
			rrules = append(rrules, workapiv1.FeedbackRule{
				Type:      workapiv1.JSONPathsType,
				JsonPaths: jsonPaths,
			})
		}
		return rrules
	}

	rrules = append(rrules, rule)
	return rrules
}

func getDeletionOrphaningRule(obj runtime.Object) (*workapiv1.OrphaningRule, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	annotations := accessor.GetAnnotations()
	if _, ok := annotations[addonapiv1alpha1.DeletionOrphanAnnotationKey]; !ok {
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

// convert config reference to annotations.
func configsToAnnotations(configReference []addonapiv1alpha1.ConfigReference) (map[string]string, error) {
	if len(configReference) == 0 {
		return nil, nil
	}

	// converts the configReference into a map, key is config name, value is spec hash.
	specHashMap := ConfigsToMap(configReference)

	// converts the map into a JSON byte string.
	jsonBytes, err := json.Marshal(specHashMap)
	if err != nil {
		return nil, err
	}

	// return a map with key as "open-cluster-management.io/config-spec-hash" and value is the JSON byte string.
	// For example:
	// open-cluster-management.io/config-spec-hash: '{"addonhubconfigs.addon.open-cluster-management.io//default":"613d134a2ec072a8a6451af913979f496d657ef5",
	// "addondeploymentconfigs.addon.open-cluster-management.io/open-cluster-management/default":"cca7df9188fb920dcfab374940452393e2037619"}'
	return map[string]string{
		workapiv1.ManifestConfigSpecHashAnnotationKey: string(jsonBytes),
	}, nil
}

// configsToMap returns a map stores the config name as the key and config spec hash as the value.
func ConfigsToMap(configReference []addonapiv1alpha1.ConfigReference) map[string]string {
	// config name follows the format of <resource>.<group>/<namespace>/<name>, for example,
	// addondeploymentconfigs.addon.open-cluster-management.io/open-cluster-management/default.
	// for a cluster scoped resource, the namespace would be empty, for example,
	// addonhubconfigs.addon.open-cluster-management.io//default.
	specHashMap := make(map[string]string, len(configReference))
	for _, v := range configReference {
		if v.DesiredConfig == nil {
			continue
		}
		resourceStr := v.Resource
		if len(v.Group) > 0 {
			resourceStr += fmt.Sprintf(".%s", v.Group)
		}
		resourceStr += fmt.Sprintf("/%s/%s", v.DesiredConfig.Namespace, v.DesiredConfig.Name)

		specHashMap[resourceStr] = v.DesiredConfig.SpecHash
	}

	return specHashMap
}
