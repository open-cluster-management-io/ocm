package main

import (
	"fmt"
	"path/filepath"

	"github.com/open-cluster-management/addon-framework/examples/helloworld/bindata"
	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/openshift/library-go/pkg/assets"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	scheme.AddToScheme(genericScheme)
}

// Another agent with registration enabled.
type helloWorldAgentWithRegistration struct{}

var manifestFiles = []string{
	"examples/helloworld/manifests/clusterrolebinding.yaml",
	"examples/helloworld/manifests/deployment.yaml",
	"examples/helloworld/manifests/service.yaml",
	"examples/helloworld/manifests/serviceaccount.yaml",
}

func (h *helloWorldAgentWithRegistration) Manifests(cluster *clusterv1.ManagedCluster) ([]runtime.Object, error) {
	objects := []runtime.Object{}

	manifestConfig := struct {
		KubeConfigSecret      string
		ClusterName           string
		AddonInstallNamespace string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", h.GetAgentAddonOptions().AddonName),
		AddonInstallNamespace: h.GetAgentAddonOptions().AddonInstallNamespace,
		ClusterName:           cluster.Name,
	}

	for _, file := range manifestFiles {
		raw := assets.MustCreateAssetFromTemplate(file, bindata.MustAsset(filepath.Join("", file)), &manifestConfig).Data
		object, _, err := genericCodec.Decode(raw, nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func (h *helloWorldAgentWithRegistration) GetAgentAddonOptions() *agent.AgentAddonOptions {
	return &agent.AgentAddonOptions{
		AddonName:             "addonwithregistration",
		AddonInstallNamespace: "default",
	}
}

func (h *helloWorldAgentWithRegistration) GetRegistrationOption() *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations("helloworldwithregistration"),
		CSRApproveCheck: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
			return true
		},
	}
}
