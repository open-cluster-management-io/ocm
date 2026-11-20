package addon

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonspiffe "open-cluster-management.io/ocm/pkg/common/spiffe"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

var errAPI = errors.New("injected API error")

// authConfig is an add-on auth config with the spiffe driver and the given settings (nil: none).
type authConfig struct{ opt *commonspiffe.Option }

func (authConfig) GetKubeClientAuth() string                          { return SPIFFEDriver }
func (authConfig) GetCSRConfiguration() register.CSRConfiguration     { return nil }
func (authConfig) GetTokenConfiguration() register.TokenConfiguration { return nil }
func (a authConfig) GetSPIFFEOption() *commonspiffe.Option            { return a.opt }

func spiffeOption(mutate func(*commonspiffe.Option)) *commonspiffe.Option {
	o := commonspiffe.NewOption()
	o.HubEndpoint, o.HubSPIFFEID = "hub:443", "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
	if mutate != nil {
		mutate(o)
	}
	return o
}

type spiffeRun struct {
	kube        *kubefake.Clientset
	addOnClient *addonfake.Clientset
	stop        context.CancelFunc
	err         error
}

// runSPIFFERegistration registers my-addon in the dedicated namespace my-addon-ns.
func runSPIFFERegistration(t *testing.T, cluster string, auth register.AddonAuthConfig, inLister bool,
	prepare func(kube *kubefake.Clientset, addOns *addonfake.Clientset)) spiffeRun {
	t.Helper()
	addOn := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: cluster}}
	addOnClient := addonfake.NewSimpleClientset(addOn)
	informers := addoninformers.NewSharedInformerFactory(addOnClient, 0)
	if inLister {
		if err := informers.Addon().V1beta1().ManagedClusterAddOns().Informer().GetStore().Add(addOn); err != nil {
			t.Fatal(err)
		}
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "my-addon-ns",
		Labels: map[string]string{addOnNamespaceLabel: "klusterlet"}}}
	kube := kubefake.NewSimpleClientset([]runtime.Object{ns}...)
	if prepare != nil {
		prepare(kube, addOnClient)
	}
	controller := &addOnRegistrationController{
		clusterName:     cluster,
		spokeKubeClient: kube,
		hubAddOnLister:  informers.Addon().V1beta1().ManagedClusterAddOns().Lister(),
		patcher: patcher.NewPatcher[
			*addonv1beta1.ManagedClusterAddOn, addonv1beta1.ManagedClusterAddOnSpec, addonv1beta1.ManagedClusterAddOnStatus](
			addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster)),
		addonDriverFactory: &testDriverFactory{},
		addonAuthConfig:    auth,
	}
	stop, err := controller.startRegistration(context.TODO(), registrationConfig{
		addOnName:          "my-addon",
		registration:       addonv1beta1.RegistrationConfig{Type: addonv1beta1.KubeClient, KubeClient: &addonv1beta1.KubeClientConfig{}},
		addonInstallOption: addonInstallOption{InstallationNamespace: "my-addon-ns"},
	})
	return spiffeRun{kube: kube, addOnClient: addOnClient, stop: stop, err: err}
}

func failOn(c *clienttesting.Fake, verb, resource string) {
	c.PrependReactor(verb, resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errAPI
	})
}

// When the add-on agent's settings can't be written, the registration is refused with
// SPIFFEConfigFailed and the reason.
func TestSPIFFEAddOnConfigFailures(t *testing.T) {
	cases := []struct {
		name    string
		cluster string
		auth    register.AddonAuthConfig
		want    string
	}{
		{"no spiffe settings", "cluster1", authConfig{}, "no spiffe settings"},
		{"an invalid hub ID", "cluster1", authConfig{spiffeOption(func(o *commonspiffe.Option) { o.HubSPIFFEID = "https://hub" })},
			"--spiffe-hub-id"},
		{"an invalid add-on ID template", "cluster1", authConfig{spiffeOption(func(o *commonspiffe.Option) {
			o.AddOnAgentIDTemplate = "spiffe://{trustDomain}/ocm/{cluster}/{addon}"
		})}, "{agent}"},
		{"a cluster name that is no SPIFFE ID segment", "bad name", authConfig{spiffeOption(nil)}, "path segment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runSPIFFERegistration(t, c.cluster, c.auth, true, nil)
			// Returned, so the registration is tried again.
			if r.err == nil || !strings.Contains(r.err.Error(), c.want) {
				t.Fatalf("error %v, want one mentioning %q", r.err, c.want)
			}
			got, err := r.addOnClient.AddonV1beta1().ManagedClusterAddOns(c.cluster).Get(context.TODO(), "my-addon", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, addonv1beta1.ManagedClusterAddOnRegistrationApplied)
			if cond == nil || cond.Reason != ReasonSPIFFEConfigFailed || !strings.Contains(cond.Message, c.want) {
				t.Errorf("condition %+v, want %s mentioning %q", cond, ReasonSPIFFEConfigFailed, c.want)
			}
		})
	}
}

// API errors are returned, so the registration is retried.
func TestSPIFFEAddOnAPIErrors(t *testing.T) {
	cases := []struct {
		name     string
		inLister bool
		prepare  func(kube *kubefake.Clientset, addOns *addonfake.Clientset)
	}{
		{"reading the install namespace", true, func(kube *kubefake.Clientset, _ *addonfake.Clientset) {
			failOn(&kube.Fake, "get", "namespaces")
		}},
		{"deleting the previous driver's hub kubeconfig", true, func(kube *kubefake.Clientset, _ *addonfake.Clientset) {
			failOn(&kube.Fake, "delete", "secrets")
		}},
		{"reading the add-on from the hub", false, nil},
		{"reporting a refusal", true, func(kube *kubefake.Clientset, addOns *addonfake.Clientset) {
			failOn(&kube.Fake, "create", "configmaps") // the refusal: SPIFFEConfigFailed
			failOn(&addOns.Fake, "patch", "managedclusteraddons")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runSPIFFERegistration(t, "cluster1", authConfig{spiffeOption(nil)}, c.inLister, c.prepare)
			if r.err == nil {
				r.stop()
				t.Fatal("no error")
			}
		})
	}
}

// Removing the add-on's settings when the registration stops may fail; it is logged, and the
// registration still stops.
func TestSPIFFEAddOnStopDeleteError(t *testing.T) {
	r := runSPIFFERegistration(t, "cluster1", authConfig{spiffeOption(nil)}, true, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	failOn(&r.kube.Fake, "delete", "configmaps")
	r.stop()
	if _, err := r.kube.CoreV1().ConfigMaps("my-addon-ns").Get(context.TODO(), SPIFFEConfigMapName("my-addon"),
		metav1.GetOptions{}); err != nil {
		t.Errorf("the ConfigMap should still be there after the failed delete: %v", err)
	}
}
