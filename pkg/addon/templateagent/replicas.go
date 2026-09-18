package templateagent

import (
	"fmt"
	"regexp"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

// workloadIDPattern validates ReplicaConfig.WorkloadID.
// Format: {resourceType}:{resourceName}
// Supported types: deployments, statefulsets. Wildcard (*) allowed in each segment.
var workloadIDPattern = regexp.MustCompile(`^(deployments|statefulsets|\*):.+$`)

// ToAddOnReplicaPrivateValues transforms AODC ReplicaConfigs into a private value
// consumed by the replica decorator. Follows the same pattern as
// ToAddOnResourceRequirementsPrivateValues.
func ToAddOnReplicaPrivateValues(config addonapiv1beta1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	if len(config.Spec.ReplicaConfigs) == 0 {
		return nil, nil
	}
	configs, err := parseReplicaConfigs(config.Spec.ReplicaConfigs)
	if err != nil {
		return nil, err
	}
	return addonfactory.Values{
		ReplicaPrivateValueKey: configs,
	}, nil
}

// ParsedReplicaConfig is the internal representation of a ReplicaConfig entry.
type ParsedReplicaConfig struct {
	// WorkloadIDRegex is compiled from ReplicaConfig.WorkloadID for matching.
	WorkloadIDRegex string
	Replicas        int32
}

func parseReplicaConfigs(configs []addonapiv1beta1.ReplicaConfig) ([]ParsedReplicaConfig, error) {
	result := make([]ParsedReplicaConfig, 0, len(configs))
	for _, c := range configs {
		if !workloadIDPattern.MatchString(c.WorkloadID) {
			return nil, fmt.Errorf("replicaConfigs entry has invalid workloadID %q; expected format: {resourceType}:{resourceName}", c.WorkloadID)
		}
		if c.Replicas < 0 {
			return nil, fmt.Errorf("replicaConfigs entry workloadID=%q has invalid replicas %d; must be >= 0", c.WorkloadID, c.Replicas)
		}
		// Convert workloadID glob (*) to regex (.*) for matching.
		// QuoteMeta first so dots/slashes are escaped, then replace \* back to .*
		regex := "^" + regexp.QuoteMeta(c.WorkloadID) + "$"
		regex = regexp.MustCompile(`\\\*`).ReplaceAllString(regex, ".*")
		result = append(result, ParsedReplicaConfig{
			WorkloadIDRegex: regex,
			Replicas:        c.Replicas,
		})
	}
	return result, nil
}
