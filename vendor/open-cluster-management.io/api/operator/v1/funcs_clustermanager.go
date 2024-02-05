package v1

func (cm *ClusterManager) GetResourceRequirement() *ResourceRequirement {
	return cm.Spec.ResourceRequirement
}
