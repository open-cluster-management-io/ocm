package rules

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

type WellKnownConditionRuleResolver interface {
	GetConditionRuleByKind(gvk schema.GroupVersionKind, condition string) workapiv1.ConditionRule
}

type DefaultWellKnownConditionResolver struct {
	rules map[schema.GroupVersionKind]map[string]workapiv1.ConditionRule
}

var jobCompleteRule = workapiv1.ConditionRule{
	Condition: workapiv1.ManifestComplete,
	Type:      workapiv1.CelConditionExpressionsType,
	CelExpressions: []workapiv1.CelConditionExpressions{
		{
			Expression: `hasConditions(object.status)
							? object.status.conditions.filter(c, c.type == 'Complete' || c.type == 'Failed').exists(c, c.status == 'True')
							: false`,
		},
	},
	MessageExpression: `result ? "Job is finished" : "Job is not finished"`,
}

var podCompleteRule = workapiv1.ConditionRule{
	Condition: workapiv1.ManifestComplete,
	Type:      workapiv1.CelConditionExpressionsType,
	CelExpressions: []workapiv1.CelConditionExpressions{
		{
			Expression: "object.status.phase in ['Succeeded', 'Failed']",
		},
	},
	MessageExpression: `"Pod is in phase " + object.status.phase`,
}

func DefaultWellKnownConditionRule() WellKnownConditionRuleResolver {
	return &DefaultWellKnownConditionResolver{
		rules: map[schema.GroupVersionKind]map[string]workapiv1.ConditionRule{
			{Group: "batch", Version: "v1", Kind: "Job"}: {workapiv1.ManifestComplete: jobCompleteRule},
			{Group: "", Version: "v1", Kind: "Pod"}:      {workapiv1.ManifestComplete: podCompleteRule},
		},
	}
}

func (w *DefaultWellKnownConditionResolver) GetConditionRuleByKind(gvk schema.GroupVersionKind, condition string) workapiv1.ConditionRule {
	if conditionRules, ok := w.rules[gvk]; ok {
		return conditionRules[condition]
	}
	return workapiv1.ConditionRule{}
}
