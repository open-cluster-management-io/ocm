package predicate

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestMatchWithClusterPredicates(t *testing.T) {
	cases := []struct {
		name                 string
		placement            *clusterapiv1beta1.Placement
		clusters             []*clusterapiv1.ManagedCluster
		expectedClusterNames []string
	}{
		{
			name: "match with label",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(&metav1.LabelSelector{
				MatchLabels: map[string]string{
					"cloud": "Amazon",
				},
			}, nil).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Google").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with claim",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				&clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "cloud",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"Amazon"},
						},
					},
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithClaim("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("cloud", "Google").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with both label and claim",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				},
				&clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").
					WithLabel("cloud", "Amazon").
					WithClaim("region", "us-east-1").Build(),
				testinghelpers.NewManagedCluster("cluster2").
					WithLabel("cloud", "Amazon").
					WithClaim("region", "us-east-2").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with multiple predicates",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(&metav1.LabelSelector{
				MatchLabels: map[string]string{
					"cloud": "Amazon",
				},
			}, nil).AddPredicate(
				nil, &clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("region", "us-east-1").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithClaim("region", "us-east-2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Predicate{}
			result, status := p.Filter(context.TODO(), c.placement, c.clusters)
			clusters := result.Filtered
			err := status.AsError()
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			expectedClusterNames := sets.NewString(c.expectedClusterNames...)
			if len(clusters) != expectedClusterNames.Len() {
				t.Errorf("expected %d clusters but got %d", expectedClusterNames.Len(), len(clusters))
			}
			for _, cluster := range clusters {
				expectedClusterNames.Delete(cluster.Name)
			}
			if expectedClusterNames.Len() > 0 {
				t.Errorf("expected clusters not selected: %s", strings.Join(expectedClusterNames.List(), ","))
			}
		})
	}

}
