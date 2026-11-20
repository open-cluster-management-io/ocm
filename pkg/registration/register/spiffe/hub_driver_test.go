package spiffe

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func registeredBy(id string) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}}
	if id != "" {
		c.Annotations = map[string]string{clusterv1.SPIFFERegisteredByAnnotationKey: id,
			clusterv1.SPIFFETrustDomainAnnotationKey: "ocm-hub.example.org"}
	}
	return c
}

func TestHubDriverAccept(t *testing.T) {
	const agent = "spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/registration"
	cases := []struct {
		name     string
		patterns []string
		cluster  *clusterv1.ManagedCluster
		want     bool
	}{
		{"a cluster no spiffe agent registered is another driver's", nil, registeredBy(""), true},
		{"no auto-approved identity: never", nil, registeredBy(agent), false},
		{"an exact match", []string{agent}, registeredBy(agent), true},
		{"a segment wildcard", []string{"spiffe://ocm-hub.example.org/ocm/cluster/*/agent/registration"}, registeredBy(agent), true},
		{"another trust domain", []string{"spiffe://east.example.org/ocm/cluster/*/agent/registration"}, registeredBy(agent), false},
		{"* doesn't cross segments", []string{"spiffe://ocm-hub.example.org/*/registration"}, registeredBy(agent), false},
		{"the singleton agent isn't the registration agent", []string{"spiffe://ocm-hub.example.org/ocm/cluster/*/agent/registration"},
			registeredBy("spiffe://ocm-hub.example.org/ocm/cluster/cluster1/agent/klusterlet"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := NewHubDriver(c.patterns)
			if err != nil {
				t.Fatal(err)
			}
			if got := d.Accept(c.cluster); got != c.want {
				t.Errorf("Accept = %v, want %v", got, c.want)
			}
		})
	}
}

func TestHubDriverNoOps(t *testing.T) {
	d, err := NewHubDriver(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreatePermissions(context.TODO(), registeredBy("")); err != nil {
		t.Error(err)
	}
	if err := d.Cleanup(context.TODO(), registeredBy("")); err != nil {
		t.Error(err)
	}
	d.Run(context.TODO(), 1) // returns at once: nothing to run
}

func TestNewHubDriverInvalidPattern(t *testing.T) {
	if _, err := NewHubDriver([]string{"spiffe://ocm-hub.example.org/ocm/[cluster"}); err == nil {
		t.Error("no error for an invalid pattern")
	}
}
