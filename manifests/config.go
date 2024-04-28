package manifests

import operatorapiv1 "open-cluster-management.io/api/operator/v1"

type HubConfig struct {
	ClusterManagerName             string
	ClusterManagerNamespace        string
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
	CloudEventsDriverEnabled       bool
	WorkDriver                     string
	AutoApproveUsers               string
	ImagePullSecret                string
	// ResourceRequirementResourceType is the resource requirement resource type for the cluster manager managed containers.
	ResourceRequirementResourceType operatorapiv1.ResourceQosClass
	// ResourceRequirements is the resource requirements for the cluster manager managed containers.
	// The type has to be []byte to use "indent" template function.
	ResourceRequirements []byte
}

type Webhook struct {
	IsIPFormat bool
	Port       int32
	Address    string
}
