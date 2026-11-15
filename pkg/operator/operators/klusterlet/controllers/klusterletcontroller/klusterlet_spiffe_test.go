package klusterletcontroller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	klusterlet.Spec.DeployOption.Mode = mode
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{
		AuthType: operatorapiv1.SPIFFEAuthType, SPIFFE: spiffe}
	klusterlet.Spec.RegistrationConfiguration.AddOnKubeClientRegistrationDriver =
		&operatorapiv1.AddOnRegistrationDriver{AuthType: operatorapiv1.SPIFFEAuthType}

	agentNamespace := helpers.AgentNamespace(klusterlet)
	syncContext := testingcommon.NewFakeSyncContext(t, name)
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false, newNamespace(agentNamespace))
	servePolicyAPI(controller.kubeClient, "system:serviceaccount:open-cluster-management:klusterlet")
	if controller.managedKubeClient != nil {
		servePolicyAPI(controller.managedKubeClient, "system:admin")
	}
	// The first sync runs recovery and stores the admission record; the agents start on the next.
	if err := controller.controller.sync(context.TODO(), syncContext, name); err != nil {
		t.Fatalf("sync: %v", err)
	}
	stored, err := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.operatorStore.Update(stored); err != nil {
		t.Fatal(err)
	}
	if err := controller.controller.sync(context.TODO(), syncContext, name); err != nil {
		t.Fatalf("second sync: %v", err)
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
	// The status as stored after both syncs.
	final, err := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r.status = final
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
			name: "Default, east, namespace template", klusterlet: "klusterlet", mode: operatorapiv1.InstallModeDefault,
			spiffe: &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "ocm-grpc.hub.internal:443", HubSPIFFEID: testSPIFFEHubID,
				TrustDomain: "east.example.org", AgentIDTemplate: "spiffe://{trustDomain}/k8s/{cluster}/ns/{namespace}/ocm/{role}"},
			want: map[string][3]string{
				"klusterlet-registration-agent": {"registration", "klusterlet-registration-sa",
					"spiffe://east.example.org/k8s/cluster1/ns/open-cluster-management-agent/ocm/registration"},
				"klusterlet-work-agent": {"work", "klusterlet-work-sa",
					"spiffe://east.example.org/k8s/cluster1/ns/open-cluster-management-agent/ocm/work"},
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

// The spiffe driver supports only the Default and Singleton modes: a hosted Klusterlet gets no
// agents, no SPIFFE IDs and a false Applied condition, before its managed kubeconfig is looked for.
func TestSPIFFEKlusterletHostedModesNotSupported(t *testing.T) {
	for _, mode := range []operatorapiv1.InstallMode{operatorapiv1.InstallModeHosted,
		operatorapiv1.InstallModeSingletonHosted} {
		t.Run(string(mode), func(t *testing.T) {
			klusterlet := newKlusterletHosted("cluster4", "open-cluster-management-agent", "cluster4")
			klusterlet.Spec.DeployOption.Mode = mode
			klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{
				AuthType: operatorapiv1.SPIFFEAuthType,
				SPIFFE:   &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "hub:443", HubSPIFFEID: testSPIFFEHubID}}
			syncContext := testingcommon.NewFakeSyncContext(t, "cluster4")
			// No external managed kubeconfig Secret: the refusal must not depend on it.
			controller := newTestControllerHosted(t, klusterlet, nil, newNamespace(helpers.AgentNamespace(klusterlet)))
			if err := controller.controller.sync(context.TODO(), syncContext, "cluster4"); err != nil {
				t.Fatalf("sync: %v", err)
			}
			for _, a := range controller.kubeClient.Actions() {
				if a.GetVerb() == createVerb {
					t.Errorf("created %s %s", a.GetResource().Resource, a.(clienttesting.CreateActionImpl).Object.(metav1.Object).GetName())
				}
			}
			got, err := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), "cluster4", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
			if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != ReasonSPIFFEInstallModeNotSupported ||
				!strings.Contains(cond.Message, string(mode)) {
				t.Errorf("Applied condition %+v", cond)
			}
			if got.Status.SPIFFE != nil && len(got.Status.SPIFFE.AgentIDs) != 0 {
				t.Errorf("agentIDs %v reported for an unsupported mode", got.Status.SPIFFE.AgentIDs)
			}
		})
	}
}

func TestSPIFFESupportsInstallMode(t *testing.T) {
	for mode, want := range map[operatorapiv1.InstallMode]bool{
		"": true, operatorapiv1.InstallModeDefault: true, operatorapiv1.InstallModeSingleton: true,
		operatorapiv1.InstallModeHosted: false, operatorapiv1.InstallModeSingletonHosted: false,
	} {
		if got := SPIFFESupportsInstallMode(mode); got != want {
			t.Errorf("SPIFFESupportsInstallMode(%q) = %v, want %v", mode, got, want)
		}
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

// Another controller writes the status during the first sync: the admission record and the
// add-on namespace's UID are still stored, or the next sync would distrust the namespace.
func TestSPIFFEStatusStoredDespiteConflict(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "open-cluster-management-agent", "cluster1")
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{
		AuthType: operatorapiv1.SPIFFEAuthType,
		SPIFFE:   &operatorapiv1.KlusterletSPIFFEConfig{HubEndpoint: "hub:443", HubSPIFFEID: testSPIFFEHubID}}
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		newNamespace("open-cluster-management-agent"))
	servePolicyAPI(controller.kubeClient, "system:serviceaccount:open-cluster-management:klusterlet")
	conflicts := 1
	controller.operatorClient.PrependReactor("patch", "klusterlets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "status" && conflicts > 0 {
			conflicts--
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: operatorapiv1.GroupName, Resource: "klusterlets"}, "klusterlet", fmt.Errorf("modified"))
		}
		return false, nil, nil
	})
	_ = controller.controller.sync(context.TODO(), syncContext, "klusterlet")

	stored, err := controller.operatorClient.OperatorV1().Klusterlets().Get(context.TODO(), "klusterlet", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status.SPIFFE == nil || len(stored.Status.SPIFFE.AdmissionPolicies) != 3 ||
		stored.Status.SPIFFE.AddOnNamespace == nil {
		t.Fatalf("status.spiffe %+v, want the admission record and the add-on namespace", stored.Status.SPIFFE)
	}
	if c := meta.FindStatusCondition(stored.Status.Conditions, operatorapiv1.ConditionSPIFFEAdmissionReady); c == nil ||
		c.Status != metav1.ConditionTrue {
		t.Errorf("SPIFFEAdmissionReady %+v", c)
	}
}
