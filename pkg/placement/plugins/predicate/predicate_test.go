package predicate

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

func TestMatchWithClusterPredicates(t *testing.T) {
	cases := []struct {
		name                 string
		placement            *clusterapiv1beta1.Placement
		clusters             []*clusterapiv1.ManagedCluster
		existingAddOnScores  []runtime.Object
		expectedClusterNames []string
	}{
		{
			name: "match with label",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(&metav1.LabelSelector{
				MatchLabels: map[string]string{
					"cloud": "Amazon",
				},
			}, nil, nil).Build(),
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
				}, nil).Build(),
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
				}, nil).Build(),
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
			}, nil, nil).AddPredicate(
				nil, &clusterapiv1beta1.ClusterClaimSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "region",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"us-east-1"},
						},
					},
				}, nil).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("region", "us-east-1").Build(),
				testinghelpers.NewManagedCluster("cluster3").WithClaim("region", "us-east-2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name: "match with CEL expression on labels",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["region"] == "us-east-1"`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("region", "us-east-1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("region", "us-west-1").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with CEL expression on claims",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.status.clusterClaims.exists(c, c.name == "cloud" && c.value == "Amazon")`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithClaim("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("cloud", "Google").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with CEL score expression",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.scores("test-addon").filter(s, s.name == 'test-score').all(e, e.value > 80)`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test-addon").WithScore("test-score", 90).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "test-addon").WithScore("test-score", 70).Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with multiple CEL expressions",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["env"] == "prod"`,
						`managedCluster.scores("test-addon").filter(s, s.name == 'test-score').all(e, e.value > 80)`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("env", "prod").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("env", "prod").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test-addon").WithScore("test-score", 90).Build(),
				testinghelpers.NewAddOnPlacementScore("cluster2", "test-addon").WithScore("test-score", 70).Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with CEL expression and other selectors",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				&metav1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "prod",
					},
				}, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.status.clusterClaims.exists(c, c.name == "cloud" && c.value == "Amazon")`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("env", "prod").WithClaim("cloud", "Amazon").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("env", "prod").WithClaim("cloud", "Azure").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "invalid CEL expression",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`invalid.expression`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
			},
			expectedClusterNames: []string{},
		},
		{
			name: "empty CEL expressions",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(nil, nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Predicate{handle: testinghelpers.NewFakePluginHandle(t, nil, c.existingAddOnScores...)}
			result, status := p.Filter(context.TODO(), c.placement, c.clusters)
			clusters := result.Filtered
			err := status.AsError()
			if err != nil && status.Code() != framework.Misconfigured {
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
