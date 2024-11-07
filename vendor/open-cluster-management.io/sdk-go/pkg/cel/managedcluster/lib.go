package managedcluster

import (
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/version"
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
//   - quantity: number|string - The quantity value, represented as:
//   - number: for pure decimal values (e.g., 3)
//   - string: for values with units or decimal places (e.g., "300Mi", "1.5Gi")
//
// Examples:
//
//	managedCluster.scores("cpu-memory") // returns [{name: "cpu", value: 3, quantity: 3"}, {name: "memory", value: 4, quantity: "300Mi"}]
//
// Version Comparisons:
//
// versionIsGreaterThan
//
// Returns true if the first version string is greater than the second version string.
// The version must follow Semantic Versioning specification (http://semver.org/).
// It can be with or without 'v' prefix (eg, "1.14.3" or "v1.14.3").
//
//	versionIsGreaterThan(<string>, <string>) <bool>
//
// Examples:
//
//	versionIsGreaterThan("1.25.0", "1.24.0") // returns true
//	versionIsGreaterThan("1.24.0", "1.25.0") // returns false
//
// versionIsLessThan
//
// Returns true if the first version string is less than the second version string.
// The version must follow Semantic Versioning specification (http://semver.org/).
// It can be with or without 'v' prefix (eg, "1.14.3" or "v1.14.3").
//
//	versionIsLessThan(<string>, <string>) <bool>
//
// Examples:
//
//	versionIsLessThan("1.24.0", "1.25.0") // returns true
//	versionIsLessThan("1.25.0", "1.24.0") // returns false
//
// Quantity Comparisons:
//
// quantityIsGreaterThan
//
// Returns true if the first quantity string is greater than the second quantity string.
//
//	quantityIsGreaterThan(<string>, <string>) <bool>
//
// Examples:
//
//	quantityIsGreaterThan("2Gi", "1Gi") // returns true
//	quantityIsGreaterThan("1Gi", "2Gi") // returns false
//	quantityIsGreaterThan("1000Mi", "1Gi") // returns false
//
// quantityIsLessThan
//
// Returns true if the first quantity string is less than the second quantity string.
//
//	quantityIsLessThan(<string>, <string>) <bool>
//
// Examples:
//
//	quantityIsLessThan("1Gi", "2Gi") // returns true
//	quantityIsLessThan("2Gi", "1Gi") // returns false
//	quantityIsLessThan("1000Mi", "1Gi") // returns true
type ManagedClusterLib struct {
	scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister
}

func NewManagedClusterLib(scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister) *ManagedClusterLib {
	return &ManagedClusterLib{
		scoreLister: scoreLister,
	}
}

// CompileOptions returns the CEL environment options for ManagedCluster evaluation
func (l *ManagedClusterLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		// The input types may either be instances of `proto.Message` or `ref.Type`.
		// Here we use func ConvertManagedCluster() to convert ManagedCluster to a Map.
		cel.Variable("managedCluster", cel.MapType(cel.StringType, cel.DynType)),

		cel.Function("scores",
			cel.MemberOverload(
				"cluster_scores",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.ListType(cel.DynType),
				cel.FunctionBinding(l.clusterScores)),
		),

		cel.Function("versionIsGreaterThan",
			cel.MemberOverload(
				"version_is_greater_than",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(l.versionIsGreaterThan)),
		),

		cel.Function("versionIsLessThan",
			cel.MemberOverload(
				"version_is_less_than",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(l.versionIsLessThan)),
		),

		cel.Function("quantityIsGreaterThan",
			cel.MemberOverload(
				"quantity_is_greater_than",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(l.quantityIsGreaterThan)),
		),

		cel.Function("quantityIsLessThan",
			cel.MemberOverload(
				"quantity_is_less_than",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.FunctionBinding(l.quantityIsLessThan)),
		),
	}
}

func (l *ManagedClusterLib) ProgramOptions() []cel.ProgramOption {
	return nil
}

