package apply

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

const defaultOwner = "test-owner"

func TestServerSideApply(t *testing.T) {
	cases := []struct {
		name            string
		owner           metav1.OwnerReference
		existing        *unstructured.Unstructured
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		conflict        bool
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "server side apply successfully",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner},
			existing: nil,
			required: testingcommon.NewUnstructured("v1", "Namespace", "", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
		{
			name:     "server side apply successfully conflict",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner},
			existing: testingcommon.NewUnstructured("v1", "Secret", "ns1", "test"),
			required: testingcommon.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			conflict: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.existing != nil {
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)

			// The fake client does not support PatchType ApplyPatchType, add an reactor to mock apply patch
			// see issue: https://github.com/kubernetes/kubernetes/issues/103816
			reactor := &reactor{}
			reactors := []clienttesting.Reactor{reactor}
			dynamicClient.ReactionChain = append(reactors, dynamicClient.ReactionChain...)

			applier := NewServerSideApply(dynamicClient)

			syncContext := testingcommon.NewFakeSyncContext(t, "test")
			option := &workapiv1.ManifestConfigOption{
				UpdateStrategy: &workapiv1.UpdateStrategy{
					Type: workapiv1.UpdateStrategyTypeServerSideApply,
				},
			}
			obj, err := applier.Apply(
				context.TODO(), c.gvr, c.required, c.owner, option, syncContext.Recorder())
			c.validateActions(t, dynamicClient.Actions())
			if !c.conflict {
				if err != nil {
					t.Errorf("expect no error, but got %v", err)
				}

				accessor, err := meta.Accessor(obj)
				if err != nil {
					t.Errorf("type %t cannot be accessed: %v", obj, err)
				}
				if accessor.GetNamespace() != c.required.GetNamespace() || accessor.GetName() != c.required.GetName() {
					t.Errorf("Expect resource %s/%s, but %s/%s",
						c.required.GetNamespace(), c.required.GetName(), accessor.GetNamespace(), accessor.GetName())
				}
				return
			}

			var ssaConflict *ServerSideApplyConflictError
			if !errors.As(err, &ssaConflict) {
				t.Errorf("expect serverside apply conflict error, but got %v", err)
			}

		})
	}
}

type reactor struct {
}

func (r *reactor) Handles(action clienttesting.Action) bool {
	switch action := action.(type) {
	case clienttesting.PatchActionImpl:
		if action.GetPatchType() == types.ApplyPatchType {
			return true
		}

	default:
		return false
	}
	return true
}

// React handles the action and returns results.  It may choose to
// delegate by indicated handled=false.
func (r *reactor) React(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
	ns := action.GetNamespace()
	name := "test"
	if ga, ok := action.(clienttesting.GetAction); ok {
		name = ga.GetName()
	} else if pa, ok := action.(clienttesting.PatchAction); ok {
		name = pa.GetName()
	}

	switch action.GetResource().Resource {
	case "namespaces":
		return true, testingcommon.NewUnstructured(
			"v1", "Namespace", "", name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "deployments":
		return true, testingcommon.NewUnstructured(
			"apps/v1", "Deployment", ns, name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "configmaps":
		return true, testingcommon.NewUnstructured(
			"v1", "ConfigMap", ns, name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "services":
		return true, testingcommon.NewUnstructured(
			"v1", "Service", ns, name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "pods":
		return true, testingcommon.NewUnstructured(
			"v1", "Pod", ns, name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "virtualservices":
		return true, testingcommon.NewUnstructured(
			"networking.istio.io/v1", "VirtualService", ns, name,
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "secrets":
		return true, nil, apierrors.NewApplyConflict([]metav1.StatusCause{
			{
				Type:    metav1.CauseTypeFieldManagerConflict,
				Message: "field managed configl",
				Field:   "metadata.annotations",
			},
		}, "server side apply secret failed")
	}

	return true, nil, fmt.Errorf("PatchType is not supported")
}

func TestRemoveCreationTime(t *testing.T) {
	cases := []struct {
		name         string
		required     *unstructured.Unstructured
		validateFunc func(t *testing.T, obj *unstructured.Unstructured)
	}{
		{
			name:     "remove creationTimestamp from a kube object",
			required: newDeployment(2),
			validateFunc: func(t *testing.T, obj *unstructured.Unstructured) {
				_, existing, err := unstructured.NestedFieldCopy(obj.Object, "metadata", "creationTimestamp")
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					t.Errorf("unexpected creationTimestamp in `metadata.creationTimestamp`")
				}
				_, existing, err = unstructured.NestedFieldCopy(obj.Object, "spec", "template", "metadata", "creationTimestamp")
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					t.Errorf("unexpected creationTimestamp in `spec.template.metadata.creationTimestamp`")
				}
			},
		},
		{
			name:     "remove creationTimestamp from a manifestwork",
			required: newManifestWork(),
			validateFunc: func(t *testing.T, obj *unstructured.Unstructured) {
				_, existing, err := unstructured.NestedFieldCopy(obj.Object, "metadata", "creationTimestamp")
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					t.Errorf("unexpected creationTimestamp in `metadata.creationTimestamp`")
				}

				manifests, existing, err := unstructured.NestedSlice(obj.Object, "spec", "workload", "manifests")
				if err != nil {
					t.Fatal(err)
				}
				if !existing {
					t.Fatalf("no manifests")
				}

				_, existing, err = unstructured.NestedFieldCopy(manifests[0].(map[string]interface{}), "metadata", "creationTimestamp")
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					t.Errorf("unexpected creationTimestamp in `spec.workload.manifests[0].metadata.creationTimestamp`")
				}
			},
		},
	}

	logger := klog.NewKlogr()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			removeCreationTimeFromMetadata(c.required.Object, logger)
			c.validateFunc(t, c.required)
		})
	}
}

