package library

import (
	"encoding/json"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// JsonLib defines the CEL library for parsing JSON.
//
// parseJSON
//
// Parses a JSON string into a CEL-compatible map or list.
//
//	parseJSON(<string>) <dyn>
//
// Takes a single string argument, attempts to parse it as JSON, and returns the resulting
// data structure as a CEL-compatible value. If the input is not a valid JSON string, it returns an error.
//
// Examples:
//
//	parseJSON("{\"key\": \"value\"}") // returns a map with key-value pairs
func JsonLib() cel.EnvOption {
	return cel.Lib(jsonLib)
}

var jsonLib = &jsonLibType{}

type jsonLibType struct{}

func (*jsonLibType) LibraryName() string {
	return "open-cluster-management.json"
}

func (j *jsonLibType) CompileOptions() []cel.EnvOption {
	options := []cel.EnvOption{
		cel.Function("parseJSON",
			cel.MemberOverload("parse_json_string", []*cel.Type{cel.StringType}, cel.DynType,
				cel.UnaryBinding(parseJSON)),
		),
	}
	return options
}

func (*jsonLibType) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// parseJSON is a custom function to parse a JSON string into a CEL-compatible map or list.
// It takes a single string argument, attempts to parse it as JSON, and returns the resulting
// data structure as a CEL-compatible value. If the input is not a valid JSON string, it returns an error.
func parseJSON(arg ref.Val) ref.Val {
	jsonString, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("failed to parse json: argument must be a string")
	}

	var result interface{}
	if err := json.Unmarshal([]byte(jsonString), &result); err != nil {
		return types.NewErr("failed to parse json: %v", err)
	}

	return types.DefaultTypeAdapter.NativeToValue(result)
}
