// Copyright Contributors to the Open Cluster Management project
package v1

func (cm *ClusterManager) GetResourceRequirement() *ResourceRequirement {
	return cm.Spec.ResourceRequirement
}