func newDeployment(replicas int32) *unstructured.Unstructured {
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"test": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
						},
					},
				},
			},
		},
	}

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(deploy)
	unstructured.RemoveNestedField(obj, "status")
	return &unstructured.Unstructured{Object: obj}
}

func newManifestWork() *unstructured.Unstructured {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm-test",
			Namespace: "default",
		},
		Data: map[string]string{
			"some": "data",
		},
	}

	cmObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	raw, _ := (&unstructured.Unstructured{Object: cmObj}).MarshalJSON()
	manifest := workapiv1.Manifest{}
	manifest.Raw = raw

	work := &workapiv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "work.open-cluster-management.io/v1",
			Kind:       "ManifestWork",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{manifest},
			},
		},
	}

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(work)
	return &unstructured.Unstructured{Object: obj}
}

func TestRemoveFieldByJSONPath(t *testing.T) {
	cases := []struct {
		name      string
		req       *unstructured.Unstructured
		exp       *unstructured.Unstructured
		jsonPaths []string
	}{
		{
			name: "remove a field",
			req: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name":     "name1",
					"replicas": int64(1),
				},
			}},
			exp: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name": "name1",
				},
			}},
			jsonPaths: []string{".spec.replicas"},
		},
		{
			name: "remove multiple fields",
			req: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name":     "name1",
					"replicas": int64(1),
					"containers": []interface{}{
						map[string]interface{}{
							"image": "test",
						},
					},
				},
			}},
			exp: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name": "name1",
				},
			}},
			jsonPaths: []string{".spec.replicas", ".spec.containers"},
		},
		{
			name: "remove filtered fields",
			req: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name":     "name1",
					"replicas": int64(1),
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "container1",
							"image": "test",
						},
						map[string]interface{}{
							"name":  "container2",
							"image": "test",
						},
					},
				},
			}},
			exp: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": int64(1),
					"name":     "name1",
					"containers": []interface{}{
						map[string]interface{}{
							"name": "container1",
						},
						map[string]interface{}{
							"name":  "container2",
							"image": "test",
						},
					},
				},
			}},
			jsonPaths: []string{".spec.containers[?(@.name==\"container1\")].image"},
		},
		{
			name: "list field is kept",
			req: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"name":     "name1",
					"replicas": int64(1),
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "container1",
							"image": "test",
						},
						map[string]interface{}{
							"name":  "container2",
							"image": "test",
						},
					},
				},
			}},
			exp: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": int64(1),
					"name":     "name1",
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "container1",
							"image": "test",
						},
						map[string]interface{}{
							"name":  "container2",
							"image": "test",
						},
					},
				},
			}},
			jsonPaths: []string{".spec.containers[?(@.name==\"container1\")]"},
		},
	}
	logger := klog.NewKlogr()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			desired := c.req.DeepCopy()
			for _, jsonPath := range c.jsonPaths {
				removeFieldByJSONPath(desired.UnstructuredContent(), jsonPath, logger)
			}
			if !equality.Semantic.DeepEqual(c.exp, desired) {
				t.Errorf("expected %v, got %v", c.exp, desired)
			}
		})
	}
}

