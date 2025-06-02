package conditions

import (
	"context"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"

	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/features"
)

const (
	deploymentJson = `
	{
		"apiVersion":"apps/v1",
		"kind":"Deployment",
		"metadata":{
			"name":"test"
		},
		"status":{
			"readyReplicas":1,
			"replicas":2,
			"conditions":[
				{
					"type":"Available",
					"status":"True"
				}
			]
		}
	}
	`
	jobJsonComplete = `
	{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "True",
					"type": "Complete"
				},
				{
					"status": "True",
					"type": "Another"
				}
			],
			"succeeded": 1
		}
	}
	`
	jobJsonFailed = `
	{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "True",
					"type": "Failed"
				}
			],
			"succeeded": 0
		}
	}
	`
	jobJsonIncomplete = `
	{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "False",
					"type": "Complete"
				},
				{
					"status": "True",
					"type": "Another"
				}
			],
			"succeeded": 0
		}
	}
	`
	podJsonSucceeded = `
	{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "False",
					"type": "Ready"
				}
			],

			"phase": "Succeeded"
		}
	}
	`
	podJsonFailed = `
	{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "False",
					"type": "Ready"
				}
			],

			"phase": "Failed"
		}
	}
	`
	podJsonRunning = `
	{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "test"
		},
		"status": {
			"conditions": [
				{
					"status": "False",
					"type": "Ready"
				}
			],

			"phase": "Running"
		}
	}
	`
)

func unstrctureObject(data string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	_ = obj.UnmarshalJSON([]byte(data))
	return obj
}

func TestConditionReader(t *testing.T) {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	cases := []struct {
		name              string
		object            *unstructured.Unstructured
		rule              workapiv1.ConditionRule
		enableRaw         bool
		expectError       bool
		expectedCondition metav1.Condition
		budget            *int64
	}{
		{
			name:   "deployment available",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.ConditionRule{
				Type:      workapiv1.CelConditionExpressionsType,
				Condition: "Available",
				CelExpressions: []string{
					`object.status.conditions.exists(c, c.type == "Available" && c.status == "True")`,
				},
				MessageExpression: `result ? "Deployment available" : "Deployment unavailable"`,
			},
			expectError: false,
			expectedCondition: metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Deployment available",
			},
		},
		{
			name:   "wrong return type",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.ConditionRule{
				Type:      workapiv1.CelConditionExpressionsType,
				Condition: "Available",
				CelExpressions: []string{
					`object.status.conditions.filter(c, c.type == "Available")[0].status`,
				},
			},
			expectError: true,
			expectedCondition: metav1.Condition{
				Type:    "Available",
				Reason:  workapiv1.ConditionRuleExpressionError,
				Status:  metav1.ConditionFalse,
				Message: "expected bool result, got string",
			},
		},
		{
			name:   "invalid CEL",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.ConditionRule{
				Type:           workapiv1.CelConditionExpressionsType,
				Condition:      "Available",
				CelExpressions: []string{`object.missing`},
			},
			expectError: true,
			expectedCondition: metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  workapiv1.ConditionRuleExpressionError,
				Message: "no such key: missing",
			},
		},
		{
			name:   "invalid message CEL",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.ConditionRule{
				Type:              workapiv1.CelConditionExpressionsType,
				Condition:         "Available",
				CelExpressions:    []string{`true`},
				MessageExpression: `badcel`,
			},
			expectError: true,
			expectedCondition: metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "ERROR: <input>:1:1: undeclared reference to 'badcel' (in container '')",
			},
		},
		{
			name:   "cel lib function hasConditions",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.ConditionRule{
				Type:      workapiv1.CelConditionExpressionsType,
				Condition: "HasConditions",
				CelExpressions: []string{
					`hasConditions(object.status)
						&& !hasConditions({})
						&& !hasConditions("badtype")
						&& !hasConditions({"conditions": []})
						&& !hasConditions({"conditions": null})`,
				},
				Message: "should work",
			},
			expectError: false,
			expectedCondition: metav1.Condition{
				Type:    "HasConditions",
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "should work",
			},
		},
		{
			name:   "Job complete",
			object: unstrctureObject(jobJsonComplete),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Job is finished",
			},
		},
		{
			name:   "Job failed",
			object: unstrctureObject(jobJsonFailed),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Job is finished",
			},
		},
		{
			name:   "Job incomplete",
			object: unstrctureObject(jobJsonIncomplete),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionFalse,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Job is not finished",
			},
		},
		{
			name:   "Pod complete",
			object: unstrctureObject(podJsonSucceeded),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Pod is in phase Succeeded",
			},
		},
		{
			name:   "Pod failed",
			object: unstrctureObject(podJsonFailed),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionTrue,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Pod is in phase Failed",
			},
		},
		{
			name:   "Pod running",
			object: unstrctureObject(podJsonRunning),
			rule:   workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionFalse,
				Reason:  workapiv1.ConditionRuleEvaluated,
				Message: "Pod is in phase Running",
			},
		},
		{
			name:        "Budget exceeded",
			object:      unstrctureObject(podJsonRunning),
			rule:        workapiv1.ConditionRule{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
			expectError: true,
			expectedCondition: metav1.Condition{
				Type:    workapiv1.ManifestComplete,
				Status:  metav1.ConditionFalse,
				Reason:  workapiv1.ConditionRuleExpressionError,
				Message: "CEL evaluation budget exceeded",
			},
			budget: ptr.To(int64(1)),
		},
	}

	reader, err := NewConditionReader()
	if err != nil {
		t.Fatalf("Expected no err when creating ConditionReader but got %v", err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var budget int64
			if c.budget != nil {
				budget = *c.budget
			} else {
				budget = int64(1000)
			}

			condition, remainingBudget, err := reader.GetConditionByRule(context.TODO(), c.object, c.rule, budget)
			if err == nil && c.expectError {
				t.Errorf("%s: Expect error but got no error", c.name)
			}

			if err != nil && !c.expectError {
				t.Errorf("%s: Expect no error but got %v", c.name, err)
			}

			if !apiequality.Semantic.DeepEqual(c.expectedCondition, condition) {
				t.Errorf("%s: Expect condition %+v, but got %+v", c.name, c.expectedCondition, condition)
			}

			if err == nil && remainingBudget >= budget {
				t.Errorf("%s: Expect remaining budget to be less than initial budget. Budget: %d, Remaining: %d", c.name, budget, remainingBudget)
			}
		})
	}
}
