package addontesting

import (
	"context"
	"fmt"
	"testing"
	"time"

	certv1 "k8s.io/api/certificates/v1"
	certv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/events"
)

type FakeSyncContext struct {
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func NewFakeSyncContext(t *testing.T) *FakeSyncContext {
	return &FakeSyncContext{
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: NewTestingEventRecorder(t),
	}
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
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

func NewHostingUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
	u.SetAnnotations(map[string]string{
		addonapiv1alpha1.HostedManifestLocationAnnotationKey: addonapiv1alpha1.HostedManifestLocationHostingValue,
	})
	return u
}

func NewHookJob(name, namespace string) *unstructured.Unstructured {
	job := NewUnstructured("batch/v1", "Job", namespace, name)
	job.SetAnnotations(map[string]string{addonapiv1alpha1.AddonPreDeleteHookAnnotationKey: ""})
	return job
}

func NewHostedHookJob(name, namespace string) *unstructured.Unstructured {
	job := NewUnstructured("batch/v1", "Job", namespace, name)
	job.SetAnnotations(map[string]string{addonapiv1alpha1.AddonPreDeleteHookAnnotationKey: "",
		addonapiv1alpha1.HostedManifestLocationAnnotationKey: addonapiv1alpha1.HostedManifestLocationHostingValue})
	return job
}

func NewAddon(name, namespace string, owners ...metav1.OwnerReference) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: owners,
		},
	}
}

func NewHostedModeAddon(name, namespace string, hostingCluster string,
	owners ...metav1.OwnerReference) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: owners,
			Annotations:     map[string]string{addonapiv1alpha1.HostingClusterNameAnnotationKey: hostingCluster},
		},
	}
}

func NewHostedModeAddonWithFinalizer(name, namespace string, hostingCluster string,
	owners ...metav1.OwnerReference) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := NewHostedModeAddon(name, namespace, hostingCluster)
	addon.SetFinalizers([]string{constants.HostingManifestFinalizer})
	return addon
}

func SetAddonDeletionTimestamp(addon *addonapiv1alpha1.ManagedClusterAddOn,
	deletionTimestamp time.Time) *addonapiv1alpha1.ManagedClusterAddOn {
	addon.DeletionTimestamp = &metav1.Time{Time: deletionTimestamp}
	return addon
}

func SetAddonFinalizers(addon *addonapiv1alpha1.ManagedClusterAddOn, finalizers ...string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon.SetFinalizers(finalizers)
	return addon
}

func NewClusterManagementAddon(name, crd, cr string) *addonapiv1alpha1.ClusterManagementAddOn {
	return &addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
			AddOnConfiguration: addonapiv1alpha1.ConfigCoordinates{
				CRDName: crd,
				CRName:  cr,
			},
			InstallStrategy: addonapiv1alpha1.InstallStrategy{
				Type: addonapiv1alpha1.AddonInstallStrategyManual,
			},
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

func DeleteManagedCluster(c *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	c.DeletionTimestamp = &metav1.Time{
		Time: time.Now(),
	}
	return c
}

func SetManagedClusterAnnotation(c *clusterv1.ManagedCluster,
	annotations map[string]string) *clusterv1.ManagedCluster {
	c.Annotations = annotations
	return c
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

func NewV1beta1CSR(addon, cluster string) *certv1beta1.CertificateSigningRequest {
	return &certv1beta1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("addon-%s", addon),
			Labels: map[string]string{
				"open-cluster-management.io/cluster-name": cluster,
				"open-cluster-management.io/addon-name":   addon,
			},
		},
		Spec: certv1beta1.CertificateSigningRequestSpec{},
	}
}

func NewDeniedV1beta1CSR(addon, cluster string) *certv1beta1.CertificateSigningRequest {
	csr := NewV1beta1CSR(addon, cluster)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1beta1.CertificateSigningRequestCondition{
		Type:   certv1beta1.CertificateDenied,
		Status: corev1.ConditionTrue,
	})
	return csr
}

func NewApprovedV1beta1CSR(addon, cluster string) *certv1beta1.CertificateSigningRequest {
	csr := NewV1beta1CSR(addon, cluster)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1beta1.CertificateSigningRequestCondition{
		Type:   certv1beta1.CertificateApproved,
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

type TestingEventRecorder struct {
	t         *testing.T
	component string
}

func (r *TestingEventRecorder) WithContext(ctx context.Context) events.Recorder {
	return r
}

// NewTestingEventRecorder provides event recorder that will log all recorded events to the error log.
func NewTestingEventRecorder(t *testing.T) events.Recorder {
	return &TestingEventRecorder{t: t, component: "test"}
}

func (r *TestingEventRecorder) ComponentName() string {
	return r.component
}

func (r *TestingEventRecorder) ForComponent(c string) events.Recorder {
	return &TestingEventRecorder{t: r.t, component: c}
}

func (r *TestingEventRecorder) Shutdown() {}

func (r *TestingEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *TestingEventRecorder) Event(reason, message string) {
	r.t.Logf("Event: %v: %v", reason, message)
}

func (r *TestingEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *TestingEventRecorder) Warning(reason, message string) {
	r.t.Logf("Warning: %v: %v", reason, message)
}

func (r *TestingEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
