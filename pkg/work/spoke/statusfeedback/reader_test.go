package statusfeedback

import (
	"fmt"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/pointer"

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
					"status":"true"
				}
			]
		}
	}
	`
	deploymentJsonMultiCondition = `
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
					"type":"Cond1",
					"status":"true"
				},
                {
					"type":"Cond2",
					"status":"false"
				},
                {
					"type":"Cond3",
					"status":"true"
				}
			]
		}
	}
	`
	deploymentJsonUknownGroup = `
	{
		"apiVersion":"extensions/v1",
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
					"status":"true"
				},
                {
					"type":"Ready",
					"status":"true"
				}
			]
		}
	}
	`
	jobJson = `
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
				}
			],
			"succeeded": 1
		}
	}
	`
	podJson = `
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
)

func unstrctureObject(data string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	_ = obj.UnmarshalJSON([]byte(data))
	return obj
}

func TestStatusReader(t *testing.T) {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	cases := []struct {
		name          string
		object        *unstructured.Unstructured
		rule          workapiv1.FeedbackRule
		enableRaw     bool
		expectError   bool
		expectedValue []workapiv1.FeedbackValue
	}{
		{
			name:        "deployment values",
			object:      unstrctureObject(deploymentJson),
			rule:        workapiv1.FeedbackRule{Type: workapiv1.WellKnownStatusType},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "ReadyReplicas",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.Integer,
						Integer: pointer.Int64(1),
					},
				},
				{
					Name: "Replicas",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.Integer,
						Integer: pointer.Int64(2),
					},
				},
			},
		},
		{
			name:   "deployment jsonpaths",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "available",
						Path: ".status.conditions[?(@.type==\"Available\")].status ",
					},
				},
			},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "available",
					Value: workapiv1.FieldValue{
						Type:   workapiv1.String,
						String: pointer.String("true"),
					},
				},
			},
		},
		{
			name:   "wrong return type",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "available",
						Path: ".status.conditions",
					},
					{
						Name: "replicas",
						Path: ".status.replicas",
					},
				},
			},
			expectError: true,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "replicas",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.Integer,
						Integer: pointer.Int64(2),
					},
				},
			},
		},
		{
			name:          "mismatched gvk",
			object:        unstrctureObject(deploymentJsonUknownGroup),
			rule:          workapiv1.FeedbackRule{Type: workapiv1.WellKnownStatusType},
			expectError:   true,
			expectedValue: []workapiv1.FeedbackValue{},
		},
		{
			name:   "wrong version set for jsonpaths",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name:    "available",
						Path:    ".status.conditions",
						Version: "v1beta1",
					},
					{
						Name: "replicas",
						Path: ".status.replicas",
					},
				},
			},
			expectError: true,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "replicas",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.Integer,
						Integer: pointer.Int64(2),
					},
				},
			},
		},
		{
			name:        "Job values",
			object:      unstrctureObject(jobJson),
			rule:        workapiv1.FeedbackRule{Type: workapiv1.WellKnownStatusType},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "JobComplete",
					Value: workapiv1.FieldValue{
						Type:   workapiv1.String,
						String: pointer.String("True"),
					},
				},
				{
					Name: "JobSucceeded",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.Integer,
						Integer: pointer.Int64(1),
					},
				},
			},
		},
		{
			name:        "Pod values",
			object:      unstrctureObject(podJson),
			rule:        workapiv1.FeedbackRule{Type: workapiv1.WellKnownStatusType},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "PodReady",
					Value: workapiv1.FieldValue{
						Type:   workapiv1.String,
						String: pointer.String("False"),
					},
				},
				{
					Name: "PodPhase",
					Value: workapiv1.FieldValue{
						Type:   workapiv1.String,
						String: pointer.String("Succeeded"),
					},
				},
			},
		},
		{
			// this is for a backward compatible test, when rawjson is disabled, and there are multiple match on
			// json path, it should return the first item.
			name:   "Return 1st item with multiple patch",
			object: unstrctureObject(deploymentJson),
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "type",
						Path: ".status.conditions[?(@.status==\"true\")].type ",
					},
				},
			},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "type",
					Value: workapiv1.FieldValue{
						Type:   workapiv1.String,
						String: pointer.String("Available"),
					},
				},
			},
		},
		{
			name:      "rawjson value format",
			object:    unstrctureObject(podJson),
			enableRaw: true,
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "conditions",
						Path: ".status.conditions",
					},
				},
			},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "conditions",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.JsonRaw,
						JsonRaw: pointer.String(`[{"status":"False","type":"Ready"}]`),
					},
				},
			},
		},
		{
			name:      "rawjson value exceed max length",
			object:    unstrctureObject(podJson),
			enableRaw: true,
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "status",
						Path: ".status",
					},
				},
			},
			expectError: true,
		},
		{
			name:      "filtered rawjson value format",
			object:    unstrctureObject(deploymentJsonMultiCondition),
			enableRaw: true,
			rule: workapiv1.FeedbackRule{
				Type: workapiv1.JSONPathsType,
				JsonPaths: []workapiv1.JsonPath{
					{
						Name: "conditions",
						Path: ".status.conditions[?(@.status==\"true\")].type",
					},
				},
			},
			expectError: false,
			expectedValue: []workapiv1.FeedbackValue{
				{
					Name: "conditions",
					Value: workapiv1.FieldValue{
						Type:    workapiv1.JsonRaw,
						JsonRaw: pointer.String(`["Cond1","Cond3"]`),
					},
				},
			},
		},
	}

	reader := NewStatusReader().WithMaxJsonRawLength(40)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=%t", ocmfeature.RawFeedbackJsonString, c.enableRaw))
			if err != nil {
				t.Fatal(err)
			}
			values, err := reader.GetValuesByRule(c.object, c.rule)
			if err == nil && c.expectError {
				t.Errorf("Expect error but got no error")
			}

			if err != nil && !c.expectError {
				t.Errorf("Expect no error but got %v", err)
			}

			if !apiequality.Semantic.DeepEqual(c.expectedValue, values) {
				t.Errorf("Expect value %v, but got %v", c.expectedValue, values)
			}
		})
	}
}
