package tainttoleration

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

func TestMatchWithClusterTaintToleration(t *testing.T) {

	cases := []struct {
		name                 string
		placement            *clusterapiv1beta1.Placement
		clusters             []*clusterapiv1.ManagedCluster
		existingDecisions    []runtime.Object
		expectedClusterNames []string
	}{
		{
			name:      "taint.Effect is NoSelect and tolerations is empty",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions:    []runtime.Object{},
			expectedClusterNames: []string{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Equal",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions:    []runtime.Object{},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Exist",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions:    []runtime.Object{},
			expectedClusterNames: []string{"cluster1"},
		},
		{
			name:      "taint.Effect is NoSelectIfNew and tolerations is empty",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key3",
						Value:     "value3",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").WithLabel(placementLabel, "test").WithDecisions("cluster2").Build(),
			},
			expectedClusterNames: []string{"cluster2"},
		},
		{
			name: "taint.Effect is NoSelectIfNew and tolerations is Exist",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key3",
						Value:     "value3",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").WithLabel(placementLabel, "test").WithDecisions("cluster2").Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name:      "taint.Effect is PreferNoSelect and tolerations is Empty",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions:    []runtime.Object{},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
		{
			name: "taint.Effect is PreferNoSelect and tolerations is Exist",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key3",
						Value:     "value3",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			existingDecisions:    []runtime.Object{},
			expectedClusterNames: []string{"cluster1", "cluster2"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &TaintToleration{
				handle: testinghelpers.NewFakePluginHandle(t, nil, c.existingDecisions...),
			}
			clusters, err := p.Filter(context.TODO(), c.placement, c.clusters)

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
