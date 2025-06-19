package library

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// ConditionsLib defines the CEL library for checking status conditions.
//
// hasConditions
//
// Checks if a status map has any conditions set
//
//	hasConditions(<map>) <bool>
//
// Takes a single map argument, checks for a "conditions" key, and returns true if it is
// a list containing any elements. Otherwise returns false.
//
// Examples:
//
//	hasConditions({'conditions': [{'type': 'Ready', 'status': 'False'}]}) // returns true
//
//  hasConditions(object.status) && object.status.conditions.exists(c, c.type == "Ready" && c.status == "True")

func ConditionsLib() cel.EnvOption {
	return cel.Lib(conditionsLib)
}

var conditionsLib = &conditionsLibType{}

type conditionsLibType struct{}

func (*conditionsLibType) LibraryName() string {
	return "open-cluster-management.conditions"
}

func (j *conditionsLibType) CompileOptions() []cel.EnvOption {
	options := []cel.EnvOption{
		cel.Function("hasConditions", cel.Overload(
			"status_has_conditions",
			[]*cel.Type{cel.DynType},
			cel.BoolType,
			cel.UnaryBinding(hasConditions),
		)),
	}
	return options
}

func (*conditionsLibType) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

func hasConditions(value ref.Val) ref.Val {
	status, ok := value.(traits.Mapper)
	if !ok {
		return types.Bool(false)
	}

	conditions, ok := status.Find(types.String("conditions"))
	if !ok {
		return types.Bool(false)
	}

	if lister, ok := conditions.(traits.Lister); !ok || lister.Size() == types.Int(0) {
		return types.Bool(false)
	}

	return types.Bool(true)
}
