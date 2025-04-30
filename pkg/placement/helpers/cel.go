package helpers

import (
	"context"
	"math"
	"time"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/klog/v2"

	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	ocmcelcommon "open-cluster-management.io/sdk-go/pkg/cel/common"
	ocmcellibrary "open-cluster-management.io/sdk-go/pkg/cel/library"

	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
)

var globalCostBudget = int64(celconfig.RuntimeCELCostBudget)

// CompilationResult represents the compilation result of a single CEL expression,
// containing either a valid program or an error.
type CompilationResult struct {
	Program cel.Program
	Error   *apiservercel.Error
}

// CELSelector handles CEL-based cluster selection by managing a set of CEL expressions
// and their compilation results.
type CELSelector struct {
	env               *cel.Env                 // CEL environment with registered libraries
	metricsRecorder   *metrics.ScheduleMetrics // Metrics recorder
	celExpressions    []string                 // Raw CEL expressions to evaluate
	compilationResult []CompilationResult      // Cached compilation results
}

// NewEnv creates a new CEL environment with managed cluster and JSON libraries.
// It takes a score lister to enable score-based cluster selection.
func NewEnv(scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister) (*cel.Env, error) {
	envOpts := append([]cel.EnvOption{
		ocmcellibrary.ManagedClusterLib(scoreLister),
		ocmcellibrary.JsonLib(),
	}, ocmcelcommon.BaseEnvOpts...)
	return cel.NewEnv(envOpts...)
}

// newEstimator creates a new cost estimator for CEL expressions.
func newEstimator() *ocmcelcommon.BaseEnvCostEstimator {
	return &ocmcelcommon.BaseEnvCostEstimator{
		CostEstimator: &ocmcellibrary.CostEstimator{},
	}
}

// NewCELSelector creates a new CEL selector with the given environment and expressions.
func NewCELSelector(env *cel.Env, expressions []string, metricsRecorder *metrics.ScheduleMetrics) *CELSelector {
	return &CELSelector{
		env:               env,
		metricsRecorder:   metricsRecorder,
		celExpressions:    expressions,
		compilationResult: make([]CompilationResult, len(expressions)),
	}
}

// Compile compiles all the CEL expressions and returns a slice containing a
// CompilationResult for each expressions.
func (c *CELSelector) Compile() []CompilationResult {
	if c.env == nil || len(c.celExpressions) == 0 {
		return c.compilationResult
	}

	estimator := newEstimator()
	for i, expr := range c.celExpressions {
		ast, issues := c.env.Compile(expr)
		if issues != nil {
			c.compilationResult[i].Error = &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: "compilation failed: " + issues.String(),
			}
			continue
		}

		prg, err := c.env.Program(ast,
			cel.CostLimit(celconfig.PerCallLimit),
			cel.CostTracking(estimator),
			cel.InterruptCheckFrequency(celconfig.CheckFrequency),
		)

		if err != nil {
			c.compilationResult[i].Error = &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: "instantiation failed: " + err.Error(),
			}
			continue
		}

		c.compilationResult[i].Program = prg
	}
	return c.compilationResult
}

// Validate evaluates all compiled CEL expressions against a managed cluster.
// Returns (true, cost) if all expressions evaluate to true and within cost budget.
// Returns (false, cost) if validation fails.
func (c *CELSelector) Validate(ctx context.Context, cluster *clusterapiv1.ManagedCluster) (bool, int64) {
	logger := klog.FromContext(ctx)

	// Convert cluster to format required by CEL
	convertedCluster, err := ocmcelcommon.ConvertObjectToUnstructured(cluster)
	if err != nil {
		logger.Error(err, "Failed to convert cluster to unstructured format", "cluster", cluster.Name)
		return false, -1
	}

	startTime := time.Now()
	ok, remainingBudget := c.evaluateAllExpressions(ctx, convertedCluster, globalCostBudget)
	if c.metricsRecorder != nil {
		metrics.CelDuration.WithLabelValues(metrics.SchedulingName).Observe(c.metricsRecorder.SinceInSeconds(startTime))
	}
	cost := globalCostBudget - remainingBudget
	return ok, cost
}

// evaluateAllExpressions evaluates each CEL expression in sequence.
// Returns (true, remainingBudget) if all expressions succeed, otherwise (false, budget at failure).
func (c *CELSelector) evaluateAllExpressions(ctx context.Context, cluster *unstructured.Unstructured, budget int64) (bool, int64) {
	logger := klog.FromContext(ctx)
	remainingBudget := budget

	for i, compiled := range c.compilationResult {
		// Validate program compilation
		if !c.isProgramValid(compiled) {
			logger.Info("Validation failed: invalid compiled program", "rule", c.celExpressions[i])
			return false, remainingBudget
		}

		// Evaluate single expression
		ok, newBudget := c.evaluateSingleExpression(ctx, compiled.Program, cluster, remainingBudget, i)
		if !ok {
			return false, newBudget
		}
		remainingBudget = newBudget
	}

	return true, remainingBudget
}

// isProgramValid checks if a compilation result contains a valid program
func (c *CELSelector) isProgramValid(compiled CompilationResult) bool {
	return compiled.Program != nil && compiled.Error == nil
}

// evaluateSingleExpression evaluates one CEL expression and handles its cost accounting.
// Returns (true, newBudget) if evaluation succeeds, otherwise (false, -1 or remaining budget).
func (c *CELSelector) evaluateSingleExpression(
	ctx context.Context,
	program cel.Program,
	cluster *unstructured.Unstructured,
	budget int64,
	index int,
) (bool, int64) {
	logger := klog.FromContext(ctx)

	// Evaluate the expression
	evalResult, evalDetails, err := program.ContextEval(ctx, map[string]interface{}{
		"managedCluster": cluster.Object,
	})

	// Cost calculation
	ok, rtCost := c.costCalculation(ctx, evalDetails, budget, index)
	if !ok {
		return false, -1
	}

	remainingBudget := budget - rtCost
	// Handle evaluation error
	if err != nil {
		logger.Info("Expression evaluation failed", "rule", c.celExpressions[index], "cluster", cluster.GetName(), "err", err)
		return false, remainingBudget
	}

	// Check expression result
	if value, ok := evalResult.Value().(bool); !ok || !value {
		return false, remainingBudget
	}

	return true, remainingBudget
}

// costCalculation processes the cost details of an evaluation
func (c *CELSelector) costCalculation(ctx context.Context, evalDetails *cel.EvalDetails, budget int64, index int) (bool, int64) {
	logger := klog.FromContext(ctx)

	// Check if cost details are available
	if evalDetails == nil {
		logger.Info("Runtime cost calculation failed: no evaluation details",
			"rule", c.celExpressions[index])
		return false, -1
	}

	rtCost := evalDetails.ActualCost()
	if rtCost == nil {
		logger.Info("Runtime cost calculation failed: no cost information",
			"rule", c.celExpressions[index])
		return false, -1
	}

	// Validate cost against budget
	if *rtCost > math.MaxInt64 || int64(*rtCost) > budget {
		logger.Info("Cost budget exceeded",
			"rule", c.celExpressions[index],
			"cost", *rtCost,
			"budget", budget)
		return false, -1
	}

	// Safe to convert since we checked for overflow
	return true, int64(*rtCost) //nolint:gosec
}
