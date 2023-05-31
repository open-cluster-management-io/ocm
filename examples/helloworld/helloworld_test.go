package helloworld

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

var (
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func TestManifestAddonAgent(t *testing.T) {
	cases := []struct {
		name                string
		managedCluster      *clusterv1.ManagedCluster
		managedClusterAddOn *addonapiv1alpha1.ManagedClusterAddOn
		configs             []runtime.Object
		verifyDeployment    func(t *testing.T, objs []runtime.Object)
	}{
		{
			name:                "no configs",
			managedCluster:      addontesting.NewManagedCluster("cluster1"),
			managedClusterAddOn: addontesting.NewAddon("helloworld", "cluster1"),
			configs:             []runtime.Object{},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findHelloWorldDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "helloworld-agent" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != DefaultHelloWorldExampleImage {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}
			},
		},
		{
			name:           "override image with annotation",
			managedCluster: addontesting.NewManagedCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := addontesting.NewAddon("test", "cluster1")
				addon.Annotations = map[string]string{
					"addon.open-cluster-management.io/values": `{"Image":"quay.io/test:test"}`}
				return addon
			}(),
			configs: []runtime.Object{},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findHelloWorldDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "helloworld-agent" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != "quay.io/test:test" {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}
			},
		},
		{
			name:           "with addon deployment config",
			managedCluster: addontesting.NewManagedCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
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
			configs: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "config",
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

				if deployment.Name != "helloworld-agent" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != DefaultHelloWorldExampleImage {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
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
		fakeAddonClient := fakeaddon.NewSimpleClientset(c.configs...)

		agentAddon, err := addonfactory.NewAgentAddonFactory(AddonName, FS, "manifests/templates").
			WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
			WithGetValuesFuncs(
				GetDefaultValues,
				addonfactory.GetAddOnDeploymentConfigValues(
					addonfactory.NewAddOnDeploymentConfigGetter(fakeAddonClient),
					addonfactory.ToAddOnDeploymentConfigValues,
				),
				addonfactory.GetValuesFromAddonAnnotation,
			).
			WithAgentRegistrationOption(NewRegistrationOption(nil, AddonName, utilrand.String(5))).
			WithAgentHealthProber(AgentHealthProber()).
			BuildTemplateAgentAddon()
		if err != nil {
			klog.Fatalf("failed to build agent %v", err)
		}

		objects, err := agentAddon.Manifests(c.managedCluster, c.managedClusterAddOn)
		if err != nil {
			t.Fatalf("failed to get manifests %v", err)
		}

		if len(objects) != 3 {
			t.Fatalf("expected 3 manifests, but %v", objects)
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
