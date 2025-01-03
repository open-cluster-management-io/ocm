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
			name: "match with cel expression - label",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["version"].matches('^1\\.(14|15)\\.\\d+$')`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("version", "1.14.3").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("version", "1.16.3").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with cel expression - claim",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.status.clusterClaims.exists(c, c.name == "version" && c.value.matches('^1\\.(14|15)\\.\\d+$'))`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithClaim("version", "1.14.3").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithClaim("version", "1.16.3").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with cel expression - kubernetes version",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.status.version.kubernetes.versionIsGreaterThan('v1.29.0')`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Withk8sVersion("v1.30.6").Build(),
				testinghelpers.NewManagedCluster("cluster2").Withk8sVersion("v1.28.3").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with cel expression - multiple expressions",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.metadata.labels["cloud"] == "Amazon"`,
						`managedCluster.status.clusterClaims.exists(c, c.name == "region" && c.value == "us-east-1")`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithLabel("cloud", "Amazon").WithClaim("region", "us-east-1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("cloud", "Amazon").WithClaim("region", "us-east-2").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "match with cel expression - score quantity",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.scores("test-score").filter(s, s.name == "memory").all(e, e.quantity.quantityIsGreaterThan("200Mi"))`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test-score").WithScore("memory", 4).WithQuantity("memory", "300Mi").Build(),
			},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "not match with cel expression - score value",
			placement: testinghelpers.NewPlacement("test", "test").AddPredicate(
				nil,
				nil,
				&clusterapiv1beta1.ClusterCelSelector{
					CelExpressions: []string{
						`managedCluster.scores("test-score").filter(s, s.name == "cpu").all(e, e.value > 3)`,
					},
				},
			).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").Build(),
			},
			existingAddOnScores: []runtime.Object{
				testinghelpers.NewAddOnPlacementScore("cluster1", "test-score").WithScore("cpu", 3).WithQuantity("cpu", "3").
					Build(),
			},
			expectedClusterNames: []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Predicate{
				handle: testinghelpers.NewFakePluginHandle(t, nil, c.existingAddOnScores...),
			}
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
