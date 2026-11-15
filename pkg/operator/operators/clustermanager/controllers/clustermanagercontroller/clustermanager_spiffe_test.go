package clustermanagercontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// spiffeRendering is what one sync created on the hub and the management cluster.
type spiffeRendering struct {
	services    map[string]*corev1.Service
	deployments map[string]*appsv1.Deployment
	secrets     []string
	bindings    map[string]*rbacv1.ClusterRoleBinding
	applied     *metav1.Condition
}

func renderSPIFFEHub(t *testing.T, cm *operatorapiv1.ClusterManager) spiffeRendering {
	t.Helper()
	ctx := t.Context()
	tc := newTestController(t, cm)
	ns := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
	setup(t, tc, setDeployment(cm.Name, ns))
	if err := tc.clusterManagerController.sync(ctx, testingcommon.NewFakeSyncContext(t, cm.Name), cm.Name); err != nil {
		t.Fatalf("sync: %v", err)
	}
	r := spiffeRendering{services: map[string]*corev1.Service{}, deployments: map[string]*appsv1.Deployment{},
		bindings: map[string]*rbacv1.ClusterRoleBinding{}}
	for _, a := range append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) {
		if a.GetVerb() != createVerb && a.GetVerb() != "update" {
			continue
		}
		var obj runtime.Object
		switch a := a.(type) {
		case clienttesting.CreateActionImpl:
			obj = a.Object
		case clienttesting.UpdateActionImpl:
			obj = a.Object
		default:
			continue
		}
		switch o := obj.(type) {
		case *corev1.Service:
			r.services[o.Name] = o
		case *appsv1.Deployment:
			r.deployments[o.Name] = o
		case *corev1.Secret:
			r.secrets = append(r.secrets, o.Name)
		case *rbacv1.ClusterRoleBinding:
			r.bindings[o.Name] = o
		}
	}
	for _, a := range tc.operatorClient.Actions() {
		p, ok := a.(clienttesting.PatchActionImpl)
		if !ok || p.GetSubresource() != "status" {
			continue
		}
		updated, err := tc.operatorClient.OperatorV1().ClusterManagers().Get(ctx, cm.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		r.applied = meta.FindStatusCondition(updated.Status.Conditions, operatorapiv1.ConditionClusterManagerApplied)
	}
	return r
}

func spiffeClusterManager(drivers []string, spiffe *operatorapiv1.SPIFFERegistrationConfig, gate bool,
	exposure ...operatorapiv1.EndpointExposure) *operatorapiv1.ClusterManager {
	cm := newClusterManager("testhub")
	rc := &operatorapiv1.RegistrationHubConfiguration{}
	for _, d := range drivers {
		driver := operatorapiv1.RegistrationDriverHub{AuthType: d}
		if d == operatorapiv1.SPIFFEAuthType {
			driver.SPIFFE = spiffe
		}
		rc.RegistrationDrivers = append(rc.RegistrationDrivers, driver)
	}
	if gate {
		rc.FeatureGates = []operatorapiv1.FeatureGate{{Feature: "SPIFFERegistration", Mode: operatorapiv1.FeatureGateModeTypeEnable}}
	}
	cm.Spec.RegistrationConfiguration = rc
	if len(exposure) > 0 {
		cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{EndpointsExposure: exposure}
	}
	return cm
}

func args(d *appsv1.Deployment) string {
	return strings.Join(d.Spec.Template.Spec.Containers[0].Args, " ")
}

