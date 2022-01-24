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
	"k8s.io/client-go/rest"
	helloworldagent "open-cluster-management.io/addon-framework/examples/helloworld/agent"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

const (
	defaultExampleImage = "quay.io/open-cluster-management/helloworld-addon:latest"
	addonName           = "helloworld"
)

//go:embed manifests
//go:embed manifests/templates
var fs embed.FS

var agentPermissionFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/permission/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/permission/rolebinding.yaml",
}

func newRegistrationOption(kubeConfig *rest.Config, recorder events.Recorder, agentName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, agentName),
		CSRApproveCheck:   utils.DefaultCSRApprover(agentName),
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
			kubeclient, err := kubernetes.NewForConfig(kubeConfig)
			if err != nil {
				return err
			}

			for _, file := range agentPermissionFiles {
				if err := applyManifestFromFile(file, cluster.Name, addon.Name, kubeclient, recorder); err != nil {
					return err
				}
			}

			return nil
		},
	}
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
		resourceapply.NewResourceCache(),
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

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = helloworldagent.HelloworldAgentInstallationNamespace
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

	return addonfactory.StructToValues(manifestConfig), nil
}
