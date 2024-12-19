package helpers

import (
	"fmt"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// ManagedClusterLib defines the CEL library for ManagedCluster evaluation.
type ManagedClusterLib struct{}

// CompileOptions implements cel.Library interface to provide compile-time options.
func (ManagedClusterLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		// The input types may either be instances of `proto.Message` or `ref.Type`.
		// Here we use func ConvertManagedCluster() to convert ManagedCluster to a Map.
		cel.Variable("managedCluster", cel.MapType(cel.StringType, cel.AnyType)),

		// TODO: version compare
		// Declare the custom contains function and its implementation.
		cel.Function("score",
			cel.MemberOverload(
				"cluster_contains_score",
				// input is clustername, score cr name and score name
				[]*cel.Type{cel.DynType, cel.StringType, cel.StringType},
				// output is score value
				cel.IntType,
				cel.FunctionBinding(clusterContainsScore)),
		),
	}
}

// ProgramOptions implements cel.Library interface to provide runtime options.
// You can use this to add custom functions or evaluators.
func (ManagedClusterLib) ProgramOptions() []cel.ProgramOption {
	return nil
}

// Evaluator is a reusable struct for CEL evaluation on ManagedCluster.
type Evaluator struct {
	env *cel.Env
}

// NewEvaluator creates a new CEL Evaluator for ManagedCluster objects.
func NewEvaluator() (*Evaluator, error) {
	env, err := cel.NewEnv(
		cel.Lib(ManagedClusterLib{}), // Add the ManagedClusterLib to the CEL environment
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	return &Evaluator{env: env}, nil
}

// Evaluate evaluates a CEL expression against a ManagedCluster.
func (e *Evaluator) Evaluate(cluster *clusterapiv1.ManagedCluster, expressions []string) (bool, error) {
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

// clusterContainsScore implements the custom function:
//
//	clustername.score(score, name) -> int.
func clusterContainsScore(args ...ref.Val) ref.Val {
	cluster := args[0].(traits.Mapper)
	metadata, _ := cluster.Find(types.String("metadata"))
	clusterName, _ := metadata.(traits.Mapper).Find(types.String("name"))
	score := args[1]
	name := args[2]
	fmt.Sprintf("%s, %s, %s", clusterName, score, name)

	// TODO: get real score
	v := 3
	return types.Int(v)
}
