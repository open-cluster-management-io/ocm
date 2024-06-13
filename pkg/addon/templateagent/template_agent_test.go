package templateagent

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/ktesting"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestAddonTemplateAgentManifests(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)
	addonName := "hello"
	clusterName := "cluster1"

	data, err := os.ReadFile("./testmanifests/addontemplate.yaml")
	if err != nil {
		t.Errorf("error reading file: %v", err)
	}

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1apha1.Install(s)
	_ = addonapiv1alpha1.Install(s)

	addonTemplate := &addonapiv1alpha1.AddOnTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hello-template",
		},
	}

	decoder := serializer.NewCodecFactory(s).UniversalDeserializer()
	_, _, err = decoder.Decode(data, nil, addonTemplate)
	if err != nil {
		t.Errorf("error decoding file: %v", err)
	}

	cases := []struct {
		name                       string
		addonTemplate              *addonapiv1alpha1.AddOnTemplate
		addonDeploymentConfig      *addonapiv1alpha1.AddOnDeploymentConfig
		managedClusterAddonBuilder *testManagedClusterAddOnBuilder
		managedCluster             *clusterv1.ManagedCluster
		expectedErr                string
		validateObjects            func(t *testing.T, objects []runtime.Object)
	}{
		{
			name:          "no addon template",
			addonTemplate: nil,
			addonDeploymentConfig: &addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-config",
					Namespace: "default",
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
						{
							Name:  "LOG_LEVEL",
							Value: "4",
						},
					},
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			managedCluster: addonfactory.NewFakeManagedCluster(clusterName, "1.10.1"),
			expectedErr:    fmt.Sprintf("addon %s/%s template not found in status", clusterName, addonName),
		},
		{
			name:          "manifests rendered successfully",
			addonTemplate: addonTemplate,
			addonDeploymentConfig: &addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-config",
					Namespace: "default",
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: "test-install-namespace",
					CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
						{
							Name:  "LOG_LEVEL",
							Value: "4",
						},
					},
					NodePlacement: &addonapiv1alpha1.NodePlacement{
						NodeSelector: map[string]string{
							"host": "ssd",
						},
						Tolerations: []corev1.Toleration{
							{
								Key:      "foo",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoExecute,
							},
						},
					},
					Registries: []addonapiv1alpha1.ImageMirror{
						{
							Source: "quay.io/open-cluster-management",
							Mirror: "quay.io/ocm",
						},
					},
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			managedCluster: addonfactory.NewFakeManagedCluster(clusterName, "1.10.1"),
			validateObjects: func(t *testing.T, objects []runtime.Object) {
				if len(objects) != 4 {
					t.Errorf("expected 4 objects, but got %v", len(objects))
				}

				unstructObject, ok := objects[0].(*unstructured.Unstructured)
				if !ok {
					t.Errorf("expected object to be *appsv1.Deployment, but got %T", objects[0])
				}
				object, err := utils.ConvertToDeployment(unstructObject)
				if err != nil {
					t.Fatal(err)
				}
				image := object.Spec.Template.Spec.Containers[0].Image
				if image != "quay.io/ocm/addon-examples:v1" {
					t.Errorf("unexpected image %v", image)
				}

				if object.Namespace != "test-install-namespace" {
					t.Errorf("unexpected namespace %s", object.Namespace)
				}

				nodeSelector := object.Spec.Template.Spec.NodeSelector
				expectedNodeSelector := map[string]string{"host": "ssd"}
				if !equality.Semantic.DeepEqual(nodeSelector, expectedNodeSelector) {
					t.Errorf("unexpected nodeSelector %v", nodeSelector)
				}

				tolerations := object.Spec.Template.Spec.Tolerations
				expectedTolerations := []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
				if !equality.Semantic.DeepEqual(tolerations, expectedTolerations) {
					t.Errorf("unexpected tolerations %v", tolerations)
				}

				envs := object.Spec.Template.Spec.Containers[0].Env
				expectedEnvs := []corev1.EnvVar{
					{Name: "LOG_LEVEL", Value: "4"},
					{Name: "HUB_KUBECONFIG", Value: "/managed/hub-kubeconfig/kubeconfig"},
					{Name: "CLUSTER_NAME", Value: clusterName},
				}
				if !equality.Semantic.DeepEqual(envs, expectedEnvs) {
					t.Errorf("unexpected envs %v", envs)
				}

				volumes := object.Spec.Template.Spec.Volumes
				expectedVolumes := []corev1.Volume{
					{
						Name: "hub-kubeconfig",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "hello-hub-kubeconfig",
							},
						},
					},
					{
						Name: "cert-example-com-signer-name",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "hello-example.com-signer-name-client-cert",
							},
						},
					},
				}

				if !equality.Semantic.DeepEqual(volumes, expectedVolumes) {
					t.Errorf("expected volumes %v, but got: %v", expectedVolumes, volumes)
				}

				volumeMounts := object.Spec.Template.Spec.Containers[0].VolumeMounts
				expectedVolumeMounts := []corev1.VolumeMount{
					{
						Name:      "hub-kubeconfig",
						MountPath: "/managed/hub-kubeconfig",
					},
					{
						Name:      "cert-example-com-signer-name",
						MountPath: "/managed/example.com-signer-name",
					},
				}
				if !equality.Semantic.DeepEqual(volumeMounts, expectedVolumeMounts) {
					t.Errorf("expected volumeMounts %v, but got: %v", expectedVolumeMounts, volumeMounts)
				}

				// check clusterrole
				unstructObject, ok = objects[2].(*unstructured.Unstructured)
				if !ok {
					t.Errorf("expected object to be unstructured, but got %T", objects[0])
				}
				clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructObject.Object, clusterRoleBinding)
				if err != nil {
					t.Fatal(err)
				}
				if clusterRoleBinding.Subjects[0].Namespace != "test-install-namespace" {
					t.Errorf("clusterRolebinding namespace does not match, got %s", clusterRoleBinding.Subjects[0].Namespace)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var managedClusterAddon *addonapiv1alpha1.ManagedClusterAddOn
			var objs []runtime.Object
			if tc.managedClusterAddonBuilder != nil {
				if tc.addonTemplate != nil {
					tc.managedClusterAddonBuilder.withAddonTemplate(tc.addonTemplate)
					objs = append(objs, tc.addonTemplate)
				}
				if tc.addonDeploymentConfig != nil {
					tc.managedClusterAddonBuilder.withAddonDeploymentConfig(tc.addonDeploymentConfig)
					objs = append(objs, tc.addonDeploymentConfig)
				}
				managedClusterAddon = tc.managedClusterAddonBuilder.build()
				objs = append(objs, managedClusterAddon)
			}
			hubKubeClient := fakekube.NewSimpleClientset()
			addonClient := fakeaddon.NewSimpleClientset(objs...)

			addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
			if managedClusterAddon != nil {
				mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
				if err := mcaStore.Add(managedClusterAddon); err != nil {
					t.Fatal(err)
				}
			}
			if tc.addonTemplate != nil {
				atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
				if err := atStore.Add(tc.addonTemplate); err != nil {
					t.Fatal(err)
				}
			}
			kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute)

			agentAddon := NewCRDTemplateAgentAddon(
				ctx,
				addonName,
				hubKubeClient,
				addonClient,
				addonInformerFactory,
				kubeInformers.Rbac().V1().RoleBindings().Lister(),
				addonfactory.GetAddOnDeploymentConfigValues(
					utils.NewAddOnDeploymentConfigGetter(addonClient),
					addonfactory.ToAddOnCustomizedVariableValues,
					ToAddOnNodePlacementPrivateValues,
					ToAddOnRegistriesPrivateValues,
					ToAddOnInstallNamespacePrivateValues,
				),
			)

			objects, err := agentAddon.Manifests(tc.managedCluster, managedClusterAddon)
			if err != nil {
				assert.Equal(t, tc.expectedErr, err.Error(), tc.name)
			} else {
				tc.validateObjects(t, objects)
			}
		})
	}
}

