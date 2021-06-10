package certificate

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	certv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type testSignAgent struct {
	name string
	cert []byte
}

func (t *testSignAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return []runtime.Object{}, nil
}

func (t *testSignAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: t.name,
		Registration: &agent.RegistrationOption{
			CSRSign: func(csr *certv1.CertificateSigningRequest) []byte {
				return t.cert
			},
		},
	}
}

func TestSignReconcile(t *testing.T) {
	cases := []struct {
		name               string
		addon              []runtime.Object
		cluster            []runtime.Object
		csr                []runtime.Object
		testaddon          *testSignAgent
		validateCSRActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:               "no cluster",
			addon:              []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateCSRActions: addontesting.AssertNoActions,
			testaddon:          &testSignAgent{name: "test"},
		},
		{
			name:               "no addon",
			cluster:            []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			validateCSRActions: addontesting.AssertNoActions,
			testaddon:          &testSignAgent{name: "test"},
		},
		{
			name:    "csr with cert already",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			csr: []runtime.Object{func() *certv1.CertificateSigningRequest {
				cert := addontesting.NewApprovedCSR("test", "cluster1")
				cert.Status.Certificate = []byte("test")
				return cert
			}()},
			validateCSRActions: addontesting.AssertNoActions,
			testaddon:          &testSignAgent{name: "test"},
		},
		{
			name:               "denied csr",
			cluster:            []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon:              []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			csr:                []runtime.Object{addontesting.NewDeniedCSR("test", "cluster1")},
			validateCSRActions: addontesting.AssertNoActions,
			testaddon:          &testSignAgent{name: "test"},
		},
		{
			name:    "csr with cert already",
			cluster: []runtime.Object{addontesting.NewManagedCluster("cluster1")},
			addon:   []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			csr:     []runtime.Object{addontesting.NewApprovedCSR("test", "cluster1")},
			validateCSRActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				csr := actual.(*certv1.CertificateSigningRequest)
				if !reflect.DeepEqual(csr.Status.Certificate, []byte("test")) {
					t.Errorf("Expect certificate to be updated, actual %v", csr.Status.Certificate)
				}
			},
			testaddon: &testSignAgent{name: "test", cert: []byte("test")},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := fakecluster.NewSimpleClientset(c.cluster...)
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)
			fakeKubeClient := fakekube.NewSimpleClientset(c.csr...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
			clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)
			kubeInfomers := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 10*time.Minute)

			for _, obj := range c.cluster {
				clusterInformers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(obj)
			}
			for _, obj := range c.addon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}
			for _, csr := range c.csr {
				kubeInfomers.Certificates().V1().CertificateSigningRequests().Informer().GetStore().Add(csr)
			}

			controller := &csrSignController{
				kubeClient:                fakeKubeClient,
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
				managedClusterLister:      clusterInformers.Cluster().V1().ManagedClusters().Lister(),
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				csrLister:                 kubeInfomers.Certificates().V1().CertificateSigningRequests().Lister(),
			}

			for _, obj := range c.csr {
				csr := obj.(*certv1.CertificateSigningRequest)
				syncContext := addontesting.NewFakeSyncContext(t, fmt.Sprintf("%s", csr.Name))
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateCSRActions(t, fakeKubeClient.Actions())
			}
		})
	}
}
