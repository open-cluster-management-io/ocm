package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"open-cluster-management.io/addon-framework/pkg/agent"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// DeploymentProber is to check the addon status based on status
// of the agent deployment status
type DeploymentProber struct {
	deployments []types.NamespacedName
}

func NewDeploymentProber(deployments ...types.NamespacedName) *agent.HealthProber {
	probeFields := []agent.ProbeField{}
	for _, deploy := range deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields: probeFields,
			HealthCheck: HealthCheck,
		},
	}
}

func (d *DeploymentProber) ProbeFields() []agent.ProbeField {
	probeFields := []agent.ProbeField{}
	for _, deploy := range d.deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return probeFields
}

func HealthCheck(identifier workapiv1.ResourceIdentifier, result workapiv1.StatusFeedbackResult) error {
	if len(result.Values) == 0 {
		return fmt.Errorf("no values are probed for deployment %s/%s", identifier.Namespace, identifier.Name)
	}
	for _, value := range result.Values {
		if value.Name != "ReadyReplicas" {
			continue
		}

		if *value.Value.Integer >= 1 {
			return nil
		}

		return fmt.Errorf("readyReplica is %d for deployement %s/%s", *value.Value.Integer, identifier.Namespace, identifier.Name)
	}
	return fmt.Errorf("readyReplica is not probed")
}
