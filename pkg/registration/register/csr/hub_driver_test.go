package csr

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
)

func TestAccept(t *testing.T) {
	cases := []struct {
		name       string
		cluster    *clusterv1.ManagedCluster
		isAccepted bool
	}{
		{
			name: "Accept cluster when annotations not present",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster1",
				},
			},
			isAccepted: true,
		},
	}

	kubeClient := kubefake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	csrHubDriver, err := NewCSRHubDriver(kubeClient, informerFactory, []string{})

	if err != nil {
		t.Error(err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAccepted := csrHubDriver.Accept(c.cluster)
			if c.isAccepted != isAccepted {
				t.Errorf("expect %t, but %t", c.isAccepted, isAccepted)
			}
		},
		)
	}
}