func TestAgentInstallNamespace(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)
	addonName := "hello"
	clusterName := "cluster1"

	deployment := testingcommon.NewUnstructured("apps/v1", "Deployment", "test-ns", "test")
	deploymentRaw, _ := deployment.MarshalJSON()

	cases := []struct {
		name                       string
		addonTemplate              *addonapiv1alpha1.AddOnTemplate
		addonDeploymentConfig      *addonapiv1alpha1.AddOnDeploymentConfig
		managedClusterAddonBuilder *testManagedClusterAddOnBuilder

		expected    string
		expectedErr string
	}{
		{
			name: "install namespace is set",
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AgentSpec: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{},
						},
					},
				},
			},
			addonDeploymentConfig: &addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-config",
					Namespace: "default",
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: "test-install-namespace",
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			expected: "test-install-namespace",
		},
		{
			name: "not support addon deployment config",
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AgentSpec: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{},
						},
					},
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			expected: "open-cluster-management-agent-addon",
		},
		{
			name: "has deployment in the template",
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AgentSpec: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: deploymentRaw}},
							},
						},
					},
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			expected: "test-ns",
		},
		{
			name: "has deployment in the template, addon status config references is not set",
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AgentSpec: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: deploymentRaw}},
							},
						},
					},
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			).withSetStatusConfigReferences(false),
			expectedErr: fmt.Sprintf("addon %s template not found in status", addonName),
		},
		{
			name: "override deployment in the template",
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AgentSpec: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: []workapiv1.Manifest{
								{RawExtension: runtime.RawExtension{Raw: deploymentRaw}},
							},
						},
					},
				},
			},
			addonDeploymentConfig: &addonapiv1alpha1.AddOnDeploymentConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-config",
					Namespace: "default",
				},
				Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
					AgentInstallNamespace: "test-install-namespace",
				},
			},
			managedClusterAddonBuilder: newManagedClusterAddonBuilder(
				&addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addonName,
						Namespace: clusterName,
					},
				},
			),
			expected: "test-install-namespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var managedClusterAddon *addonapiv1alpha1.ManagedClusterAddOn
			var objs []runtime.Object
			if tc.managedClusterAddonBuilder != nil {
				if tc.addonTemplate != nil {
					tc.managedClusterAddonBuilder.withAddonTemplate(tc.addonTemplate)
					objs = append(objs, tc.addonTemplate)
				}
				if tc.addonDeploymentConfig != nil {
					tc.managedClusterAddonBuilder.withAddonDeploymentConfig(tc.addonDeploymentConfig)
					objs = append(objs, tc.addonDeploymentConfig)
				}
				managedClusterAddon = tc.managedClusterAddonBuilder.build()
				objs = append(objs, managedClusterAddon)
			}
			hubKubeClient := fakekube.NewSimpleClientset()
			addonClient := fakeaddon.NewSimpleClientset(objs...)

			addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
			if managedClusterAddon != nil {
				mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
				if err := mcaStore.Add(managedClusterAddon); err != nil {
					t.Fatal(err)
				}
			}
			if tc.addonTemplate != nil {
				atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
				if err := atStore.Add(tc.addonTemplate); err != nil {
					t.Fatal(err)
				}
			}
			kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute)

			agentAddon := NewCRDTemplateAgentAddon(
				ctx,
				addonName,
				hubKubeClient,
				addonClient,
				addonInformerFactory,
				kubeInformers.Rbac().V1().RoleBindings().Lister(),
				addonfactory.GetAddOnDeploymentConfigValues(
					utils.NewAddOnDeploymentConfigGetter(addonClient),
					addonfactory.ToAddOnCustomizedVariableValues,
					ToAddOnNodePlacementPrivateValues,
					ToAddOnRegistriesPrivateValues,
				),
			)

			ns, err := agentAddon.GetAgentAddonOptions().Registration.AgentInstallNamespace(managedClusterAddon)
			if err == nil {
				assert.Equal(t, tc.expected, ns, tc.name)
			} else if tc.expectedErr != err.Error() {
				t.Fatalf("Expected error %v, but got %v", tc.expectedErr, err)
			}

		})
	}
}

