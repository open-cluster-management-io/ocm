package debugger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	"open-cluster-management.io/ocm/pkg/placement/controllers/scheduling"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

type testScheduler struct {
	result *testResult
}

type testResult struct {
	filterResults     []scheduling.FilterResult
	prioritizeResults []scheduling.PrioritizerResult
	prioritizerScore  scheduling.PrioritizerScore
}

func (r *testResult) FilterResults() []scheduling.FilterResult {
	return r.filterResults
}

func (r *testResult) PrioritizerResults() []scheduling.PrioritizerResult {
	return r.prioritizeResults
}

func (r *testResult) PrioritizerScores() scheduling.PrioritizerScore {
	return r.prioritizerScore
}

func (r *testResult) Decisions() []*clusterapiv1.ManagedCluster {
	return []*clusterapiv1.ManagedCluster{}
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

func assertErrorResponse(t *testing.T, res *http.Response, expectStatus int, errorContains string) {
	t.Helper()

	if res.StatusCode != expectStatus {
		t.Errorf("Expected HTTP status %d, got %d", expectStatus, res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	var result DebugResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Error == "" {
		t.Fatal("Expected error in response body")
	}
	if errorContains != "" && !strings.Contains(result.Error, errorContains) {
		t.Errorf("Expected error to contain %q, got: %s", errorContains, result.Error)
	}
}

func newAuthenticatedDebuggerServer(t *testing.T, initObjs []runtime.Object, mutate func(*Debugger)) (*httptest.Server, func()) {
	t.Helper()

	clusterClient := clusterfake.NewSimpleClientset(initObjs...)
	clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, initObjs...)
	kubeClient := kubefake.NewClientset()
	kubeClient.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sar := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		sar.Status.Allowed = true
		return true, sar, nil
	})

	debugger := NewDebugger(
		&testScheduler{result: &testResult{}},
		kubeClient,
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1().ManagedClusters(),
		clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
	)
	if mutate != nil {
		mutate(debugger)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userInfo := &user.DefaultInfo{
			Name:   "test-user",
			Groups: []string{"system:authenticated"},
		}
		ctx := request.WithUser(r.Context(), userInfo)
		debugger.Handler(w, r.WithContext(ctx))
	})

	server := httptest.NewServer(handler)
	return server, server.Close
}

type fakePlacementLister struct {
	getErr error
}

func (f *fakePlacementLister) List(labels.Selector) ([]*clusterapiv1beta1.Placement, error) {
	return nil, nil
}

func (f *fakePlacementLister) Placements(string) clusterlisterv1beta1.PlacementNamespaceLister {
	return fakePlacementNamespaceLister{getErr: f.getErr}
}

type fakePlacementNamespaceLister struct {
	getErr error
}

func (f fakePlacementNamespaceLister) List(labels.Selector) ([]*clusterapiv1beta1.Placement, error) {
	return nil, nil
}

func (f fakePlacementNamespaceLister) Get(string) (*clusterapiv1beta1.Placement, error) {
	return nil, f.getErr
}

type fakeClusterSetBindingLister struct {
	listErr error
}

func (f *fakeClusterSetBindingLister) List(labels.Selector) ([]*clusterapiv1beta2.ManagedClusterSetBinding, error) {
	return nil, nil
}

func (f *fakeClusterSetBindingLister) ManagedClusterSetBindings(string) clusterlisterv1beta2.ManagedClusterSetBindingNamespaceLister {
	return fakeClusterSetBindingNamespaceLister{listErr: f.listErr}
}

type fakeClusterSetBindingNamespaceLister struct {
	listErr error
}

func (f fakeClusterSetBindingNamespaceLister) List(labels.Selector) ([]*clusterapiv1beta2.ManagedClusterSetBinding, error) {
	return nil, f.listErr
}

func (f fakeClusterSetBindingNamespaceLister) Get(string) (*clusterapiv1beta2.ManagedClusterSetBinding, error) {
	return nil, nil
}

type fakeManagedClusterLister struct {
	listErr error
}

func (f *fakeManagedClusterLister) List(labels.Selector) ([]*clusterapiv1.ManagedCluster, error) {
	return nil, f.listErr
}

func (f *fakeManagedClusterLister) Get(string) (*clusterapiv1.ManagedCluster, error) {
	return nil, nil
}

