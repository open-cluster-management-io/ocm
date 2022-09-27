package workbuilder

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/common/workapplier"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeData(size int) string {
	if size <= 0 {
		return ""
	}

	s := make([]byte, size)
	for i := 0; i < size; i++ {
		s[i] = 'a'
	}
	return string(s)
}

func newFakeDeployment(name, namespace string, size int) runtime.Object {
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"data": newFakeData(size),
			},
		},
		Spec: appsv1.DeploymentSpec{},
	}
	return deploy
}

func newFakeRole(name, namespace string, size int) runtime.Object {
	role := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"data": newFakeData(size),
			},
		},
		Rules: nil,
	}
	return role
}

func newFakeCRD(name string, size int) runtime.Object {
	crd := &v1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"data": newFakeData(size),
			},
		},
		Spec: v1.CustomResourceDefinitionSpec{},
	}

	return crd
}

func newFakeCR(name, namespace string, size int) runtime.Object {
	cr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "test/v1",
			"kind":       "Test",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
				"annotation": map[string]interface{}{
					"data": newFakeData(size),
				},
			},
		},
	}
	return cr
}

func newFakeManifest(object runtime.Object) workapiv1.Manifest {
	manifest, err := buildManifest(object)
	if err != nil {
		panic(err)
	}
	return manifest
}

func newFakeWorkClient(works []workapiv1.ManifestWork) client.Client {
	s := scheme.Scheme
	s.AddKnownTypes(workapiv1.GroupVersion, &workapiv1.ManifestWork{}, &workapiv1.ManifestWorkList{})
	var objects []runtime.Object
	for i := 0; i < len(works); i++ {
		objects = append(objects, &works[i])
	}
	return fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objects...).Build()

}

func generateManifestWorkObjectMeta(index int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      fmt.Sprintf("test-work-%v", index),
		Namespace: "cluster1",
		Labels: map[string]string{
			"app": "test-app",
		},
	}
}