func TestAgentManifestConfigs(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)
	addonName := "hello"

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1apha1.Install(s)
	_ = addonapiv1alpha1.Install(s)

	cases := []struct {
		name                   string
		addonTemplate          *addonapiv1alpha1.AddOnTemplate
		clusterManagementAddOn *addonapiv1alpha1.ClusterManagementAddOn
		expected               []workapiv1.ManifestConfigOption
	}{
		{
			name:                   "no addontemplate in cma status",
			clusterManagementAddOn: addontesting.NewClusterManagementAddon("hello", "", "").Build(),
			expected:               nil,
		},
		{
			name: "no addontemplate",
			clusterManagementAddOn: addontesting.NewClusterManagementAddon("hello", "", "").
				WithDefaultConfigReferences(
					addonapiv1alpha1.DefaultConfigReference{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{Name: "hello-template"},
							SpecHash:       "hash",
						},
					},
				).Build(),
			expected: nil,
		},
		{
			name: "addontemplate does not have manifestconfigs",
			clusterManagementAddOn: addontesting.NewClusterManagementAddon("hello", "", "").
				WithDefaultConfigReferences(
					addonapiv1alpha1.DefaultConfigReference{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{Name: "hello-template"},
							SpecHash:       "hash",
						},
					},
				).Build(),
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
			},
			expected: nil,
		},
		{
			name: "addontemplate has manifestconfigs",
			clusterManagementAddOn: addontesting.NewClusterManagementAddon("hello", "", "").
				WithDefaultConfigReferences(
					addonapiv1alpha1.DefaultConfigReference{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							ConfigReferent: addonapiv1alpha1.ConfigReferent{Name: "hello-template"},
							SpecHash:       "hash",
						},
					},
				).Build(),
			addonTemplate: &addonapiv1alpha1.AddOnTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-template",
				},
				Spec: addonapiv1alpha1.AddOnTemplateSpec{
					AddonName: "hello",
					AgentSpec: workapiv1.ManifestWorkSpec{
						ManifestConfigs: []workapiv1.ManifestConfigOption{
							{
								ResourceIdentifier: workapiv1.ResourceIdentifier{
									Group:     "apps",
									Resource:  "deployment",
									Name:      "hello",
									Namespace: "default",
								},
								FeedbackRules: []workapiv1.FeedbackRule{
									{
										Type: workapiv1.WellKnownStatusType,
									},
								},
							},
						},
					},
				},
			},
			expected: []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployment",
						Name:      "hello",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		var objs []runtime.Object
		if tc.clusterManagementAddOn != nil {
			objs = append(objs, tc.clusterManagementAddOn)
		}
		if tc.addonTemplate != nil {
			objs = append(objs, tc.addonTemplate)
		}
		hubKubeClient := fakekube.NewSimpleClientset()
		addonClient := fakeaddon.NewSimpleClientset(objs...)

		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		if tc.clusterManagementAddOn != nil {
			cmaStore := addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore()
			if err := cmaStore.Add(tc.clusterManagementAddOn); err != nil {
				t.Fatal(err)
			}
		}
		if tc.addonTemplate != nil {
			atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
			if err := atStore.Add(tc.addonTemplate); err != nil {
				t.Fatal(err)
			}
		}
		kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute)

		agentAddon := NewCRDTemplateAgentAddon(
			ctx,
			addonName,
			hubKubeClient,
			addonClient,
			addonInformerFactory,
			kubeInformers.Rbac().V1().RoleBindings().Lister(),
			addonfactory.GetAddOnDeploymentConfigValues(
				utils.NewAddOnDeploymentConfigGetter(addonClient),
				addonfactory.ToAddOnCustomizedVariableValues,
				ToAddOnNodePlacementPrivateValues,
				ToAddOnRegistriesPrivateValues,
			),
		)

		agentManifestConfigs := agentAddon.GetAgentAddonOptions().ManifestConfigs
		assert.Equal(t, tc.expected, agentManifestConfigs)
	}
}

