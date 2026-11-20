package helpers

import (
	"strings"
	"testing"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

func TestSPIFFEConfig(t *testing.T) {
	cm := &operatorapiv1.ClusterManager{}
	if SPIFFEConfig(cm) != nil {
		t.Error("a ClusterManager without registrationConfiguration has spiffe settings")
	}
	cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
		RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{{AuthType: "csr"}}}
	if SPIFFEConfig(cm) != nil {
		t.Error("a ClusterManager without the spiffe driver has spiffe settings")
	}
	cm.Spec.RegistrationConfiguration.RegistrationDrivers = append(cm.Spec.RegistrationConfiguration.RegistrationDrivers,
		operatorapiv1.RegistrationDriverHub{AuthType: operatorapiv1.SPIFFEAuthType})
	if c := SPIFFEConfig(cm); c == nil || c.TrustDomain != "" {
		t.Errorf("spiffe without settings: %+v, want empty settings", c)
	}
}

func TestSPIFFEServerEndpointType(t *testing.T) {
	cm := &operatorapiv1.ClusterManager{}
	if got := SPIFFEServerEndpointType(cm); got != string(operatorapiv1.EndpointTypeHostname) {
		t.Errorf("no server configuration: %s", got)
	}
	cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{EndpointsExposure: []operatorapiv1.EndpointExposure{
		{Protocol: "grpc", GRPC: &operatorapiv1.Endpoint{Type: operatorapiv1.EndpointTypeLoadBalancer}},
		{Protocol: operatorapiv1.GRPCSPIFFEProtocol},
	}}
	if got := SPIFFEServerEndpointType(cm); got != string(operatorapiv1.EndpointTypeHostname) {
		t.Errorf("grpc-spiffe without a type: %s, want hostname (the grpc entry doesn't count)", got)
	}
	cm.Spec.ServerConfiguration.EndpointsExposure[1].GRPC = &operatorapiv1.Endpoint{Type: operatorapiv1.EndpointTypeLoadBalancer}
	if got := SPIFFEServerEndpointType(cm); got != string(operatorapiv1.EndpointTypeLoadBalancer) {
		t.Errorf("grpc-spiffe as loadBalancer: %s", got)
	}
	cm.Spec.ServerConfiguration.EndpointsExposure = cm.Spec.ServerConfiguration.EndpointsExposure[:1]
	if got := SPIFFEServerEndpointType(cm); got != string(operatorapiv1.EndpointTypeHostname) {
		t.Errorf("only a grpc entry: %s, want hostname", got)
	}
}

func TestSPIFFEServerIDErrors(t *testing.T) {
	for _, c := range []struct {
		config operatorapiv1.SPIFFERegistrationConfig
		want   string
	}{
		{operatorapiv1.SPIFFERegistrationConfig{TrustDomain: "Not A Domain"}, "trustDomain"},
		{operatorapiv1.SPIFFERegistrationConfig{TrustDomain: "ocm-hub.example.org", ServerSPIFFEID: "https://hub"}, "serverSPIFFEID"},
	} {
		if _, err := SPIFFEServerID(&c.config); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error %v, want %q", c.config, err, c.want)
		}
	}
}
