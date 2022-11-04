package addonfactory

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func TestGetAddOnDeloymentConfigValues(t *testing.T) {
	cases := []struct {
		name           string
		toValuesFuncs  []AddOnDeloymentConfigToValuesFunc
		addOnObjs      []runtime.Object
		expectedValues Values
	}{
		{
			name: "no addon deloyment configs",
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "config.test",
								Resource: "testconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "testConfig",
							},
						},
					}
					return addon
				}(),
			},
			expectedValues: Values{},
		},
		{
			name:          "mutiple addon deployment configs",
			toValuesFuncs: []AddOnDeloymentConfigToValuesFunc{ToAddOnDeloymentConfigValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config1",
							},
						},
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config2",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config1",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{Name: "Test", Value: "test1"},
						},
					},
				},
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config2",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{Name: "Test", Value: "test2"},
						},
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: map[string]string{"test": "test"},
						},
					},
				},
			},
			expectedValues: Values{
				"Test":         "test2",
				"NodeSelector": map[string]string{"test": "test"},
				"Tolerations":  []corev1.Toleration{},
			},
		},
		{
			name:          "to addon node placement",
			toValuesFuncs: []AddOnDeloymentConfigToValuesFunc{ToAddOnNodePlacementValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: nodeSelector,
							Tolerations:  tolerations,
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{"nodeSelector": map[string]interface{}{"kubernetes.io/os": "linux"}},
				"tolerations": []interface{}{
					map[string]interface{}{"effect": "NoExecute", "key": "foo", "operator": "Exists"},
				},
			},
		},
		{
			name:          "multiple toValuesFuncs",
			toValuesFuncs: []AddOnDeloymentConfigToValuesFunc{ToAddOnNodePlacementValues, ToAddOnCustomizedVariableValues},
			addOnObjs: []runtime.Object{
				func() *addonapiv1alpha1.ManagedClusterAddOn {
					addon := addontesting.NewAddon("test", "cluster1")
					addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
						{
							ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
								Group:    "addon.open-cluster-management.io",
								Resource: "addondeploymentconfigs",
							},
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "config",
							},
						},
					}
					return addon
				}(),
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							NodeSelector: nodeSelector,
							Tolerations:  tolerations,
						},
						CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
							{
								Name:  "managedKubeConfigSecret",
								Value: "external-managed-kubeconfig",
							},
						},
					},
				},
			},
			expectedValues: Values{
				"global": map[string]interface{}{"nodeSelector": map[string]interface{}{"kubernetes.io/os": "linux"}},
				"tolerations": []interface{}{
					map[string]interface{}{"effect": "NoExecute", "key": "foo", "operator": "Exists"},
				},
				"managedKubeConfigSecret": "external-managed-kubeconfig",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addOnObjs...)

			getter := NewAddOnDeloymentConfigGetter(fakeAddonClient)

			addOn, ok := c.addOnObjs[0].(*addonapiv1alpha1.ManagedClusterAddOn)
			if !ok {
				t.Errorf("expected addon object, but failed")
			}

			values, err := GetAddOnDeloymentConfigValues(getter, c.toValuesFuncs...)(nil, addOn)
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}

			if !equality.Semantic.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, values)
			}
		})
	}
}
