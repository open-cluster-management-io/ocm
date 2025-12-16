package addon

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestGet(t *testing.T) {
	cases := []struct {
		name          string
		addons        []runtime.Object
		resourceID    string
		expectedError bool
	}{
		{
			name:          "addon not found",
			addons:        []runtime.Object{},
			resourceID:    "test-namespace/test-addon",
			expectedError: true,
		},
		{
			name:       "get addon",
			resourceID: "test-namespace/test-addon",
			addons: []runtime.Object{&addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addonClient := addonfake.NewSimpleClientset(c.addons...)
			addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
			addonInformer := addonInformers.Addon().V1alpha1().ManagedClusterAddOns()
			for _, obj := range c.addons {
				if err := addonInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewAddonService(addonClient, addonInformer)
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
		name           string
		addons         []runtime.Object
		clusterName    string
		expectedAddons int
	}{
		{
			name:           "no addons",
			addons:         []runtime.Object{},
			clusterName:    "test-cluster",
			expectedAddons: 0,
		},
		{
			name: "list addons",
			addons: []runtime.Object{
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-cluster1"},
				},
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon2", Namespace: "test-cluster2"},
				},
			},
			clusterName:    "test-cluster1",
			expectedAddons: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addonClient := addonfake.NewSimpleClientset(c.addons...)
			addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
			addonInformer := addonInformers.Addon().V1alpha1().ManagedClusterAddOns()
			for _, obj := range c.addons {
				if err := addonInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewAddonService(addonClient, addonInformer)
			evts, err := service.List(types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedAddons {
				t.Errorf("expected %d addons, got %d", c.expectedAddons, len(evts))
			}
		})
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name            string
		addons          []runtime.Object
		addonEvt        *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name:   "invalid event type",
			addons: []runtime.Object{},
			addonEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name:   "invalid action",
			addons: []runtime.Object{},
			addonEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.CreateRequestAction,
				}).NewEvent()
				addon := &addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, addon)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "update addon status",
			addons: []runtime.Object{
				&addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
				},
			},
			addonEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: addonce.ManagedClusterAddOnEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.UpdateRequestAction,
				}).NewEvent()
				addon := &addonv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
				}
				evt.SetData(cloudevents.ApplicationJSON, addon)
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
			addonClient := addonfake.NewSimpleClientset(c.addons...)
			addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
			addonInformer := addonInformers.Addon().V1alpha1().ManagedClusterAddOns()

			service := NewAddonService(addonClient, addonInformer)
			err := service.HandleStatusUpdate(context.Background(), c.addonEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, addonClient.Actions())
		})
	}
}

func TestEventHandlerFuncs(t *testing.T) {
	handler := &addOnHandler{}
	service := &AddonService{}
	eventHandlerFuncs := service.EventHandlerFuncs(context.Background(), handler)

	addon := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"},
	}
	eventHandlerFuncs.AddFunc(addon)
	if !handler.onCreateCalled {
		t.Errorf("onCreate not called")
	}

	eventHandlerFuncs.UpdateFunc(nil, addon)
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}
}

type addOnHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
}

func (m *addOnHandler) OnCreate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != addonce.ManagedClusterAddOnEventDataType {
		return fmt.Errorf("expected %v, got %v", addonce.ManagedClusterAddOnEventDataType, t)
	}
	m.onCreateCalled = true
	return nil
}

func (m *addOnHandler) OnUpdate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != addonce.ManagedClusterAddOnEventDataType {
		return fmt.Errorf("expected %v, got %v", addonce.ManagedClusterAddOnEventDataType, t)
	}
	m.onUpdateCalled = true
	return nil
}

func (m *addOnHandler) OnDelete(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	return nil
}
