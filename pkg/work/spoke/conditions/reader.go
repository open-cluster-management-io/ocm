package conditions

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"slices"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cel/common"

	"open-cluster-management.io/ocm/pkg/work/spoke/conditions/rules"
)

type ConditionReader struct {
	wellKnownConditions rules.WellKnownConditionRuleResolver
}

func NewConditionReader() *ConditionReader {
	return &ConditionReader{
		wellKnownConditions: rules.DefaultWellKnownConditionResolver(),
	}
}

func (s *ConditionReader) GetConditionByRule(obj *unstructured.Unstructured, rule workapiv1.ConditionRule) (metav1.Condition, error) {
	switch rule.Type {
	case workapiv1.WellKnownCompletionsType:
		r := s.wellKnownConditions.GetRuleByKindCondition(obj.GroupVersionKind(), workapiv1.ManifestComplete)
		if len(r.CelExpressions) == 0 {
			err := fmt.Errorf("cannot find the wellknown conditions for resource with gvk %s", obj.GroupVersionKind().String())
			return metav1.Condition{
				Type:    rule.Condition,
				Status:  metav1.ConditionUnknown,
				Reason:  workapiv1.ConditionRuleInvalid,
				Message: err.Error(),
			}, err
		}

		// Check for supported overrides in rule
		if rule.Condition != "" {
			r.Condition = rule.Condition
		}
		if rule.Message != "" {
			r.Message = rule.Message
		}
		if rule.MessageExpression != "" {
			r.MessageExpression = rule.MessageExpression
		}
		return s.getConditionByCelRule(obj, r)
	case workapiv1.CelConditionExpressionsType:
		return s.getConditionByCelRule(obj, rule)
	default:
		err := fmt.Errorf("unrecognized condition rule type %s", rule.Type)
		return metav1.Condition{
			Type:    rule.Condition,
			Status:  metav1.ConditionUnknown,
			Reason:  workapiv1.ConditionRuleInvalid,
			Message: err.Error(),
		}, err
	}
}

func (s *ConditionReader) getConditionByCelRule(obj *unstructured.Unstructured, rule workapiv1.ConditionRule) (metav1.Condition, error) {
	var message string
	var err error
	status, reason, err := s.evaluateCelExpressions(obj, rule.CelExpressions)
	if err != nil {
		message = err.Error()
	} else {
		message, err = s.getConditionMessageByRule(obj, rule, status == metav1.ConditionTrue)
		if err != nil && message == "" {
			message = err.Error()
		}
	}

	return metav1.Condition{
		Type:    rule.Condition,
		Status:  status,
		Message: message,
		Reason:  reason,
	}, err
}

func (s *ConditionReader) evaluateCelExpressions(obj *unstructured.Unstructured, expressions []string) (metav1.ConditionStatus, string, error) {
	env, err := s.celEnv(cel.Variable("object", cel.DynType))
	if err != nil {
		return metav1.ConditionUnknown, workapiv1.ConditionRuleInternalError, err
	}

	for _, expression := range expressions {
		ast, iss := env.Compile(expression)
		err = iss.Err()
		if err != nil {
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, err
		}

		prg, err := env.Program(ast)
		if err != nil {
			return metav1.ConditionUnknown, workapiv1.ConditionRuleInternalError, err
		}

		out, _, err := prg.Eval(map[string]any{
			"object": obj.Object,
		})
		if err != nil {
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, err
		}

		switch result := out.Value().(type) {
		case bool:
			// If result is false return early
			// Otherwise continue checking expressions
			if !result {
				return metav1.ConditionFalse, workapiv1.ConditionRuleEvaluated, nil
			}
		default:
			err = fmt.Errorf("expected bool result, got %v", reflect.TypeOf(result))
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, err
		}
	}

	return metav1.ConditionTrue, workapiv1.ConditionRuleEvaluated, nil
}

func (s *ConditionReader) getConditionMessageByRule(obj *unstructured.Unstructured, rule workapiv1.ConditionRule, result bool) (string, error) {
	if rule.MessageExpression != "" {
		env, err := s.celEnv(cel.Variable("object", cel.DynType), cel.Variable("result", cel.BoolType))
		if err != nil {
			return "", err
		}

		ast, iss := env.Compile(rule.MessageExpression)
		err = iss.Err()
		if err != nil {
			// Trim CEL code snippets out of message
			message := strings.Split(iss.String(), "\n | ")[0]
			return message, err
		}

		prg, err := env.Program(ast)
		if err != nil {
			return "", err
		}

		out, _, err := prg.Eval(map[string]any{
			"object": obj.Object,
			"result": result,
		})
		if err != nil {
			return "", err
		}

		switch message := out.Value().(type) {
		case string:
			if message != "" {
				return message, nil
			}
		default:
			return "", fmt.Errorf("expected message expression to have a string result, got %v", reflect.TypeOf(message))
		}
	}

	if rule.Message != "" {
		return rule.Message, nil
	}
	if result {
		return "Manifest is " + rule.Condition, nil
	}
	return "Manifest is not " + rule.Condition, nil
}

func (s *ConditionReader) celEnv(opts ...cel.EnvOption) (*cel.Env, error) {
	opts = slices.Concat(
		opts,
		common.BaseEnvOpts,
		[]cel.EnvOption{
			cel.Function("hasConditions", cel.Overload(
				"status_has_conditions",
				[]*cel.Type{cel.DynType},
				cel.BoolType,
				cel.FunctionBinding(hasConditions),
			)),
		},
	)
	return cel.NewEnv(opts...)
}

func hasConditions(values ...ref.Val) ref.Val {
	if len(values) == 0 {
		return types.Bool(false)
	}

	status, ok := values[0].(traits.Mapper)
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
