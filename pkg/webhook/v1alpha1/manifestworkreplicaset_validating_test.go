package v1alpha1

import (
	"context"
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	helpertest "open-cluster-management.io/work/pkg/hub/test"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"testing"
)

var manifestWorkReplicaSetSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1alpha1",
	Resource: "manifestworkreplicasets",
}

func TestWebHookValidateRequest(t *testing.T) {
	setupFeatureGate(t)

	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  manifestWorkReplicaSetSchema,
			Operation: admissionv1.Connect,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	webHook := ManifestWorkReplicaSetWebhook{}
	placementRef := workv1alpha1.LocalPlacementReference{Name: "place"}
	mwrSet := &workv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "ns",
		},
		Spec: workv1alpha1.ManifestWorkReplicaSetSpec{
			PlacementRefs: []workv1alpha1.LocalPlacementReference{placementRef},
		},
	}

	err := webHook.validateRequest(mwrSet, nil, ctx)
	if err == nil {
		t.Fatal("Expecting error for empty ManifestWorkTemplate")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	mwrSet = helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	err = webHook.validateRequest(mwrSet, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebHookCreateRequest(t *testing.T) {
	setupFeatureGate(t)

	webHook := ManifestWorkReplicaSetWebhook{}
	mwrSet := &workv1alpha1.ManifestWorkReplicaSet{}
	err := webHook.ValidateCreate(context.Background(), mwrSet)
	if err == nil {
		t.Fatal("Expecting error for empty mwrSet")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}
	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  manifestWorkReplicaSetSchema,
			Operation: admissionv1.Create,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	mwrSet = helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	err = webHook.ValidateCreate(ctx, mwrSet)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebHookUpdateRequest(t *testing.T) {
	setupFeatureGate(t)

	webHook := ManifestWorkReplicaSetWebhook{}
	mwrSet := helpertest.CreateTestManifestWorkReplicaSet("mwrSet-test", "default", "place-test")
	err := webHook.ValidateUpdate(context.Background(), nil, mwrSet)
	if err == nil {
		t.Fatal("Expecting error for nil oldPlaceMW")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	err = webHook.ValidateUpdate(context.Background(), mwrSet, nil)
	if err == nil {
		t.Fatal("Expecting error for nil newPlaceMW")
	}
	if !apierrors.IsBadRequest(err) {
		t.Fatal("Expecting bad request error type")
	}

	request := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Resource:  manifestWorkReplicaSetSchema,
			Operation: admissionv1.Update,
		},
	}
	ctx := admission.NewContextWithRequest(context.Background(), request)
	err = webHook.ValidateUpdate(ctx, mwrSet, mwrSet)
	if err != nil {
		t.Fatal(err)
	}
}

func setupFeatureGate(t *testing.T) {
	defaultFG := utilfeature.DefaultMutableFeatureGate
	if err := defaultFG.Add(ocmfeature.DefaultHubWorkFeatureGates); err != nil {
		t.Fatal(err)
	}
	if err := defaultFG.Set(fmt.Sprintf("%s=true", ocmfeature.ManifestWorkReplicaSet)); err != nil {
		t.Fatal(err)
	}
}
