package klusterletcontroller

import (
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/openshift/library-go/pkg/assets"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/manifests"
	azureauth "open-cluster-management.io/ocm/pkg/registration/register/azure_auth"
)

const (
	testAzureID  = "11111111-1111-1111-1111-111111111111"
	testClientID = "22222222-2222-2222-2222-222222222222"
	testTenantID = "33333333-3333-3333-3333-333333333333"
)

func azureKlusterletConfig(azure *Azure) klusterletConfig {
	return klusterletConfig{
		KlusterletName:             "klusterlet",
		KlusterletNamespace:        "open-cluster-management-agent",
		AgentNamespace:             "open-cluster-management-agent",
		ClusterName:                "cluster1",
		Replica:                    1,
		RegistrationServiceAccount: "klusterlet-registration-sa",
		WorkServiceAccount:         "klusterlet-work-sa",
		BootStrapKubeConfigSecret:  "bootstrap-hub-kubeconfig",
		HubKubeConfigSecret:        "hub-kubeconfig-secret",
		RegistrationDriver: RegistrationDriver{
			AuthType: operatorapiv1.AzureAuthType,
			Azure:    azure,
		},
	}
}

func renderDeployment(t *testing.T, file string, config klusterletConfig) *appsv1.Deployment {
	t.Helper()
	template, err := manifests.KlusterletManifestFiles.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read manifest %s: %v", file, err)
	}
	objData := assets.MustCreateAssetFromTemplate(file, template, config).Data
	deployment := &appsv1.Deployment{}
	if err := yaml.Unmarshal(objData, deployment); err != nil {
		t.Fatalf("rendered manifest is not valid Deployment YAML: %v\n%s", err, objData)
	}
	return deployment
}

func renderServiceAccount(t *testing.T, file string, config klusterletConfig) *corev1.ServiceAccount {
	t.Helper()
	template, err := manifests.KlusterletManifestFiles.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read manifest %s: %v", file, err)
	}
	objData := assets.MustCreateAssetFromTemplate(file, template, config).Data
	sa := &corev1.ServiceAccount{}
	if err := yaml.Unmarshal(objData, sa); err != nil {
		t.Fatalf("rendered manifest is not valid ServiceAccount YAML: %v\n%s", err, objData)
	}
	return sa
}

