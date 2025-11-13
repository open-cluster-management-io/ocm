package lease

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestGet(t *testing.T) {
	cases := []struct {
		name          string
		leases        []runtime.Object
		resourceID    string
		expectedError bool
	}{
		{
			name:          "lease not found",
			leases:        []runtime.Object{},
			resourceID:    "test-cluster",
			expectedError: true,
		},
		{
			name:       "lease found",
			resourceID: "test-lease-namespace/test-lease",
			leases: []runtime.Object{&coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: "test-lease", Namespace: "test-lease-namespace"},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.leases...)
			kubeInformers := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
			kubeInformer := kubeInformers.Coordination().V1().Leases()
			for _, obj := range c.leases {
				if err := kubeInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewLeaseService(kubeClient, kubeInformer)
			_, err := service.Get(context.Background(), c.resourceID)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestList(t *testing.T) {
	cases := []struct {
		name             string
		leases           []runtime.Object
		clusterName      string
		expectedClusters int
	}{
		{
			name:             "no leases",
			leases:           []runtime.Object{},
			clusterName:      "test-cluster",
			expectedClusters: 0,
		},
		{
			name: "list leases",
			leases: []runtime.Object{
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lease1",
						Namespace: "test-cluster1",
						Labels: map[string]string{
							"open-cluster-management.io/cluster-name": "test-cluster1",
						},
					},
				},
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: "test-lease2", Namespace: "test-cluster2"},
				},
			},
			clusterName:      "test-cluster1",
			expectedClusters: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.leases...)
			kubeInformers := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
			kubeInformer := kubeInformers.Coordination().V1().Leases()
			for _, obj := range c.leases {
				if err := kubeInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewLeaseService(kubeClient, kubeInformer)
			evts, err := service.List(types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedClusters {
				t.Errorf("expected %d leases, got %d", c.expectedClusters, len(evts))
			}
		})
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name            string
		leases          []runtime.Object
		leaseEvt        *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name:   "invalid event type",
			leases: []runtime.Object{},
			leaseEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:   "invalid action",
			leases: []runtime.Object{},
			leaseEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: leasece.LeaseEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.DeleteRequestAction,
				}).NewEvent()
				lease := &coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: "test-lease", Namespace: "test-lease-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, lease)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "update lease",
			leases: []runtime.Object{
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: "test-lease", Namespace: "test-lease-namespace"},
				},
			},
			leaseEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: leasece.LeaseEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.UpdateRequestAction,
				}).NewEvent()
				lease := &coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: "test-lease", Namespace: "test-lease-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, lease)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				if len(actions[0].GetSubresource()) != 0 {
					t.Errorf("unexpected subresource %s", actions[0].GetSubresource())
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.leases...)
			kubeInformers := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
			kubeInformer := kubeInformers.Coordination().V1().Leases()

			service := NewLeaseService(kubeClient, kubeInformer)
			err := service.HandleStatusUpdate(context.Background(), c.leaseEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, kubeClient.Actions())
		})
	}
}

func TestEventHandlerFuncs(t *testing.T) {
	handler := &leaseHandler{}
	service := &LeaseService{}
	eventHandlerFuncs := service.EventHandlerFuncs(context.Background(), handler)

	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "test-lease", Namespace: "test-lease-namespace"},
	}
	eventHandlerFuncs.AddFunc(lease)
	if !handler.onCreateCalled {
		t.Errorf("onCreate not called")
	}

	eventHandlerFuncs.UpdateFunc(nil, lease)
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}
}

type leaseHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
}

func (m *leaseHandler) OnCreate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != leasece.LeaseEventDataType {
		return fmt.Errorf("expected %v, got %v", leasece.LeaseEventDataType, t)
	}
	m.onCreateCalled = true
	return nil
}

func (m *leaseHandler) OnUpdate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != leasece.LeaseEventDataType {
		return fmt.Errorf("expected %v, got %v", leasece.LeaseEventDataType, t)
	}
	m.onUpdateCalled = true
	return nil
}

func (m *leaseHandler) OnDelete(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	return nil
}