func TestReportErr(t *testing.T) {
	debugger := &Debugger{}
	rec := httptest.NewRecorder()

	debugger.reportErr(rec, http.StatusBadRequest, fmt.Errorf("test error"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected HTTP status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", ct)
	}

	var result DebugResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result.Error != "test error" {
		t.Errorf("Expected error %q, got %q", "test error", result.Error)
	}
}

func TestReportPermissionErr(t *testing.T) {
	debugger := &Debugger{}

	cases := []struct {
		name         string
		err          error
		expectStatus int
	}{
		{
			name:         "missing user",
			err:          fmt.Errorf("user information not found in request context"),
			expectStatus: http.StatusUnauthorized,
		},
		{
			name:         "forbidden create",
			err:          fmt.Errorf("user does not have permission to create placements in namespace test: denied"),
			expectStatus: http.StatusForbidden,
		},
		{
			name:         "forbidden get",
			err:          fmt.Errorf("user does not have permission to get placements in namespace test: denied"),
			expectStatus: http.StatusForbidden,
		},
		{
			name:         "unsupported method",
			err:          fmt.Errorf("unsupported method DELETE"),
			expectStatus: http.StatusMethodNotAllowed,
		},
		{
			name:         "internal",
			err:          fmt.Errorf("failed to check permissions: boom"),
			expectStatus: http.StatusInternalServerError,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			debugger.reportPermissionErr(rec, c.err)
			if rec.Code != c.expectStatus {
				t.Errorf("Expected HTTP status %d, got %d", c.expectStatus, rec.Code)
			}
		})
	}
}

func TestDebuggerHandlerReportErr(t *testing.T) {
	const (
		placementNamespace = "test-ns"
		placementName      = "test-placement"
	)

	validObjs := []runtime.Object{
		testinghelpers.NewPlacement(placementNamespace, placementName).WithClusterSets("test-set").Build(),
		testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set").Build(),
		testinghelpers.NewClusterSet("test-set").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
			SelectorType: clusterapiv1beta2.LabelSelector,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"clusterset": "test-set"},
			},
		}).Build(),
		testinghelpers.NewClusterSetBinding(placementNamespace, "test-set"),
	}

	cases := []struct {
		name          string
		initObjs      []runtime.Object
		method        string
		pathSuffix    string
		body          []byte
		mutate        func(*Debugger)
		expectStatus  int
		errorContains string
	}{
		{
			name:          "POST invalid JSON",
			method:        http.MethodPost,
			body:          []byte("{"),
			expectStatus:  http.StatusBadRequest,
			errorContains: "failed to unmarshal placement JSON",
		},
		{
			name:          "GET incomplete path",
			method:        http.MethodGet,
			pathSuffix:    "placement-only",
			expectStatus:  http.StatusBadRequest,
			errorContains: "namespace and name required",
		},
		{
			name:          "GET placement not found",
			method:        http.MethodGet,
			pathSuffix:    placementNamespace + "/does-not-exist",
			expectStatus:  http.StatusNotFound,
			errorContains: "does-not-exist",
		},
		{
			name:          "DELETE method not allowed",
			method:        http.MethodDelete,
			pathSuffix:    placementNamespace + "/" + placementName,
			expectStatus:  http.StatusMethodNotAllowed,
			errorContains: "method DELETE not allowed",
		},
		{
			name:          "PUT method not allowed",
			method:        http.MethodPut,
			pathSuffix:    placementNamespace + "/" + placementName,
			expectStatus:  http.StatusMethodNotAllowed,
			errorContains: "method PUT not allowed",
		},
		{
			name:          "PATCH method not allowed",
			method:        http.MethodPatch,
			pathSuffix:    placementNamespace + "/" + placementName,
			expectStatus:  http.StatusMethodNotAllowed,
			errorContains: "method PATCH not allowed",
		},
		{
			name:       "GET placement lister failure",
			initObjs:   validObjs,
			method:     http.MethodGet,
			pathSuffix: placementNamespace + "/" + placementName,
			mutate: func(d *Debugger) {
				d.placementLister = &fakePlacementLister{getErr: fmt.Errorf("lister unavailable")}
			},
			expectStatus:  http.StatusInternalServerError,
			errorContains: "lister unavailable",
		},
		{
			name:       "GET clusterset binding list failure",
			initObjs:   validObjs,
			method:     http.MethodGet,
			pathSuffix: placementNamespace + "/" + placementName,
			mutate: func(d *Debugger) {
				d.clusterSetBindingLister = &fakeClusterSetBindingLister{listErr: fmt.Errorf("binding lister unavailable")}
			},
			expectStatus:  http.StatusInternalServerError,
			errorContains: "binding lister unavailable",
		},
		{
			name:       "GET managed cluster list failure",
			initObjs:   validObjs,
			method:     http.MethodGet,
			pathSuffix: placementNamespace + "/" + placementName,
			mutate: func(d *Debugger) {
				d.clusterLister = &fakeManagedClusterLister{listErr: fmt.Errorf("cluster lister unavailable")}
			},
			expectStatus:  http.StatusInternalServerError,
			errorContains: "cluster lister unavailable",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, closeServer := newAuthenticatedDebuggerServer(t, c.initObjs, c.mutate)
			defer closeServer()

			method := c.method
			if method == "" {
				method = http.MethodGet
			}

			var body io.Reader
			if len(c.body) > 0 {
				body = bytes.NewBuffer(c.body)
			}

			req, err := http.NewRequest(method, fmt.Sprintf("%s%s%s", server.URL, DebugPath, c.pathSuffix), body)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer res.Body.Close()

			assertErrorResponse(t, res, c.expectStatus, c.errorContains)
		})
	}
}