func volumes(d *appsv1.Deployment) []string {
	var names []string
	for _, v := range d.Spec.Template.Spec.Volumes {
		names = append(names, v.Name)
	}
	return names
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestSPIFFEHubRendering(t *testing.T) {
	td := &operatorapiv1.SPIFFERegistrationConfig{
		TrustDomain:            "ocm-hub.example.org",
		AgentTrustDomains:      []string{"east.example.org"},
		AutoApprovedIdentities: []string{"spiffe://ocm-hub.example.org/ocm/cluster/*/agent/registration"},
		MaxSVIDLifetime:        &metav1.Duration{Duration: time.Hour},
	}

	t.Run("spiffe only", func(t *testing.T) {
		r := renderSPIFFEHub(t, spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType}, td, true))
		server := r.deployments["testhub-grpc-server"]
		if server == nil {
			t.Fatal("the gRPC server Deployment is not rendered")
		}
		a := args(server)
		for _, want := range []string{
			"--grpc-enabled=false",
			"--spiffe-enabled",
			"--spiffe-listen-address=:8091",
			"--spiffe-trust-domain=ocm-hub.example.org",
			"--spiffe-agent-trust-domains=ocm-hub.example.org,east.example.org",
			"--spiffe-server-id=spiffe://ocm-hub.example.org/ocm/hub/grpc-server",
			"--spiffe-workload-api-socket=unix:///spiffe-workload-api/spire-agent.sock",
			"--spiffe-agent-id-template=" + operatorapiv1.DefaultSPIFFEAgentIDTemplate,
			"--spiffe-addon-agent-id-template=" + operatorapiv1.DefaultSPIFFEAddOnAgentIDTemplate,
		} {
			if !strings.Contains(a, want) {
				t.Errorf("args %q miss %q", a, want)
			}
		}
		if strings.Contains(a, "--server-config") {
			t.Errorf("args %q configure the grpc listener", a)
		}
		v := volumes(server)
		if !contains(v, "spiffe-workload-api") {
			t.Errorf("volumes %v miss the Workload API", v)
		}
		for _, secretVolume := range []string{"grpc-server-serving-cert", "grpc-server-ca", "grpc-server-config"} {
			if contains(v, secretVolume) {
				t.Errorf("volumes %v have %s in spiffe-only mode", v, secretVolume)
			}
		}
		for _, vol := range server.Spec.Template.Spec.Volumes {
			if vol.Name == "spiffe-workload-api" && (vol.CSI == nil || vol.CSI.Driver != "csi.spiffe.io") {
				t.Errorf("the Workload API volume is %+v, want the csi.spiffe.io CSI driver", vol)
			}
		}
		svc := r.services["testhub-grpc-server-spiffe"]
		if svc == nil {
			t.Fatal("the SPIFFE Service is not rendered")
		}
		if svc.Spec.Type != corev1.ServiceTypeClusterIP || svc.Spec.Ports[0].Port != 8091 || svc.Spec.Ports[0].TargetPort.IntVal != 8091 {
			t.Errorf("SPIFFE Service %+v, want ClusterIP 8091->8091", svc.Spec)
		}
		if r.services["testhub-grpc-server"] != nil {
			t.Error("the grpc Service is rendered in spiffe-only mode")
		}
		if contains(r.secrets, helpers.GRPCServerSecret) {
			t.Errorf("secrets %v include the grpc serving certificate", r.secrets)
		}
		reg := args(r.deployments["testhub-registration-controller"])
		for _, want := range []string{"--spiffe-max-svid-lifetime=1h0m0s", "--spiffe-max-clock-skew=5m0s", "spiffe",
			"--spiffe-auto-approved-identities=spiffe://ocm-hub.example.org/ocm/cluster/*/agent/registration"} {
			if !strings.Contains(reg, want) {
				t.Errorf("registration controller args %q miss %q", reg, want)
			}
		}
		if r.applied == nil || r.applied.Status != metav1.ConditionTrue {
			t.Errorf("Applied condition %+v, want True", r.applied)
		}
		b := r.bindings["open-cluster-management:spiffe-agent"]
		if b == nil || len(b.Subjects) != 1 || b.Subjects[0].Kind != "Group" ||
			b.Subjects[0].Name != "system:open-cluster-management:managed-clusters" {
			t.Errorf("pre-acceptance binding %+v, want the managed-clusters group", b)
		}
	})

	t.Run("spiffe and grpc", func(t *testing.T) {
		r := renderSPIFFEHub(t, spiffeClusterManager(
			[]string{operatorapiv1.GRPCAuthType, operatorapiv1.SPIFFEAuthType}, td, true,
			operatorapiv1.EndpointExposure{Protocol: operatorapiv1.GRPCSPIFFEProtocol,
				GRPC: &operatorapiv1.Endpoint{Type: operatorapiv1.EndpointTypeLoadBalancer}}))
		a := args(r.deployments["testhub-grpc-server"])
		if !strings.Contains(a, "--server-config=") || !strings.Contains(a, "--spiffe-enabled") ||
			strings.Contains(a, "--grpc-enabled=false") {
			t.Errorf("args %q, want both listeners", a)
		}
		v := volumes(r.deployments["testhub-grpc-server"])
		if !contains(v, "grpc-server-serving-cert") || !contains(v, "spiffe-workload-api") {
			t.Errorf("volumes %v, want the grpc certificate and the Workload API", v)
		}
		if r.services["testhub-grpc-server"] == nil {
			t.Error("the grpc Service is not rendered")
		}
		svc := r.services["testhub-grpc-server-spiffe"]
		if svc == nil || svc.Spec.Type != corev1.ServiceTypeLoadBalancer || svc.Spec.Ports[0].Port != 443 {
			t.Errorf("SPIFFE Service %+v, want LoadBalancer 443", svc)
		}
	})

	t.Run("feature gate off", func(t *testing.T) {
		r := renderSPIFFEHub(t, spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType}, td, false))
		if r.deployments["testhub-grpc-server"] != nil || r.services["testhub-grpc-server-spiffe"] != nil ||
			r.bindings["open-cluster-management:spiffe-agent"] != nil {
			t.Error("SPIFFE is rendered without the SPIFFERegistration feature gate")
		}
	})

	invalid := []struct {
		name    string
		config  *operatorapiv1.SPIFFERegistrationConfig
		message string
	}{
		{"server ID in another trust domain", &operatorapiv1.SPIFFERegistrationConfig{
			TrustDomain: "ocm-hub.example.org", ServerSPIFFEID: "spiffe://ocm-hub.example.org.evil/ocm/hub/grpc-server"},
			"is not in trust domain ocm-hub.example.org"},
		{"unknown placeholder", &operatorapiv1.SPIFFERegistrationConfig{
			TrustDomain: "ocm-hub.example.org", AgentIDTemplate: "spiffe://{trustDomain}/ocm/{clutser}/{role}"},
			"{clutser}"},
		{"overlapping templates", &operatorapiv1.SPIFFERegistrationConfig{
			TrustDomain:          "ocm-hub.example.org",
			AgentIDTemplate:      "spiffe://{trustDomain}/ocm/{cluster}/x/{role}",
			AddOnAgentIDTemplate: "spiffe://{trustDomain}/ocm/{cluster}/{addon}/{agent}"},
			"can match the same SPIFFE ID"},
		{"server ID matches the agent template", &operatorapiv1.SPIFFERegistrationConfig{
			TrustDomain: "ocm-hub.example.org", ServerSPIFFEID: "spiffe://ocm-hub.example.org/ocm/cluster/hub/agent/work"},
			"agents could present it"},
	}
	for _, c := range invalid {
		t.Run("invalid: "+c.name, func(t *testing.T) {
			r := renderSPIFFEHub(t, spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType}, c.config, true))
			if r.deployments["testhub-grpc-server"] != nil || r.services["testhub-grpc-server-spiffe"] != nil {
				t.Error("SPIFFE is rendered with invalid settings")
			}
			if r.applied == nil || r.applied.Status != metav1.ConditionFalse ||
				r.applied.Reason != ReasonInvalidSPIFFEConfiguration || !strings.Contains(r.applied.Message, c.message) {
				t.Errorf("Applied condition %+v, want False/%s containing %q", r.applied, ReasonInvalidSPIFFEConfiguration, c.message)
			}
		})
	}
}

