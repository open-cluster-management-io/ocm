package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-cluster-management/addon-framework/examples/helloworld/bindata"
	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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

const manifestDir = "examples/helloworld/manifests"

var manifestFiles = []string{
	"clusterrolebinding.yaml",
	"deployment.yaml",
	"serviceaccount.yaml",
}

var agentPermissionFiles = []string{
	"role.yaml",
	"rolebinding.yaml",
}

// Another agent with registration enabled.
type helloWorldAgent struct {
	kubeConfig *rest.Config
	recorder   events.Recorder
	agentName  string
}

func init() {
	scheme.AddToScheme(genericScheme)
}

func (h *helloWorldAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects := []runtime.Object{}

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
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", h.GetAgentAddonOptions().AddonName),
		AddonInstallNamespace: installNamespace,
		ClusterName:           cluster.Name,
		Image:                 image,
	}

	for _, file := range manifestFiles {
		raw := assets.MustCreateAssetFromTemplate(file, bindata.MustAsset(filepath.Join(manifestDir, file)), &manifestConfig).Data
		object, _, err := genericCodec.Decode(raw, nil, nil)
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
			CSRApproveCheck: func(
				cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return true
			},
			PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
				groups := agent.DefaultGroups(cluster.Name, addon.Name)
				config := struct {
					ClusterName string
					Group       string
				}{
					ClusterName: cluster.Name,
					Group:       groups[0],
				}

				kubeclient, err := kubernetes.NewForConfig(h.kubeConfig)
				if err != nil {
					return err
				}

				results := resourceapply.ApplyDirectly(
					resourceapply.NewKubeClientHolder(kubeclient),
					h.recorder,
					func(name string) ([]byte, error) {
						return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join(manifestDir, name)), config).Data, nil
					},
					agentPermissionFiles...,
				)

				for _, result := range results {
					if result.Error != nil {
						return result.Error
					}
				}

				return nil
			},
		},
	}
}
