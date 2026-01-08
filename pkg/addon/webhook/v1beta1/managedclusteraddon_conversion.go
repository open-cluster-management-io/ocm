// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"

	internalv1alpha1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1alpha1"
)

const (
	// InstallNamespaceAnnotation is the annotation key for storing installNamespace
	// This is used because installNamespace field was removed in v1beta1
	InstallNamespaceAnnotation = "addon.open-cluster-management.io/v1alpha1-install-namespace"
)

// ConvertTo converts this ManagedClusterAddOn (v1beta1) to the Hub version (v1alpha1)
func (src *ManagedClusterAddOn) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*internalv1alpha1.ManagedClusterAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ManagedClusterAddOn but got %T", dstRaw)
	}
	klog.V(4).Infof("Converting ManagedClusterAddOn %s/%s from v1beta1 to v1alpha1 (Hub)",
		src.Namespace, src.Name)

	// Convert the embedded v1beta1 type to v1alpha1 using the native conversion
	var v1alpha1Obj addonv1alpha1.ManagedClusterAddOn
	if err := addonv1beta1.Convert_v1beta1_ManagedClusterAddOn_To_v1alpha1_ManagedClusterAddOn(
		&src.ManagedClusterAddOn, &v1alpha1Obj, nil); err != nil {
		return fmt.Errorf("failed to convert ManagedClusterAddOn: %w", err)
	}

	// Set TypeMeta for the target version - the native conversion doesn't copy these fields
	// We must set the hub version (v1alpha1) here, not preserve the source version
	v1alpha1Obj.TypeMeta = metav1.TypeMeta{
		Kind:       "ManagedClusterAddOn",
		APIVersion: addonv1alpha1.GroupVersion.String(),
	}

	// Restore installNamespace from annotation
	// This field was removed in v1beta1, so we store it in annotation
	if installNs, ok := src.Annotations[InstallNamespaceAnnotation]; ok {
		v1alpha1Obj.Spec.InstallNamespace = installNs
		// Remove the internal annotation from v1alpha1 object
		// This annotation is only used for v1beta1 storage, not for v1alpha1 API
		delete(v1alpha1Obj.Annotations, InstallNamespaceAnnotation)
	}

	// Manually populate deprecated ConfigReferent field in ConfigReferences
	// The native conversion doesn't handle this deprecated field from v1alpha1.ConfigReference
	// We need to copy from DesiredConfig.ConfigReferent if DesiredConfig exists
	for i := range v1alpha1Obj.Status.ConfigReferences {
		if v1alpha1Obj.Status.ConfigReferences[i].DesiredConfig != nil {
			v1alpha1Obj.Status.ConfigReferences[i].ConfigReferent = v1alpha1Obj.Status.ConfigReferences[i].DesiredConfig.ConfigReferent
		}
	}

	// Copy to the internal wrapper type
	dst.ManagedClusterAddOn = v1alpha1Obj

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

	// Convert the embedded v1alpha1 type to v1beta1 using the native conversion
	var v1beta1Obj addonv1beta1.ManagedClusterAddOn
	if err := addonv1beta1.Convert_v1alpha1_ManagedClusterAddOn_To_v1beta1_ManagedClusterAddOn(
		&src.ManagedClusterAddOn, &v1beta1Obj, nil); err != nil {
		return fmt.Errorf("failed to convert ManagedClusterAddOn: %w", err)
	}

	// Set TypeMeta for the target version - the native conversion doesn't copy these fields
	// We must set the target version (v1beta1) here, not preserve the source version
	v1beta1Obj.TypeMeta = metav1.TypeMeta{
		Kind:       "ManagedClusterAddOn",
		APIVersion: addonv1beta1.GroupVersion.String(),
	}

	// Copy to the wrapper type
	dst.ManagedClusterAddOn = v1beta1Obj

	// Save installNamespace to annotation (removed in v1beta1)
	// This field exists in v1alpha1 but not in v1beta1, so we preserve it in annotations
	if src.Spec.InstallNamespace != "" {
		if dst.ManagedClusterAddOn.Annotations == nil {
			dst.ManagedClusterAddOn.Annotations = make(map[string]string)
		}
		dst.ManagedClusterAddOn.Annotations[InstallNamespaceAnnotation] = src.Spec.InstallNamespace
	}

	return nil
}
