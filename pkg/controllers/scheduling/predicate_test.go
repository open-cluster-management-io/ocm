package scheduling

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/placement/pkg/helpers/testing"
)

func TestMatchWithClusterPredicates(t *testing.T) {
	cases := []struct {
		name                 string
		predicates           []clusterapiv1alpha1.ClusterPredicate
		clusters             []*clusterapiv1.ManagedCluster
		expectedClusterNames []string
	}{
		{
			name: "match with label",
			predicates: []clusterapiv1alpha1.ClusterPredicate{
				testinghelpers.NewClusterPredicate(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				}, nil),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Google").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with claim",
			predicates: []clusterapiv1alpha1.ClusterPredicate{
				testinghelpers.NewClusterPredicate(nil,
					&clusterapiv1alpha1.ClusterClaimSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "cloud",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"Amazon"},
							},
						},
					}),
			},
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithClaim("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("cloud", "Google").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with both label and claim",
			predicates: []clusterapiv1alpha1.ClusterPredicate{
				testinghelpers.NewClusterPredicate(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				}, &clusterapiv1alpha1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				}),
			},
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
			predicates: []clusterapiv1alpha1.ClusterPredicate{
				testinghelpers.NewClusterPredicate(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cloud": "Amazon",
					},
				}, nil),
				testinghelpers.NewClusterPredicate(nil, &clusterapiv1alpha1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				}),
			},
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
			clusters, err := matchWithClusterPredicates(c.predicates, c.clusters)
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
