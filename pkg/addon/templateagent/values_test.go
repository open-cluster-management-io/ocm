package templateagent

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/ktesting"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestGetAddOnRegistriesPrivateValuesFromClusterAnnotation(t *testing.T) {
	cases := []struct {
		name           string
		cluster        *clusterv1.ManagedCluster
		expectedValues addonfactory.Values
		expectedError  string
	}{
		{
			name:           "no values",
			cluster:        &clusterv1.ManagedCluster{},
			expectedValues: addonfactory.Values{},
		},
		{
			name: "not expected annotation",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries-test":[{"mirror-test":"quay.io/ocm","source-test":"quay-test.io/ocm"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{},
		},
		{
			name: "annotation invalid",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":`,
					},
				},
			},
			expectedValues: addonfactory.Values{},
			expectedError:  "unexpected end of JSON input",
		},
		{
			name: "override registries",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"quay.io/ocm","source":"quay-test.io/ocm"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{
				RegistriesPrivateValueKey: []addonapiv1alpha1.ImageMirror{
					{
						Source: "quay-test.io/ocm",
						Mirror: "quay.io/ocm",
					},
				},
			},
		},
		{
			name: "override image",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						clusterv1.ClusterImageRegistriesAnnotationKey: `{"registries":[{"mirror":"quay.io/ocm/test","source":"quay.io/open-cluster-management/test"}]}`,
					},
				},
			},
			expectedValues: addonfactory.Values{
				RegistriesPrivateValueKey: []addonapiv1alpha1.ImageMirror{
					{
						Source: "quay.io/open-cluster-management/test",
						Mirror: "quay.io/ocm/test",
					},
				},
			},
		},
	}

	for _, c := range cases {
		logger, _ := ktesting.NewTestContext(t)
		t.Run(c.name, func(t *testing.T) {
			values, err := GetAddOnRegistriesPrivateValuesFromClusterAnnotation(logger, c.cluster, nil)
			if err != nil || len(c.expectedError) > 0 {
				assert.ErrorContains(t, err, c.expectedError, "expected error: %v, got: %v", c.expectedError, err)
			}

			if !reflect.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values: %v, got: %v", c.expectedValues, values)
			}
		})
	}
}