func TestDebuggerGETMethod(t *testing.T) {
	placementNamespace := "test"

	placementName := "test"

	cases := []struct {
		name              string
		initObjs          []runtime.Object
		filterResults     []scheduling.FilterResult
		prioritizeResults []scheduling.PrioritizerResult
		prioritizerScore  scheduling.PrioritizerScore
		key               string
	}{
		{
			name: "A valid placement",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("clusterset", "test-set").Build(),
				testinghelpers.NewClusterSet("test-set").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"clusterset": "test-set"},
					},
				}).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "test-set"),
			},
			filterResults:     []scheduling.FilterResult{{Name: "filter1", FilteredClusters: []string{"cluster1", "cluster2"}}},
			prioritizeResults: []scheduling.PrioritizerResult{{Name: "prioritize1", Scores: map[string]int64{"cluster1": 0, "cluster2": 100}}},
			prioritizerScore:  scheduling.PrioritizerScore{"cluster1": 0, "cluster2": 100},
			key:               placementNamespace + "/" + placementName,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)
			kubeClient := kubefake.NewClientset()

			// Mock SAR for authorization
			kubeClient.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
				createAction := action.(k8stesting.CreateAction)
				sar := createAction.GetObject().(*authorizationv1.SubjectAccessReview)
				sar.Status.Allowed = true
				return true, sar, nil
			})

			s := &testScheduler{result: &testResult{filterResults: c.filterResults, prioritizeResults: c.prioritizeResults, prioritizerScore: c.prioritizerScore}}
			debugger := NewDebugger(
				s,
				kubeClient,
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)

			// Wrap handler to inject user context (simulating GenericAPIServer authentication)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				userInfo := &user.DefaultInfo{
					Name:   "test-user",
					Groups: []string{"system:authenticated"},
				}
				ctx := request.WithUser(r.Context(), userInfo)
				r = r.WithContext(ctx)
				debugger.Handler(w, r)
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s", server.URL, DebugPath, c.key), nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("Expect no error but get %v", err)
			}
			defer res.Body.Close()

			responseBody, err := io.ReadAll(res.Body)
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

			// Verify aggregatedScores are sorted
			expectedScores := []ClusterScore{
				{ClusterName: "cluster2", Score: 100},
				{ClusterName: "cluster1", Score: 0},
			}
			if !reflect.DeepEqual(result.AggregatedScores, expectedScores) {
				t.Errorf("Expect aggregatedScores to be: %v, but got: %v", expectedScores, result.AggregatedScores)
			}
		})
	}
}