// clusterScores implements the CEL function scores(cluster, scoreName) that returns
// a list of AddOnPlacementScores for the given cluster and score resource name.
// Each score in the returned list contains:
//   - name: the score identifier
//   - value: the numeric score value
//   - quantity: the resource quantity (as number or string)
func (l *ManagedClusterLib) clusterScores(args ...ref.Val) ref.Val {
	cluster := args[0].(traits.Mapper)
	metadata, _ := cluster.Find(types.String("metadata"))
	clusterName, _ := metadata.(traits.Mapper).Find(types.String("name"))
	scoreName := args[1]

	scores, err := l.scoreLister.AddOnPlacementScores(clusterName.Value().(string)).Get(scoreName.Value().(string))
	if err != nil {
		return types.NewErr("failed to list scores: %v", err)
	}

	celScores := make([]ref.Val, len(scores.Status.Scores))
	for i, score := range scores.Status.Scores {
		celScores[i] = scoreToCel(score.Name, score.Value, score.Quantity)
	}

	return types.NewDynamicList(types.DefaultTypeAdapter, celScores)
}

// ScoreToCel converts an AddOnPlacementScoreItem to a CEL-compatible map structure.
// For quantities that are pure integers (e.g., 3), it uses numeric values.
// For quantities with units or decimals (e.g., "300Mi", "1.5Gi"), it uses string representation.
func scoreToCel(name string, value int32, q resource.Quantity) ref.Val {
	var quantityValue interface{}
	if q.Format == resource.DecimalSI && q.MilliValue()%1000 == 0 {
		quantityValue = q.Value()
	} else {
		quantityValue = q.String()
	}

	return types.NewStringInterfaceMap(types.DefaultTypeAdapter, map[string]interface{}{
		"name":     name,
		"value":    value,
		"quantity": quantityValue,
	})
}

func (l *ManagedClusterLib) versionIsGreaterThan(args ...ref.Val) ref.Val {
	return compareVersions(args[0], args[1], "versionIsGreaterThan")
}

func (l *ManagedClusterLib) versionIsLessThan(args ...ref.Val) ref.Val {
	return compareVersions(args[0], args[1], "versionIsLessThan")
}

func (l *ManagedClusterLib) quantityIsGreaterThan(args ...ref.Val) ref.Val {
	return compareQuantities(args[0], args[1], "quantityIsGreaterThan")
}

func (l *ManagedClusterLib) quantityIsLessThan(args ...ref.Val) ref.Val {
	return compareQuantities(args[0], args[1], "quantityIsLessThan")
}

// compareVersions is a helper function that compares two version strings
func compareVersions(v1Str, v2Str ref.Val, op string) ref.Val {
	if v1Str == nil || v2Str == nil {
		return types.NewErr("%s: requires exactly two arguments", op)
	}

	// Convert arguments to strings
	v1, ok1 := v1Str.Value().(string)
	v2, ok2 := v2Str.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("%s: both arguments must be strings", op)
	}

	// Parse first version
	v1Parsed, err := version.ParseSemantic(v1)
	if err != nil {
		return types.NewErr("%s: invalid first version: %v", op, err)
	}

	// Compare versions
	cmp, err := v1Parsed.Compare(v2)
	if err != nil {
		return types.NewErr("%s: comparison failed: %v", op, err)
	}

	if op == "versionIsGreaterThan" {
		return types.Bool(cmp > 0)
	}
	return types.Bool(cmp < 0)
}

// compareQuantities is a helper function that compares two quantity strings
func compareQuantities(q1Str, q2Str ref.Val, op string) ref.Val {
	if q1Str == nil || q2Str == nil {
		return types.NewErr("%s: requires exactly two arguments", op)
	}

	// Convert arguments to strings
	q1, ok1 := q1Str.Value().(string)
	q2, ok2 := q2Str.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("%s: both arguments must be strings", op)
	}

	// Parse quantities
	q1Val, err := resource.ParseQuantity(q1)
	if err != nil {
		return types.NewErr("%s: invalid first quantity: %v", op, err)
	}

	q2Val, err := resource.ParseQuantity(q2)
	if err != nil {
		return types.NewErr("%s: invalid second quantity: %v", op, err)
	}

	cmp := q1Val.Cmp(q2Val)
	if op == "quantityIsGreaterThan" {
		return types.Bool(cmp > 0)
	}
	return types.Bool(cmp < 0)
}