func Test_Build(t *testing.T) {
	cases := []struct {
		name                 string
		manifestLimit        int
		generateManifestWork GenerateManifestWorkObjectMeta
		newObjects           func() []runtime.Object
		options              []WorkBuilderOption
		validateWorks        func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error)
	}{
		{
			name:                 "created 1 work without options",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeCRD("test2", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
				}
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 1, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 5, len(appliedWorks[0].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "created 2 works without options",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 5*1024),
					newFakeCRD("test2", 10*1024),
					newFakeCRD("test3", 20*1024),
					newFakeCRD("test4", 11*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
				}
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 2, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 2, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, "test-work-1", appliedWorks[1].Name)
				assert.Equal(t, 5, len(appliedWorks[1].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "created 3 works with options",
			manifestLimit:        30 * 1024,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 5*1024),
					newFakeCRD("test2", 7*1024),
					newFakeCRD("test3", 20*1024),
					newFakeCRD("test4", 15*1024),
					newFakeDeployment("test", "test", 8*1024),
					newFakeRole("test", "test", 6*1024),
					newFakeCR("test", "test", 1*1024),
				}
			},
			options: []WorkBuilderOption{
				DeletionOption(&workapiv1.DeleteOption{
					PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground,
				}),
				ManifestConfigOption([]workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "app",
							Resource:  "deployments",
							Name:      "test",
							Namespace: "test",
						},
						FeedbackRules: nil,
					},
				}),
				ManifestWorkExecutorOption(&workapiv1.ManifestWorkExecutor{Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
				}}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 3, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 1, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[0].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 1, len(appliedWorks[0].Spec.ManifestConfigs))
				assert.Equal(t, workapiv1.ExecutorSubjectTypeServiceAccount, appliedWorks[0].Spec.Executor.Subject.Type)

				assert.Equal(t, "test-work-1", appliedWorks[1].Name)
				assert.Equal(t, 2, len(appliedWorks[1].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[1].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 1, len(appliedWorks[1].Spec.ManifestConfigs))
				assert.Equal(t, workapiv1.ExecutorSubjectTypeServiceAccount, appliedWorks[1].Spec.Executor.Subject.Type)

				assert.Equal(t, "test-work-2", appliedWorks[2].Name)
				assert.Equal(t, 4, len(appliedWorks[2].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[2].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 1, len(appliedWorks[2].Spec.ManifestConfigs))
				assert.Equal(t, workapiv1.ExecutorSubjectTypeServiceAccount, appliedWorks[2].Spec.Executor.Subject.Type)
			},
		},
		{
			name:                 "update manifest of existing work",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					// update the size of manifest
					newFakeCRD("test2", 11*1024),
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 1, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 5, len(appliedWorks[0].Spec.Workload.Manifests))
				// the size of manifest is updated
				assert.Equal(t, true, appliedWorks[0].Spec.Workload.Manifests[1].Size() > 11*1024)
			},
		},
		{
			name:                 "delete manifests in the existing work",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					// delete 1 manifest in work0, 4 manifests in work1
					newFakeCRD("test1", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeDeployment("test1", "test1", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(1),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test3", 10*1024)),
									newFakeManifest(newFakeCRD("test4", 10*1024)),
									newFakeManifest(newFakeDeployment("test1", "test1", 1*1024)),
									newFakeManifest(newFakeRole("test1", "test1", 1*1024)),
									newFakeManifest(newFakeCR("test1", "test1", 1*1024)),
								},
							},
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 2, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 4, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, "test-work-1", appliedWorks[1].Name)
				assert.Equal(t, 1, len(appliedWorks[1].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "update manifests and delete empty existing work",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					// delete test2 crd in work0, delete test3 crd in work1
					newFakeCRD("test1", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(1),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test3", 10*1024)),
								},
							},
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 1, len(appliedWorks))
				assert.Equal(t, 1, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 4, len(appliedWorks[0].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "add manifest to existing works",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeCRD("test2", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					newFakeCRD("test3", 10*1024),
					// add CRD test4
					newFakeCRD("test4", 20*1024),
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(1),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test3", 10*1024)),
								},
							},
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 2, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 5, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, "test-work-1", appliedWorks[1].Name)
				assert.Equal(t, 2, len(appliedWorks[1].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "add manifests and work with options",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeCRD("test2", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					newFakeCRD("test3", 10*1024),
					// add
					newFakeCRD("test4", 20*1024),                 // will be added to work1
					newFakeDeployment("test2", "test2", 10*1024), // will be added to work0
					newFakeCR("test2", "test2", 10*1024),         // will be added to a new work work2
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							DeleteOption: &workapiv1.DeleteOption{
								PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
							},
							ManifestConfigs: []workapiv1.ManifestConfigOption{
								{
									ResourceIdentifier: workapiv1.ResourceIdentifier{
										Group:     "apps",
										Resource:  "deployment",
										Name:      "test",
										Namespace: "test",
									},
								},
							},
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(1),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test3", 10*1024)),
								},
							},
						},
					},
				}),
				DeletionOption(&workapiv1.DeleteOption{
					PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground,
				},
				),
				ManifestConfigOption([]workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployment",
							Name:      "test",
							Namespace: "test",
						},
					},
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "test",
							Resource:  "test",
							Name:      "test",
							Namespace: "test",
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 3, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-2", appliedWorks[0].Name)
				assert.Equal(t, 1, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[0].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(appliedWorks[0].Spec.ManifestConfigs))
				assert.Equal(t, "test-work-0", appliedWorks[1].Name)
				assert.Equal(t, 6, len(appliedWorks[1].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[1].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(appliedWorks[1].Spec.ManifestConfigs))
				assert.Equal(t, "test-work-1", appliedWorks[2].Name)
				assert.Equal(t, 2, len(appliedWorks[2].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, appliedWorks[2].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(appliedWorks[2].Spec.ManifestConfigs))
			},
		},
		{
			name:                 "delete and add manifests",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					// delete CRD test1, test3
					newFakeCRD("test2", 10*1024),
					newFakeCRD("test4", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					newFakeCR("test1", "test1", 1*1024),
					// add
					newFakeCRD("test5", 21*1024),                 // will be added to work1
					newFakeDeployment("test2", "test2", 20*1024), // will be added to work2
					newFakeCR("test2", "test2", 10*1024),         // will be added to work0
				}
			},
			options: []WorkBuilderOption{
				ExistingManifestWorksOption([]workapiv1.ManifestWork{
					{
						ObjectMeta: generateManifestWorkObjectMeta(0),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test1", 10*1024)),
									newFakeManifest(newFakeCRD("test2", 10*1024)),
									newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
									newFakeManifest(newFakeRole("test", "test", 1*1024)),
									newFakeManifest(newFakeCR("test", "test", 1*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(1),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test3", 10*1024)),
								},
							},
						},
					},
					{
						ObjectMeta: generateManifestWorkObjectMeta(2),
						Spec: workapiv1.ManifestWorkSpec{
							Workload: workapiv1.ManifestsTemplate{
								Manifests: []workapiv1.Manifest{
									newFakeManifest(newFakeCRD("test4", 10*1024)),
									newFakeManifest(newFakeCR("test1", "test1", 1*1024)),
								},
							},
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
				assert.NoError(t, err)
				assert.Equal(t, 3, len(appliedWorks))
				assert.Equal(t, 0, len(deletedWorks))
				assert.Equal(t, "test-work-0", appliedWorks[0].Name)
				assert.Equal(t, 5, len(appliedWorks[0].Spec.Workload.Manifests))
				assert.Equal(t, "test-work-1", appliedWorks[1].Name)
				assert.Equal(t, 1, len(appliedWorks[1].Spec.Workload.Manifests))
				assert.Equal(t, "test-work-2", appliedWorks[2].Name)
				assert.Equal(t, 3, len(appliedWorks[2].Spec.Workload.Manifests))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testWorkBuilder := NewWorkBuilder().WithManifestsLimit(c.manifestLimit)
			appliedWorks, deletedWorks, err := testWorkBuilder.Build(c.newObjects(),
				c.generateManifestWork, c.options...)
			c.validateWorks(t, appliedWorks, deletedWorks, err)
		})
	}
}