type testManagedClusterAddOnBuilder struct {
	managedClusterAddOn       *addonapiv1alpha1.ManagedClusterAddOn
	addonTemplate             *addonapiv1alpha1.AddOnTemplate
	addonDeploymentConfig     *addonapiv1alpha1.AddOnDeploymentConfig
	setStatusConfigReferences bool
}

func newManagedClusterAddonBuilder(mca *addonapiv1alpha1.ManagedClusterAddOn) *testManagedClusterAddOnBuilder {
	return &testManagedClusterAddOnBuilder{
		managedClusterAddOn:       mca,
		setStatusConfigReferences: true,
	}
}

func (b *testManagedClusterAddOnBuilder) withAddonTemplate(
	at *addonapiv1alpha1.AddOnTemplate) *testManagedClusterAddOnBuilder {
	b.addonTemplate = at
	return b
}

func (b *testManagedClusterAddOnBuilder) withAddonDeploymentConfig(
	adc *addonapiv1alpha1.AddOnDeploymentConfig) *testManagedClusterAddOnBuilder {
	b.addonDeploymentConfig = adc
	return b
}

func (b *testManagedClusterAddOnBuilder) withSetStatusConfigReferences(set bool) *testManagedClusterAddOnBuilder {
	b.setStatusConfigReferences = set
	return b
}

