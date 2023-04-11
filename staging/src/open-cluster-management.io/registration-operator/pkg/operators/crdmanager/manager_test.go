/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package crdmanager

import (
	"context"
	"encoding/json"
	"fmt"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	clienttesting "k8s.io/client-go/testing"
	testinghelpers "open-cluster-management.io/registration-operator/pkg/helpers/testing"
	"strconv"
	"testing"
)

func TestApplyV1CRD(t *testing.T) {
	cases := []struct {
		name           string
		desiredVersion string
		requiredCRDs   []runtime.Object
		existingCRDs   []runtime.Object
		verify         func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:           "create crd",
			desiredVersion: "v0.9.0",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "create")
			},
		},
		{
			name:           "update crd",
			desiredVersion: "v0.9.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "v0.8.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "update")
				obj := actions[1].(clienttesting.UpdateActionImpl).Object
				assertCRDVersion(t, obj, "0.9.0-16-g889bd8b")
			},
		},
		{
			name:           "update crd from none",
			desiredVersion: "v0.9.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "update")
				obj := actions[1].(clienttesting.UpdateActionImpl).Object
				assertCRDVersion(t, obj, "0.9.0-16-g889bd8b")
			},
		},
		{
			name:           "noop crd",
			desiredVersion: "v0.8.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "v0.9.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "get")
			},
		},
		{
			name:           "crd version equals",
			desiredVersion: "0.0.0",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "0.0.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fakeapiextensions.NewSimpleClientset(c.existingCRDs...)
			manager := NewManager[*apiextensionsv1.CustomResourceDefinition](client.ApiextensionsV1().CustomResourceDefinitions(), EqualV1)
			v, _ := versionutil.ParseSemantic(c.desiredVersion)
			manager.version = v
			var indices []string
			for i := range c.requiredCRDs {
				indices = append(indices, fmt.Sprintf("%d", i))
			}
			err := manager.Apply(context.TODO(), func(index string) ([]byte, error) {
				i, _ := strconv.Atoi(index)
				return json.Marshal(c.requiredCRDs[i])
			}, indices...)

			if err != nil {
				t.Errorf("apply error: %v", err)
			}

			c.verify(t, client.Actions())
		})
	}
}

func TestApplyV1Beta1CRD(t *testing.T) {
	cases := []struct {
		name           string
		desiredVersion string
		requiredCRDs   []runtime.Object
		existingCRDs   []runtime.Object
		verify         func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:           "create crd",
			desiredVersion: "v0.9.0",
			requiredCRDs:   []runtime.Object{newV1Beta1CRD("foo", "")},
			existingCRDs:   []runtime.Object{},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "create")
			},
		},
		{
			name:           "update crd",
			desiredVersion: "v0.9.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1Beta1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1Beta1CRD("foo", "v0.8.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "update")
				obj := actions[1].(clienttesting.UpdateActionImpl).Object
				assertCRDVersion(t, obj, "0.9.0-16-g889bd8b")
			},
		},
		{
			name:           "update crd from none",
			desiredVersion: "v0.9.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1Beta1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1Beta1CRD("foo", "")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "update")
				obj := actions[1].(clienttesting.UpdateActionImpl).Object
				assertCRDVersion(t, obj, "0.9.0-16-g889bd8b")
			},
		},
		{
			name:           "noop crd",
			desiredVersion: "v0.8.0-16-g889bd8b",
			requiredCRDs:   []runtime.Object{newV1Beta1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1Beta1CRD("foo", "v0.9.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fakeapiextensions.NewSimpleClientset(c.existingCRDs...)
			manager := NewManager[*apiextensionsv1beta1.CustomResourceDefinition](client.ApiextensionsV1beta1().CustomResourceDefinitions(), EqualV1Beta1)
			v, _ := versionutil.ParseSemantic(c.desiredVersion)
			manager.version = v
			var indices []string
			for i := range c.requiredCRDs {
				indices = append(indices, fmt.Sprintf("%d", i))
			}
			err := manager.Apply(context.TODO(), func(index string) ([]byte, error) {
				i, _ := strconv.Atoi(index)
				return json.Marshal(c.requiredCRDs[i])
			}, indices...)

			if err != nil {
				t.Errorf("apply error: %v", err)
			}

			c.verify(t, client.Actions())
		})
	}
}

func TestClean(t *testing.T) {
	cases := []struct {
		name           string
		desiredVersion string
		skip           bool
		expectErr      bool
		requiredCRDs   []runtime.Object
		existingCRDs   []runtime.Object
		verify         func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:           "delete crd",
			desiredVersion: "v0.9.0",
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "delete")
			},
		},
		{
			name:           "delete existing crd",
			desiredVersion: "v0.9.0",
			expectErr:      true,
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "delete")
			},
		},
		{
			name:           "skip delete existing crd",
			desiredVersion: "v0.9.0",
			skip:           true,
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "0.9.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[1], "update")
				obj := actions[1].(clienttesting.UpdateActionImpl).Object
				accessor, _ := meta.Accessor(obj)
				if len(accessor.GetAnnotations()) != 0 {
					t.Errorf("annotation should be cleaned")
				}
			},
		},
		{
			name:           "skip delete existing crd not owned",
			desiredVersion: "v0.9.0",
			skip:           true,
			requiredCRDs:   []runtime.Object{newV1CRD("foo", "")},
			existingCRDs:   []runtime.Object{newV1CRD("foo", "0.10.0")},
			verify: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("actions are not expected: %v", actions)
				}
				testinghelpers.AssertAction(t, actions[0], "get")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fakeapiextensions.NewSimpleClientset(c.existingCRDs...)
			manager := NewManager[*apiextensionsv1.CustomResourceDefinition](client.ApiextensionsV1().CustomResourceDefinitions(), EqualV1)
			v, _ := versionutil.ParseSemantic(c.desiredVersion)
			manager.version = v
			var indices []string
			for i := range c.requiredCRDs {
				indices = append(indices, fmt.Sprintf("%d", i))
			}
			err := manager.Clean(context.TODO(), c.skip, func(index string) ([]byte, error) {
				i, _ := strconv.Atoi(index)
				return json.Marshal(c.requiredCRDs[i])
			}, indices...)

			if c.expectErr && err == nil {
				t.Errorf("should have err")
			}
			if !c.expectErr && err != nil {
				t.Errorf("apply error: %v", err)
			}

			c.verify(t, client.Actions())
		})
	}
}

func newV1Beta1CRD(name, version string) *apiextensionsv1beta1.CustomResourceDefinition {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1beta1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if len(version) > 0 {
		crd.Annotations = map[string]string{versionAnnotationKey: version}
	}
	return crd
}

func newV1CRD(name, version string) *apiextensionsv1.CustomResourceDefinition {
	crd := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.NoneConverter,
			},
		},
	}

	if len(version) > 0 {
		crd.Annotations = map[string]string{versionAnnotationKey: version}
	}
	return crd
}

func assertCRDVersion(t *testing.T, obj interface{}, version string) {
	accessor, _ := meta.Accessor(obj)
	annotation := accessor.GetAnnotations()
	if len(annotation) == 0 {
		t.Fatalf("Expect a version annotation but got none")
	}
	if annotation[versionAnnotationKey] != version {
		t.Errorf("Expect version %s, but got %s", version, annotation[versionAnnotationKey])
	}
}