func TestDebuggerPOSTMethod(t *testing.T) {
	placementNamespace := "test-ns"
	placementName := "test-placement"

	initObjs := []runtime.Object{
		testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set").Build(),
		testinghelpers.NewManagedCluster("cluster2").WithLabel("clusterset", "test-set").Build(),
		testinghelpers.NewClusterSet("test-set").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
			SelectorType: clusterapiv1beta2.LabelSelector,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"clusterset": "test-set"},
			},
		}).Build(),
		testinghelpers.NewClusterSetBinding(placementNamespace, "test-set"),
	}

	clusterClient := clusterfake.NewSimpleClientset(initObjs...)
	clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, initObjs...)
	kubeClient := kubefake.NewClientset()

	// Mock SAR for authorization
	kubeClient.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		sar := createAction.GetObject().(*authorizationv1.SubjectAccessReview)
		sar.Status.Allowed = true
		return true, sar, nil
	})

	scheduler := &testScheduler{
		result: &testResult{
			filterResults:     []scheduling.FilterResult{{Name: "filter1", FilteredClusters: []string{"cluster1", "cluster2"}}},
			prioritizeResults: []scheduling.PrioritizerResult{{Name: "prioritize1", Scores: map[string]int64{"cluster1": 100, "cluster2": 50}}},
			prioritizerScore:  scheduling.PrioritizerScore{"cluster1": 100, "cluster2": 50},
		},
	}

	debugger := NewDebugger(
		scheduler,
		kubeClient,
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1().ManagedClusters(),
		clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
		clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
	)

	// Wrap handler to inject user context (simulating GenericAPIServer authentication)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userInfo := &user.DefaultInfo{
			Name:   "test-user",
			Groups: []string{"system:authenticated"},
		}
		ctx := request.WithUser(r.Context(), userInfo)
		r = r.WithContext(ctx)
		debugger.Handler(w, r)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Create placement JSON
	placement := &clusterapiv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      placementName,
			Namespace: placementNamespace,
		},
		Spec: clusterapiv1beta1.PlacementSpec{
			ClusterSets: []string{"test-set"},
		},
	}
	placementJSON, _ := json.Marshal(placement)

	req, err := http.NewRequest("POST", server.URL+DebugPath, bytes.NewBuffer(placementJSON))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer res.Body.Close()

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	result := &DebugResult{}
	if err := json.Unmarshal(responseBody, result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.Error != "" {
		t.Errorf("Expected no error but got: %s", result.Error)
	}

	if result.Placement == nil {
		t.Errorf("Expected placement in result")
	}

	if result.Placement.Name != placementName {
		t.Errorf("Expected placement name %s, got %s", placementName, result.Placement.Name)
	}

	if result.Placement.Namespace != placementNamespace {
		t.Errorf("Expected placement namespace %s, got %s", placementNamespace, result.Placement.Namespace)
	}

	if len(result.FilterResults) != 1 {
		t.Errorf("Expected 1 filter result, got %d", len(result.FilterResults))
	}

	// Verify aggregatedScores are sorted
	expectedScores := []ClusterScore{
		{ClusterName: "cluster1", Score: 100},
		{ClusterName: "cluster2", Score: 50},
	}
	if !reflect.DeepEqual(result.AggregatedScores, expectedScores) {
		t.Errorf("Expect aggregatedScores to be: %v, but got: %v", expectedScores, result.AggregatedScores)
	}
}

type testSchedulerWithCapture struct {
	result           *testResult
	capturedClusters []*clusterapiv1.ManagedCluster
}

