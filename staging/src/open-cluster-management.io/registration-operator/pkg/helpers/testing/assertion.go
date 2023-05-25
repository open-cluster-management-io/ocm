package testing

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"

	opratorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

func AssertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}

func AssertGet(t *testing.T, actual clienttesting.Action, group, version, resource string) {
	t.Helper()
	if actual.GetVerb() != "get" {
		t.Error(spew.Sdump(actual))
	}
	if actual.GetResource() != (schema.GroupVersionResource{Group: group, Version: version, Resource: resource}) {
		t.Error(spew.Sdump(actual))
	}
}

func AssertDelete(t *testing.T, actual clienttesting.Action, resource, namespace, name string) {
	t.Helper()
	deleteAction, ok := actual.(clienttesting.DeleteAction)
	if !ok {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetResource().Resource != resource {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetNamespace() != namespace {
		t.Error(spew.Sdump(actual))
	}
	if deleteAction.GetName() != name {
		t.Error(spew.Sdump(actual))
	}
}

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

func AssertEqualNumber(t *testing.T, actual, expected int) {
	if actual != expected {
		t.Errorf("expected %d number of actions but got: %d", expected, actual)
	}
}

func AssertEqualNameNamespace(t *testing.T, actualName, actualNamespace, name, namespace string) {
	if actualName != name {
		t.Errorf("Name of the object does not match, expected %s, actual %s", name, actualName)
	}

	if actualNamespace != namespace {
		t.Errorf("Namespace of the object does not match, expected %s, actual %s", namespace, actualNamespace)
	}
}
