package clustermanagercontroller

import (
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/openshift/library-go/pkg/assets"
	appsv1 "k8s.io/api/apps/v1"

	"open-cluster-management.io/ocm/manifests"
)

// TestAzureRegistrationDeploymentManifest renders the hub registration deployment
// manifest directly (no envtest control plane required) with the
// azure flags populated, to catch template syntax/execution errors and verify the
// expected flags are produced.
func TestAzureRegistrationDeploymentManifest(t *testing.T) {
	config := manifests.HubConfig{
		ClusterManagerName:          "cluster-manager",
		ClusterManagerNamespace:     "open-cluster-management-hub",
		Replica:                     1,
		EnabledRegistrationDrivers:  "csr,azure",
		AutoApprovedAzureIDPatterns: "^11111111-.*$,^22222222-.*$",
		AzureOIDCIssuerURL:          "https://login.microsoftonline.com/tenant/v2.0",
	}

	file := "cluster-manager/management/registration/deployment.yaml"
	template, err := manifests.ClusterManagerManifestFiles.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read manifest %s: %v", file, err)
	}
	objData := assets.MustCreateAssetFromTemplate(file, template, config).Data

	deployment := &appsv1.Deployment{}
	if err := yaml.Unmarshal(objData, deployment); err != nil {
		t.Fatalf("rendered manifest is not valid Deployment YAML: %v\n%s", err, objData)
	}

	args := strings.Join(deployment.Spec.Template.Spec.Containers[0].Args, "\n")
	for _, want := range []string{
		"--enabled-registration-drivers=csr,azure",
		"--auto-approved-azure-identity-patterns=^11111111-.*$,^22222222-.*$",
		"--azure-oidc-issuer-url=https://login.microsoftonline.com/tenant/v2.0",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("expected container args to contain %q, got:\n%s", want, args)
		}
	}
}
