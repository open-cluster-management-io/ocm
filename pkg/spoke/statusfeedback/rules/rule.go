package rules

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type WellKnownStatusRuleResolver interface {
	GetPathsByKind(schema.GroupVersionKind) []workapiv1.JsonPath
}

type DefaultWellKnownStatusResolver struct {
	rules map[schema.GroupVersionKind][]workapiv1.JsonPath
}

var deploymentRule = []workapiv1.JsonPath{
	{
		Name: "ReadyReplicas",
		Path: ".status.readyReplicas",
	},
	{
		Name: "Replicas",
		Path: ".status.replicas",
	},
	{
		Name: "AvailableReplicas",
		Path: ".status.availableReplicas",
	},
}

func DefaultWellKnownStatusRule() WellKnownStatusRuleResolver {
	return &DefaultWellKnownStatusResolver{
		rules: map[schema.GroupVersionKind][]workapiv1.JsonPath{
			{Group: "apps", Version: "v1", Kind: "Deployment"}: deploymentRule,
		},
	}
}

func (w *DefaultWellKnownStatusResolver) GetPathsByKind(gvk schema.GroupVersionKind) []workapiv1.JsonPath {
	return w.rules[gvk]
}