func (s *testSchedulerWithCapture) Schedule(ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (scheduling.ScheduleResult, *framework.Status) {
	s.capturedClusters = clusters
	return s.result, nil
}

func TestDebuggerWithClusterSetValidation(t *testing.T) {
	placementNamespace := "test"
	placementName := "test"

	cases := []struct {
		name             string
		initObjs         []runtime.Object
		expectedClusters int
		expectError      bool
		errorContains    string
	}{
		{
			name: "No ClusterSetBinding - should return empty clusters",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("clusterset", "test-set").Build(),
				testinghelpers.NewClusterSet("test-set").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"clusterset": "test-set"},
					},
				}).Build(),
				// No ClusterSetBinding
			},
			expectedClusters: 0,
		},
		{
			name: "Placement specifies ClusterSets - should filter clusters",
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).WithClusterSets("test-set-1").Build(),
				testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set-1").Build(),
				testinghelpers.NewManagedCluster("cluster2").WithLabel("clusterset", "test-set-2").Build(),
				testinghelpers.NewClusterSet("test-set-1").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"clusterset": "test-set-1"},
					},
				}).Build(),
				testinghelpers.NewClusterSet("test-set-2").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
					SelectorType: clusterapiv1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"clusterset": "test-set-2"},
					},
				}).Build(),
				testinghelpers.NewClusterSetBinding(placementNamespace, "test-set-1"),
				testinghelpers.NewClusterSetBinding(placementNamespace, "test-set-2"),
			},
			expectedClusters: 1, // Only cluster1 from test-set-1
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, c.initObjs...)
			kubeClient := kubefake.NewClientset()

			// Mock SAR for authorization
			kubeClient.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
				createAction := action.(k8stesting.CreateAction)
				sar := createAction.GetObject().(*authorizationv1.SubjectAccessReview)
				sar.Status.Allowed = true
				return true, sar, nil
			})

			// Track which clusters were passed to scheduler
			scheduler := &testSchedulerWithCapture{
				result: &testResult{
					filterResults:     []scheduling.FilterResult{},
					prioritizeResults: []scheduling.PrioritizerResult{},
				},
			}

			debugger := NewDebugger(
				scheduler,
				kubeClient,
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)

			// Wrap handler to inject user context (simulating GenericAPIServer authentication)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				userInfo := &user.DefaultInfo{
					Name:   "test-user",
					Groups: []string{"system:authenticated"},
				}
				ctx := request.WithUser(r.Context(), userInfo)
				r = r.WithContext(ctx)
				debugger.Handler(w, r)
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s", server.URL, DebugPath, placementNamespace+"/"+placementName), nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer res.Body.Close()

			responseBody, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}
			result := &DebugResult{}
			if err := json.Unmarshal(responseBody, result); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if c.expectError {
				if result.Error == "" {
					t.Errorf("Expected error but got none")
				}
				if c.errorContains != "" && !strings.Contains(result.Error, c.errorContains) {
					t.Errorf("Expected error to contain %q, but got: %s", c.errorContains, result.Error)
				}
			} else {
				if result.Error != "" {
					t.Errorf("Expected no error but got: %s", result.Error)
				}
				if len(scheduler.capturedClusters) != c.expectedClusters {
					t.Errorf("Expected %d clusters, got %d", c.expectedClusters, len(scheduler.capturedClusters))
				}
			}
		})
	}
}

