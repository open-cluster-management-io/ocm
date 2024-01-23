package v1

func (k *Klusterlet) GetResourceRequirement() *ResourceRequirement {
	return k.Spec.ResourceRequirement
}
