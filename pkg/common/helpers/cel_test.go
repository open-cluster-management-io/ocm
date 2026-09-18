package helpers

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

type noopCostEstimator struct{}

func (noopCostEstimator) CallCost(function, overloadID string, args []ref.Val, result ref.Val) *uint64 {
	return nil
}

func newTestCelEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(cel.Variable("x", cel.IntType))
	if err != nil {
		t.Fatalf("failed to create cel env: %v", err)
	}
	return env
}

func mustCompileCel(t *testing.T, env *cel.Env, expression string) cel.Program {
	t.Helper()
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("failed to compile expression %q: %v", expression, issues.Err())
	}
	prg, err := env.Program(ast, cel.CostTracking(noopCostEstimator{}))
	if err != nil {
		t.Fatalf("failed to instantiate program for %q: %v", expression, err)
	}
	return prg
}

func TestEvaluateSingleExpression(t *testing.T) {
	env := newTestCelEnv(t)

	tests := []struct {
		name          string
		expression    string
		input         map[string]any
		budget        int64
		wantResult    bool
		wantNegBudget bool
	}{
		{
			name:       "successful evaluation returns result and remaining budget",
			expression: "x == 10",
			input:      map[string]any{"x": int64(10)},
			budget:     1000,
			wantResult: true,
		},
		{
			name:       "runtime error returns nil result but still charges budget",
			expression: "10 / x == 5",
			input:      map[string]any{"x": int64(0)},
			budget:     1000,
			wantResult: false,
		},
		{
			name:          "budget already exhausted returns nil result and -1",
			expression:    "x == 10",
			input:         map[string]any{"x": int64(10)},
			budget:        -1,
			wantResult:    false,
			wantNegBudget: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prg := mustCompileCel(t, env, tt.expression)
			result, remaining := EvaluateSingleExpression(context.Background(), prg, tt.budget, tt.expression, tt.input)

			if tt.wantResult && result == nil {
				t.Fatalf("expected a non-nil result, got nil")
			}
			if !tt.wantResult && result != nil {
				t.Fatalf("expected a nil result, got %v", result)
			}
			if tt.wantNegBudget && remaining != -1 {
				t.Fatalf("expected remaining budget -1, got %d", remaining)
			}
			if !tt.wantNegBudget && remaining >= tt.budget {
				t.Fatalf("expected remaining budget to be less than starting budget %d, got %d", tt.budget, remaining)
			}
		})
	}
}

func TestCostCalculation(t *testing.T) {
	env := newTestCelEnv(t)
	prg := mustCompileCel(t, env, "x == 10")

	_, details, err := prg.ContextEval(context.Background(), map[string]any{"x": int64(10)})
	if err != nil {
		t.Fatalf("failed to evaluate program: %v", err)
	}
	if details == nil || details.ActualCost() == nil {
		t.Fatalf("expected a cost-tracked program to return actual cost details")
	}
	actualCost := int64(*details.ActualCost())

	tests := []struct {
		name     string
		details  *cel.EvalDetails
		budget   int64
		wantOK   bool
		wantCost int64
	}{
		{
			name:    "nil eval details fails",
			details: nil,
			budget:  1000,
			wantOK:  false,
		},
		{
			name:     "cost within budget succeeds",
			details:  details,
			budget:   actualCost + 10,
			wantOK:   true,
			wantCost: actualCost,
		},
		{
			name:    "cost exceeding budget fails",
			details: details,
			budget:  actualCost - 1,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, cost := CostCalculation(context.Background(), tt.details, tt.budget, "x == 10")
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v", tt.wantOK, ok)
			}
			if !tt.wantOK && cost != -1 {
				t.Fatalf("expected cost -1 on failure, got %d", cost)
			}
			if tt.wantOK && cost != tt.wantCost {
				t.Fatalf("expected cost %d, got %d", tt.wantCost, cost)
			}
		})
	}
}