func (b *testManagedClusterAddOnBuilder) build() *addonapiv1alpha1.ManagedClusterAddOn {
	if b.addonDeploymentConfig != nil {
		hash, _ := utils.GetAddOnDeploymentConfigSpecHash(b.addonDeploymentConfig)
		configReference := addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    "addon.open-cluster-management.io",
				Resource: "addondeploymentconfigs",
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Name:      b.addonDeploymentConfig.Name,
				Namespace: b.addonDeploymentConfig.Namespace,
			},
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Name:      b.addonDeploymentConfig.Name,
					Namespace: b.addonDeploymentConfig.Namespace,
				},
				SpecHash: hash,
			},
		}
		if b.setStatusConfigReferences {
			b.managedClusterAddOn.Status.ConfigReferences = append(
				b.managedClusterAddOn.Status.ConfigReferences, configReference)
		}

		b.managedClusterAddOn.Spec.Configs = append(b.managedClusterAddOn.Spec.Configs,
			addonapiv1alpha1.AddOnConfig{
				ConfigGroupResource: configReference.ConfigGroupResource,
				ConfigReferent:      configReference.ConfigReferent,
			})
	}

	if b.addonTemplate != nil {
		hash, _ := GetAddOnTemplateSpecHash(b.addonTemplate)
		configReference := addonapiv1alpha1.ConfigReference{
			ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
				Group:    "addon.open-cluster-management.io",
				Resource: "addontemplates",
			},
			ConfigReferent: addonapiv1alpha1.ConfigReferent{
				Name: b.addonTemplate.Name,
			},
			DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Name: b.addonTemplate.Name,
				},
				SpecHash: hash,
			},
		}
		if b.setStatusConfigReferences {
			b.managedClusterAddOn.Status.ConfigReferences = append(
				b.managedClusterAddOn.Status.ConfigReferences, configReference)
		}
		b.managedClusterAddOn.Spec.Configs = append(b.managedClusterAddOn.Spec.Configs,
			addonapiv1alpha1.AddOnConfig{
				ConfigGroupResource: configReference.ConfigGroupResource,
				ConfigReferent:      configReference.ConfigReferent,
			})
	}
	return b.managedClusterAddOn
}
