package templateagent

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func TestToAddOnReplicaPrivateValues(t *testing.T) {
	config := addonapiv1beta1.AddOnDeploymentConfig{
		Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
			ReplicaConfigs: []addonapiv1beta1.ReplicaConfig{
				{WorkloadID: "deployments:cert-manager-webhook", Replicas: 3},
				{WorkloadID: "deployments:cert-manager", Replicas: 3},
			},
		},
	}

	values, err := ToAddOnReplicaPrivateValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	configs, ok := values[ReplicaPrivateValueKey].([]ParsedReplicaConfig)
	if !ok {
		t.Fatalf("expected []ParsedReplicaConfig in private values, got %T", values[ReplicaPrivateValueKey])
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 replica configs, got %d", len(configs))
	}
}

func TestToAddOnReplicaPrivateValues_Empty(t *testing.T) {
	config := addonapiv1beta1.AddOnDeploymentConfig{
		Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{},
	}
	values, err := ToAddOnReplicaPrivateValues(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values != nil {
		t.Fatalf("expected nil values when no replicaConfigs, got %v", values)
	}
}

func TestToAddOnReplicaPrivateValues_InvalidWorkloadID(t *testing.T) {
	config := addonapiv1beta1.AddOnDeploymentConfig{
		Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
			ReplicaConfigs: []addonapiv1beta1.ReplicaConfig{
				{WorkloadID: "daemonsets:foo", Replicas: 1},
			},
		},
	}
	_, err := ToAddOnReplicaPrivateValues(config)
	if err == nil {
		t.Fatalf("expected error for unsupported workloadID resource type")
	}
}

func TestToAddOnReplicaPrivateValues_NegativeReplicas(t *testing.T) {
	config := addonapiv1beta1.AddOnDeploymentConfig{
		Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
			ReplicaConfigs: []addonapiv1beta1.ReplicaConfig{
				{WorkloadID: "deployments:cert-manager", Replicas: -1},
			},
		},
	}
	_, err := ToAddOnReplicaPrivateValues(config)
	if err == nil {
		t.Fatalf("expected error for negative replicas")
	}
}

func TestDeploymentDecoratorAppliesReplicaOverride(t *testing.T) {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert-manager-webhook",
			Namespace: "cert-manager",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decorator := newDeploymentDecorator(
		klog.Background(),
		"cert-manager-addon",
		&addonapiv1alpha1.AddOnTemplate{},
		nil,
		map[string]interface{}{
			ReplicaPrivateValueKey: []ParsedReplicaConfig{
				{WorkloadIDRegex: `^deployments:cert-manager-webhook$`, Replicas: 3},
			},
		},
	)

	result, err := decorator.decorate(&unstructured.Unstructured{Object: obj})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Spec.Replicas == nil || *updated.Spec.Replicas != 3 {
		t.Fatalf("expected replicas 3, got %#v", updated.Spec.Replicas)
	}
}

func TestDeploymentDecoratorWildcardWorkloadID(t *testing.T) {
	deployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "cert-manager-cainjector", Namespace: "cert-manager"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{}},
		},
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decorator := newDeploymentDecorator(
		klog.Background(),
		"cert-manager-addon",
		&addonapiv1alpha1.AddOnTemplate{},
		nil,
		map[string]interface{}{
			ReplicaPrivateValueKey: []ParsedReplicaConfig{
				// wildcard: all deployments get 2 replicas
				{WorkloadIDRegex: `^deployments:.*$`, Replicas: 2},
			},
		},
	)

	result, err := decorator.decorate(&unstructured.Unstructured{Object: obj})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Spec.Replicas == nil || *updated.Spec.Replicas != 2 {
		t.Fatalf("expected replicas 2 from wildcard, got %#v", updated.Spec.Replicas)
	}
}
