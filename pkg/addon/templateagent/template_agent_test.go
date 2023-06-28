package templateagent

import (
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

func TestAddonTemplateAgent_Manifests(t *testing.T) {
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

	addonTemplateSpecHash, err := GetTemplateSpecHash(addonTemplate)
	if err != nil {
		t.Errorf("error getting template spec hash: %v", err)
	}
	addonDeploymentConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
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
	}

	managedClusterAddon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      addonName,
			Namespace: clusterName,
		},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			ConfigReferences: []addonapiv1alpha1.ConfigReference{
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addontemplates",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Name: "hello-template",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name: "hello-template",
						},
						SpecHash: addonTemplateSpecHash,
					},
				},
				{
					ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Name:      "hello-config",
						Namespace: "default",
					},
					DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
						ConfigReferent: addonapiv1alpha1.ConfigReferent{
							Name:      "hello-config",
							Namespace: "default",
						},
					},
				},
			},
		},
	}
	decoder := serializer.NewCodecFactory(s).UniversalDeserializer()
	_, _, err = decoder.Decode(data, nil, addonTemplate)
	if err != nil {
		t.Errorf("error decoding file: %v", err)
	}

	hubKubeClient := fakekube.NewSimpleClientset()
	addonClient := fakeaddon.NewSimpleClientset(addonTemplate, managedClusterAddon, addonDeploymentConfig)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
	mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
	if err := mcaStore.Add(managedClusterAddon); err != nil {
		t.Fatal(err)
	}
	atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
	if err := atStore.Add(addonTemplate); err != nil {
		t.Fatal(err)
	}
	kubeInformers := kubeinformers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute)

	agentAddon := NewCRDTemplateAgentAddon(
		addonName,
		"test-agent",
		hubKubeClient,
		addonClient,
		addonInformerFactory,
		kubeInformers.Rbac().V1().RoleBindings().Lister(),
		addonfactory.GetAddOnDeploymentConfigValues(
			addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
			addonfactory.ToAddOnCustomizedVariableValues,
			ToAddOnNodePlacementPrivateValues,
			ToAddOnRegistriesPrivateValues,
		),
	)

	cluster := addonfactory.NewFakeManagedCluster("cluster1", "1.10.1")

	objects, err := agentAddon.Manifests(cluster, managedClusterAddon)
	if err != nil {
		t.Errorf("expected no error, got err %v", err)
	}
	if len(objects) != 4 {
		t.Errorf("expected 4 objects, but got %v", len(objects))
	}

	object, ok := objects[0].(*appsv1.Deployment)
	if !ok {
		t.Errorf("expected object to be *appsv1.Deployment, but got %T", objects[0])
	}
	image := object.Spec.Template.Spec.Containers[0].Image
	if image != "quay.io/ocm/addon-examples:v1" {
		t.Errorf("unexpected image %v", image)
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
		{Name: "INSTALL_NAMESPACE", Value: "open-cluster-management-agent-addon"},
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
}
