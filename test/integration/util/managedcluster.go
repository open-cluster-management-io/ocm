package util

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func GetManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) (*clusterv1.ManagedCluster, error) {
	spokeCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), spokeClusterName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return spokeCluster, nil
}

func AcceptManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) error {
	return AcceptManagedClusterWithLeaseDuration(clusterClient, spokeClusterName, 60)
}

func AcceptManagedClusterWithLeaseDuration(clusterClient clusterclientset.Interface, spokeClusterName string, leaseDuration int32) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		spokeCluster, err := GetManagedCluster(clusterClient, spokeClusterName)
		if err != nil {
			return err
		}
		spokeCluster.Spec.HubAcceptsClient = true
		spokeCluster.Spec.LeaseDurationSeconds = leaseDuration
		_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), spokeCluster, metav1.UpdateOptions{})
		return err
	})
}

func CreateNode(kubeClient kubernetes.Interface, name string, capacity, allocatable corev1.ResourceList) error {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}
	_, err := kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	return err
}

func CordonNode(kubeClient kubernetes.Interface, name string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	node = node.DeepCopy()
	node.Spec.Unschedulable = true
	_, err = kubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

func NewResourceList(cpu, mem int) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalExponent),
		corev1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*mem), resource.BinarySI),
	}
}

func CmpResourceQuantity(key string, nodeResourceList corev1.ResourceList, clusterResorceList clusterv1.ResourceList) bool {
	nodeResource, ok := nodeResourceList[corev1.ResourceName(key)]
	if !ok {
		return false
	}
	clusterResouce, ok := clusterResorceList[clusterv1.ResourceName(key)]
	if !ok {
		return false
	}
	return nodeResource.Equal(clusterResouce)
}
