package helpers

import (
	"context"

	"github.com/google/cel-go/cel"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/klog/v2"

	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	ocmcelcommon "open-cluster-management.io/sdk-go/pkg/cel/common"
	ocmcellibrary "open-cluster-management.io/sdk-go/pkg/cel/library"
)

// CompilationResult represents the compilation result of a single CEL expression,
// containing either a valid program or an error.
type CompilationResult struct {
	Program cel.Program
	Error   *apiservercel.Error
}

// CELSelector handles CEL-based cluster selection by managing a set of CEL expressions
// and their compilation results.
type CELSelector struct {
	env               *cel.Env            // CEL environment with registered libraries
	celExpressions    []string            // Raw CEL expressions to evaluate
	compilationResult []CompilationResult // Cached compilation results
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

// NewCELSelector creates a new CEL selector with the given environment and expressions.
func NewCELSelector(env *cel.Env, expressions []string) *CELSelector {
	return &CELSelector{
		env:               env,
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

	for i, expr := range c.celExpressions {
		ast, issues := c.env.Compile(expr)
		if issues != nil {
			c.compilationResult[i].Error = &apiservercel.Error{
				Type:   apiservercel.ErrorTypeInvalid,
				Detail: "compilation failed: " + issues.String(),
			}
			continue
		}

		prg, err := c.env.Program(ast)
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
// Returns true only if all expressions evaluate to true.
// Note: Compile() must be called before calling this method to ensure expressions are properly compiled.
// If Compile() has not been called, this method will return false.
func (c *CELSelector) Validate(ctx context.Context, cluster *clusterapiv1.ManagedCluster) bool {
	logger := klog.FromContext(ctx)
	convertedCluster, err := ocmcelcommon.ConvertObjectToUnstructured(cluster)
	if err != nil {
		logger.Error(err, "Failed to convert cluster to unstructured format", "cluster", cluster.Name)
		return false
	}

	for i, compiled := range c.compilationResult {
		if !isValidProgram(compiled) {
			logger.Info("Validation failed: invalid compiled program", "rule", c.celExpressions[i])
			return false
		}

		result, _, err := compiled.Program.Eval(map[string]interface{}{
			"managedCluster": convertedCluster.Object,
		})

		if err != nil {
			logger.Error(err, "Evaluation failed", "rule", c.celExpressions[i])
			return false
		}

		if value, ok := result.Value().(bool); !ok || !value {
			return false
		}
	}
	return true
}

// isValidProgram checks if a compilation result contains a valid program.
func isValidProgram(compiled CompilationResult) bool {
	return compiled.Program != nil && compiled.Error == nil
}
