package conditions

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/klog/v2"

	workapiv1 "open-cluster-management.io/api/work/v1"
	ocmcelcommon "open-cluster-management.io/sdk-go/pkg/cel/common"
	ocmcellibrary "open-cluster-management.io/sdk-go/pkg/cel/library"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/spoke/conditions/rules"
)

var globalCostBudget = int64(celconfig.RuntimeCELCostBudget)

type ConditionReader struct {
	wellKnownConditions rules.WellKnownConditionRuleResolver
	ruleEnv             *cel.Env
	messageEnv          *cel.Env
}

func NewConditionReader() (*ConditionReader, error) {
	ruleEnv, err := celEnv(cel.Variable("object", cel.DynType))
	if err != nil {
		return nil, err
	}
	messageEnv, err := celEnv(cel.Variable("object", cel.DynType), cel.Variable("result", cel.BoolType))
	if err != nil {
		return nil, err
	}

	return &ConditionReader{
		wellKnownConditions: rules.DefaultWellKnownConditionResolver(),
		ruleEnv:             ruleEnv,
		messageEnv:          messageEnv,
	}, nil
}

func (s *ConditionReader) EvaluateConditions(ctx context.Context, obj *unstructured.Unstructured, rules []workapiv1.ConditionRule) []metav1.Condition {
	var conditionResults []metav1.Condition
	remainingBudget := globalCostBudget
	for _, rule := range rules {
		var err error
		var condition metav1.Condition

		condition, remainingBudget, err = s.GetConditionByRule(ctx, obj, rule, remainingBudget)
		if err != nil {
			// Errors from rule evaluation are not returned since they don't impact resource apply
			// The error is set in condition message for users to handle
			klog.Errorf("failed to evaluate condition rule: %s", err.Error())
		}

		// If type is not set, then it was a WellKnownCondition with no match and should be ignored.
		if condition.Type != "" {
			conditionResults = append(conditionResults, condition)
		}
	}

	return conditionResults
}

func (s *ConditionReader) GetConditionByRule(
	ctx context.Context, obj *unstructured.Unstructured, rule workapiv1.ConditionRule, budget int64,
) (metav1.Condition, int64, error) {
	switch rule.Type {
	case workapiv1.WellKnownConditionsType:
		r := s.wellKnownConditions.GetRuleByKindCondition(obj.GroupVersionKind(), rule.Condition)
		if len(r.CelExpressions) == 0 {
			return metav1.Condition{}, budget, nil
		}

		// Check for supported overrides in rule
		if rule.Message != "" {
			r.Message = rule.Message
		}
		if rule.MessageExpression != "" {
			r.MessageExpression = rule.MessageExpression
		}
		return s.getConditionByCelRule(ctx, obj, r, budget)
	case workapiv1.CelConditionExpressionsType:
		return s.getConditionByCelRule(ctx, obj, rule, budget)
	default:
		err := fmt.Errorf("unrecognized condition rule type %s", rule.Type)
		return metav1.Condition{
			Type:    rule.Condition,
			Status:  metav1.ConditionUnknown,
			Reason:  workapiv1.ConditionRuleInvalid,
			Message: err.Error(),
		}, budget, err
	}
}

func (s *ConditionReader) getConditionByCelRule(
	ctx context.Context, obj *unstructured.Unstructured, rule workapiv1.ConditionRule, budget int64,
) (metav1.Condition, int64, error) {
	var message string
	var err error
	status, reason, remainingBudget, err := s.evaluateCelExpressions(ctx, obj, rule.CelExpressions, budget)
	if err != nil {
		message = err.Error()
	} else {
		message, remainingBudget, err = s.getConditionMessageByRule(ctx, obj, rule, status == metav1.ConditionTrue, remainingBudget)
		if err != nil && message == "" {
			message = err.Error()
		}
	}

	return metav1.Condition{
		Type:    rule.Condition,
		Status:  status,
		Message: message,
		Reason:  reason,
	}, remainingBudget, err
}

