package testing

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	opratorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

func NamedCondition(name, reason string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: name, Status: status, Reason: reason}
}

func AssertOnlyConditions(t *testing.T, actual runtime.Object, expectedConditions ...metav1.Condition) {
	t.Helper()

	var actualConditions []metav1.Condition
	if klusterlet, ok := actual.(*opratorapiv1.Klusterlet); ok {
		actualConditions = klusterlet.Status.Conditions
	} else {
		clustermanager := actual.(*opratorapiv1.ClusterManager)
		actualConditions = clustermanager.Status.Conditions
	}
	if len(actualConditions) != len(expectedConditions) {
		t.Errorf("expected %v condition but got: %v", len(expectedConditions), spew.Sdump(actualConditions))
	}

	for _, expectedCondition := range expectedConditions {
		actual := meta.FindStatusCondition(actualConditions, expectedCondition.Type)
		if actual == nil {
			t.Errorf("missing %v in %v", spew.Sdump(expectedCondition), spew.Sdump(actual))
		}
		if actual.Status != expectedCondition.Status {
			t.Errorf("wrong result for %v in %v", spew.Sdump(expectedCondition), spew.Sdump(actual))
		}
		if actual.Reason != expectedCondition.Reason {
			t.Errorf("wrong result for %v in %v", spew.Sdump(expectedCondition), spew.Sdump(actual))
		}
	}
}

func NamedDeploymentGenerationStatus(name, namespace string, generation int64) opratorapiv1.GenerationStatus {
	gvr := appsv1.SchemeGroupVersion.WithResource("deployments")
	return opratorapiv1.GenerationStatus{
		Group:          gvr.Group,
		Version:        gvr.Version,
		Resource:       gvr.Resource,
		Name:           name,
		Namespace:      namespace,
		LastGeneration: generation,
	}
}

func AssertOnlyGenerationStatuses(t *testing.T, actual runtime.Object, expectedGenerations ...opratorapiv1.GenerationStatus) {
	t.Helper()

	var actualGenerations []opratorapiv1.GenerationStatus
	if klusterlet, ok := actual.(*opratorapiv1.Klusterlet); ok {
		actualGenerations = klusterlet.Status.Generations
	} else {
		clustermanager := actual.(*opratorapiv1.ClusterManager)
		actualGenerations = clustermanager.Status.Generations
	}

	if len(actualGenerations) != len(expectedGenerations) {
		t.Errorf("expected %v generations but got: %v", len(expectedGenerations), spew.Sdump(actualGenerations))
	}

	for _, expectedGeneration := range expectedGenerations {
		actualGeneration := helpers.FindGenerationStatus(actualGenerations, expectedGeneration)
		if actualGeneration == nil {
			t.Errorf("missing %v in %v", spew.Sdump(expectedGeneration), spew.Sdump(actualGeneration))
		}

		if expectedGeneration.LastGeneration != actualGeneration.LastGeneration {
			t.Errorf("wrong result for %v in %v", spew.Sdump(expectedGeneration), spew.Sdump(actualGeneration))
		}
	}

}
