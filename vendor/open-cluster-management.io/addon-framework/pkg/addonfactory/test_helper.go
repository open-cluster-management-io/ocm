package addonfactory

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func NewFakeManagedCluster(name string, k8sVersion string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec:   clusterv1.ManagedClusterSpec{},
		Status: clusterv1.ManagedClusterStatus{Version: clusterv1.ManagedClusterVersion{Kubernetes: k8sVersion}},
	}
}

func NewFakeManagedClusterAddon(name, clusterName, installNamespace, values string) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
			Annotations: map[string]string{
				AnnotationValuesName: values,
			},
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{InstallNamespace: installNamespace},
	}
}