func (s *ConditionReader) evaluateCelExpressions(
	ctx context.Context, obj *unstructured.Unstructured, expressions []string, budget int64,
) (status metav1.ConditionStatus, reason string, remainingBudget int64, err error) {
	remainingBudget = budget
	estimator := newEstimator()
	for _, expression := range expressions {
		ast, iss := s.ruleEnv.Compile(expression)
		err = iss.Err()
		if err != nil {
			// A error in compiling the rule gives False condition status by convention
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, remainingBudget, err
		}

		prg, err := s.ruleEnv.Program(
			ast,
			cel.CostLimit(celconfig.PerCallLimit),
			cel.CostTracking(estimator),
			cel.InterruptCheckFrequency(celconfig.CheckFrequency),
		)
		if err != nil {
			// User has no control over this error, so we return Unknown condition status
			return metav1.ConditionUnknown, workapiv1.ConditionRuleInternalError, remainingBudget, err
		}

		out, newBudget, err := evaluate(ctx, prg, remainingBudget, expression, map[string]any{
			"object": obj.Object,
		})
		if err != nil {
			// A error in evaluating the rule gives False condition status by convention
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, remainingBudget, err
		}
		remainingBudget = newBudget

		switch result := out.Value().(type) {
		case bool:
			// If result is false return early
			// Otherwise continue checking expressions
			if !result {
				return metav1.ConditionFalse, workapiv1.ConditionRuleEvaluated, remainingBudget, nil
			}
		default:
			err = fmt.Errorf("expected bool result, got %v", reflect.TypeOf(result))
			return metav1.ConditionFalse, workapiv1.ConditionRuleExpressionError, remainingBudget, err
		}
	}

	return metav1.ConditionTrue, workapiv1.ConditionRuleEvaluated, remainingBudget, nil
}

func (s *ConditionReader) getConditionMessageByRule(
	ctx context.Context, obj *unstructured.Unstructured, rule workapiv1.ConditionRule, result bool, budget int64,
) (string, int64, error) {
	if rule.MessageExpression != "" {
		ast, iss := s.messageEnv.Compile(rule.MessageExpression)
		err := iss.Err()
		if err != nil {
			// Trim CEL code snippets out of message
			message := strings.Split(iss.String(), "\n | ")[0]
			return message, budget, err
		}

		prg, err := s.messageEnv.Program(ast)
		if err != nil {
			return "", budget, err
		}

		out, newBudget, err := evaluate(ctx, prg, budget, rule.MessageExpression, map[string]any{
			"object": obj.Object,
			"result": result,
		})
		if err != nil {
			return "", newBudget, err
		}
		budget = newBudget

		switch message := out.Value().(type) {
		case string:
			if message != "" {
				return message, budget, nil
			}
		default:
			return "", budget, fmt.Errorf("expected message expression to have a string result, got %v", reflect.TypeOf(message))
		}
	}

	if rule.Message != "" {
		return rule.Message, budget, nil
	}
	if result {
		return "Manifest is " + rule.Condition, budget, nil
	}
	return "Manifest is not " + rule.Condition, budget, nil
}

func celEnv(opts ...cel.EnvOption) (*cel.Env, error) {
	opts = slices.Concat(
		opts,
		ocmcelcommon.BaseEnvOpts,
		[]cel.EnvOption{
			ocmcellibrary.ConditionsLib(),
		},
	)
	return cel.NewEnv(opts...)
}

func evaluate(
	ctx context.Context,
	program cel.Program,
	budget int64,
	expression string,
	input any,
) (ref.Val, int64, error) {
	logger := klog.FromContext(ctx)

	// Evaluate the expression
	evalResult, evalDetails, err := program.ContextEval(ctx, input)

	// Cost calculation
	var remainingBudget = budget
	if evalDetails != nil {
		ok, rtCost := helpers.CostCalculation(ctx, evalDetails, budget, expression)
		if !ok {
			costErr := fmt.Errorf("CEL evaluation budget exceeded")
			if err != nil {
				err = utilerrors.NewAggregate([]error{err, costErr})
			} else {
				err = costErr
			}
			return nil, -1, err
		}
		remainingBudget -= rtCost
	}

	// Handle evaluation error
	if err != nil {
		logger.Info("Expression evaluation failed", "rule", expression, "cluster", "err", err)
		return nil, remainingBudget, err
	}

	return evalResult, remainingBudget, nil
}

// newEstimator creates a new cost estimator for CEL expressions.
func newEstimator() *ocmcelcommon.BaseEnvCostEstimator {
	return &ocmcelcommon.BaseEnvCostEstimator{
		CostEstimator: &ocmcellibrary.CostEstimator{},
	}
}
