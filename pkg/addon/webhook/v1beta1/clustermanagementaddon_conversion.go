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

// ConvertTo converts this ClusterManagementAddOn (v1beta1) to the Hub version (v1alpha1)
func (src *ClusterManagementAddOn) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*internalv1alpha1.ClusterManagementAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ClusterManagementAddOn but got %T", dstRaw)
	}
	klog.V(4).Infof("Converting ClusterManagementAddOn %s from v1beta1 to v1alpha1 (Hub)", src.Name)

	// Convert the embedded v1beta1 type to v1alpha1 using the native conversion
	var v1alpha1Obj addonv1alpha1.ClusterManagementAddOn
	if err := addonv1beta1.Convert_v1beta1_ClusterManagementAddOn_To_v1alpha1_ClusterManagementAddOn(
		&src.ClusterManagementAddOn, &v1alpha1Obj, nil); err != nil {
		return fmt.Errorf("failed to convert ClusterManagementAddOn: %w", err)
	}

	// Set TypeMeta for the target version - the native conversion doesn't copy these fields
	// We must set the hub version (v1alpha1) here, not preserve the source version
	v1alpha1Obj.TypeMeta = metav1.TypeMeta{
		Kind:       "ClusterManagementAddOn",
		APIVersion: addonv1alpha1.GroupVersion.String(),
	}

	// Copy to the internal wrapper type
	dst.ClusterManagementAddOn = v1alpha1Obj

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version (v1beta1)
func (dst *ClusterManagementAddOn) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*internalv1alpha1.ClusterManagementAddOn)
	if !ok {
		return fmt.Errorf("expected *internalv1alpha1.ClusterManagementAddOn but got %T", srcRaw)
	}
	klog.V(4).Infof("Converting ClusterManagementAddOn %s from v1alpha1 (Hub) to v1beta1", src.Name)

	// Convert the embedded v1alpha1 type to v1beta1 using the native conversion
	var v1beta1Obj addonv1beta1.ClusterManagementAddOn
	if err := addonv1beta1.Convert_v1alpha1_ClusterManagementAddOn_To_v1beta1_ClusterManagementAddOn(
		&src.ClusterManagementAddOn, &v1beta1Obj, nil); err != nil {
		return fmt.Errorf("failed to convert ClusterManagementAddOn: %w", err)
	}

	// Set TypeMeta for the target version - the native conversion doesn't copy these fields
	// We must set the target version (v1beta1) here, not preserve the source version
	v1beta1Obj.TypeMeta = metav1.TypeMeta{
		Kind:       "ClusterManagementAddOn",
		APIVersion: addonv1beta1.GroupVersion.String(),
	}

	// Copy to the internal wrapper type
	dst.ClusterManagementAddOn = v1beta1Obj

	return nil
}
