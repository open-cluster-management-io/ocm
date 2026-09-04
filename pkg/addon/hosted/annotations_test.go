package hosted

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestDerivedAddonHostingClusterName(t *testing.T) {
	cases := []struct {
		name     string
		cluster  *clusterv1.ManagedCluster
		expected string
	}{
		{
			name:     "nil cluster",
			cluster:  nil,
			expected: "",
		},
		{
			name: "hosted addons enabled",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationKlusterletDeployMode:         "Hosted",
						AnnotationKlusterletHostingClusterName: "local-cluster",
						AnnotationEnableHostedModeAddons:       "true",
					},
				},
			},
			expected: "local-cluster",
		},
		{
			name: "singleton hosted deploy mode",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationKlusterletDeployMode:         "SingletonHosted",
						AnnotationKlusterletHostingClusterName: "local-cluster",
						AnnotationEnableHostedModeAddons:       "true",
					},
				},
			},
			expected: "local-cluster",
		},
		{
			name: "hosted addons gate disabled",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationKlusterletDeployMode:         "Hosted",
						AnnotationKlusterletHostingClusterName: "local-cluster",
						AnnotationEnableHostedModeAddons:       "false",
					},
				},
			},
			expected: "",
		},
		{
			name: "default deploy mode",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationKlusterletDeployMode:         "Default",
						AnnotationKlusterletHostingClusterName: "local-cluster",
						AnnotationEnableHostedModeAddons:       "true",
					},
				},
			},
			expected: "",
		},
		{
			name: "missing hosting cluster name",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationKlusterletDeployMode:   "Hosted",
						AnnotationEnableHostedModeAddons: "true",
					},
				},
			},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DerivedAddonHostingClusterName(c.cluster)
			if got != c.expected {
				t.Fatalf("expected %q, got %q", c.expected, got)
			}
		})
	}
}

func TestAddonAnnotationsFromManagedCluster_nilCluster(t *testing.T) {
	if got := AddonAnnotationsFromManagedCluster(nil); len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestAddonAnnotationsFromManagedCluster(t *testing.T) {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Annotations: map[string]string{
				AnnotationKlusterletDeployMode:                   "Hosted",
				AnnotationKlusterletHostingClusterName:           "local-cluster",
				AnnotationEnableHostedModeAddons:                 "true",
				addonv1beta1.HostedManifestLocationAnnotationKey: "hosting",
				"non-addon-annotation":                           "ignored",
			},
		},
	}

	annotations := AddonAnnotationsFromManagedCluster(cluster)
	if annotations[addonv1beta1.HostingClusterNameAnnotationKey] != "local-cluster" {
		t.Fatalf("expected hosting cluster name local-cluster, got %q",
			annotations[addonv1beta1.HostingClusterNameAnnotationKey])
	}
	if annotations[addonv1beta1.HostedManifestLocationAnnotationKey] != "hosting" {
		t.Fatalf("expected hosted manifest location hosting, got %q",
			annotations[addonv1beta1.HostedManifestLocationAnnotationKey])
	}
	if annotations[AnnotationEnableHostedModeAddons] != "true" {
		t.Fatalf("expected enable-hosted-mode-addons true, got %q",
			annotations[AnnotationEnableHostedModeAddons])
	}
	if _, ok := annotations["non-addon-annotation"]; ok {
		t.Fatal("non-addon annotation should not be copied")
	}
}

func TestAddonAnnotationsFromManagedCluster_excludesNearMatchKeys(t *testing.T) {
	nearMatchKey := addonv1beta1.GroupName + "-extra/near-match"
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Annotations: map[string]string{
				nearMatchKey: "should-not-copy",
				addonv1beta1.HostedManifestLocationAnnotationKey: "hosting",
			},
		},
	}

	annotations := AddonAnnotationsFromManagedCluster(cluster)
	if _, ok := annotations[nearMatchKey]; ok {
		t.Fatalf("near-match annotation %q should not be copied", nearMatchKey)
	}
	if annotations[addonv1beta1.HostedManifestLocationAnnotationKey] != "hosting" {
		t.Fatalf("expected hosted manifest location hosting, got %q",
			annotations[addonv1beta1.HostedManifestLocationAnnotationKey])
	}
}
