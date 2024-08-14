package framework

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CheckDeploymentReady(ctx context.Context, kubeClient kubernetes.Interface, namespace, name string) error {
	deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
		return fmt.Errorf("deployment %s is not ready, ready replicas: %d, replicas: %d", name, deployment.Status.ReadyReplicas, deployment.Status.Replicas)
	}

	return nil
}
