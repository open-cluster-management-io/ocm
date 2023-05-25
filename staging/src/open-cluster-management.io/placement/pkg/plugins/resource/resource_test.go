package resource

import (
	"context"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestScoreClusterWithResource(t *testing.T) {
	cases := []struct {
		name              string
		resource          clusterapiv1.ResourceName
		algorithm         string
		placement         *clusterapiv1beta1.Placement
		clusters          []*clusterapiv1.ManagedCluster
		existingDecisions []runtime.Object
		expectedScores    map[string]int64
	}{
		{
			name:      "scores of ResourceAllocatableMemory",
			resource:  clusterapiv1.ResourceMemory,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceMemory, "20", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceMemory, "60", "100").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceMemory, "100", "100").Build(),
			},
			expectedScores: map[string]int64{"cluster1": -100, "cluster2": 0, "cluster3": 100},
		},
		{
			name:      "scores of ResourceAllocatableMemory with same resource value",
			resource:  clusterapiv1.ResourceMemory,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceMemory, "50", "100").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "scores of ResourceAllocatableMemory with zero resource value",
			resource:  clusterapiv1.ResourceMemory,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceMemory, "0", "100").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceMemory, "0", "100").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceMemory, "0", "100").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "scores of ResourceAllocatableMemory with no cluster resource",
			resource:  clusterapiv1.ResourceMemory,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			expectedScores: map[string]int64{},
		},
		{
			name:      "scores of ResourceAllocatableCPU",
			resource:  clusterapiv1.ResourceCPU,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceCPU, "10", "10").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceCPU, "6", "10").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceCPU, "2", "10").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 0, "cluster3": -100},
		},
		{
			name:      "scores of ResourceAllocatableCPU with same resource value",
			resource:  clusterapiv1.ResourceCPU,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceCPU, "5", "10").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceCPU, "5", "10").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceCPU, "5", "10").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "scores of ResourceAllocatableCPU with zero resource value",
			resource:  clusterapiv1.ResourceCPU,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithResource(clusterapiv1.ResourceCPU, "0", "10").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithResource(clusterapiv1.ResourceCPU, "0", "10").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithResource(clusterapiv1.ResourceCPU, "0", "10").Build(),
			},
			expectedScores: map[string]int64{"cluster1": 100, "cluster2": 100, "cluster3": 100},
		},
		{
			name:      "scores of ResourceAllocatableCPU with no cluster resource",
			resource:  clusterapiv1.ResourceCPU,
			algorithm: "Allocatable",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
				testinghelpers.NewManagedCluster("cluster3").Build(),
			},
			expectedScores: map[string]int64{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resource := &ResourcePrioritizer{
				handle:    testinghelpers.NewFakePluginHandle(t, nil, c.existingDecisions...),
				resource:  c.resource,
				algorithm: c.algorithm,
			}

			scoreResult, status := resource.Score(context.TODO(), c.placement, c.clusters)
			scores := scoreResult.Scores
			err := status.AsError()

			if err != nil {
				t.Errorf("Expect no error, but got %v", err)
			}

			if !apiequality.Semantic.DeepEqual(scores, c.expectedScores) {
				t.Errorf("Expect score %v, but got %v", c.expectedScores, scores)
			}
		})
	}
}
