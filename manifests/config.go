package manifests

import (
	"net"
	"strconv"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

type HubConfig struct {
	ClusterManagerName             string
	ClusterManagerNamespace        string
	OperatorNamespace              string
	RegistrationImage              string
	RegistrationAPIServiceCABundle string
	WorkImage                      string
	WorkAPIServiceCABundle         string
	PlacementImage                 string
	Replica                        int32
	HostedMode                     bool
	RegistrationWebhook            Webhook
	WorkWebhook                    Webhook
	RegistrationFeatureGates       []string
	WorkFeatureGates               []string
	AddOnManagerImage              string
	AddOnManagerEnabled            bool
	MWReplicaSetEnabled            bool
	ClusterProfileEnabled          bool
	AgentImage                     string
	CloudEventsDriverEnabled       bool
	ClusterImporterEnabled         bool
	WorkDriver                     string
	AutoApproveUsers               string
	ImagePullSecret                string
	// ResourceRequirementResourceType is the resource requirement resource type for the cluster manager managed containers.
	ResourceRequirementResourceType operatorapiv1.ResourceQosClass
	// ResourceRequirements is the resource requirements for the cluster manager managed containers.
	// The type has to be []byte to use "indent" template function.
	ResourceRequirements              []byte
	ManagedClusterIdentityCreatorRole string
	HubClusterArn                     string
	DisableManagedIam                 bool
	EnabledRegistrationDrivers        string
	AutoApprovedCSRUsers              string
	AutoApprovedARNPatterns           string
	AwsResourceTags                   string
	Labels                            map[string]string
	LabelsString                      string
}

type Webhook struct {
	IsIPFormat             bool
	HostNetwork            bool
	Port                   int32
	HealthProbeBindAddress string
	MetricsBindAddress     string
	Address                string
}

func (w Webhook) HealthProbePort() int32 {
	_, port, err := parseHostPort(w.HealthProbeBindAddress)
	if err != nil {
		return 0
	}
	return port
}

func (w Webhook) MetricsPort() int32 {
	_, port, err := parseHostPort(w.MetricsBindAddress)
	if err != nil {
		return 0
	}
	return port
}

func parseHostPort(address string) (host string, port int32, err error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return host, port, err
	}
	port64, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		return host, port, err
	}
	return host, int32(port64), nil
}
