package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

const defaultExampleImage = "quay.io/open-cluster-management/helloworld-addon:latest"

func init() {
	scheme.AddToScheme(genericScheme)
}

//go:embed manifests
var fs embed.FS

var manifestFiles = []string{
	// serviceaccount to run addon-agent
	"manifests/serviceaccount.yaml",
	// clusterrolebinding to bind appropriate clusterrole to the serviceaccount
	"manifests/clusterrolebinding.yaml",
	// deployment to deploy addon-agent
	"manifests/deployment.yaml",
}

var agentPermissionFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/rolebinding.yaml",
}

// Another agent with registration enabled.
type helloWorldAgent struct {
	kubeConfig *rest.Config
	recorder   events.Recorder
	agentName  string
}

func (h *helloWorldAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects := []runtime.Object{}
	for _, file := range manifestFiles {
		object, err := loadManifestFromFile(file, cluster, addon)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func (h *helloWorldAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: "helloworld",
		Registration: &agent.RegistrationOption{
			CSRConfigurations: agent.KubeClientSignerConfigurations("helloworld", h.agentName),
			CSRApproveCheck:   agent.ApprovalAllCSRs,
			PermissionConfig:  h.setupAgentPermissions,
		},
		InstallStrategy: agent.InstallAllStrategy("default"),
	}
}

func (h *helloWorldAgent) setupAgentPermissions(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	kubeclient, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}

	for _, file := range agentPermissionFiles {
		if err := applyManifestFromFile(file, cluster.Name, addon.Name, kubeclient, h.recorder); err != nil {
			return err
		}
	}

	return nil
}

func loadManifestFromFile(file string, cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (runtime.Object, error) {
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = "default"
	}

	image := os.Getenv("EXAMPLE_IMAGE_NAME")
	if len(image) == 0 {
		image = defaultExampleImage
	}

	manifestConfig := struct {
		KubeConfigSecret      string
		ClusterName           string
		AddonInstallNamespace string
		Image                 string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", addon.Name),
		AddonInstallNamespace: installNamespace,
		ClusterName:           cluster.Name,
		Image:                 image,
	}

	template, err := fs.ReadFile(file)
	if err != nil {
		return nil, err
	}

	raw := assets.MustCreateAssetFromTemplate(file, template, &manifestConfig).Data
	object, _, err := genericCodec.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}
	return object, nil
}

func applyManifestFromFile(file, clusterName, addonName string, kubeclient *kubernetes.Clientset, recorder events.Recorder) error {
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		ClusterName string
		Group       string
	}{
		ClusterName: clusterName,
		Group:       groups[0],
	}

	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(kubeclient),
		recorder,
		func(name string) ([]byte, error) {
			template, err := fs.ReadFile(file)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		file,
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}
