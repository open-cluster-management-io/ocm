package tainttoleration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	testingclock "k8s.io/utils/clock/testing"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
	"open-cluster-management.io/placement/pkg/plugins"
)

var fakeTime = time.Date(2022, time.January, 01, 0, 0, 0, 0, time.UTC)
var requeueTime_1 = fakeTime.Add(1 * time.Second)
var addedTime_8 = fakeTime.Add(-8 * time.Second)
var addedTime_9 = fakeTime.Add(-9 * time.Second)
var addedTime_10 = fakeTime.Add(-10 * time.Second)
var tolerationSeconds_10 = int64(10)

func TestMatchWithClusterTaintToleration(t *testing.T) {

	cases := []struct {
		name                  string
		placement             *clusterapiv1beta1.Placement
		clusters              []*clusterapiv1.ManagedCluster
		initObjs              []runtime.Object
		expectedClusterNames  []string
		expectedRequeueResult plugins.PluginRequeueResult
		expectedErr           error
	}{
		// taint.Effect is NoSelect, testing tolerations with different operator and key/value.
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Equal, key/value matches",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Equal, toleration has empty key",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
			expectedErr:           errors.New("If the key is empty, operator must be Exists."),
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Equal, toleration has empty value",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Equal, key/value doesn't match",
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
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Exist, key matches",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Exist, toleration has empty key",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Value:    "value1",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
			expectedErr:           errors.New("If the operator is Exists, the value should be empty."),
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Exist, toleration has empty value",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect and tolerations.Operator is Exist, toleration key doesn't match",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key2",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		// taint.Effect is NoSelectIfNew, testing tolerations with differet key/value, and cluster is/not in decision
		{
			name:      "taint.Effect is NoSelectIfNew and tolerations is empty, cluster is not in decision",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name:      "taint.Effect is NoSelectIfNew and tolerations is empty, cluster is in decision",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew and tolerations matches, cluster is not in decision",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew and tolerations matches, cluster is in decision",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew and tolerations doesn't match, cluster is not in decision",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew and tolerations doesn't match, cluster is in decision",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		// taint.Effect is PreferNoSelect, testing tolerations with differet key/value
		{
			name:      "taint.Effect is PreferNoSelect and tolerations is Empty",
			placement: testinghelpers.NewPlacement("test", "test").Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is PreferNoSelect and tolerations matches",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is PreferNoSelect and tolerations doesn't match",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Effect:    clusterapiv1.TaintEffectPreferNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		// multiple taints and tolerations
		{
			name: "cluster has multiple taints and placement has multiple tolerations, all the taints are matched",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
				}).AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpExists,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).WithTaint(
					&clusterapiv1.Taint{
						Key:       "key2",
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
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1", "cluster2"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		// testing effect matches
		{
			name: "taint.Effect is NoSelect, key/value matches, toleration effect is empty",
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
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect, key/value matches, toleration effect doesn't match",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelectIfNew,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelect, key/value matches, toleration effect matches",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelect,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, key/value matches, toleration effect is empty",
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
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, key/value matches, toleration effect matches",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value1",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelectIfNew,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, key/value matches, toleration effect doesn't match",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value2",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelectIfNew,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, cluster is in decision, toleration effect is empty",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value2",
					Operator: clusterapiv1beta1.TolerationOpEqual,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, cluster is in decision, toleration effect matches",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value2",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelectIfNew,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "taint.Effect is NoSelectIfNew, cluster is in decision, toleration effect doesn't match",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:      "key1",
					Value:    "value2",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Effect:   clusterapiv1.TaintEffectNoSelect,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelectIfNew,
						TimeAdded: metav1.Time{},
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1").
					Build(),
			},
			expectedClusterNames:  []string{"cluster1"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		// testing TolerationSeconds
		{
			name: "tanits match tolerations by toleration.TolerationSeconds",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:               "key1",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_10,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_8),
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_9),
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value3",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_10),
					}).Build(),
			},
			initObjs:              []runtime.Object{},
			expectedClusterNames:  []string{"cluster1", "cluster2"},
			expectedRequeueResult: plugins.PluginRequeueResult{},
		},
		{
			name: "placement requeue placement when expire toleration.TolerationSeconds",
			placement: testinghelpers.NewPlacement("test", "test").AddToleration(
				&clusterapiv1beta1.Toleration{
					Key:               "key1",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_10,
				}).Build(),
			clusters: []*clusterapiv1.ManagedCluster{
				testinghelpers.NewManagedCluster("cluster1").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value1",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_8),
					}).Build(),
				testinghelpers.NewManagedCluster("cluster2").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value2",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_9),
					}).Build(),
				testinghelpers.NewManagedCluster("cluster3").WithTaint(
					&clusterapiv1.Taint{
						Key:       "key1",
						Value:     "value3",
						Effect:    clusterapiv1.TaintEffectNoSelect,
						TimeAdded: metav1.NewTime(addedTime_10),
					}).Build(),
			},
			initObjs: []runtime.Object{
				testinghelpers.NewPlacementDecision("test", "test").
					WithLabel(placementLabel, "test").
					WithDecisions("cluster1", "cluster2").
					Build(),
			},
			expectedClusterNames: []string{"cluster1", "cluster2"},
			expectedRequeueResult: plugins.PluginRequeueResult{
				RequeueTime: &requeueTime_1,
			},
		},
	}

	TolerationClock = testingclock.NewFakeClock(fakeTime)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, cluster := range c.clusters {
				c.initObjs = append(c.initObjs, cluster)
			}
			p := &TaintToleration{
				handle: testinghelpers.NewFakePluginHandle(t, nil, c.initObjs...),
			}
			result, status := p.Filter(context.TODO(), c.placement, c.clusters)
			clusters := result.Filtered
			err := status.AsError()

			if err != nil && err.Error() != c.expectedErr.Error() {
				t.Errorf("expect err %v but get %v", c.expectedErr, err)
			}

			expectedClusterNames := sets.NewString(c.expectedClusterNames...)
			if len(clusters) != expectedClusterNames.Len() {
				t.Errorf(
					"expected %d clusters but got %d",
					expectedClusterNames.Len(),
					len(clusters),
				)
			}
			for _, cluster := range clusters {
				expectedClusterNames.Delete(cluster.Name)
			}
			if expectedClusterNames.Len() > 0 {
				t.Errorf(
					"expected clusters not selected: %s",
					strings.Join(expectedClusterNames.List(), ","),
				)
			}

			requeueResult, _ := p.RequeueAfter(context.TODO(), c.placement)
			expectedRequeueTime := c.expectedRequeueResult.RequeueTime
			actualRequeueTime := requeueResult.RequeueTime
			if !((expectedRequeueTime == nil && actualRequeueTime == nil) ||
				(expectedRequeueTime != nil && actualRequeueTime != nil && expectedRequeueTime.Equal(*actualRequeueTime))) {
				t.Errorf(
					"expected clusters requeued at: %s, but actual requeue at %s",
					expectedRequeueTime,
					actualRequeueTime,
				)
			}
		})
	}

}
