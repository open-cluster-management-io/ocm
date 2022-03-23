package addon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	"open-cluster-management.io/registration/pkg/clientcert"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestFilterCSREvents(t *testing.T) {
	clusterName := "cluster1"
	addonName := "addon1"
	signerName := "signer1"

	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "csr not from the managed cluster",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr not for the addon",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr with different signer name",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "valid csr",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// the labels are only hints. Anyone could set/modify them.
						clientcert.ClusterNameLabel: clusterName,
						clientcert.AddonNameLabel:   addonName,
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: signerName,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filterFunc := createCSREventFilterFunc(clusterName, addonName, signerName)
			actual := filterFunc(c.csr)
			if actual != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, actual)
			}
		})
	}
}

func TestRegistrationSync(t *testing.T) {
	clusterName := "cluster1"
	addonName := "addon1"
	signerName := "signer1"

	config1 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
	}

	config2 := addonv1alpha1.RegistrationConfig{
		SignerName: signerName,
		Subject: addonv1alpha1.Subject{
			User: addonName,
		},
	}

	cases := []struct {
		name                                 string
		queueKey                             string
		addOn                                *addonv1alpha1.ManagedClusterAddOn
		addOnRegistrationConfigs             map[string]map[string]registrationConfig
		expectedAddOnRegistrationConfigHashs map[string][]string
		validateActions                      func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "addon registration not enabled",
			queueKey: addonName,
			addOn:    newManagedClusterAddOn(clusterName, addonName, nil),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
			},
		},
		{
			name:     "addon registration enabled",
			queueKey: addonName,
			addOn:    newManagedClusterAddOn(clusterName, addonName, []addonv1alpha1.RegistrationConfig{config1}),
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config1)},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expect 0 actions but got %d", len(actions))
				}
			},
		},
		{
			name:     "addon registration updated",
			queueKey: addonName,
			addOn:    newManagedClusterAddOn(clusterName, addonName, []addonv1alpha1.RegistrationConfig{config2}),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1): {
						secretName:            "secret1",
						installationNamespace: addonName,
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config2)},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "addon is deleted",
			queueKey: addonName,
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1): {
						secretName:            "secret1",
						installationNamespace: addonName,
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
		{
			name:     "resync",
			queueKey: factory.DefaultQueueKey,
			addOn:    newManagedClusterAddOn(clusterName, addonName, []addonv1alpha1.RegistrationConfig{config1}),
			addOnRegistrationConfigs: map[string]map[string]registrationConfig{
				addonName: {
					hash(config1): {
						secretName:            "secret1",
						installationNamespace: addonName,
					},
				},
				"addon2": {
					hash(config1): {
						secretName:            "secret2",
						installationNamespace: "addon2",
					},
				},
			},
			expectedAddOnRegistrationConfigHashs: map[string][]string{
				addonName: {hash(config1)},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions but got %d", len(actions))
				}
				testinghelpers.AssertActions(t, actions, "delete")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset()
			addons := []runtime.Object{}
			if c.addOn != nil {
				addons = append(addons, c.addOn)
			}
			addonClient := addonfake.NewSimpleClientset(addons...)
			addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, time.Minute*10)
			addonStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
			if c.addOn != nil {
				addonStore.Add(c.addOn)
			}

			if c.addOnRegistrationConfigs == nil {
				c.addOnRegistrationConfigs = map[string]map[string]registrationConfig{}
			}

			controller := addOnRegistrationController{
				clusterName:     clusterName,
				spokeKubeClient: kubeClient,
				hubAddOnLister:  addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				recorder:        eventstesting.NewTestingEventRecorder(t),
				startRegistrationFunc: func(ctx context.Context, config registrationConfig) context.CancelFunc {
					_, cancel := context.WithCancel(context.Background())
					return cancel
				},
				addOnRegistrationConfigs: c.addOnRegistrationConfigs,
			}

			err := controller.sync(context.Background(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(c.expectedAddOnRegistrationConfigHashs) != len(controller.addOnRegistrationConfigs) {
				t.Errorf("expected %d addOns, but got %d", len(c.expectedAddOnRegistrationConfigHashs), len(controller.addOnRegistrationConfigs))
			}

			for addOnName, hashs := range c.expectedAddOnRegistrationConfigHashs {
				addonRegistrationConfigs := controller.addOnRegistrationConfigs[addOnName]
				if len(addonRegistrationConfigs) != len(hashs) {
					t.Errorf("expected %d config items for addOn %q, but got %d", len(hashs), addOnName, len(addonRegistrationConfigs))
				}
				for _, hash := range hashs {
					if _, ok := addonRegistrationConfigs[hash]; !ok {
						t.Errorf("registration config with hash %q is not found for addOn %q", hash, addOnName)
					}
				}
			}

			if c.validateActions != nil {
				c.validateActions(t, kubeClient.Actions())
			}
		})
	}
}

func newManagedClusterAddOn(namespace, name string, registrations []addonv1alpha1.RegistrationConfig) *addonv1alpha1.ManagedClusterAddOn {
	return &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: registrations,
		},
	}
}

func hash(registration addonv1alpha1.RegistrationConfig) string {
	data, _ := json.Marshal(registration)
	h := sha256.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}