func TestGetValues(t *testing.T) {
	cases := []struct {
		name             string
		templateSpec     addonapiv1alpha1.AddOnTemplateSpec
		values           addonfactory.Values
		expectedPreset   orderedValues
		expectedOverride map[string]interface{}
		expectedPrivate  map[string]interface{}
	}{
		{
			name: "with default value, registration set in template",
			templateSpec: addonapiv1alpha1.AddOnTemplateSpec{
				Registration: []addonapiv1alpha1.RegistrationSpec{
					{
						Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					},
				},
			},
			expectedPreset: orderedValues{
				{
					name:  "HUB_KUBECONFIG",
					value: "/managed/hub-kubeconfig/kubeconfig",
				},
				{
					name:  "CLUSTER_NAME",
					value: "test-cluster",
				},
			},
			expectedOverride: map[string]interface{}{
				"CLUSTER_NAME":   "test-cluster",
				"HUB_KUBECONFIG": "/managed/hub-kubeconfig/kubeconfig",
			},
			expectedPrivate: map[string]interface{}{},
		},
		{
			name:         "without default value, registration not set in template",
			templateSpec: addonapiv1alpha1.AddOnTemplateSpec{},
			values: addonfactory.Values{
				InstallNamespacePrivateValueKey: "default-ns",
			},
			expectedPreset: orderedValues{
				{
					name:  "CLUSTER_NAME",
					value: "test-cluster",
				},
				{
					name:  "INSTALL_NAMESPACE",
					value: "default-ns",
				},
			},
			expectedOverride: map[string]interface{}{
				"CLUSTER_NAME":      "test-cluster",
				"INSTALL_NAMESPACE": "default-ns",
			},
			expectedPrivate: map[string]interface{}{
				InstallNamespacePrivateValueKey: "default-ns",
			},
		},
		{
			name:         "with private and user defined values",
			templateSpec: addonapiv1alpha1.AddOnTemplateSpec{},
			values: addonfactory.Values{
				InstallNamespacePrivateValueKey: "default-ns",
				"key1":                          "value1",
			},
			expectedPreset: orderedValues{
				{
					name:  "CLUSTER_NAME",
					value: "test-cluster",
				},
				{
					name:  "INSTALL_NAMESPACE",
					value: "default-ns",
				},
			},
			expectedOverride: map[string]interface{}{
				"CLUSTER_NAME":      "test-cluster",
				"INSTALL_NAMESPACE": "default-ns",
				"key1":              "value1",
			},
			expectedPrivate: map[string]interface{}{
				InstallNamespacePrivateValueKey: "default-ns",
			},
		},
		{
			name: "default value should be overridden",
			templateSpec: addonapiv1alpha1.AddOnTemplateSpec{
				Registration: []addonapiv1alpha1.RegistrationSpec{
					{
						Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					},
				},
			},
			values: addonfactory.Values{
				"HUB_KUBECONFIG": "/managed/hub-kubeconfig/kubeconfig-test",
			},
			expectedPreset: orderedValues{
				{
					name:  "HUB_KUBECONFIG",
					value: "/managed/hub-kubeconfig/kubeconfig-test",
				},
				{
					name:  "CLUSTER_NAME",
					value: "test-cluster",
				},
			},
			expectedOverride: map[string]interface{}{
				"HUB_KUBECONFIG": "/managed/hub-kubeconfig/kubeconfig-test",
				"CLUSTER_NAME":   "test-cluster",
			},
			expectedPrivate: map[string]interface{}{},
		},
		{
			name: "builtIn value should not be overridden",
			templateSpec: addonapiv1alpha1.AddOnTemplateSpec{
				Registration: []addonapiv1alpha1.RegistrationSpec{
					{
						Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					},
				},
			},
			values: addonfactory.Values{
				InstallNamespacePrivateValueKey: "default-ns",
				"HUB_KUBECONFIG":                "/managed/hub-kubeconfig/kubeconfig-test",
				"CLUSTER_NAME":                  "cluster1",
			},
			expectedPreset: orderedValues{
				{
					name:  "HUB_KUBECONFIG",
					value: "/managed/hub-kubeconfig/kubeconfig-test",
				},
				{
					name:  "CLUSTER_NAME",
					value: "test-cluster",
				},
				{
					name:  "INSTALL_NAMESPACE",
					value: "default-ns",
				},
			},
			expectedOverride: map[string]interface{}{
				"HUB_KUBECONFIG":    "/managed/hub-kubeconfig/kubeconfig-test",
				"CLUSTER_NAME":      "test-cluster",
				"INSTALL_NAMESPACE": "default-ns",
			},
			expectedPrivate: map[string]interface{}{
				InstallNamespacePrivateValueKey: "default-ns",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			getValueFunc := func(
				cluster *clusterv1.ManagedCluster,
				addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
				return c.values, nil
			}

			agentAddon := &CRDTemplateAgentAddon{
				logger:         klog.FromContext(context.TODO()),
				getValuesFuncs: []addonfactory.GetValuesFunc{getValueFunc},
			}

			cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}
			addon := &addonapiv1alpha1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{
				Name:      "test-addon",
				Namespace: "test-cluster",
			}}

			addonTemplate := &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon"},
				Spec:       c.templateSpec,
			}

			presetValues, overrideValues, privateValues, err := agentAddon.getValues(cluster, addon, addonTemplate)
			if err != nil {
				t.Fatal(err)
			}

			if !orderedValueEquals(presetValues, c.expectedPreset) {
				t.Errorf("preset value is not correct, expect %v, got %v", c.expectedPreset, presetValues)
			}

			if !apiequality.Semantic.DeepEqual(overrideValues, c.expectedOverride) {
				t.Errorf("override value is not correct, expect %v, got %v", c.expectedOverride, overrideValues)
			}

			if !apiequality.Semantic.DeepEqual(privateValues, c.expectedPrivate) {
				t.Errorf("builtin value is not correct, expect %v, got %v", c.expectedPrivate, privateValues)
			}
		})
	}
}

func orderedValueEquals(new, old orderedValues) bool {
	if len(new) != len(old) {
		return false
	}
	for index := range new {
		if new[index] != old[index] {
			return false
		}
	}
	return true
}
