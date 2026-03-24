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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
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
		injectUser       bool
		userInfo         *user.DefaultInfo
		sarAllowed       bool
		sarError         error
		expectError      bool
		errorContains    string
		verifySARRequest func(t *testing.T, sar *authorizationv1.SubjectAccessReview)
	}{
		{
			name:          "No user in context - should reject",
			injectUser:    false,
			expectError:   true,
			errorContains: "user information not found in request context",
		},
		{
			name:        "User with permission - should allow",
			injectUser:  true,
			sarAllowed:  true,
			expectError: false,
		},
		{
			name:          "User without permission - should reject",
			injectUser:    true,
			sarAllowed:    false,
			expectError:   true,
			errorContains: "does not have permission",
		},
		{
			name:          "SAR API call fails",
			injectUser:    true,
			sarError:      fmt.Errorf("API server error"),
			expectError:   true,
			errorContains: "failed to check permissions",
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
			sarAllowed:  true,
			expectError: false,
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
			url := fmt.Sprintf("%s%s%s/%s", server.URL, DebugPath, placementNamespace, placementName)
			res, err := http.Get(url)
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
