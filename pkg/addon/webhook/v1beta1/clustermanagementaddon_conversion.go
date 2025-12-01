// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	"fmt"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	addonconversion "open-cluster-management.io/ocm/pkg/addon/webhook/conversion"
	internalv1alpha1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1alpha1"
)

// ConvertTo converts this ClusterManagementAddOn (v1beta1) to the Hub version (v1alpha1)
func (src *ClusterManagementAddOn) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*internalv1alpha1.ClusterManagementAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ClusterManagementAddOn but got %T", dstRaw)
	}
	klog.V(4).Infof("Converting ClusterManagementAddOn %s from v1beta1 to v1alpha1 (Hub)", src.Name)

	dst.ObjectMeta = src.ObjectMeta

	// AddOnMeta has identical structure, use type conversion
	dst.Spec.AddOnMeta = addonv1alpha1.AddOnMeta(src.Spec.AddOnMeta)

	// Convert defaultConfigs to supportedConfigs
	dst.Spec.SupportedConfigs = addonconversion.ConvertAddOnConfigToConfigMeta(src.Spec.DefaultConfigs)

	// Convert install strategy
	dst.Spec.InstallStrategy = addonconversion.ConvertInstallStrategyFromV1Beta1(src.Spec.InstallStrategy)

	// Convert status
	dst.Status.DefaultConfigReferences = addonconversion.ConvertDefaultConfigReferencesFromV1Beta1(src.Status.DefaultConfigReferences)
	dst.Status.InstallProgressions = addonconversion.ConvertInstallProgressionsFromV1Beta1(src.Status.InstallProgressions)

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version (v1beta1)
func (dst *ClusterManagementAddOn) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*internalv1alpha1.ClusterManagementAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ClusterManagementAddOn but got %T", srcRaw)
	}
	klog.V(4).Infof("Converting ClusterManagementAddOn %s from v1alpha1 (Hub) to v1beta1", src.Name)

	dst.ObjectMeta = src.ObjectMeta

	// AddOnMeta has identical structure, use type conversion
	dst.Spec.AddOnMeta = addonv1beta1.AddOnMeta(src.Spec.AddOnMeta)

	// Convert supportedConfigs to defaultConfigs
	dst.Spec.DefaultConfigs = addonconversion.ConvertConfigMetaToAddOnConfig(src.Spec.SupportedConfigs)

	// Convert install strategy
	dst.Spec.InstallStrategy = addonconversion.ConvertInstallStrategyToV1Beta1(src.Spec.InstallStrategy)

	// Convert status
	dst.Status.DefaultConfigReferences = addonconversion.ConvertDefaultConfigReferencesToV1Beta1(src.Status.DefaultConfigReferences)
	dst.Status.InstallProgressions = addonconversion.ConvertInstallProgressionsToV1Beta1(src.Status.InstallProgressions)

	return nil
}
