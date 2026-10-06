package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestList(t *testing.T) {
	cases := []struct {
		name             string
		clusters         []runtime.Object
		clusterName      string
		expectedClusters int
	}{
		{
			name:             "no clusters",
			clusters:         []runtime.Object{},
			clusterName:      "test-cluster",
			expectedClusters: 0,
		},
		{
			name: "list clusters",
			clusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster1"},
				},
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster2"},
				},
			},
			clusterName:      "test-cluster1",
			expectedClusters: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
			clusterInformer := clusterInformers.Cluster().V1().ManagedClusters()
			for _, obj := range c.clusters {
				if err := clusterInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewClusterService(clusterClient, clusterInformer)
			evts, err := service.List(context.Background(), types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedClusters {
				t.Errorf("expected %d clusters, got %d", c.expectedClusters, len(evts))
			}
		})
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	newClusterEvent := func(eventType types.CloudEventsType, clusterName string, cluster *clusterv1.ManagedCluster) *cloudevents.Event {
		evt := types.NewEventBuilder("test", eventType).WithClusterName(clusterName).NewEvent()
		if err := evt.SetData(cloudevents.ApplicationJSON, cluster); err != nil {
			t.Fatal(err)
		}
		return &evt
	}
	clusterCreate := types.CloudEventsType{
		CloudEventsDataType: clusterce.ManagedClusterEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}
	clusterUpdate := types.CloudEventsType{
		CloudEventsDataType: clusterce.ManagedClusterEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.UpdateRequestAction,
	}
	clusterStatusCreate := types.CloudEventsType{
		CloudEventsDataType: clusterce.ManagedClusterEventDataType,
		SubResource:         types.SubResourceStatus,
		Action:              types.CreateRequestAction,
	}
	clusterWithSet := func(clusterSet string) *clusterv1.ManagedCluster {
		return &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-cluster",
				Labels: map[string]string{clusterv1beta2.ClusterSetLabel: clusterSet, "other": "label"},
			},
		}
	}
	assertClusterSetLabel := func(t *testing.T, action clienttesting.Action, expected string, expectPresent bool) {
		t.Helper()
		obj := action.(clienttesting.CreateAction).GetObject().(*clusterv1.ManagedCluster)
		clusterSet, present := obj.Labels[clusterv1beta2.ClusterSetLabel]
		if present != expectPresent || clusterSet != expected {
			t.Errorf("expected clusterset label present=%v value=%q, got present=%v value=%q", expectPresent, expected, present, clusterSet)
		}
		if obj.Labels["other"] != "label" {
			t.Errorf("expected the other labels to be kept, got %v", obj.Labels)
		}
	}

	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterEvt      *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name:          "cluster name does not match the event cluster name",
			clusters:      []runtime.Object{},
			clusterEvt:    newClusterEvent(clusterCreate, "other-cluster", &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}),
			expectedError: true,
		},
		{
			name:          "create cluster with status subresource",
			clusters:      []runtime.Object{},
			clusterEvt:    newClusterEvent(clusterStatusCreate, "test-cluster", &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}),
			expectedError: true,
		},
		{
			name:       "create cluster drops the clusterset label",
			clusters:   []runtime.Object{},
			clusterEvt: newClusterEvent(clusterCreate, "test-cluster", clusterWithSet("spoofed")),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				assertClusterSetLabel(t, actions[0], "", false)
			},
		},
		{
			name:       "update cluster keeps the clusterset label of the existing cluster",
			clusters:   []runtime.Object{clusterWithSet("default")},
			clusterEvt: newClusterEvent(clusterUpdate, "test-cluster", clusterWithSet("spoofed")),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				assertClusterSetLabel(t, actions[1], "default", true)
			},
		},
		{
			name:     "update cluster restores the clusterset label removed by the agent",
			clusters: []runtime.Object{clusterWithSet("default")},
			clusterEvt: newClusterEvent(clusterUpdate, "test-cluster", &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Labels: map[string]string{"other": "label"}},
			}),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				assertClusterSetLabel(t, actions[1], "default", true)
			},
		},
		{
			name: "update cluster drops the clusterset label when the existing cluster has none",
			clusters: []runtime.Object{
				&clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}},
			},
			clusterEvt: newClusterEvent(clusterUpdate, "test-cluster", clusterWithSet("spoofed")),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				assertClusterSetLabel(t, actions[1], "", false)
			},
		},
		{
			name:          "update missing cluster",
			clusters:      []runtime.Object{},
			clusterEvt:    newClusterEvent(clusterUpdate, "test-cluster", &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}),
			expectedError: true,
		},
		{
			name:     "invalid event type",
			clusters: []runtime.Object{},
			clusterEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:     "invalid action",
			clusters: []runtime.Object{},
			clusterEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: clusterce.ManagedClusterEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.DeleteRequestAction,
				}).WithClusterName("test-cluster").NewEvent()
				cluster := &clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				}
				evt.SetData(cloudevents.ApplicationJSON, cluster)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:     "create cluster",
			clusters: []runtime.Object{},
			clusterEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: clusterce.ManagedClusterEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.CreateRequestAction,
				}).WithClusterName("test-cluster").NewEvent()
				cluster := &clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				}
				evt.SetData(cloudevents.ApplicationJSON, cluster)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "update cluster",
			clusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				},
			},
			clusterEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: clusterce.ManagedClusterEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.UpdateRequestAction,
				}).WithClusterName("test-cluster").NewEvent()
				cluster := &clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				}
				evt.SetData(cloudevents.ApplicationJSON, cluster)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "update")
				if len(actions[1].GetSubresource()) != 0 {
					t.Errorf("unexpected subresource %s", actions[1].GetSubresource())
				}
			},
		},
		{
			name: "update cluster status",
			clusters: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				},
			},
			clusterEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: clusterce.ManagedClusterEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.UpdateRequestAction,
				}).WithClusterName("test-cluster").NewEvent()
				cluster := &clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				}
				evt.SetData(cloudevents.ApplicationJSON, cluster)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				if actions[0].GetSubresource() != "status" {
					t.Errorf("expected subresource %s, got %s", "status", actions[0].GetSubresource())
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
			clusterInformer := clusterInformers.Cluster().V1().ManagedClusters()

			service := NewClusterService(clusterClient, clusterInformer)
			err := service.HandleStatusUpdate(context.Background(), c.clusterEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestEventHandlerFuncs(t *testing.T) {
	handler := &clusterHandler{}
	service := &ClusterService{}
	eventHandlerFuncs := service.EventHandlerFuncs(context.Background(), handler)

	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
	}
	eventHandlerFuncs.AddFunc(cluster)
	if !handler.onCreateCalled {
		t.Errorf("onCreate not called")
	}

	eventHandlerFuncs.UpdateFunc(nil, cluster)
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}
}

type clusterHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
}

func (m *clusterHandler) HandleEvent(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return err
	}

	if eventType.CloudEventsDataType != clusterce.ManagedClusterEventDataType {
		return fmt.Errorf("expected %v, got %v", clusterce.ManagedClusterEventDataType, eventType.CloudEventsDataType)
	}

	m.onCreateCalled = true
	m.onUpdateCalled = true
	return nil
}
