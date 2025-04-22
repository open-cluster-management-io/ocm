package library

import (
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// ManagedClusterLib defines the CEL library for ManagedCluster evaluation.
// It provides functions and variables for evaluating ManagedCluster properties
// and their associated resources.
//
// Variables:
//
// managedCluster
//
// Provides access to ManagedCluster properties.
//
// Functions:
//
// scores
//
// Returns a list of AddOnPlacementScoreItem for a given cluster and AddOnPlacementScore resource name.
//
//	scores(<ManagedCluster>, <string>) <list>
//
// The returned list contains maps with the following structure:
//   - name: string - The name of the score
//   - value: int - The numeric score value
//   - quantity: string - The resource quantity as string
//
// Examples:
//
//	managedCluster.scores("cpu-memory") // returns [{name: "cpu", value: 3, quantity: "3"}, {name: "memory", value: 4, quantity: "300Mi"}]
func ManagedClusterLib(scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister) cel.EnvOption {
	managedClusterLib := &managedClusterLibType{
		scoreLister: scoreLister,
	}
	return cel.Lib(managedClusterLib)
}

type managedClusterLibType struct {
	scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister
}

func (*managedClusterLibType) LibraryName() string {
	return "open-cluster-management.managedcluster"
}

func (m *managedClusterLibType) CompileOptions() []cel.EnvOption {
	listStrDyn := cel.ListType(cel.DynType)
	options := []cel.EnvOption{
		cel.Variable("managedCluster", cel.MapType(cel.StringType, cel.DynType)),
		cel.Function("scores",
			cel.MemberOverload("cluster_scores", []*cel.Type{cel.DynType, cel.StringType}, listStrDyn,
				cel.BinaryBinding(m.clusterScores)),
		),
	}
	return options
}

func (*managedClusterLibType) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// clusterScores implements the CEL function scores(cluster, scoreName) that returns
// a list of AddOnPlacementScores for the given cluster and score resource name.
// Each score in the returned list contains:
//   - name: the score identifier
//   - value: the numeric score value
//   - quantity: the resource quantity as string
func (m *managedClusterLibType) clusterScores(arg1, arg2 ref.Val) ref.Val {
	if m.scoreLister == nil {
		return types.NewErr("scoreLister is nil")
	}

	cluster := arg1.(traits.Mapper)
	metadata, found := cluster.Find(types.String("metadata"))
	if !found {
		return types.NewErr("failed to get cluster metadata")
	}
	name, found := metadata.(traits.Mapper).Find(types.String("name"))
	if !found {
		return types.NewErr("failed to get cluster name")
	}

	clusterName := name.Value().(string)
	scoreName := arg2.Value().(string)
	scores, err := m.scoreLister.AddOnPlacementScores(clusterName).Get(scoreName)
	if err != nil {
		return types.NewErr("failed to get score: %v", err)
	}

	valScores := make([]ref.Val, len(scores.Status.Scores))
	for i, score := range scores.Status.Scores {
		valScores[i] = types.NewStringInterfaceMap(types.DefaultTypeAdapter, map[string]interface{}{
			"name":  score.Name,
			"value": score.Value,
		})
	}

	return types.NewDynamicList(types.DefaultTypeAdapter, valScores)
}