// TestAzureRegistrationManifests renders the agent deployments and service accounts
// directly (no envtest control plane required) for each azure credential type, and
// checks that each renders exactly what that credential type needs: the Workload
// Identity label/annotations only for workload-identity-credential, and the
// Secret-backed environment and certificate mount only for the environment-credential
// types.
func TestAzureRegistrationManifests(t *testing.T) {
	deployments := []struct {
		file string
		// runsAgent is true for deployments whose container runs the registration
		// agent and so needs the azure flags. Every deployment may exec the
		// get-azure-token plugin and so needs the credential environment.
		runsAgent bool
	}{
		{"klusterlet/management/klusterlet-registration-deployment.yaml", true},
		{"klusterlet/management/klusterlet-agent-deployment.yaml", true},
		{"klusterlet/management/klusterlet-work-deployment.yaml", false},
	}
	serviceAccounts := []string{
		"klusterlet/managed/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/managed/klusterlet-work-serviceaccount.yaml",
		"klusterlet/management/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/management/klusterlet-work-serviceaccount.yaml",
	}

	cases := []struct {
		name        string
		azure       *Azure
		expectArgs  []string
		absentArgs  []string
		expectEnv   map[string]string // name -> Secret key, or "value:<v>" for a plain value
		expectMount bool
		expectWI    bool
	}{
		{
			name: "managed identity, system-assigned",
			azure: &Azure{
				Credential:            string(operatorapiv1.AzureManagedIdentityCredential),
				ManagedClusterAzureID: testAzureID,
			},
			expectArgs: []string{
				"--registration-auth=azure",
				"--azure-credential=managed-identity-credential",
				"--managed-cluster-azure-id=" + testAzureID,
			},
			absentArgs: []string{"--azure-client-id", "--azure-tenant-id", "--azure-federated-token-file", "--azure-token-audience"},
		},
		{
			name: "environment credential with secret",
			azure: &Azure{
				Credential:            string(operatorapiv1.AzureEnvironmentCredentialSecret),
				ManagedClusterAzureID: testAzureID,
				ClientID:              testClientID,
				TenantID:              testTenantID,
			},
			expectArgs: []string{
				"--azure-credential=environment-credential-secret",
				"--azure-client-id=" + testClientID,
				"--azure-tenant-id=" + testTenantID,
			},
			expectEnv: map[string]string{azureauth.EnvClientSecret: azureauth.EnvClientSecret},
		},
		{
			name: "environment credential with certificate",
			azure: &Azure{
				Credential:            string(operatorapiv1.AzureEnvironmentCredentialCertificate),
				ManagedClusterAzureID: testAzureID,
				ClientID:              testClientID,
				TenantID:              testTenantID,
			},
			expectArgs: []string{"--azure-credential=environment-credential-certificate"},
			expectEnv: map[string]string{
				azureauth.EnvClientCertificatePath:      "value:" + azureauth.ClientCertificatePath,
				azureauth.EnvClientCertificatePassword:  azureauth.EnvClientCertificatePassword,
				azureauth.EnvClientSendCertificateChain: azureauth.EnvClientSendCertificateChain,
			},
			expectMount: true,
		},
		{
			name: "workload identity",
			azure: &Azure{
				Credential:            string(operatorapiv1.AzureWorkloadIdentityCredential),
				ManagedClusterAzureID: testAzureID,
				ClientID:              testClientID,
				TenantID:              testTenantID,
				FederatedTokenFile:    "/var/run/token",
				TokenAudience:         "api://hub/.default",
			},
			expectArgs: []string{
				"--azure-credential=workload-identity-credential",
				"--azure-client-id=" + testClientID,
				"--azure-tenant-id=" + testTenantID,
				"--azure-federated-token-file=/var/run/token",
				"--azure-token-audience=api://hub/.default",
			},
			expectWI: true,
		},
	}

	for _, c := range cases {
		config := azureKlusterletConfig(c.azure)

		for _, d := range deployments {
			t.Run(c.name+"/"+d.file, func(t *testing.T) {
				deployment := renderDeployment(t, d.file, config)
				podSpec := deployment.Spec.Template.Spec
				container := podSpec.Containers[0]

				_, hasLabel := deployment.Spec.Template.Labels["azure.workload.identity/use"]
				if hasLabel != c.expectWI {
					t.Errorf("expected azure.workload.identity/use pod label present=%v, got labels %v",
						c.expectWI, deployment.Spec.Template.Labels)
				}

				args := strings.Join(container.Args, "\n")
				if d.runsAgent {
					for _, want := range c.expectArgs {
						if !strings.Contains(args, want) {
							t.Errorf("expected container args to contain %q, got:\n%s", want, args)
						}
					}
					for _, absent := range c.absentArgs {
						if strings.Contains(args, absent) {
							t.Errorf("expected container args not to contain %q, got:\n%s", absent, args)
						}
					}
				} else if strings.Contains(args, "--azure-") {
					t.Errorf("expected no azure flags on a container that doesn't run the registration agent, got:\n%s", args)
				}

				azureEnv := map[string]corev1.EnvVar{}
				for _, e := range container.Env {
					if strings.HasPrefix(e.Name, "AZURE_") {
						azureEnv[e.Name] = e
					}
				}
				if len(azureEnv) != len(c.expectEnv) {
					t.Errorf("expected AZURE_* env %v, got %v", c.expectEnv, azureEnv)
				}
				for name, want := range c.expectEnv {
					got, ok := azureEnv[name]
					switch {
					case !ok:
						t.Errorf("expected env %s", name)
					case strings.HasPrefix(want, "value:"):
						if got.Value != strings.TrimPrefix(want, "value:") || got.ValueFrom != nil {
							t.Errorf("expected env %s to be the plain value %q, got %+v", name, want, got)
						}
					case got.ValueFrom == nil || got.ValueFrom.SecretKeyRef == nil ||
						got.ValueFrom.SecretKeyRef.Name != azureauth.CredentialSecretName || got.ValueFrom.SecretKeyRef.Key != want:
						t.Errorf("expected env %s from key %q of Secret %q, got %+v", name, want, azureauth.CredentialSecretName, got)
					}
				}

				var mount bool
				for _, m := range container.VolumeMounts {
					if m.Name == "azure-client-certificate" {
						mount = m.MountPath == azureauth.ClientCertificateMountDir && m.ReadOnly
					}
				}
				var volume bool
				for _, v := range podSpec.Volumes {
					if v.Name == "azure-client-certificate" {
						volume = v.Secret != nil && v.Secret.SecretName == azureauth.CredentialSecretName &&
							len(v.Secret.Items) == 1 && v.Secret.Items[0].Key == azureauth.ClientCertificateSecretKey &&
							v.Secret.Items[0].Path == azureauth.ClientCertificateSecretKey
					}
				}
				if mount != c.expectMount || volume != c.expectMount {
					t.Errorf("expected certificate volume and mount present=%v, got volume=%v mount=%v", c.expectMount, volume, mount)
				}
			})
		}

		for _, file := range serviceAccounts {
			t.Run(c.name+"/"+file, func(t *testing.T) {
				sa := renderServiceAccount(t, file, config)
				wantClientID, wantTenantID := "", ""
				if c.expectWI {
					wantClientID, wantTenantID = testClientID, testTenantID
				}
				if got := sa.Annotations["azure.workload.identity/client-id"]; got != wantClientID {
					t.Errorf("expected azure.workload.identity/client-id annotation %q, got %q", wantClientID, got)
				}
				if got := sa.Annotations["azure.workload.identity/tenant-id"]; got != wantTenantID {
					t.Errorf("expected azure.workload.identity/tenant-id annotation %q, got %q", wantTenantID, got)
				}
			})
		}
	}
}

// TestAzureRegistrationManifestsWithoutAzureBlock checks that authType azure with no
// azure block (normally rejected by the CRD) still renders, and still selects the azure
// driver, so the agent fails loudly on its missing configuration instead of silently
// registering with the default csr driver.
func TestAzureRegistrationManifestsWithoutAzureBlock(t *testing.T) {
	config := azureKlusterletConfig(nil)
	deployment := renderDeployment(t, "klusterlet/management/klusterlet-registration-deployment.yaml", config)
	args := strings.Join(deployment.Spec.Template.Spec.Containers[0].Args, "\n")
	if !strings.Contains(args, "--registration-auth=azure") {
		t.Errorf("expected --registration-auth=azure, got:\n%s", args)
	}
	if strings.Contains(args, "--azure-credential") {
		t.Errorf("expected no --azure-credential without an azure block, got:\n%s", args)
	}
}
