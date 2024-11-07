package managedcluster

import (
	"fmt"

	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/google/cel-go/cel"
)

// ManagedClusterEvaluator is a reusable struct for CEL evaluation on ManagedCluster.
type ManagedClusterEvaluator struct {
	env *cel.Env
}

// NewManagedClusterEvaluator creates a new CEL Evaluator for ManagedCluster objects.
func NewManagedClusterEvaluator(scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister) (*ManagedClusterEvaluator, error) {
	// Add the ManagedClusterLib to the CEL environment
	lib := NewManagedClusterLib(scoreLister)
	env, err := cel.NewEnv(lib.CompileOptions()...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &ManagedClusterEvaluator{
		env: env,
	}, nil
}

// Evaluate evaluates a CEL expression against a ManagedCluster.
func (e *ManagedClusterEvaluator) Evaluate(cluster *clusterapiv1.ManagedCluster, expressions []string) (bool, error) {
	convertedCluster := convertManagedCluster(cluster)

	for _, expr := range expressions {
		ast, iss := e.env.Compile(expr)
		if iss.Err() != nil {
			return false, fmt.Errorf("failed to compile CEL expression '%s': %w", expr, iss.Err())
		}

		prg, _ := e.env.Program(ast)
		result, _, err := prg.Eval(map[string]interface{}{
			"managedCluster": convertedCluster,
		})
		if err != nil {
			return false, fmt.Errorf("CEL evaluation error: %w", err)
		}

		if value, ok := result.Value().(bool); !ok || !value {
			return false, nil
		}
	}

	return true, nil
}

// convertManagedCluster converts a ManagedCluster object to a CEL-compatible format.
func convertManagedCluster(cluster *clusterapiv1.ManagedCluster) map[string]interface{} {
	convertedClusterClaims := []map[string]interface{}{}
	for _, claim := range cluster.Status.ClusterClaims {
		convertedClusterClaims = append(convertedClusterClaims, map[string]interface{}{
			"name":  claim.Name,
			"value": claim.Value,
		})
	}

	convertedVersion := map[string]interface{}{
		"kubernetes": cluster.Status.Version.Kubernetes,
	}

	// TODO: more fields to add
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        cluster.Name,
			"labels":      cluster.Labels,
			"annotations": cluster.Annotations,
		},
		"status": map[string]interface{}{
			"clusterClaims": convertedClusterClaims,
			"version":       convertedVersion,
		},
	}
}
