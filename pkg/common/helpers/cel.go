package helpers

import (
	"context"
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/klog/v2"
)

// EvaluateSingleExpression evaluates one CEL expression and handles its cost accounting.
// Returns (evalResult, newBudget) if evaluation succeeds, otherwise (nil, -1 or remaining budget).
func EvaluateSingleExpression(
	ctx context.Context,
	program cel.Program,
	budget int64,
	expression string,
	input any,
) (ref.Val, int64) {
	logger := klog.FromContext(ctx)

	// Evaluate the expression
	evalResult, evalDetails, err := program.ContextEval(ctx, input)

	// Cost calculation
	ok, rtCost := CostCalculation(ctx, evalDetails, budget, expression)
	if !ok {
		return nil, -1
	}

	remainingBudget := budget - rtCost
	// Handle evaluation error
	if err != nil {
		logger.Info("Expression evaluation failed", "rule", expression, "cluster", "err", err)
		return nil, remainingBudget
	}

	return evalResult, remainingBudget
}

// CostCalculation processes the cost details of an evaluation
func CostCalculation(ctx context.Context, evalDetails *cel.EvalDetails, budget int64, expression string) (bool, int64) {
	logger := klog.FromContext(ctx)

	// Check if cost details are available
	if evalDetails == nil {
		logger.Info("Runtime cost calculation failed: no evaluation details",
			"rule", expression)
		return false, -1
	}

	rtCost := evalDetails.ActualCost()
	if rtCost == nil {
		logger.Info("Runtime cost calculation failed: no cost information",
			"rule", expression)
		return false, -1
	}

	// Validate cost against budget
	if *rtCost > math.MaxInt64 || int64(*rtCost) > budget {
		logger.Info("Cost budget exceeded",
			"rule", expression,
			"cost", *rtCost,
			"budget", budget)
		return false, -1
	}

	// Safe to convert since we checked for overflow
	return true, int64(*rtCost) //nolint:gosec
}
