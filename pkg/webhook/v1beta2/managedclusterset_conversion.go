package v1beta2

import (
	"k8s.io/klog/v2"
	"open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/api/cluster/v1beta2"
	internalv1beta1 "open-cluster-management.io/registration/pkg/webhook/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

/*
ConvertTo is expected to modify its argument to contain the converted object.
Most of the conversion is straightforward copying, except for converting our changed field.
*/
// ConvertTo converts this ManagedClusterSet to the Hub(v1beta1) version.
func (src *ManagedClusterSet) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*internalv1beta1.ManagedClusterSet)
	klog.V(4).Infof("Converting ManagedClusterset %v from v1beta2 to v1beta1", src.Name)

	dst.ObjectMeta = src.ObjectMeta
	if len(src.Spec.ClusterSelector.SelectorType) == 0 || src.Spec.ClusterSelector.SelectorType == v1beta2.ExclusiveClusterSetLabel {
		dst.Spec.ClusterSelector.SelectorType = v1beta1.SelectorType(v1beta1.LegacyClusterSetLabel)
	} else {
		dst.Spec.ClusterSelector.SelectorType = v1beta1.SelectorType(src.Spec.ClusterSelector.SelectorType)
		dst.Spec.ClusterSelector.LabelSelector = src.Spec.ClusterSelector.LabelSelector
	}
	dst.Status = v1beta1.ManagedClusterSetStatus(src.Status)
	return nil
}

/*
ConvertFrom is expected to modify its receiver to contain the converted object.
Most of the conversion is straightforward copying, except for converting our changed field.
*/

// ConvertFrom converts from the Hub version (v1beta1) to this version.
func (dst *ManagedClusterSet) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*internalv1beta1.ManagedClusterSet)
	klog.V(4).Infof("Converting ManagedClusterset %v from v1beta1 to v1beta2", src.Name)

	dst.ObjectMeta = src.ObjectMeta
	if len(src.Spec.ClusterSelector.SelectorType) == 0 || src.Spec.ClusterSelector.SelectorType == v1beta1.LegacyClusterSetLabel {
		dst.Spec.ClusterSelector.SelectorType = v1beta2.ExclusiveClusterSetLabel
	} else {
		dst.Spec.ClusterSelector.SelectorType = v1beta2.SelectorType(src.Spec.ClusterSelector.SelectorType)
		dst.Spec.ClusterSelector.LabelSelector = src.Spec.ClusterSelector.LabelSelector
	}
	dst.Status = v1beta2.ManagedClusterSetStatus(src.Status)
	return nil
}
