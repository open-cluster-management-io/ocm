package klusterletcontroller

import (
	"encoding/json"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// The ValidatingWebhook enforcement evaluates the same ValidatingAdmissionPolicies the
// ValidatingAdmissionPolicy enforcement installs, with the variables the API server gives them:
// object, oldObject, request and variables. So both enforcements apply one set of rules.

var celEnv = mustCELEnv(cel.NewEnv(
	cel.Variable("object", cel.DynType),
	cel.Variable("oldObject", cel.DynType),
	cel.Variable("request", cel.DynType),
	cel.Variable("variables", cel.MapType(cel.StringType, cel.DynType)),
))

// mustCELEnv fails at start-up only: the declarations are constant.
func mustCELEnv(env *cel.Env, err error) *cel.Env {
	utilruntime.Must(err)
	return env
}

type celNamed struct {
	name    string
	program cel.Program
}

type celValidation struct {
	program cel.Program
	message string
	// messageProgram computes the message, when the validation has a MessageExpression.
	messageProgram cel.Program
}

// celPolicy is a compiled ValidatingAdmissionPolicy.
type celPolicy struct {
	variables   []celNamed
	validations []celValidation
}

func celCompile(expression string) (cel.Program, error) {
	ast, issues := celEnv.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling %q: %w", expression, issues.Err())
	}
	return celEnv.Program(ast)
}

// compilePolicy compiles a policy's variables and validations.
func compilePolicy(spec admissionregistrationv1.ValidatingAdmissionPolicySpec) (*celPolicy, error) {
	p := &celPolicy{}
	for _, v := range spec.Variables {
		prg, err := celCompile(v.Expression)
		if err != nil {
			return nil, err
		}
		p.variables = append(p.variables, celNamed{name: v.Name, program: prg})
	}
	for _, v := range spec.Validations {
		prg, err := celCompile(v.Expression)
		if err != nil {
			return nil, err
		}
		c := celValidation{program: prg, message: v.Message}
		if v.MessageExpression != "" {
			if c.messageProgram, err = celCompile(v.MessageExpression); err != nil {
				return nil, err
			}
		}
		p.validations = append(p.validations, c)
	}
	return p, nil
}

// celObject decodes an object of an admission request; an empty one is null, as for the API
// server.
func celObject(raw []byte) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// celRequest is the request variable: like the API server's, subResource is absent rather than
// empty on a request without a subresource.
func celRequest(req *admissionv1.AdmissionRequest) map[string]interface{} {
	out := map[string]interface{}{
		"kind":      map[string]interface{}{"group": req.Kind.Group, "version": req.Kind.Version, "kind": req.Kind.Kind},
		"resource":  map[string]interface{}{"group": req.Resource.Group, "version": req.Resource.Version, "resource": req.Resource.Resource},
		"name":      req.Name,
		"namespace": req.Namespace,
		"operation": string(req.Operation),
		"userInfo": map[string]interface{}{
			"username": req.UserInfo.Username,
			"uid":      req.UserInfo.UID,
			"groups":   req.UserInfo.Groups,
		},
	}
	if req.SubResource != "" {
		out["subResource"] = req.SubResource
	}
	return out
}

// admit evaluates the policy on a request: it returns "" when every validation holds, else the
// first failed validation's message. An expression that fails to evaluate refuses the request,
// as the policies' failurePolicy Fail does.
func (p *celPolicy) admit(req *admissionv1.AdmissionRequest) string {
	object, err := celObject(req.Object.Raw)
	if err != nil {
		return "the object can't be decoded: " + err.Error()
	}
	oldObject, err := celObject(req.OldObject.Raw)
	if err != nil {
		return "the old object can't be decoded: " + err.Error()
	}
	variables := map[string]interface{}{}
	activation := map[string]interface{}{
		"object": object, "oldObject": oldObject, "request": celRequest(req), "variables": variables,
	}
	eval := func(prg cel.Program) (ref.Val, error) {
		out, _, err := prg.Eval(activation)
		return out, err
	}
	for _, v := range p.variables {
		out, err := eval(v.program)
		if err != nil {
			return fmt.Sprintf("variable %s: %v", v.name, err)
		}
		variables[v.name] = out
	}
	for _, v := range p.validations {
		out, err := eval(v.program)
		if err != nil {
			return "validation failed to evaluate: " + err.Error()
		}
		if out == types.True {
			continue
		}
		if v.messageProgram != nil {
			if msg, err := eval(v.messageProgram); err == nil {
				if s, ok := msg.Value().(string); ok {
					return s
				}
			}
		}
		return v.message
	}
	return ""
}
