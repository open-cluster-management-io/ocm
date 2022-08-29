package spoketesting

import (
	"fmt"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type FakeSyncContext struct {
	workKey  string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func NewFakeSyncContext(t *testing.T, workKey string) *FakeSyncContext {
	return &FakeSyncContext{
		workKey:  workKey,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.workKey }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewSecret(name, namespace string, content string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"test": []byte(content),
		},
	}
}

func NewSecretWithType(name, namespace string, content string, t corev1.SecretType) *corev1.Secret {
	secret := NewSecret(name, namespace, content)
	secret.Type = t
	return secret
}

func NewUnstructuredSecretBySize(namespace, name string, size int32) *unstructured.Unstructured {
	data := ""
	for i := int32(0); i < size; i++ {
		data += "a"
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
			"data": data,
		},
	}
}

func NewUnstructuredSecret(namespace, name string, terminated bool, uid string, owners ...metav1.OwnerReference) *unstructured.Unstructured {
	u := NewUnstructured("v1", "Secret", namespace, name, owners...)
	if terminated {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
	}
	if uid != "" {
		u.SetUID(types.UID(uid))
	}
	return u
}

func NewUnstructured(apiVersion, kind, namespace, name string, owners ...metav1.OwnerReference) *unstructured.Unstructured {
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

	u.SetOwnerReferences(owners)

	return u
}

func NewUnstructuredWithContent(
	apiVersion, kind, namespace, name string, content map[string]interface{}) *unstructured.Unstructured {
	object := NewUnstructured(apiVersion, kind, namespace, name)
	for key, val := range content {
		object.Object[key] = val
	}

	return object
}

func NewManifestWork(index int, objects ...*unstructured.Unstructured) (*workapiv1.ManifestWork, string) {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("work-%d", index),
			Namespace: "cluster1",
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

	return work, work.Name
}

func NewAppliedManifestWork(hash string, index int, uid types.UID) *workapiv1.AppliedManifestWork {
	workName := fmt.Sprintf("work-%d", index)
	return &workapiv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", hash, workName),
			UID:  uid,
		},
		Spec: workapiv1.AppliedManifestWorkSpec{
			HubHash:          hash,
			ManifestWorkName: workName,
		},
		Status: workapiv1.AppliedManifestWorkStatus{},
	}
}

func NewFakeRestMapper() meta.RESTMapper {
	resources := []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Name: "",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "secrets", Namespaced: true, Kind: "Secret"},
					{Name: "pods", Namespaced: true, Kind: "Pod"},
					{Name: "newobjects", Namespaced: true, Kind: "NewObject"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1", GroupVersion: "apps/v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1", GroupVersion: "apps/v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "deployments", Group: "apps", Namespaced: true, Kind: "Deployment"},
				},
			},
		},
	}
	return restmapper.NewDiscoveryRESTMapper(resources)
}

func AssertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}
