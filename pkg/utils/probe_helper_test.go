package utils

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func boolPtr(n int64) *int64 {
	return &n
}

func TestDeploymentProbe(t *testing.T) {
	cases := []struct {
		name        string
		result      workapiv1.StatusFeedbackResult
		expectedErr bool
	}{
		{
			name:        "no result",
			result:      workapiv1.StatusFeedbackResult{},
			expectedErr: true,
		},
		{
			name: "no matched value",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "check failed with 0 replica",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(0),
						},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "check passed",
			result: workapiv1.StatusFeedbackResult{
				Values: []workapiv1.FeedbackValue{
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(1),
						},
					},
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Integer: boolPtr(2),
						},
					},
				},
			},
			expectedErr: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prober := NewDeploymentProber(types.NamespacedName{Name: "test", Namespace: "testns"})

			fields := prober.WorkProber.ProbeFields

			err := prober.WorkProber.HealthCheck(fields[0].ResourceIdentifier, c.result)
			if err != nil && !c.expectedErr {
				t.Errorf("expected no error but got %v", err)
			}

			if err == nil && c.expectedErr {
				t.Error("expected error but got no error")
			}
		})
	}
}
