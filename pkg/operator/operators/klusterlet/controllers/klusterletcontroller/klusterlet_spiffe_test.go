package klusterletcontroller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const testSPIFFEHubID = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"

type spiffeKlusterletRendering struct {
	deployments map[string]*appsv1.Deployment
	status      *operatorapiv1.Klusterlet
}

func renderSPIFFEKlusterlet(t *testing.T, name string, mode operatorapiv1.InstallMode,
	spiffe *operatorapiv1.KlusterletSPIFFEConfig) spiffeKlusterletRendering {
	t.Helper()
	klusterlet := newKlusterlet(name, "open-cluster-management-agent", "cluster1")
	if helpers.IsHosted(mode) {
		klusterlet = newKlusterletHosted(name, "open-cluster-management-agent", "cluster4")
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
			Message: "Klusterlet is ready to apply, the external managed kubeconfig secret was created at: " +
				hostedKubeconfigCreationTime,
		})
	}
	klusterlet.Spec.DeployOption.Mode = mode
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{
		AuthType: operatorapiv1.SPIFFEAuthType, SPIFFE: spiffe}
	klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver =
		&operatorapiv1.AddOnRegistrationDriver{AuthType: operatorapiv1.SPIFFEAuthType}

	agentNamespace := helpers.AgentNamespace(klusterlet)
	syncContext := testingcommon.NewFakeSyncContext(t, name)
	var controller *testController
	if helpers.IsHosted(mode) {
		controller = newTestControllerHosted(t, klusterlet, nil, newNamespace(agentNamespace),
			newSecret(helpers.ImagePullSecret, "open-cluster-management"))
	} else {
		controller = newTestController(t, klusterlet, syncContext.Recorder(), nil, false, newNamespace(agentNamespace))
	}
	servePolicyAPI(controller.kubeClient, "system:serviceaccount:open-cluster-management:klusterlet")
	if controller.managedKubeClient != nil {
		servePolicyAPI(controller.managedKubeClient, "system:admin")
	}
	if err := controller.controller.sync(context.TODO(), syncContext, name); err != nil {
		t.Fatalf("sync: %v", err)
	}

	r := spiffeKlusterletRendering{deployments: map[string]*appsv1.Deployment{}}
	for _, a := range controller.kubeClient.Actions() {
		if a.GetVerb() != createVerb {
			continue
		}
		if d, ok := a.(clienttesting.CreateActionImpl).Object.(*appsv1.Deployment); ok {
			r.deployments[d.Name] = d
		}
		if s, ok := a.(clienttesting.CreateActionImpl).Object.(interface{ GetName() string }); ok &&
			(s.GetName() == helpers.HubKubeConfig || s.GetName() == helpers.BootstrapHubKubeConfig) {
			t.Errorf("created %s in spiffe mode", s.GetName())
		}
	}
	for _, a := range controller.operatorClient.Actions() {
		p, ok := a.(clienttesting.PatchActionImpl)
		if !ok || p.GetSubresource() != "status" {
			continue
		}
		k := &operatorapiv1.Klusterlet{}
		if err := json.Unmarshal(p.Patch, k); err != nil {
			t.Fatal(err)
		}
		r.status = k
	}
	return r
}