func TestDebuggerPermissionCheck(t *testing.T) {
	const (
		placementNamespace = "test-ns"
		placementName      = "test-placement"
	)

	// Default user info (can be overridden in test cases)
	defaultUserInfo := &user.DefaultInfo{
		Name:   "test-user",
		Groups: []string{"system:authenticated"},
	}

	cases := []struct {
		name             string
		method           string
		body             []byte
		injectUser       bool
		userInfo         *user.DefaultInfo
		sarAllowed       bool
		sarError         error
		expectError      bool
		expectStatusCode int
		errorContains    string
		verifySARRequest func(t *testing.T, sar *authorizationv1.SubjectAccessReview)
	}{
		{
			name:             "No user in context - should reject",
			injectUser:       false,
			expectError:      true,
			expectStatusCode: http.StatusUnauthorized,
			errorContains:    "user information not found in request context",
		},
		{
			name:             "User with permission - should allow",
			injectUser:       true,
			sarAllowed:       true,
			expectError:      false,
			expectStatusCode: http.StatusOK,
		},
		{
			name:             "GET user without permission - should reject with get verb",
			method:           http.MethodGet,
			injectUser:       true,
			sarAllowed:       false,
			expectError:      true,
			expectStatusCode: http.StatusForbidden,
			errorContains:    "does not have permission to get placements",
			verifySARRequest: func(t *testing.T, sar *authorizationv1.SubjectAccessReview) {
				if sar.Spec.ResourceAttributes.Verb != "get" {
					t.Errorf("Expected SAR verb get, got %s", sar.Spec.ResourceAttributes.Verb)
				}
			},
		},
		{
			name:             "POST user without permission - should reject with create verb",
			method:           http.MethodPost,
			body:             []byte(`{"apiVersion":"cluster.open-cluster-management.io/v1beta1","kind":"Placement","metadata":{"name":"test-placement","namespace":"test-ns"},"spec":{"numberOfClusters":1}}`),
			injectUser:       true,
			sarAllowed:       false,
			expectError:      true,
			expectStatusCode: http.StatusForbidden,
			errorContains:    "does not have permission to create placements",
			verifySARRequest: func(t *testing.T, sar *authorizationv1.SubjectAccessReview) {
				if sar.Spec.ResourceAttributes.Verb != "create" {
					t.Errorf("Expected SAR verb create, got %s", sar.Spec.ResourceAttributes.Verb)
				}
			},
		},
		{
			name:             "SAR API call fails",
			injectUser:       true,
			sarError:         fmt.Errorf("API server error"),
			expectError:      true,
			expectStatusCode: http.StatusInternalServerError,
			errorContains:    "failed to check permissions",
		},
		{
			name:       "User with Extra fields - should pass all fields to SAR",
			injectUser: true,
			userInfo: &user.DefaultInfo{
				Name:   "test-user",
				Groups: []string{"system:authenticated", "developers"},
				Extra: map[string][]string{
					"authentication.kubernetes.io/scopes": {"read", "write"},
					"custom.example.com/department":       {"engineering"},
				},
			},
			sarAllowed:       true,
			expectError:      false,
			expectStatusCode: http.StatusOK,
			verifySARRequest: func(t *testing.T, sar *authorizationv1.SubjectAccessReview) {
				expected := map[string]authorizationv1.ExtraValue{
					"authentication.kubernetes.io/scopes": {"read", "write"},
					"custom.example.com/department":       {"engineering"},
				}
				if !reflect.DeepEqual(sar.Spec.Extra, expected) {
					t.Errorf("Expected Extra %v, got %v", expected, sar.Spec.Extra)
				}
			},
		},
	}

	// Setup common test objects (reused across all test cases)
	initObjs := []runtime.Object{
		testinghelpers.NewPlacement(placementNamespace, placementName).Build(),
		testinghelpers.NewManagedCluster("cluster1").WithLabel("clusterset", "test-set").Build(),
		testinghelpers.NewClusterSet("test-set").WithClusterSelector(clusterapiv1beta2.ManagedClusterSelector{
			SelectorType: clusterapiv1beta2.LabelSelector,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"clusterset": "test-set"},
			},
		}).Build(),
		testinghelpers.NewClusterSetBinding(placementNamespace, "test-set"),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(initObjs...)
			clusterInformerFactory := testinghelpers.NewClusterInformerFactory(clusterClient, initObjs...)
			kubeClient := kubefake.NewClientset()

			// Mock SAR
			kubeClient.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if c.sarError != nil {
					return true, nil, c.sarError
				}
				sar := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)

				if c.verifySARRequest != nil {
					c.verifySARRequest(t, sar)
				}

				sar.Status.Allowed = c.sarAllowed
				if !c.sarAllowed {
					sar.Status.Reason = "user lacks permission"
				}
				return true, sar, nil
			})

			debugger := NewDebugger(
				&testScheduler{result: &testResult{}},
				kubeClient,
				clusterInformerFactory.Cluster().V1beta1().Placements(),
				clusterInformerFactory.Cluster().V1().ManagedClusters(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets(),
				clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings(),
			)

			// Setup HTTP handler
			var handler http.Handler
			if c.injectUser {
				userInfo := c.userInfo
				if userInfo == nil {
					userInfo = defaultUserInfo
				}
				handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx := request.WithUser(r.Context(), userInfo)
					debugger.Handler(w, r.WithContext(ctx))
				})
			} else {
				handler = http.HandlerFunc(debugger.Handler)
			}

			server := httptest.NewServer(handler)
			defer server.Close()

			// Send request
			method := c.method
			if method == "" {
				method = http.MethodGet
			}

			var url string
			var body io.Reader
			if method == http.MethodPost {
				url = fmt.Sprintf("%s%s", server.URL, DebugPath)
				if len(c.body) > 0 {
					body = bytes.NewBuffer(c.body)
				}
			} else {
				url = fmt.Sprintf("%s%s%s/%s", server.URL, DebugPath, placementNamespace, placementName)
			}

			req, err := http.NewRequest(method, url, body)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer res.Body.Close()

			// Parse response
			var result DebugResult
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Verify result
			if res.StatusCode != c.expectStatusCode {
				t.Errorf("Expected HTTP status %d, got %d", c.expectStatusCode, res.StatusCode)
			}
			if c.expectError {
				if result.Error == "" {
					t.Errorf("Expected error but got none")
				}
				if c.errorContains != "" && !strings.Contains(result.Error, c.errorContains) {
					t.Errorf("Expected error to contain %q, but got: %s", c.errorContains, result.Error)
				}
			} else if result.Error != "" {
				t.Errorf("Expected no error but got: %s", result.Error)
			}
		})
	}
}
