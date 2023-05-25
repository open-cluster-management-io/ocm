package debugger

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	scheduling "open-cluster-management.io/placement/pkg/controllers/scheduling"
	testinghelpers "open-cluster-management.io/placement/pkg/helpers/testing"
)

type testScheduler struct {
	result *testResult
}

type testResult struct {
	filterResults     []scheduling.FilterResult
	prioritizeResults []scheduling.PrioritizerResult
	scoreSum          scheduling.PrioritizerScore
}

func (r *testResult) FilterResults() []scheduling.FilterResult {
	return r.filterResults
}

func (r *testResult) PrioritizerResults() []scheduling.PrioritizerResult {
	return r.prioritizeResults
}

func (r *testResult) PrioritizerScores() scheduling.PrioritizerScore {
	return r.scoreSum
}

func (r *testResult) Decisions() []clusterapiv1beta1.ClusterDecision {
	return []clusterapiv1beta1.ClusterDecision{}
}

func (r *testResult) NumOfUnscheduled() int {
	return 0
}

func (s *testScheduler) Schedule(ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (scheduling.ScheduleResult, *framework.Status) {
	return s.result, nil
}

func (r *testResult) RequeueAfter() *time.Duration {
	return nil
}

func TestDebugger(t *testing.T) {
	placementNamespace := "test"

	placementName := "test"

	cases := []struct {
		name              string
		initObjs          []runtime.Object
		filterResults     []scheduling.FilterResult
		prioritizeResults []scheduling.PrioritizerResult
		key               string
	}{
		{
			name: "A valid placement",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
				testinghelpers.NewManagedCluster("cluster1").Build(),
				testinghelpers.NewManagedCluster("cluster2").Build(),
			},
			filterResults:     []scheduling.FilterResult{{Name: "filter1", FilteredClusters: []string{"cluster1", "cluster2"}}},
			prioritizeResults: []scheduling.PrioritizerResult{{Name: "prioritize1", Scores: map[string]int64{"cluster1": 100, "cluster2": 0}}},
			key:               placementNamespace + "/" + placementName,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)
			s := &testScheduler{result: &testResult{filterResults: c.filterResults, prioritizeResults: c.prioritizeResults}}
			debugger := NewDebugger(
				s, clusterInformerFactory.Cluster().V1beta1().Placements(), clusterInformerFactory.Cluster().V1().ManagedClusters())
			server := httptest.NewServer(http.HandlerFunc(debugger.Handler))
			res, err := http.Get(fmt.Sprintf("%s%s%s", server.URL, DebugPath, c.key))

			if err != nil {
				t.Errorf("Expect no error but get %v", err)
			}

			responseBody, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Errorf("Unexpected error reading response body: %v", err)
			}

			result := &DebugResult{}
			err = json.Unmarshal(responseBody, result)
			if err != nil {
				t.Errorf("Unexpected error unmarshaling reulst: %v", err)
			}

			if !reflect.DeepEqual(result.FilterResults, c.filterResults) {
				t.Errorf("Expect filter result to be: %v. but got: %v", c.filterResults, result.FilterResults)
			}

			if !reflect.DeepEqual(result.PrioritizeResults, c.prioritizeResults) {
				t.Errorf("Expect prioritize result to be: %v. but got: %v", c.prioritizeResults, result.PrioritizeResults)
			}

			server.Close()
		})
	}
}