func TestSPIFFEKlusterletRendering(t *testing.T) {
	spiffe := &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "ocm-grpc.hub.internal:443", HubSPIFFEID: testSPIFFEHubID}
	cases := []struct {
		name       string
		klusterlet string
		mode       operatorapiv1.InstallMode
		spiffe     *operatorapiv1.KlusterletSPIFFEConfig
		// deployment name -> role, ServiceAccount, SPIFFE ID
		want map[string][3]string
	}{
		{
			name: "Default", klusterlet: "klusterlet", mode: operatorapiv1.InstallModeDefault, spiffe: spiffe,
			want: map[string][3]string{
				"klusterlet-registration-agent": {"registration", "klusterlet-registration-sa",
					"spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/registration"},
				"klusterlet-work-agent": {"work", "klusterlet-work-sa",
					"spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/work"},
			},
		},
		{
			name: "Singleton", klusterlet: "klusterlet", mode: operatorapiv1.InstallModeSingleton, spiffe: spiffe,
			want: map[string][3]string{
				"klusterlet-agent": {"klusterlet", "klusterlet-work-sa",
					"spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/klusterlet"},
			},
		},
		{
			name: "Hosted, east, namespace template", klusterlet: "cluster4", mode: operatorapiv1.InstallModeHosted,
			spiffe: &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "ocm-grpc.hub.internal:443", HubSPIFFEID: testSPIFFEHubID,
				TrustDomain: "east.example.org", AgentIDTemplate: "spiffe://{trustDomain}/k8s/{cluster}/ns/{namespace}/ocm/{role}"},
			want: map[string][3]string{
				"cluster4-registration-agent": {"registration", "cluster4-registration-sa",
					"spiffe://east.example.org/k8s/cluster4/ns/cluster4/ocm/registration"},
				"cluster4-work-agent": {"work", "cluster4-work-sa",
					"spiffe://east.example.org/k8s/cluster4/ns/cluster4/ocm/work"},
			},
		},
		{
			name: "SingletonHosted", klusterlet: "cluster4", mode: operatorapiv1.InstallModeSingletonHosted, spiffe: spiffe,
			want: map[string][3]string{
				"cluster4-agent": {"klusterlet", "cluster4-work-sa",
					"spiffe://ocm-hub.example.org/ocm/cluster/cluster4/agent/klusterlet"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := renderSPIFFEKlusterlet(t, c.klusterlet, c.mode, c.spiffe)
			var wantIDs []operatorapiv1.SPIFFEAgentID
			for name, w := range c.want {
				d := r.deployments[name]
				if d == nil {
					t.Fatalf("deployment %s not rendered; got %v", name, keys(r.deployments))
				}
				if got := d.Spec.Template.Labels["open-cluster-management.io/spiffe-role"]; got != w[0] {
					t.Errorf("%s: spiffe-role %q, want %q", name, got, w[0])
				}
				if got := d.Spec.Template.Spec.ServiceAccountName; got != w[1] {
					t.Errorf("%s: ServiceAccount %q, want %q", name, got, w[1])
				}
				args := strings.Join(d.Spec.Template.Spec.Containers[0].Args, " ")
				for _, want := range []string{"--spiffe-agent-id=" + w[2], "--spiffe-hub-endpoint=ocm-grpc.hub.internal:443",
					"--spiffe-hub-id=" + testSPIFFEHubID, "--spiffe-workload-api-socket=unix:///spiffe-workload-api/spire-agent.sock"} {
					if !strings.Contains(args, want) {
						t.Errorf("%s: args %q miss %q", name, args, want)
					}
				}
				if w[0] == "registration" && !strings.Contains(args, KlusterletNameClusterAnnotation+"=") {
					t.Errorf("%s: args %q don't report the Klusterlet name", name, args)
				}
				for _, unwanted := range []string{"--bootstrap-kubeconfig", "--workload-source-config", "--grpc-config"} {
					if strings.Contains(args, unwanted) {
						t.Errorf("%s: args %q have %q", name, args, unwanted)
					}
				}
				csi := false
				for _, v := range d.Spec.Template.Spec.Volumes {
					if v.Name == "spiffe-workload-api" && v.CSI != nil && v.CSI.Driver == "csi.spiffe.io" {
						csi = true
					}
					if v.Secret != nil && (v.Secret.SecretName == helpers.HubKubeConfig || v.Secret.SecretName == helpers.BootstrapHubKubeConfig) {
						t.Errorf("%s: mounts Secret %s in spiffe mode", name, v.Secret.SecretName)
					}
				}
				if !csi {
					t.Errorf("%s: no csi.spiffe.io volume", name)
				}
				if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
					t.Errorf("%s: scaled to 0 without a hub kubeconfig Secret", name)
				}
				wantIDs = append(wantIDs, operatorapiv1.SPIFFEAgentID{Role: w[0], SPIFFEID: w[2]})
			}
			if r.status == nil || r.status.Status.SPIFFE == nil {
				t.Fatalf("status.spiffe not reported")
			}
			if len(r.status.Status.SPIFFE.AgentIDs) != len(wantIDs) {
				t.Errorf("agentIDs %v, want %v", r.status.Status.SPIFFE.AgentIDs, wantIDs)
			}
			for _, want := range wantIDs {
				found := false
				for _, got := range r.status.Status.SPIFFE.AgentIDs {
					found = found || got == want
				}
				if !found {
					t.Errorf("agentIDs %v miss %v", r.status.Status.SPIFFE.AgentIDs, want)
				}
			}
		})
	}
}

func TestSPIFFEKlusterletInvalidTemplate(t *testing.T) {
	r := renderSPIFFEKlusterlet(t, "klusterlet", operatorapiv1.InstallModeDefault, &operatorapiv1.KlusterletSPIFFEConfig{
		HubEndpoint: "hub:443", HubSPIFFEID: testSPIFFEHubID, AgentIDTemplate: "spiffe://{trustDomain}/ocm/{clutser}/{role}"})
	if len(r.deployments) != 0 {
		t.Errorf("rendered %v with an invalid template", keys(r.deployments))
	}
	cond := meta.FindStatusCondition(r.status.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonInvalidSPIFFEConfiguration ||
		!strings.Contains(cond.Message, "{clutser}") {
		t.Errorf("Applied condition %+v", cond)
	}
}

func keys(m map[string]*appsv1.Deployment) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// servePolicyAPI makes a fake client serve the ValidatingAdmissionPolicy API and answer
// SelfSubjectReviews as user.
func servePolicyAPI(c *fakekube.Clientset, user string) {
	c.Resources = append(c.Resources, &metav1.APIResourceList{
		GroupVersion: admissionv1.SchemeGroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: "validatingadmissionpolicies"}, {Name: "validatingadmissionpolicybindings"}},
	})
	c.PrependReactor("create", "selfsubjectreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, &authenticationv1.SelfSubjectReview{Status: authenticationv1.SelfSubjectReviewStatus{
			UserInfo: authenticationv1.UserInfo{Username: user}}}, nil
	})
}