// Removing grpc from a hub whose clusters all moved to spiffe deletes the grpc listener's Service
// and keeps the gRPC server, which now serves the SPIFFE listener only.
func TestSPIFFEHubRemovingGRPC(t *testing.T) {
	ctx := t.Context()
	td := &operatorapiv1.SPIFFERegistrationConfig{TrustDomain: "ocm-hub.example.org"}
	cm := spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType}, td, true)
	tc := newTestController(t, cm)
	ns := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
	setup(t, tc, setDeployment(cm.Name, ns))
	if err := tc.clusterManagerController.sync(ctx, testingcommon.NewFakeSyncContext(t, cm.Name), cm.Name); err != nil {
		t.Fatalf("sync: %v", err)
	}
	deleted, kept := false, false
	for _, a := range append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) {
		switch a := a.(type) {
		case clienttesting.DeleteActionImpl:
			if a.GetResource().Resource == "services" && a.GetName() == "testhub-grpc-server" {
				deleted = true
			}
			if a.GetResource().Resource == "deployments" && a.GetName() == "testhub-grpc-server" {
				t.Error("the gRPC server Deployment was deleted; it serves the SPIFFE listener")
			}
		case clienttesting.CreateActionImpl:
			if d, ok := a.Object.(*appsv1.Deployment); ok && d.Name == "testhub-grpc-server" {
				kept = true
			}
		case clienttesting.UpdateActionImpl:
			if d, ok := a.Object.(*appsv1.Deployment); ok && d.Name == "testhub-grpc-server" {
				kept = true
			}
		}
	}
	if !deleted {
		t.Error("the grpc Service wasn't deleted")
	}
	if !kept {
		t.Error("the gRPC server Deployment isn't rendered")
	}
}

