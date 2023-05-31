package helloworld_helm

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

const (
	testImg           = "test.io/open-cluster-management/addon-examples:latest"
	testImgPullSecret = "test-pull-secret"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func TestManifestAddonAgent(t *testing.T) {
	cases := []struct {
		name                   string
		managedCluster         *clusterv1.ManagedCluster
		managedClusterAddOn    *addonapiv1alpha1.ManagedClusterAddOn
		configMaps             []runtime.Object
		addOnDeploymentConfigs []runtime.Object
		verifyDeployment       func(t *testing.T, objs []runtime.Object)
	}{
		{
			name:                   "no configs",
			managedCluster:         addontesting.NewManagedCluster("cluster1"),
			managedClusterAddOn:    addontesting.NewAddon("helloworld", "cluster1"),
			configMaps:             []runtime.Object{},
			addOnDeploymentConfigs: []runtime.Object{},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findHelloWorldDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "helloworldhelm-agent" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != defaultImage {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}
			},
		},
		{
			name:           "with image config, addon deployment config and annotation",
			managedCluster: addontesting.NewManagedCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.SetAnnotations(map[string]string{
					"addon.open-cluster-management.io/values": `{"global":{"imagePullSecret":"test-pull-secret","imagePullPolicy":"Never"}}`,
				})
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "",
							Resource: "configmaps",
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: "cluster1",
							Name:      "image-config",
						},
					},
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Namespace: "cluster1",
							Name:      "deploy-config",
						},
					},
				}
				return addon
			}(),
			configMaps: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "image-config",
						Namespace: "cluster1",
					},
					Data: map[string]string{
						"image":           testImg,
						"imagePullPolicy": "Always",
					},
				},
			},
			addOnDeploymentConfigs: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy-config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							Tolerations:  tolerations,
							NodeSelector: nodeSelector,
						},
					},
				},
			},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findHelloWorldDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "helloworldhelm-agent" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.ImagePullSecrets[0].Name != testImgPullSecret {
					t.Errorf("unexpected image pull secret %s", deployment.Spec.Template.Spec.ImagePullSecrets[0].Name)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != testImg {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}

				if deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy != "Never" {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.NodeSelector, nodeSelector) {
					t.Errorf("unexpected nodeSeletcor %v", deployment.Spec.Template.Spec.NodeSelector)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.Tolerations, tolerations) {
					t.Errorf("unexpected tolerations %v", deployment.Spec.Template.Spec.Tolerations)
				}
			},
		},
	}

	for _, c := range cases {
		fakeKubeClient := fakekube.NewSimpleClientset(c.configMaps...)
		fakeAddonClient := fakeaddon.NewSimpleClientset(c.addOnDeploymentConfigs...)

		agentAddon, err := addonfactory.NewAgentAddonFactory(AddonName, FS, "manifests/charts/helloworld").
			WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
			WithGetValuesFuncs(
				GetDefaultValues,
				addonfactory.GetAddOnDeploymentConfigValues(
					addonfactory.NewAddOnDeploymentConfigGetter(fakeAddonClient),
					addonfactory.ToAddOnNodePlacementValues,
				),
				GetImageValues(fakeKubeClient),
				addonfactory.GetValuesFromAddonAnnotation,
			).
			WithAgentRegistrationOption(&agent.RegistrationOption{}).
			BuildHelmAgentAddon()
		if err != nil {
			klog.Fatalf("failed to build agent %v", err)
		}

		objects, err := agentAddon.Manifests(c.managedCluster, c.managedClusterAddOn)
		if err != nil {
			t.Fatalf("failed to get manifests %v", err)
		}

		if len(objects) != 4 {
			t.Fatalf("expected 4 manifests, but %v", objects)
		}

		c.verifyDeployment(t, objects)
	}
}

func findHelloWorldDeployment(objs []runtime.Object) *appsv1.Deployment {
	for _, obj := range objs {
		switch obj := obj.(type) {
		case *appsv1.Deployment:
			return obj
		}
	}

	return nil
}
