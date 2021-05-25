package addontesting

import (
	"fmt"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type FakeSyncContext struct {
	addonKey string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func NewFakeSyncContext(t *testing.T, addonKey string) *FakeSyncContext {
	return &FakeSyncContext{
		addonKey: addonKey,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.addonKey }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

func NewAddon(name, namespace string) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewManifestWork(name, namespace string, objects ...*unstructured.Unstructured) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{},
			},
		},
	}

	for _, object := range objects {
		objectStr, _ := object.MarshalJSON()
		manifest := workapiv1.Manifest{}
		manifest.Raw = objectStr
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifest)
	}

	return work
}

func NewManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func NewCSR(addon, cluster string) *certv1.CertificateSigningRequest {
	return &certv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("addon-%s", addon),
			Labels: map[string]string{
				"open-cluster-management.io/cluster-name": cluster,
				"open-cluster-management.io/addon-name":   addon,
			},
		},
		Spec: certv1.CertificateSigningRequestSpec{},
	}
}

func NewDeniedCSR(addon, cluster string) *certv1.CertificateSigningRequest {
	csr := NewCSR(addon, cluster)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1.CertificateSigningRequestCondition{
		Type:   certv1.CertificateDenied,
		Status: corev1.ConditionTrue,
	})
	return csr
}

func NewApprovedCSR(addon, cluster string) *certv1.CertificateSigningRequest {
	csr := NewCSR(addon, cluster)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1.CertificateSigningRequestCondition{
		Type:   certv1.CertificateApproved,
		Status: corev1.ConditionTrue,
	})
	return csr
}

// AssertActions asserts the actual actions have the expected action verb
func AssertActions(t *testing.T, actualActions []clienttesting.Action, expectedVerbs ...string) {
	if len(actualActions) != len(expectedVerbs) {
		t.Fatalf("expected %d call but got: %#v", len(expectedVerbs), actualActions)
	}
	for i, expected := range expectedVerbs {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

// AssertNoActions asserts no actions are happened
func AssertNoActions(t *testing.T, actualActions []clienttesting.Action) {
	AssertActions(t, actualActions)
}