// The gRPC server, which serves the SPIFFE listener, uses the server image when one is set.
func TestSPIFFEHubServerImage(t *testing.T) {
	td := &operatorapiv1.SPIFFERegistrationConfig{TrustDomain: "ocm-hub.example.org",
		MaxClockSkew: &metav1.Duration{Duration: 2 * time.Minute}}
	cm := spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType}, td, true)
	cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{ImagePullSpec: "example.com/server:v1"}
	r := renderSPIFFEHub(t, cm)
	server := r.deployments["testhub-grpc-server"]
	if server == nil {
		t.Fatal("the gRPC server Deployment is not rendered")
	}
	if img := server.Spec.Template.Spec.Containers[0].Image; img != "example.com/server:v1" {
		t.Errorf("image %s, want the server image", img)
	}
	reg := r.deployments["testhub-registration-controller"]
	if reg == nil || !strings.Contains(args(reg), "--spiffe-max-clock-skew=2m0s") {
		t.Errorf("the clock skew isn't passed to the registration controller")
	}
}

// A hosted ClusterManager with only the spiffe driver still gets the gRPC server's kubeconfig.
func TestSPIFFEHubHostedServerKubeconfig(t *testing.T) {
	ctx := t.Context()
	cm := spiffeClusterManager([]string{operatorapiv1.SPIFFEAuthType},
		&operatorapiv1.SPIFFERegistrationConfig{TrustDomain: "ocm-hub.example.org"}, true)
	cm.Spec.DeployOption.Mode = operatorapiv1.InstallModeHosted
	cm.Spec.DeployOption.Hosted = &operatorapiv1.HostedClusterManagerConfiguration{
		RegistrationWebhookConfiguration: operatorapiv1.HostedWebhookConfiguration{Address: "localhost", Port: 443},
		WorkWebhookConfiguration:         operatorapiv1.HostedWebhookConfiguration{Address: "localhost", Port: 443},
	}
	tc := newTestController(t, cm)
	ns := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
	setup(t, tc, setDeployment(cm.Name, ns))
	// The hub clients are stubbed; the operator only needs a kubeconfig it can parse.
	if _, err := tc.managementKubeClient.CoreV1().Secrets(ns).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: helpers.ExternalHubKubeConfig, Namespace: ns},
		Data: map[string][]byte{"kubeconfig": []byte(`apiVersion: v1
kind: Config
clusters: [{name: hub, cluster: {server: "https://hub.example.com"}}]
contexts: [{name: hub, context: {cluster: hub, user: admin}}]
current-context: hub
users: [{name: admin, user: {token: test}}]
`)}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	var grpcServer bool
	tc.clusterManagerController.ensureSAKubeconfigs = func(_ context.Context, _, _ string, _ *rest.Config,
		_, _ kubernetes.Interface, _ events.Recorder, _, _, grpc bool) error {
		grpcServer = grpc
		return nil
	}
	_ = tc.clusterManagerController.sync(ctx, testingcommon.NewFakeSyncContext(t, cm.Name), cm.Name)
	if !grpcServer {
		t.Error("the gRPC server's kubeconfig wasn't requested for a spiffe-only hosted hub")
	}
}
