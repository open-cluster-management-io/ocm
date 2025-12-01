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

// ConvertTo converts this ManagedClusterAddOn (v1beta1) to the Hub version (v1alpha1)
func (src *ManagedClusterAddOn) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*internalv1alpha1.ManagedClusterAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ManagedClusterAddOn but got %T", dstRaw)
	}
	klog.V(4).Infof("Converting ManagedClusterAddOn %s/%s from v1beta1 to v1alpha1 (Hub)",
		src.Namespace, src.Name)

	dst.ObjectMeta = src.ObjectMeta

	// Restore installNamespace from annotation
	if installNs, ok := src.Annotations[addonconversion.InstallNamespaceAnnotation]; ok {
		dst.Spec.InstallNamespace = installNs
	}
	dst.Spec.Configs = addonconversion.ConvertAddOnConfigsFromV1Beta1(src.Spec.Configs)

	// Convert status
	dst.Status.Namespace = src.Status.Namespace
	dst.Status.Registrations = addonconversion.ConvertRegistrationsFromV1Beta1(src.Status.Registrations)
	dst.Status.ConfigReferences = addonconversion.ConvertConfigReferencesFromV1Beta1(src.Status.ConfigReferences)
	dst.Status.SupportedConfigs = addonconversion.ConvertSupportedConfigsFromV1Beta1(src.Status.SupportedConfigs)
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.RelatedObjects = addonconversion.ConvertRelatedObjectsFromV1Beta1(src.Status.RelatedObjects)

	// AddOnMeta has identical structure, use type conversion
	dst.Status.AddOnMeta = addonv1alpha1.AddOnMeta(src.Status.AddOnMeta)

	// HealthCheck.Mode needs type conversion (HealthCheckMode types differ)
	dst.Status.HealthCheck.Mode = addonv1alpha1.HealthCheckMode(src.Status.HealthCheck.Mode)

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version (v1beta1)
func (dst *ManagedClusterAddOn) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*internalv1alpha1.ManagedClusterAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ManagedClusterAddOn but got %T", srcRaw)
	}
	klog.V(4).Infof("Converting ManagedClusterAddOn %s/%s from v1alpha1 (Hub) to v1beta1",
		src.Namespace, src.Name)

	dst.ObjectMeta = src.ObjectMeta

	// Save installNamespace to annotation (removed in v1beta1)
	if src.Spec.InstallNamespace != "" {
		// Deep copy annotations to avoid mutating the source
		if src.Annotations != nil {
			dst.Annotations = make(map[string]string, len(src.Annotations)+1)
			for k, v := range src.Annotations {
				dst.Annotations[k] = v
			}
		} else {
			dst.Annotations = make(map[string]string)
		}
		dst.Annotations[addonconversion.InstallNamespaceAnnotation] = src.Spec.InstallNamespace
	}
	dst.Spec.Configs = addonconversion.ConvertAddOnConfigsToV1Beta1(src.Spec.Configs)

	// Convert status
	dst.Status.Namespace = src.Status.Namespace
	dst.Status.Registrations = addonconversion.ConvertRegistrationsToV1Beta1(src.Status.Registrations)
	dst.Status.ConfigReferences = addonconversion.ConvertConfigReferencesToV1Beta1(src.Status.ConfigReferences)
	dst.Status.SupportedConfigs = addonconversion.ConvertSupportedConfigsToV1Beta1(src.Status.SupportedConfigs)
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.RelatedObjects = addonconversion.ConvertRelatedObjectsToV1Beta1(src.Status.RelatedObjects)

	// AddOnMeta has identical structure, use type conversion
	dst.Status.AddOnMeta = addonv1beta1.AddOnMeta(src.Status.AddOnMeta)

	// HealthCheck.Mode needs type conversion (HealthCheckMode types differ)
	dst.Status.HealthCheck.Mode = addonv1beta1.HealthCheckMode(src.Status.HealthCheck.Mode)

	return nil
}
