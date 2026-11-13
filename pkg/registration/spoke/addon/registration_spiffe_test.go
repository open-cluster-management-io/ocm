package addon

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonspiffe "open-cluster-management.io/ocm/pkg/common/spiffe"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

func TestSPIFFEAddOnRegistration(t *testing.T) {
	const cluster = "cluster1"
	dedicated := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "my-addon-ns",
		Labels: map[string]string{addOnNamespaceLabel: "klusterlet"}}}
	shared := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}

	cases := []struct {
		name       string
		regType    addonv1beta1.RegistrationType
		namespace  string
		current    *metav1.Condition
		wantReason string // "" means the condition must be left alone
	}{
		{"kubeClient in a dedicated namespace: the hub owns the condition", addonv1beta1.KubeClient, "my-addon-ns", nil, ""},
		{"customSigner", addonv1beta1.CustomSigner, "my-addon-ns", nil, ReasonCustomSignerNotSupported},
		{"shared namespace", addonv1beta1.KubeClient, "default", nil, ReasonAddOnNamespaceNotDedicated},
		{"missing namespace", addonv1beta1.KubeClient, "nowhere", nil, ReasonAddOnNamespaceNotDedicated},
		{"refusal cleared once the namespace is dedicated", addonv1beta1.KubeClient, "my-addon-ns",
			&metav1.Condition{Type: addonv1beta1.ManagedClusterAddOnRegistrationApplied, Status: metav1.ConditionFalse,
				Reason: ReasonAddOnNamespaceNotDedicated}, ReasonSPIFFERegistrationReady},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addOn := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: cluster}}
			if c.current != nil {
				addOn.Status.Conditions = []metav1.Condition{*c.current}
			}
			addonClient := addonfake.NewSimpleClientset(addOn)
			informers := addoninformers.NewSharedInformerFactory(addonClient, 0)
			if err := informers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(addOn); err != nil {
				t.Fatal(err)
			}
			kubeClient := kubefake.NewSimpleClientset([]runtime.Object{dedicated, shared}...)
			controller := &addOnRegistrationController{
				clusterName:     cluster,
				spokeKubeClient: kubeClient,
				hubAddOnLister:  informers.Addon().V1beta1().ManagedClusterAddOns().Lister(),
				patcher: patcher.NewPatcher[
					*addonv1beta1.ManagedClusterAddOn, addonv1beta1.ManagedClusterAddOnSpec, addonv1beta1.ManagedClusterAddOnStatus](
					addonClient.AddonV1beta1().ManagedClusterAddOns(cluster)),
				addonDriverFactory: &testDriverFactory{},
				addonAuthConfig:    spiffeAuthConfig{},
			}
			stop, err := controller.startRegistration(context.TODO(), registrationConfig{
				addOnName: "my-addon",
				registration: addonv1beta1.RegistrationConfig{Type: c.regType, KubeClient: &addonv1beta1.KubeClientConfig{
					Subject: addonv1beta1.KubeClientSubject{BaseSubject: addonv1beta1.BaseSubject{
						User: "system:open-cluster-management:cluster:cluster1:addon:my-addon:agent:my-agent"}}}},
				addonInstallOption: addonInstallOption{InstallationNamespace: c.namespace},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()

			for _, a := range kubeClient.Actions() {
				if a.GetVerb() == "create" && a.GetResource().Resource != "configmaps" {
					t.Errorf("created %s on the spoke; the spiffe driver creates no credential", a.GetResource().Resource)
				}
			}
			cm, cmErr := kubeClient.CoreV1().ConfigMaps(c.namespace).Get(context.TODO(), SPIFFEConfigMapName("my-addon"), metav1.GetOptions{})
			if c.wantReason == "" || c.wantReason == ReasonSPIFFERegistrationReady {
				if cmErr != nil {
					t.Fatalf("no SPIFFE ConfigMap: %v", cmErr)
				}
				want := map[string]string{
					SPIFFEConfigHubEndpoint:       "hub:443",
					SPIFFEConfigHubSPIFFEID:       "spiffe://ocm-hub.example.org/ocm/hub/grpc-server",
					SPIFFEConfigAgentSPIFFEID:     "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/addon/my-addon/agent/my-agent",
					SPIFFEConfigWorkloadAPISocket: "unix:///spiffe-workload-api/spire-agent.sock",
				}
				for k, v := range want {
					if cm.Data[k] != v {
						t.Errorf("ConfigMap %s = %q, want %q", k, cm.Data[k], v)
					}
				}
				stop2, _ := controller.startRegistration(context.TODO(), registrationConfig{addOnName: "my-addon",
					registration: addonv1beta1.RegistrationConfig{Type: c.regType}, addonInstallOption: addonInstallOption{InstallationNamespace: c.namespace}})
				stop2()
				if _, err := kubeClient.CoreV1().ConfigMaps(c.namespace).Get(context.TODO(), SPIFFEConfigMapName("my-addon"), metav1.GetOptions{}); err == nil {
					t.Error("the ConfigMap stays after the registration stops")
				}
			} else if cmErr == nil {
				t.Errorf("wrote a SPIFFE ConfigMap for a refused registration")
			}
			got, err := addonClient.AddonV1beta1().ManagedClusterAddOns(cluster).Get(context.TODO(), "my-addon", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, addonv1beta1.ManagedClusterAddOnRegistrationApplied)
			if c.wantReason == "" {
				if cond != nil {
					t.Errorf("condition %+v, want it left to the hub", cond)
				}
				return
			}
			if cond == nil || cond.Reason != c.wantReason {
				t.Errorf("condition %+v, want reason %s", cond, c.wantReason)
			}
		})
	}
}

// spiffeAuthConfig is the agent's add-on auth config with the spiffe driver and its settings.
type spiffeAuthConfig struct{}

func (spiffeAuthConfig) GetKubeClientAuth() string                          { return SPIFFEDriver }
func (spiffeAuthConfig) GetCSRConfiguration() register.CSRConfiguration     { return nil }
func (spiffeAuthConfig) GetTokenConfiguration() register.TokenConfiguration { return nil }
func (spiffeAuthConfig) GetSPIFFEOption() *commonspiffe.Option {
	o := commonspiffe.NewOption()
	o.HubEndpoint, o.HubSPIFFEID = "hub:443", "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	return o
}
