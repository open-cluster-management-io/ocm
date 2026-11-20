package chart

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	operatorv1 "open-cluster-management.io/api/operator/v1"
)

// The klusterlet-spiffe ClusterRole and its binding are rendered only where the operator needs
// them: for a spiffe Klusterlet, or when asked for, and never without the operator.
func TestKlusterletChartSPIFFERBAC(t *testing.T) {
	spiffe := func(c *KlusterletChartConfig) {
		c.Klusterlet.RegistrationConfiguration = operatorv1.RegistrationConfiguration{
			RegistrationDriver: operatorv1.RegistrationDriver{
				AuthType: operatorv1.SPIFFEAuthType,
				SPIFFE: &operatorv1.KlusterletSPIFFEConfig{HubEndpoint: "hub:443",
					HubSPIFFEID: "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"},
			},
		}
	}
	cases := []struct {
		name   string
		mutate func(c *KlusterletChartConfig)
		want   bool
	}{
		{"the default driver", func(c *KlusterletChartConfig) {}, false},
		{"the csr driver", func(c *KlusterletChartConfig) {
			c.Klusterlet.RegistrationConfiguration.RegistrationDriver.AuthType = operatorv1.CSRAuthType
		}, false},
		{"the spiffe driver", spiffe, true},
		{"the spiffe driver in Default mode", func(c *KlusterletChartConfig) {
			spiffe(c)
			c.Klusterlet.Mode = operatorv1.InstallModeDefault
		}, true},
		{"enableSPIFFE without a spiffe Klusterlet", func(c *KlusterletChartConfig) { c.EnableSPIFFE = true }, true},
		{"the spiffe driver without the operator", func(c *KlusterletChartConfig) {
			spiffe(c)
			c.NoOperator = true
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := NewDefaultKlusterletChartConfig()
			config.Klusterlet.ClusterName = "cluster1"
			config.Klusterlet.Mode = operatorv1.InstallModeSingleton
			c.mutate(config)
			_, raw, err := RenderKlusterletChart(context.Background(), config, "open-cluster-management")
			if err != nil {
				t.Fatal(err)
			}
			var role *rbacv1.ClusterRole
			var binding *rbacv1.ClusterRoleBinding
			var service *corev1.Service
			var operator *appsv1.Deployment
			for _, o := range raw {
				obj, _, err := decoder.Decode(o, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				switch obj := obj.(type) {
				case *rbacv1.ClusterRole:
					if obj.Name == "klusterlet-spiffe" {
						role = obj
					}
				case *rbacv1.ClusterRoleBinding:
					if obj.Name == "klusterlet-spiffe" {
						binding = obj
					}
				case *corev1.Service:
					if obj.Name == "klusterlet-spiffe-webhook" {
						service = obj
					}
				case *appsv1.Deployment:
					if obj.Name == "klusterlet" {
						operator = obj
					}
				}
			}
			if (role != nil) != c.want || (binding != nil) != c.want || (service != nil) != c.want {
				t.Fatalf("klusterlet-spiffe role rendered %v, binding %v, webhook Service %v; want %v",
					role != nil, binding != nil, service != nil, c.want)
			}
			csi := false
			if operator != nil {
				for _, v := range operator.Spec.Template.Spec.Volumes {
					if v.CSI != nil && v.CSI.Driver == "csi.spiffe.io" {
						csi = true
					}
				}
				ports := operator.Spec.Template.Spec.Containers[0].Ports
				if csi != c.want || (len(ports) == 1 && ports[0].ContainerPort == 9443) != c.want {
					t.Errorf("operator Workload API volume %v, webhook port %v; want %v", csi, ports, c.want)
				}
			}
			if !c.want {
				return
			}
			if service.Spec.Selector["app"] != "klusterlet" || service.Spec.Ports[0].Port != 443 ||
				service.Spec.Ports[0].TargetPort.IntValue() != 9443 || service.Namespace != "open-cluster-management" {
				t.Errorf("webhook Service %+v", service.Spec)
			}
			if binding.RoleRef.Name != "klusterlet-spiffe" || len(binding.Subjects) != 1 ||
				binding.Subjects[0].Name != "klusterlet" || binding.Subjects[0].Namespace != "open-cluster-management" {
				t.Errorf("binding %+v", binding)
			}
			// Admission objects, SelfSubjectReviews, the workloads cleaned in recovery, Jobs (also
			// created as the dry-run probe of a switch), CronJobs and Pods.
			if len(role.Rules) != 6 {
				t.Errorf("role has %d rules, want 6", len(role.Rules))
			}
		})
	}
}