func TestServerSideApplyWithIgnoreFields(t *testing.T) {
	cases := []struct {
		name            string
		existing        *unstructured.Unstructured
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		validateActions func(t *testing.T, actions []clienttesting.Action)
		condition       workapiv1.IgnoreFieldsCondition
		jsonPath        string
		jsonPointers    []string
		jqExpressions   []string
	}{
		{
			name: "server side apply ignore replicas",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(1),
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(2),
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				replicas, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected replicas to be preserved in the patch")
				}
				if replicas != int64(1) {
					t.Errorf("expected replicas to be copied from the live value 1, got %d", replicas)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".spec.replicas",
		},
		{
			name: "server side apply should not ignore when create",
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(2),
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				actualReplicas, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected replicas to exist in the patch")
				}
				if actualReplicas != int64(2) {
					t.Errorf("expected replicas to be 2 but got %d", actualReplicas)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".spec.replicas",
		},
		{
			name: "server side apply ignore update",
			existing: func() *unstructured.Unstructured {
				obj := testingcommon.NewUnstructuredWithContent(
					"v1", "ConfigMap", "default", "test",
					map[string]interface{}{
						"data": map[string]interface{}{
							"foo": "bar",
						},
					})
				obj.SetAnnotations(map[string]string{
					workapiv1.ManifestConfigSpecHashAnnotationKey: "4c07ba481d04e9c38e5ed3bf24139537",
				})
				return obj
			}(),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo1": "bar1",
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get")
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokeChange,
			jsonPath:  ".data",
		},
		{
			name: "server side apply with JSON Pointer - ignore replicas",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(5),
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"app": "test",
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(3),
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"app": "test",
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				replicas, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected replicas to be preserved in the patch")
				}
				if replicas != int64(5) {
					t.Errorf("expected replicas to be copied from the live value 5, got %d", replicas)
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/spec/replicas"},
		},
		{
			name: "server side apply with JSON Pointer - ignore annotation with special chars",
			existing: func() *unstructured.Unstructured {
				obj := testingcommon.NewUnstructuredWithContent(
					"v1", "Service", "default", "svc1",
					map[string]interface{}{
						"spec": map[string]interface{}{
							"ports": []interface{}{
								map[string]interface{}{
									"port": int64(80),
								},
							},
						},
					})
				obj.SetAnnotations(map[string]string{
					"prometheus.io/scrape": "true",
					"app":                  "myapp",
				})
				return obj
			}(),
			required: func() *unstructured.Unstructured {
				obj := testingcommon.NewUnstructuredWithContent(
					"v1", "Service", "default", "svc1",
					map[string]interface{}{
						"spec": map[string]interface{}{
							"ports": []interface{}{
								map[string]interface{}{
									"port": int64(80),
								},
							},
						},
					})
				obj.SetAnnotations(map[string]string{
					"prometheus.io/scrape": "false",
					"app":                  "myapp-updated",
				})
				return obj
			}(),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "services"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				scrape, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || scrape != "true" {
					t.Errorf("expected prometheus.io/scrape annotation to be preserved with the live value 'true', got %q", scrape)
				}
				app, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "app")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || app != "myapp-updated" {
					t.Errorf("expected app annotation to be updated to myapp-updated")
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/metadata/annotations/prometheus.io~1scrape"},
		},
		{
			name: "server side apply with JQ Expression - filter containers",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "myapp:v1",
									},
									map[string]interface{}{
										"name":  "istio-proxy",
										"image": "istio/proxyv2:1.20.0",
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "myapp:v2",
									},
									map[string]interface{}{
										"name":  "istio-proxy",
										"image": "istio/proxyv2:1.20.0",
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected containers to exist")
				}
				// the ignored container is preserved with its live content, and appended
				// after the managed containers
				if len(containers) != 2 {
					t.Fatalf("expected 2 containers (managed app + preserved istio-proxy), got %d", len(containers))
				}
				if container, ok := containers[0].(map[string]interface{}); ok {
					if container["name"] != "app" {
						t.Errorf("expected container name to be 'app', got %v", container["name"])
					}
					if container["image"] != "myapp:v2" {
						t.Errorf("expected app image to be updated to 'myapp:v2', got %v", container["image"])
					}
				}
				if container, ok := containers[1].(map[string]interface{}); ok {
					if container["name"] != "istio-proxy" {
						t.Errorf("expected preserved container name to be 'istio-proxy', got %v", container["name"])
					}
					if container["image"] != "istio/proxyv2:1.20.0" {
						t.Errorf("expected istio-proxy image to be the live value 'istio/proxyv2:1.20.0', got %v", container["image"])
					}
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | select(.name == \"istio-proxy\")"},
		},
		{
			name: "server side apply with combined selectors - JSONPointer, and JQ",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							"prometheus.io/scrape": "true",
							"managed-by":           "ocm",
						},
					},
					"spec": map[string]interface{}{
						"replicas": int64(5),
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "myapp:v1",
									},
									map[string]interface{}{
										"name":  "istio-proxy",
										"image": "istio/proxyv2:1.20.0",
									},
								},
								"volumes": []interface{}{
									map[string]interface{}{
										"name": "data",
									},
									map[string]interface{}{
										"name": "istio-token",
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							"prometheus.io/scrape": "false",
							"managed-by":           "hub",
						},
					},
					"spec": map[string]interface{}{
						"replicas": int64(3),
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "myapp:v2",
									},
								},
								"volumes": []interface{}{
									map[string]interface{}{
										"name": "data",
									},
									map[string]interface{}{
										"name": "istio-token",
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				// Verify replicas preserved with the live value (JSONPointer)
				replicas, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected replicas to be preserved")
				}
				if replicas != int64(5) {
					t.Errorf("expected replicas to be copied from the live value 5, got %d", replicas)
				}
				// Verify prometheus annotation preserved with the live value (JSON Pointer)
				scrape, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || scrape != "true" {
					t.Errorf("expected prometheus.io/scrape annotation to be preserved with the live value 'true', got %q", scrape)
				}
				// Verify istio volumes preserved from live by JQ (data from required + istio-token from live)
				volumes, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "volumes")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected volumes to exist")
				}
				if len(volumes) != 2 {
					t.Fatalf("expected 2 volumes (managed data + preserved istio-token), got %d", len(volumes))
				}
				// Verify istio-token volume was preserved with its live content
				if volume, ok := volumes[1].(map[string]interface{}); ok {
					if name, ok := volume["name"].(string); !ok || name != "istio-token" {
						t.Errorf("expected preserved volume to be istio-token, got %v", volume["name"])
					}
				}
				// Verify managed-by annotation updated (not ignored)
				managedBy, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "managed-by")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || managedBy != "hub" {
					t.Errorf("expected managed-by annotation to be updated to 'hub'")
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers:  []string{"/spec/replicas", "/metadata/annotations/prometheus.io~1scrape"},
			jqExpressions: []string{".spec.template.spec.volumes[] | select(.name | startswith(\"istio-\"))"},
		},
		{
			name: "server side apply with pod - ignore Istio injected sidecars and volumes",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "production", "my-application",
				map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app":     "my-application",
							"version": "v1",
						},
						"annotations": map[string]interface{}{
							"sidecar.istio.io/status": `{"version":"1.20.0"}`,
							"prometheus.io/scrape":    "true",
							"prometheus.io/port":      "15020",
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "application",
								"image": "myapp:1.2.3",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": int64(8080),
									},
								},
							},
							map[string]interface{}{
								"name":  "istio-proxy",
								"image": "istio/proxyv2:1.20.0",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": int64(15090),
									},
								},
							},
						},
						"initContainers": []interface{}{
							map[string]interface{}{
								"name":  "istio-init",
								"image": "istio/proxyv2:1.20.0",
							},
						},
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "app-data",
							},
							map[string]interface{}{
								"name": "istio-envoy",
							},
							map[string]interface{}{
								"name": "istio-token",
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "production", "my-application",
				map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app":     "my-application",
							"version": "v2",
						},
						"annotations": map[string]interface{}{
							"sidecar.istio.io/status": `{"version":"1.19.0"}`,
							"prometheus.io/scrape":    "false",
							"prometheus.io/port":      "9090",
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "application",
								"image": "myapp:2.0.0",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": int64(8080),
									},
								},
							},
							map[string]interface{}{
								"name":  "istio-proxy",
								"image": "istio/proxyv2:1.19.0",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": int64(15090),
									},
								},
							},
						},
						"initContainers": []interface{}{
							map[string]interface{}{
								"name":  "istio-init",
								"image": "istio/proxyv2:1.19.0",
							},
						},
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "app-data",
							},
							map[string]interface{}{
								"name": "istio-envoy",
							},
							map[string]interface{}{
								"name": "istio-token",
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				// Verify istio-proxy container is preserved from live (application from required + istio-proxy from live)
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected containers to exist")
				}
				if len(containers) != 2 {
					t.Fatalf("expected 2 containers (managed application + preserved istio-proxy), got %d", len(containers))
				}
				if container, ok := containers[0].(map[string]interface{}); ok {
					if container["name"] != "application" {
						t.Errorf("expected container name to be 'application', got %v", container["name"])
					}
					if container["image"] != "myapp:2.0.0" {
						t.Errorf("expected image to be updated to 'myapp:2.0.0', got %v", container["image"])
					}
				}
				if container, ok := containers[1].(map[string]interface{}); ok {
					if container["name"] != "istio-proxy" {
						t.Errorf("expected preserved container name to be 'istio-proxy', got %v", container["name"])
					}
					if container["image"] != "istio/proxyv2:1.20.0" {
						t.Errorf("expected istio-proxy image to be the live value 'istio/proxyv2:1.20.0', got %v", container["image"])
					}
				}
				// Verify init containers are preserved from live
				initContainers, _, err := unstructured.NestedSlice(actual.Object, "spec", "initContainers")
				if err != nil {
					t.Fatal(err)
				}
				if len(initContainers) != 1 {
					t.Fatalf("expected 1 initContainer preserved from live, got %d", len(initContainers))
				}
				initContainer, ok := initContainers[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected initContainer to be an object, got %T", initContainers[0])
				}
				if initContainer["name"] != "istio-init" {
					t.Errorf("expected initContainer name to be 'istio-init', got %v", initContainer["name"])
				}
				if initContainer["image"] != "istio/proxyv2:1.20.0" {
					t.Errorf("expected initContainer image to be the live value 'istio/proxyv2:1.20.0', got %v", initContainer["image"])
				}
				// Verify istio volumes are preserved from live (app-data from required + istio volumes from live)
				volumes, exist, err := unstructured.NestedSlice(actual.Object, "spec", "volumes")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected volumes to exist")
				}
				if len(volumes) != 3 {
					t.Fatalf("expected 3 volumes (managed app-data + preserved istio volumes), got %d", len(volumes))
				}
				scrape, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || scrape != "true" {
					t.Errorf("expected prometheus.io/scrape annotation to be preserved with the live value 'true', got %q", scrape)
				}
				// Verify version label is updated (not ignored)
				version, exist, err := unstructured.NestedString(actual.Object, "metadata", "labels", "version")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || version != "v2" {
					t.Errorf("expected version label to be updated to 'v2', got %v", version)
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/metadata/annotations/prometheus.io~1scrape"},
			jqExpressions: []string{
				".spec.containers[] | select(.name == \"istio-proxy\" or (.name | startswith(\"istio-\")))",
				".spec.volumes[]? | select(.name | startswith(\"istio-\"))",
				".spec.initContainers[]? | select(.name | startswith(\"istio-\"))",
			},
		},
		{
			// reproducer for https://github.com/open-cluster-management-io/ocm/issues/1785:
			// the ignored field is the only http route of the VirtualService, removing it from
			// the apply payload makes server side apply delete the whole spec.http block.
			name: "server side apply preserves the whole ignored list entry with the live content",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{
											"host":   "svc1",
											"subset": "stable",
										},
										"weight": int64(80),
									},
									map[string]interface{}{
										"destination": map[string]interface{}{
											"host":   "svc1",
											"subset": "canary",
										},
										"weight": int64(20),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1", "svc2"},
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{
											"host":   "svc1",
											"subset": "stable",
										},
										"weight": int64(100),
									},
									map[string]interface{}{
										"destination": map[string]interface{}{
											"host":   "svc1",
											"subset": "canary",
										},
										"weight": int64(0),
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				// the whole http block must be preserved with the complete live content
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(http) != 1 {
					t.Fatalf("expected the rollout route to be preserved in the patch, got %v", http)
				}
				expectedRouteEntry := map[string]interface{}{
					"name": "rollout",
					"route": []interface{}{
						map[string]interface{}{
							"destination": map[string]interface{}{
								"host":   "svc1",
								"subset": "stable",
							},
							"weight": int64(80),
						},
						map[string]interface{}{
							"destination": map[string]interface{}{
								"host":   "svc1",
								"subset": "canary",
							},
							"weight": int64(20),
						},
					},
				}
				if !equality.Semantic.DeepEqual(http[0], expectedRouteEntry) {
					t.Errorf("expected the ignored http entry to match the live value, got %v", http[0])
				}
				// non-ignored fields are still updated from the manifest
				hosts, exist, err := unstructured.NestedStringSlice(actual.Object, "spec", "hosts")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(hosts) != 2 {
					t.Errorf("expected hosts to be updated from the manifest, got %v", hosts)
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/spec/http/0"},
		},
		{
			name: "server side apply omits the ignored field missing on the live object",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo": "bar",
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo":  "bar",
						"foo1": "bar1",
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				err := actual.UnmarshalJSON(p)
				if err != nil {
					t.Fatal(err)
				}
				// the ignored field does not exist on the live object: adding it would be
				// an update to an ignored field, so it is omitted from the payload
				_, exist, err := unstructured.NestedString(actual.Object, "data", "foo1")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected foo1 to be omitted from the patch")
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/data/foo1"},
		},
		{
			name: "server side apply omits the ignored jsonpath field missing on the live object",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo": "bar",
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo":  "bar",
						"foo1": "bar1",
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				_, exist, err := unstructured.NestedString(actual.Object, "data", "foo1")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected foo1 to be omitted from the patch")
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".data.foo1",
		},
		{
			name: "server side apply with JQ omits the ignored leaf missing on the live element",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "myapp:v2",
										"resources": map[string]interface{}{
											"requests": map[string]interface{}{"cpu": "500m"},
										},
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 1 {
					t.Fatalf("expected 1 container, got %v", containers)
				}
				app, ok := containers[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected container to be an object, got %T", containers[0])
				}
				// the ignored leaf is absent on the live element: adding it would update
				// an ignored field, so it is omitted from the payload
				if _, exist := app["resources"]; exist {
					t.Errorf("expected resources to be omitted from the patch, got %v", app)
				}
				// non-ignored fields still apply from the manifest
				if app["image"] != "myapp:v2" {
					t.Errorf("expected image to be updated from the manifest, got %v", app["image"])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | select(.name == \"app\") | .resources"},
		},
		{
			name: "server side apply preserves a non-final ignored list entry and keeps the following manifest entries",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(80),
									},
								},
							},
							map[string]interface{}{
								"name": "default",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1"},
										"weight":      int64(100),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(100),
									},
								},
							},
							map[string]interface{}{
								"name": "default",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc2"},
										"weight":      int64(100),
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(http) != 2 {
					t.Fatalf("expected 2 http entries (preserved rollout + updated default), got %v", http)
				}
				// the ignored non-final entry is inserted back with the live content
				first, ok := http[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected http entry to be an object, got %T", http[0])
				}
				if first["name"] != "rollout" {
					t.Errorf("expected the first entry to be the preserved rollout route, got %v", first["name"])
				}
				routes, exist, err := unstructured.NestedSlice(first, "route")
				if err != nil || !exist || len(routes) != 1 {
					t.Fatalf("expected rollout routes to be preserved, got %v", routes)
				}
				route0, ok := routes[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected route to be an object, got %T", routes[0])
				}
				w, exist, err := unstructured.NestedInt64(route0, "weight")
				if err != nil || !exist || w != int64(80) {
					t.Errorf("expected the preserved route weight to be the live value 80, got %v", w)
				}
				// the manifest entry after the ignored one is still applied
				second, ok := http[1].(map[string]interface{})
				if !ok {
					t.Fatalf("expected http entry to be an object, got %T", http[1])
				}
				if second["name"] != "default" {
					t.Errorf("expected the second entry to be the manifest's default route, got %v", second["name"])
				}
				routes2, exist, err := unstructured.NestedSlice(second, "route")
				if err != nil || !exist || len(routes2) != 1 {
					t.Fatalf("expected default routes to be applied from the manifest, got %v", routes2)
				}
				route1, ok := routes2[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected route to be an object, got %T", routes2[0])
				}
				h, exist, err := unstructured.NestedString(route1, "destination", "host")
				if err != nil || !exist || h != "svc2" {
					t.Errorf("expected the default route host to be updated to svc2, got %v", h)
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/spec/http/0"},
		},
		{
			name: "server side apply preserves a non-final ignored list entry selected by jq",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
									map[string]interface{}{"name": "istio-proxy", "image": "istio/proxyv2:1.20.0"},
									map[string]interface{}{"name": "metrics", "image": "metrics:v1"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
									map[string]interface{}{"name": "istio-proxy", "image": "istio/proxyv2:1.19.0"},
									map[string]interface{}{"name": "metrics", "image": "metrics:v2"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 3 {
					t.Fatalf("expected 3 containers (app + preserved istio-proxy + metrics), got %v", containers)
				}
				expected := []struct {
					name, image string
				}{
					{"app", "myapp:v2"},                     // updated from the manifest
					{"istio-proxy", "istio/proxyv2:1.20.0"}, // preserved from live
					{"metrics", "metrics:v2"},               // updated from the manifest
				}
				for i, e := range expected {
					container, ok := containers[i].(map[string]interface{})
					if !ok {
						t.Fatalf("expected container to be an object, got %T", containers[i])
					}
					if container["name"] != e.name || container["image"] != e.image {
						t.Errorf("expected container %d to be %s:%s, got %v:%v",
							i, e.name, e.image, container["name"], container["image"])
					}
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | select(.name == \"istio-proxy\")"},
		},
		{
			name: "server side apply seeds the ignored list entry from live when the manifest does not have it",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(50),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				// the manifest has no http block, so the ignored entry is appended from live
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(http) != 1 {
					t.Fatalf("expected the live rollout route to be preserved in the patch, got %v", http)
				}
				entry, ok := http[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected http entry to be an object, got %T", http[0])
				}
				if entry["name"] != "rollout" {
					t.Errorf("expected the preserved entry to be the rollout route, got %v", entry["name"])
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/spec/http/0"},
		},
		{
			name: "server side apply with JSONPath pairs reordered list elements by name",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "proxy", "image": "proxy:live"},
									map[string]interface{}{"name": "app", "image": "app:live"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "app:new"},
									map[string]interface{}{"name": "proxy", "image": "proxy:new"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 2 {
					t.Fatalf("expected 2 containers, got %v", containers)
				}
				// each container must get the live image of the container with the same
				// name, not the one at the same position (live order is proxy, app)
				app, ok := containers[0].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "app:live" {
					t.Errorf("expected app container to keep its own live image, got %v", containers[0])
				}
				proxy, ok := containers[1].(map[string]interface{})
				if !ok || proxy["name"] != "proxy" || proxy["image"] != "proxy:live" {
					t.Errorf("expected proxy container to keep its own live image, got %v", containers[1])
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".spec.template.spec.containers[*].image",
		},
		{
			name: "server side apply with JSONPath copies the complete live list when the manifest omits the parent list",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(70),
									},
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "canary"},
										"weight":      int64(30),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				// the manifest omits spec.http; the complete live entries must be
				// preserved, otherwise SSA deletes the whole block
				expected := []interface{}{
					map[string]interface{}{
						"name": "rollout",
						"route": []interface{}{
							map[string]interface{}{
								"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
								"weight":      int64(70),
							},
							map[string]interface{}{
								"destination": map[string]interface{}{"host": "svc1", "subset": "canary"},
								"weight":      int64(30),
							},
						},
					},
				}
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Fatal("expected the live http block to be preserved in the patch")
				}
				if !equality.Semantic.DeepEqual(http, expected) {
					t.Errorf("expected the complete live http entries, got %v", http)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".spec.http[*].route",
		},
		{
			name: "server side apply with JSONPath carries the complete live parent when the manifest omits it",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo":   "live-value",
						"other": "x",
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				// the manifest omits the data parent, so the whole parent subtree is
				// spoke-managed: the complete live parent is carried over
				value, exist, err := unstructured.NestedString(actual.Object, "data", "foo")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || value != "live-value" {
					t.Errorf("expected data.foo to be preserved with the live value, got %q", value)
				}
				other, exist, err := unstructured.NestedString(actual.Object, "data", "other")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || other != "x" {
					t.Errorf("expected the complete live parent, data.other should be 'x', got %q", other)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".data.foo",
		},
		{
			name: "server side apply with JQ resolves reordered list elements by name",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
									map[string]interface{}{"name": "proxy", "image": "proxy:live"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "proxy", "image": "proxy:new"},
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 2 {
					t.Fatalf("expected 2 containers, got %v", containers)
				}
				// the live index of proxy is 1, but it is at index 0 in the manifest;
				// the live image must land on the proxy container, not on app
				proxy, ok := containers[0].(map[string]interface{})
				if !ok || proxy["name"] != "proxy" || proxy["image"] != "proxy:live" {
					t.Errorf("expected proxy container to keep its live image, got %v", containers[0])
				}
				app, ok := containers[1].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "myapp:v2" {
					t.Errorf("expected app container to be updated from the manifest, got %v", containers[1])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | select(.name == \"proxy\") | .image"},
		},
		{
			name: "server side apply with JSONPath keeps the manifest value for a renamed container",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "app:live"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "web", "image": "web:new"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 1 {
					t.Fatalf("expected 1 container, got %v", containers)
				}
				// the renamed container has no live match: its manifest image must be kept,
				// never the live image of the container at the same index
				web, ok := containers[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected container to be an object, got %T", containers[0])
				}
				if web["name"] != "web" || web["image"] != "web:new" {
					t.Errorf("expected the renamed container to keep its manifest image, got %v", web)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  ".spec.template.spec.containers[*].image",
		},
		{
			name: "server side apply with JSON Pointer does not carry an out-of-range element for a leaf pointer",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
									map[string]interface{}{
										"name":  "sidecar",
										"image": "sidecar:live",
										"ports": []interface{}{
											map[string]interface{}{"containerPort": int64(15090)},
										},
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				// the pointer selects a leaf of an element the manifest does not have:
				// the element's lifecycle is not covered by the ignored path, so it is
				// neither synthesized nor carried from live
				if !exist || len(containers) != 1 {
					t.Fatalf("expected only the manifest container, got %v", containers)
				}
				app, ok := containers[0].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "myapp:v2" {
					t.Errorf("expected app to keep its manifest image, got %v", containers[0])
				}
			},
			condition:    workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPointers: []string{"/spec/template/spec/containers/1/image"},
		},
		{
			name: "server side apply with JQ does not add back an element removed from the manifest when only a leaf is ignored",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
									map[string]interface{}{
										"name":  "proxy",
										"image": "proxy:live",
										"ports": []interface{}{
											map[string]interface{}{"containerPort": int64(15090)},
										},
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				// the element is absent from the manifest and only a leaf of it is
				// selected: the removal must apply, the element must not be added back
				// from live
				if !exist || len(containers) != 1 {
					t.Fatalf("expected only the manifest container, got %v", containers)
				}
				app, ok := containers[0].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "myapp:v2" {
					t.Errorf("expected app to keep its manifest image, got %v", containers[0])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | select(.name == \"proxy\") | .image"},
		},
		{
			name: "server side apply with JSONPath filter keeps existing list entries when the selection misses them",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(80),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"name": "default",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc2"},
										"weight":      int64(100),
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(http) != 2 {
					t.Fatalf("expected the manifest's default entry plus the selected live entry, got %v", http)
				}
				// the manifest entry the selection missed must not be replaced
				first, ok := http[0].(map[string]interface{})
				if !ok || first["name"] != "default" {
					t.Fatalf("expected the first entry to be the manifest's default entry, got %v", http[0])
				}
				// the selected live entry is carried whole under its own identity
				second, ok := http[1].(map[string]interface{})
				if !ok || second["name"] != "rollout" {
					t.Fatalf("expected the selected live rollout entry to be carried, got %v", http[1])
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  `.spec.http[?(@.name=="rollout")].route`,
		},
		{
			name: "server side apply with JSONPath strips the root marker when creating the missing parent",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo": "live-value",
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				value, exist, err := unstructured.NestedString(actual.Object, "data", "foo")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || value != "live-value" {
					t.Errorf("expected data.foo to be preserved with the live value, got %q", value)
				}
				if _, exist := actual.Object["$"]; exist {
					t.Errorf("expected no literal $ key in the patch, got %v", actual.Object)
				}
			},
			condition: workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jsonPath:  "$.data.foo",
		},
		{
			name: "server side apply with JQ does not carry an out-of-range unnamed element for a leaf selection",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1"},
										"weight":      int64(100),
									},
								},
							},
							map[string]interface{}{
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc2"},
										"weight":      int64(50),
									},
								},
								"timeout": "10s",
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"http": []interface{}{
							map[string]interface{}{
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1"},
										"weight":      int64(90),
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				// the selection targets a leaf of an entry the manifest does not have:
				// the entry is not carried over
				if !exist || len(http) != 1 {
					t.Fatalf("expected only the manifest entry, got %v", http)
				}
				// the first entry keeps its manifest weight (not selected by the jq expression)
				first, ok := http[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected http entry to be an object, got %T", http[0])
				}
				routes, exist, err := unstructured.NestedSlice(first, "route")
				if err != nil || !exist || len(routes) != 1 {
					t.Fatalf("expected routes in the first entry, got %v", first)
				}
				route0, ok := routes[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected route to be an object, got %T", routes[0])
				}
				w, exist, err := unstructured.NestedInt64(route0, "weight")
				if err != nil || !exist || w != int64(90) {
					t.Errorf("expected the first entry to keep its manifest weight 90, got %v", w)
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.http[] | select(.timeout == \"10s\") | .route[0].weight"},
		},
		{
			name: "server side apply with JQ tolerates the selected list missing on the live object",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1", "subset": "stable"},
										"weight":      int64(100),
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// the expression uses the optional []? form, so the list missing on live
				// is skipped and the manifest values apply (rule 3)
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				http, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(http) != 1 {
					t.Fatalf("expected the manifest http entries to be applied, got %v", http)
				}
				entry, ok := http[0].(map[string]interface{})
				if !ok || entry["name"] != "rollout" {
					t.Fatalf("expected the manifest rollout entry, got %v", http[0])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.http[]? | select(.name == \"rollout\")"},
		},
		{
			name: "server side apply with JQ tolerates the selected path missing in the manifest",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo": "bar",
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "ConfigMap", "default", "test",
				map[string]interface{}{
					"data": map[string]interface{}{
						"foo": "bar",
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// the optional []? form makes removal and overlay no-ops when the
				// selected list exists on neither side, and the apply proceeds
				testingcommon.AssertActions(t, actions, "get", "patch")
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.http[]? | select(.name == \"rollout\")"},
		},
		{
			name: "server side apply with JQ keeps the manifest value for a new named element inserted before existing ones",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "sidecar",
										"image": "sidecar:v1",
										"securityContext": map[string]interface{}{
											"runAsNonRoot": true,
										},
									},
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 2 {
					t.Fatalf("expected 2 containers, got %v", containers)
				}
				// sidecar is new: it has no live counterpart, so its manifest
				// securityContext must be kept. A positional fallback would resolve the
				// live element at index 0 (app), conclude the field is absent on live,
				// and omit it — the new sidecar would be created without it and every
				// later apply would keep omitting it.
				sidecar, ok := containers[0].(map[string]interface{})
				if !ok || sidecar["name"] != "sidecar" {
					t.Fatalf("expected the first container to be sidecar, got %v", containers[0])
				}
				expectedSecurityContext := map[string]interface{}{"runAsNonRoot": true}
				if !equality.Semantic.DeepEqual(sidecar["securityContext"], expectedSecurityContext) {
					t.Errorf("expected the new sidecar to keep its manifest securityContext, got %v",
						sidecar["securityContext"])
				}
				// app is matched by name and keeps its manifest image
				app, ok := containers[1].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "myapp:v2" {
					t.Errorf("expected app to keep its manifest image, got %v", containers[1])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[].securityContext"},
		},
		{
			name: "server side apply with JQ tolerates a scalar list element when checking the live object",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1", "svc2"},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// a scalar list element is not a map: the identity check must not panic,
				// and the selection targeting the element itself carries it from live
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				hosts, exist, err := unstructured.NestedStringSlice(actual.Object, "spec", "hosts")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(hosts) != 2 || hosts[0] != "svc1" || hosts[1] != "svc2" {
					t.Errorf("expected hosts [svc1 svc2], got %v", hosts)
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.hosts[]"},
		},
		{
			name: "server side apply with JQ evaluates later elements when an earlier one lacks the list",
			existing: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v1"},
									map[string]interface{}{
										"name":  "sidecar",
										"image": "sidecar:v1",
										"env": []interface{}{
											map[string]interface{}{"name": "JAVA_OPTS", "value": "live"},
										},
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"apps/v1", "Deployment", "default", "deploy1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{"name": "app", "image": "myapp:v2"},
									map[string]interface{}{
										"name":  "sidecar",
										"image": "sidecar:v1",
										"env": []interface{}{
											map[string]interface{}{"name": "JAVA_OPTS", "value": "new"},
										},
									},
								},
							},
						},
					},
				}),
			gvr: schema.GroupVersionResource{Version: "v1", Group: "apps", Resource: "deployments"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(containers) != 2 {
					t.Fatalf("expected 2 containers, got %v", containers)
				}
				// the first container has no env list: iterating it must not abort the
				// evaluation of the later container that contains the ignored field
				sidecar, ok := containers[1].(map[string]interface{})
				if !ok || sidecar["name"] != "sidecar" {
					t.Fatalf("expected the second container to be sidecar, got %v", containers[1])
				}
				env, exist, err := unstructured.NestedSlice(sidecar, "env")
				if err != nil || !exist || len(env) != 1 {
					t.Fatalf("expected sidecar env to be preserved, got %v", sidecar)
				}
				entry, ok := env[0].(map[string]interface{})
				if !ok {
					t.Fatalf("expected env entry to be an object, got %T", env[0])
				}
				if entry["value"] != "live" {
					t.Errorf("expected the ignored env value to keep the live value, got %v", entry["value"])
				}
				// app keeps its manifest image
				app, ok := containers[0].(map[string]interface{})
				if !ok || app["name"] != "app" || app["image"] != "myapp:v2" {
					t.Errorf("expected app to keep its manifest image, got %v", containers[0])
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.template.spec.containers[] | .env[]? | select(.name == \"JAVA_OPTS\")"},
		},
		{
			name: "server side apply does not synthesize empty parents for a skipped overlay",
			existing: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1"},
						"http": []interface{}{
							map[string]interface{}{
								"name": "rollout",
								"route": []interface{}{
									map[string]interface{}{
										"destination": map[string]interface{}{"host": "svc1"},
										"weight":      int64(80),
									},
								},
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"networking.istio.io/v1", "VirtualService", "default", "vs1",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"hosts": []interface{}{"svc1", "svc2"},
					},
				}),
			gvr: schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "patch")
				p := actions[1].(clienttesting.PatchActionImpl).Patch
				actual := &unstructured.Unstructured{}
				if err := actual.UnmarshalJSON(p); err != nil {
					t.Fatal(err)
				}
				// the selection targets a leaf of an element the payload does not have:
				// the overlay is skipped, and skipping must not synthesize empty parent
				// lists into the payload
				_, exist, err := unstructured.NestedSlice(actual.Object, "spec", "http")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected no spec.http in the patch, got %v", actual.Object["spec"])
				}
				// the non-ignored field still applies
				hosts, exist, err := unstructured.NestedStringSlice(actual.Object, "spec", "hosts")
				if err != nil {
					t.Fatal(err)
				}
				if !exist || len(hosts) != 2 {
					t.Errorf("expected hosts to be updated from the manifest, got %v", hosts)
				}
			},
			condition:     workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions: []string{".spec.http[]? | select(.name == \"rollout\") | .route[0].weight"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			owner := metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}
			if c.existing != nil {
				c.existing.SetOwnerReferences([]metav1.OwnerReference{owner})
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
			applier := NewServerSideApply(dynamicClient)

			// The fake client does not support PatchType ApplyPatchType, add an reactor to mock apply patch
			// see issue: https://github.com/kubernetes/kubernetes/issues/103816
			reactor := &reactor{}
			reactors := []clienttesting.Reactor{reactor}
			dynamicClient.ReactionChain = append(reactors, dynamicClient.ReactionChain...)

			syncContext := testingcommon.NewFakeSyncContext(t, "test")
			option := &workapiv1.ManifestConfigOption{
				UpdateStrategy: &workapiv1.UpdateStrategy{
					Type: workapiv1.UpdateStrategyTypeServerSideApply,
					ServerSideApply: &workapiv1.ServerSideApplyConfig{
						FieldManager: "test-agent",
						IgnoreFields: []workapiv1.IgnoreField{
							{
								Condition: c.condition,
								JSONPaths: func() []string {
									if c.jsonPath != "" {
										return []string{c.jsonPath}
									}
									return nil
								}(),
								JSONPointers:      c.jsonPointers,
								JQPathExpressions: c.jqExpressions,
							},
						},
					},
				},
			}
			_, err := applier.Apply(
				context.TODO(), c.gvr, c.required, owner, option, syncContext.Recorder())
			if err != nil {
				t.Fatal(err)
			}
			c.validateActions(t, dynamicClient.Actions())
		})
	}
}

// TestServerSideApplyWithIgnoreFieldErrors tests error handling for ignoreFields
func TestServerSideApplyWithIgnoreFieldErrors(t *testing.T) {
	cases := []struct {
		name             string
		existing         *unstructured.Unstructured
		required         *unstructured.Unstructured
		gvr              schema.GroupVersionResource
		condition        workapiv1.IgnoreFieldsCondition
		jsonPointers     []string
		jqExpressions    []string
		expectedErrorMsg string
		cancelContext    bool
	}{
		{
			name: "invalid JQ expression syntax",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "default", "test-pod",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "myapp:v1",
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "default", "test-pod",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "myapp:v2",
							},
						},
					},
				}),
			gvr:              schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			condition:        workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions:    []string{".spec.containers[] | invalid syntax here"},
			expectedErrorMsg: "JQ expression error",
		},
		{
			name: "pre-cancelled context before JQ execution",
			existing: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "default", "test-pod",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "myapp:v1",
							},
						},
					},
				}),
			required: testingcommon.NewUnstructuredWithContent(
				"v1", "Pod", "default", "test-pod",
				map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "myapp:v2",
							},
						},
					},
				}),
			gvr:              schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			condition:        workapiv1.IgnoreFieldsConditionOnSpokePresent,
			jqExpressions:    []string{".spec.containers[] | select(.name == \"app\")"},
			cancelContext:    true,
			expectedErrorMsg: "JQ expression error",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			owner := metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}
			if c.existing != nil {
				c.existing.SetOwnerReferences([]metav1.OwnerReference{owner})
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
			applier := NewServerSideApply(dynamicClient)

			reactor := &reactor{}
			reactors := []clienttesting.Reactor{reactor}
			dynamicClient.ReactionChain = append(reactors, dynamicClient.ReactionChain...)

			syncContext := testingcommon.NewFakeSyncContext(t, "test")
			option := &workapiv1.ManifestConfigOption{
				UpdateStrategy: &workapiv1.UpdateStrategy{
					Type: workapiv1.UpdateStrategyTypeServerSideApply,
					ServerSideApply: &workapiv1.ServerSideApplyConfig{
						FieldManager: "test-agent",
						IgnoreFields: []workapiv1.IgnoreField{
							{
								Condition:         c.condition,
								JSONPointers:      c.jsonPointers,
								JQPathExpressions: c.jqExpressions,
							},
						},
					},
				},
			}

			ctx := context.Background()
			if c.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			_, err := applier.Apply(ctx, c.gvr, c.required, owner, option, syncContext.Recorder())

			if err == nil {
				t.Fatal("expected error but got none")
			}

			for _, action := range dynamicClient.Actions() {
				if action.GetVerb() == "patch" {
					t.Fatalf("unexpected patch action on ignore-field failure: %#v", action)
				}
			}

			var ignoreFieldErr *IgnoreFieldError
			if !errors.As(err, &ignoreFieldErr) {
				t.Fatalf("expected IgnoreFieldError, got %T: %v", err, err)
			}

			if !strings.Contains(err.Error(), c.expectedErrorMsg) {
				t.Errorf("expected error message to contain '%s', got: %v", c.expectedErrorMsg, err)
			}

			if c.cancelContext && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation to be preserved, got: %v", err)
			}

			t.Logf("Successfully caught expected error: %v", err)
		})
	}
}
