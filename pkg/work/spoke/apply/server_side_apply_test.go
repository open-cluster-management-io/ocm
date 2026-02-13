package apply

import (
	"context"
	"fmt"
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
			dynamicClient.Fake.ReactionChain = append(reactors, dynamicClient.Fake.ReactionChain...)

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
	switch action.GetResource().Resource {
	case "namespaces":
		return true, testingcommon.NewUnstructured(
			"v1", "Namespace", "", "test",
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "deployments":
		return true, testingcommon.NewUnstructured(
			"apps/v1", "Deployment", "", "test",
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "configmaps":
		return true, testingcommon.NewUnstructured(
			"v1", "ConfigMap", "", "test",
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "services":
		return true, testingcommon.NewUnstructured(
			"v1", "Service", "", "test",
			metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner}), nil
	case "pods":
		return true, testingcommon.NewUnstructured(
			"v1", "Pod", "", "test",
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
				_, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected replicas to be removed in the patch")
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
				_, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected replicas to be removed in the patch")
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
				_, exist, err := unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected prometheus.io/scrape annotation to be removed in the patch")
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
				// Should only have app container, istio-proxy should be filtered out
				if len(containers) != 1 {
					t.Errorf("expected 1 container after filtering, got %d", len(containers))
				}
				if container, ok := containers[0].(map[string]interface{}); ok {
					if container["name"] != "app" {
						t.Errorf("expected container name to be 'app', got %v", container["name"])
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
				// Verify replicas removed (JSONPointer)
				_, exist, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected replicas to be removed")
				}
				// Verify prometheus annotation removed (JSONPath)
				_, exist, err = unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected prometheus.io/scrape annotation to be removed")
				}
				// Verify istio volumes filtered out (JQ)
				volumes, exist, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "volumes")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected volumes to exist")
				}
				if len(volumes) != 1 {
					t.Errorf("expected 1 volume after filtering, got %d", len(volumes))
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
				// Verify istio-proxy container is filtered out
				containers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "containers")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected containers to exist")
				}
				if len(containers) != 1 {
					t.Errorf("expected 1 container after filtering istio-proxy, got %d", len(containers))
				}
				if container, ok := containers[0].(map[string]interface{}); ok {
					if container["name"] != "application" {
						t.Errorf("expected container name to be 'application', got %v", container["name"])
					}
					if container["image"] != "myapp:2.0.0" {
						t.Errorf("expected image to be updated to 'myapp:2.0.0', got %v", container["image"])
					}
				}
				// Verify init containers are filtered out
				initContainers, exist, err := unstructured.NestedSlice(actual.Object, "spec", "initContainers")
				if err != nil {
					t.Fatal(err)
				}
				if len(initContainers) != 0 {
					t.Errorf("expected initContainers to be removed")
				}
				// Verify istio volumes are filtered out
				volumes, exist, err := unstructured.NestedSlice(actual.Object, "spec", "volumes")
				if err != nil {
					t.Fatal(err)
				}
				if !exist {
					t.Errorf("expected volumes to exist")
				}
				if len(volumes) != 1 {
					t.Errorf("expected 1 volume after filtering istio volumes, got %d", len(volumes))
				}
				_, exist, err = unstructured.NestedString(actual.Object, "metadata", "annotations", "prometheus.io/scrape")
				if err != nil {
					t.Fatal(err)
				}
				if exist {
					t.Errorf("expected prometheus.io/scrape annotation to be removed")
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
				".spec.containers[] | select(.name != \"application\")",
				".spec.volumes[]? | select(.name | startswith(\"istio-\"))",
				".spec.initContainers[]?",
			},
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
			dynamicClient.Fake.ReactionChain = append(reactors, dynamicClient.Fake.ReactionChain...)

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