func Test_BuildAndApply(t *testing.T) {
	cases := []struct {
		name                     string
		manifestLimit            int
		generateManifestWork     GenerateManifestWorkObjectMeta
		getExistingManifestWorks func() []workapiv1.ManifestWork
		newObjects               func() []runtime.Object
		options                  []WorkBuilderOption
		validateWorks            func(*testing.T, client.Client, error)
	}{
		{
			name:                 "delete work",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			getExistingManifestWorks: func() []workapiv1.ManifestWork {
				var works []workapiv1.ManifestWork
				work0 := workapiv1.ManifestWork{
					ObjectMeta: generateManifestWorkObjectMeta(0),
				}
				work0.Spec.Workload.Manifests = append(work0.Spec.Workload.Manifests,
					newFakeManifest(newFakeCRD("test1", 10*1024)),
					newFakeManifest(newFakeCRD("test2", 10*1024)),
					newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
					newFakeManifest(newFakeRole("test", "test", 1*1024)),
					newFakeManifest(newFakeCR("test", "test", 1*1024)),
				)
				work1 := workapiv1.ManifestWork{
					ObjectMeta: generateManifestWorkObjectMeta(1),
				}
				work1.Spec.Workload.Manifests = append(work1.Spec.Workload.Manifests,
					newFakeManifest(newFakeCRD("test3", 10*1024)),
				)

				works = append(works, work0, work1)
				return works
			},
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					// delete test2 crd, test3 crd
				}
			},
			validateWorks: func(t *testing.T, c client.Client, err error) {
				assert.NoError(t, err)
				works := &workapiv1.ManifestWorkList{}
				rstErr := c.List(context.TODO(), works)
				assert.NoError(t, rstErr)
				assert.Equal(t, 1, len(works.Items))
				assert.Equal(t, "test-work-0", works.Items[0].Name)
				assert.Equal(t, 4, len(works.Items[0].Spec.Workload.Manifests))
			},
		},
		{
			name:                 "add manifests and work",
			manifestLimit:        DefaultManifestLimit,
			generateManifestWork: generateManifestWorkObjectMeta,
			getExistingManifestWorks: func() []workapiv1.ManifestWork {
				var works []workapiv1.ManifestWork
				work0 := workapiv1.ManifestWork{
					ObjectMeta: generateManifestWorkObjectMeta(0),
					Spec: workapiv1.ManifestWorkSpec{
						DeleteOption: &workapiv1.DeleteOption{
							PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
						},
						ManifestConfigs: []workapiv1.ManifestConfigOption{
							{
								ResourceIdentifier: workapiv1.ResourceIdentifier{
									Group:     "apps",
									Resource:  "deployment",
									Name:      "test",
									Namespace: "test",
								},
							},
						},
					},
				}
				work0.Spec.Workload.Manifests = append(work0.Spec.Workload.Manifests,
					newFakeManifest(newFakeCRD("test1", 10*1024)),
					newFakeManifest(newFakeCRD("test2", 10*1024)),
					newFakeManifest(newFakeDeployment("test", "test", 1*1024)),
					newFakeManifest(newFakeRole("test", "test", 1*1024)),
					newFakeManifest(newFakeCR("test", "test", 1*1024)),
				)
				work1 := workapiv1.ManifestWork{
					ObjectMeta: generateManifestWorkObjectMeta(1),
				}
				work1.Spec.Workload.Manifests = append(work1.Spec.Workload.Manifests,
					newFakeManifest(newFakeCRD("test3", 10*1024)),
				)

				works = append(works, work0, work1)
				return works
			},
			newObjects: func() []runtime.Object {
				return []runtime.Object{
					newFakeCRD("test1", 10*1024),
					newFakeCRD("test2", 10*1024),
					newFakeCRD("test3", 10*1024),
					newFakeDeployment("test", "test", 1*1024),
					newFakeRole("test", "test", 1*1024),
					newFakeCR("test", "test", 1*1024),
					// add
					newFakeCRD("test4", 20*1024),                 // will be added to work1
					newFakeDeployment("test2", "test2", 10*1024), // will be added to work0
					newFakeCR("test2", "test2", 10*1024),         // will be added to a new work work2
				}
			},
			options: []WorkBuilderOption{
				DeletionOption(&workapiv1.DeleteOption{
					PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground,
				},
				),
				ManifestConfigOption([]workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployment",
							Name:      "test",
							Namespace: "test",
						},
					},
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "test",
							Resource:  "test",
							Name:      "test",
							Namespace: "test",
						},
					},
				}),
			},
			validateWorks: func(t *testing.T, c client.Client, err error) {
				assert.NoError(t, err)
				works := &workapiv1.ManifestWorkList{}
				rstErr := c.List(context.TODO(), works)
				assert.NoError(t, rstErr)
				assert.Equal(t, 3, len(works.Items))
				assert.Equal(t, "test-work-0", works.Items[0].Name)
				assert.Equal(t, 6, len(works.Items[0].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, works.Items[0].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(works.Items[0].Spec.ManifestConfigs))
				assert.Equal(t, "test-work-1", works.Items[1].Name)
				assert.Equal(t, 2, len(works.Items[1].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, works.Items[1].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(works.Items[1].Spec.ManifestConfigs))
				assert.Equal(t, "test-work-2", works.Items[2].Name)
				assert.Equal(t, 1, len(works.Items[2].Spec.Workload.Manifests))
				assert.Equal(t, workapiv1.DeletePropagationPolicyTypeForeground, works.Items[2].Spec.DeleteOption.PropagationPolicy)
				assert.Equal(t, 2, len(works.Items[2].Spec.ManifestConfigs))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			existingWorks := c.getExistingManifestWorks()
			options := []WorkBuilderOption{
				ExistingManifestWorksOption(existingWorks),
			}
			options = append(options, c.options...)
			workClient := newFakeWorkClient(existingWorks)
			testWorkBuilder := NewWorkBuilder().WithManifestsLimit(c.manifestLimit)
			workApplier := workapplier.NewWorkApplierWithRuntimeClient(workClient)
			err := testWorkBuilder.BuildAndApply(context.TODO(), c.newObjects(),
				c.generateManifestWork, workApplier, options...)
			c.validateWorks(t, workClient, err)
		})
	}
}
