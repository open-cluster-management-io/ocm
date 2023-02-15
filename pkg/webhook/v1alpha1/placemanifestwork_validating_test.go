package v1alpha1

import (
	"context"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"testing"
)

var placeManifestWorkSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "placemanifestworks",
}

func TestWebHookValidateRequest(t *testing.T) {
	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  placeManifestWorkSchema,
			Operation: admissionv1.Connect,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	webHook := PlaceManifestWorkWebhook{}
	pmw := &workv1alpha1.PlaceManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "ns",
		},
		Spec: workv1alpha1.PlaceManifestWorkSpec{
			PlacementRef: workv1alpha1.LocalPlacementReference{
				Name: "place",
			},
		},
	}

	err := webHook.validateRequest(pmw, nil, ctx)
	if err == nil {
		t.Fatal("Expecting error for empty ManifestWorkTemplate")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	pmw = helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	err = webHook.validateRequest(pmw, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebHookCreateRequest(t *testing.T) {
	webHook := PlaceManifestWorkWebhook{}
	pmw := &workv1alpha1.PlaceManifestWork{}
	err := webHook.ValidateCreate(context.Background(), pmw)
	if err == nil {
		t.Fatal("Expecting error for empty pmw")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}
	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  placeManifestWorkSchema,
			Operation: admissionv1.Create,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	pmw = helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	err = webHook.ValidateCreate(ctx, pmw)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebHookUpdateRequest(t *testing.T) {
	webHook := PlaceManifestWorkWebhook{}
	pmw := helpertest.CreateTestPlaceManifestWork("pmw-test", "default", "place-test")
	err := webHook.ValidateUpdate(context.Background(), nil, pmw)
	if err == nil {
		t.Fatal("Expecting error for nil oldPlaceMW")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	err = webHook.ValidateUpdate(context.Background(), pmw, nil)
	if err == nil {
		t.Fatal("Expecting error for nil newPlaceMW")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  placeManifestWorkSchema,
			Operation: admissionv1.Update,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	err = webHook.ValidateUpdate(ctx, pmw, pmw)
	if err != nil {
		t.Fatal(err)
	}
}
