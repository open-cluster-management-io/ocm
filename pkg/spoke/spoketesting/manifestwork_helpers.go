package spoketesting

import (
	"fmt"
	"testing"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
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

func NewUnstructuredSecret(namespace, name string, terminated bool, uid string) *unstructured.Unstructured {
	u := NewUnstructured("v1", "Secret", namespace, name)
	if terminated {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
	}
	if uid != "" {
		u.SetUID(types.UID(uid))
	}
	return u
}

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

	return work, fmt.Sprintf("%s", work.Name)
}

func NewAppliedManifestWork(hash string, index int) *workapiv1.AppliedManifestWork {
	workName := fmt.Sprintf("work-%d", index)
	return &workapiv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", hash, workName),
		},
		Spec: workapiv1.AppliedManifestWorkSpec{
			HubHash:          hash,
			ManifestWorkName: workName,
		},
		Status: workapiv1.AppliedManifestWorkStatus{},
	}
}

func NewFakeRestMapper() *resource.Mapper {
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
	return &resource.Mapper{
		Mapper: restmapper.NewDiscoveryRESTMapper(resources),
	}
}

func AssertAction(t *testing.T, actual clienttesting.Action, expected string) {
	if actual.GetVerb() != expected {
		t.Errorf("expected %s action but got: %#v", expected, actual)
	}
}
