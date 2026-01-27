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
	cases := []struct {
		name            string
		clusters        []runtime.Object
		clusterEvt      *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
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
				}).NewEvent()
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
				}).NewEvent()
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
				}).NewEvent()
				cluster := &clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
				}
				evt.SetData(cloudevents.ApplicationJSON, cluster)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				if len(actions[0].GetSubresource()) != 0 {
					t.Errorf("unexpected subresource %s", actions[0].GetSubresource())
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
				}).NewEvent()
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
