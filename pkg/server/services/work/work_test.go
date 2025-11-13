package work

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	workfake "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestGet(t *testing.T) {
	cases := []struct {
		name          string
		works         []runtime.Object
		resourceID    string
		expectedError bool
	}{
		{
			name:          "work not found",
			works:         []runtime.Object{},
			resourceID:    "test-namespace/test-work",
			expectedError: true,
		},
		{
			name:       "get work",
			resourceID: "test-namespace/test-work",
			works: []runtime.Object{&workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-work",
					Namespace:       "test-namespace",
					ResourceVersion: "1",
				},
			}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			workClient := workfake.NewSimpleClientset(c.works...)
			workInformers := workinformers.NewSharedInformerFactory(workClient, 10*time.Minute)
			workInformer := workInformers.Work().V1().ManifestWorks()
			for _, obj := range c.works {
				if err := workInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewWorkService(workClient, workInformer)
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
		name          string
		works         []runtime.Object
		clusterName   string
		expectedWorks int
	}{
		{
			name:          "no works",
			works:         []runtime.Object{},
			clusterName:   "test-cluster",
			expectedWorks: 0,
		},
		{
			name: "list works",
			works: []runtime.Object{
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-work1",
						Namespace:       "test-cluster1",
						ResourceVersion: "1",
					},
				},
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-work2",
						Namespace:       "test-cluster2",
						ResourceVersion: "1",
					},
				},
			},
			clusterName:   "test-cluster1",
			expectedWorks: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			workClient := workfake.NewSimpleClientset(c.works...)
			workInformers := workinformers.NewSharedInformerFactory(workClient, 10*time.Minute)
			workInformer := workInformers.Work().V1().ManifestWorks()
			for _, obj := range c.works {
				if err := workInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewWorkService(workClient, workInformer)
			evts, err := service.List(types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedWorks {
				t.Errorf("expected %d works, got %d", c.expectedWorks, len(evts))
			}
		})
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name            string
		works           []runtime.Object
		workEvt         *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name:  "invalid event type",
			works: []runtime.Object{},
			workEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "invalid action",
			works: []runtime.Object{
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						UID:             "test-cluster/test-work",
						Name:            "test-work",
						Namespace:       "test-cluster",
						ResourceVersion: "1",
					},
				},
			},
			workEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: payload.ManifestBundleEventDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.CreateRequestAction,
				}).WithResourceVersion(1).
					WithClusterName("test-cluster").
					WithResourceID("test-cluster/test-work").
					WithStatusUpdateSequenceID("1").NewEvent()
				manifestBundleStatus := &payload.ManifestBundleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Test",
							Status: metav1.ConditionTrue,
						},
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, manifestBundleStatus)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "update work status",
			works: []runtime.Object{
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						UID:        "test-cluster/test-work",
						Name:       "test-work",
						Namespace:  "test-cluster",
						Generation: 1,
					},
				},
			},
			workEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: payload.ManifestBundleEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.UpdateRequestAction,
				}).WithResourceVersion(1).
					WithClusterName("test-cluster").
					WithResourceID("test-cluster/test-work").
					WithStatusUpdateSequenceID("1").NewEvent()
				manifestBundleStatus := &payload.ManifestBundleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Test",
							Status: metav1.ConditionTrue,
						},
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, manifestBundleStatus)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch", "patch")
				if len(actions[0].GetSubresource()) != 0 {
					// add finalizer
					t.Errorf("unexpected subresource %s", actions[0].GetSubresource())
				}
				if actions[1].GetSubresource() != "status" {
					// update status
					t.Errorf("expected subresource %s, got %s", "status", actions[1].GetSubresource())
				}
			},
		},
		{
			name: "update work status (resource version mismatch)",
			works: []runtime.Object{
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						UID:        "test-cluster/test-work",
						Name:       "test-work",
						Namespace:  "test-cluster",
						Generation: 2,
					},
				},
			},
			workEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: payload.ManifestBundleEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.UpdateRequestAction,
				}).WithResourceVersion(1).
					WithClusterName("test-cluster").
					WithResourceID("test-cluster/test-work").
					WithStatusUpdateSequenceID("1").NewEvent()
				manifestBundleStatus := &payload.ManifestBundleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Test",
							Status: metav1.ConditionTrue,
						},
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, manifestBundleStatus)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected 0 actions, got %d", len(actions))
				}
			},
		},
		{
			name: "update work status (deleted)",
			works: []runtime.Object{
				&workv1.ManifestWork{
					ObjectMeta: metav1.ObjectMeta{
						UID:        "test-cluster/test-work",
						Name:       "test-work",
						Namespace:  "test-cluster",
						Generation: 1,
						Finalizers: []string{workv1.ManifestWorkFinalizer},
					},
				},
			},
			workEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: payload.ManifestBundleEventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.UpdateRequestAction,
				}).WithResourceVersion(1).
					WithClusterName("test-cluster").
					WithResourceID("test-cluster/test-work").
					WithStatusUpdateSequenceID("1").NewEvent()
				manifestBundleStatus := &payload.ManifestBundleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   common.ResourceDeleted,
							Status: metav1.ConditionTrue,
						},
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, manifestBundleStatus)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				if len(actions[0].GetSubresource()) != 0 {
					// remove finalizer
					t.Errorf("unexpected subresource %s", actions[0].GetSubresource())
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			workClient := workfake.NewSimpleClientset(c.works...)
			workInformers := workinformers.NewSharedInformerFactory(workClient, 10*time.Minute)
			workInformer := workInformers.Work().V1().ManifestWorks()
			for _, obj := range c.works {
				if err := workInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewWorkService(workClient, workInformer)
			err := service.HandleStatusUpdate(context.Background(), c.workEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, workClient.Actions())
		})
	}
}

func TestEventHandlerFuncs(t *testing.T) {
	handler := &workHandler{}
	service := &WorkService{}
	eventHandlerFuncs := service.EventHandlerFuncs(context.Background(), handler)

	work := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-work",
			Namespace:  "test-namespace",
			Generation: 1,
		},
	}
	eventHandlerFuncs.AddFunc(work)
	if !handler.onCreateCalled {
		t.Errorf("onCreate not called")
	}

	eventHandlerFuncs.UpdateFunc(work, &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-work",
			Namespace:  "test-namespace",
			Generation: 2,
		},
	})
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}

	eventHandlerFuncs.UpdateFunc(work, &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-work",
			Namespace:  "test-namespace",
			Generation: 1,
			Labels:     map[string]string{"test": "test"},
		},
	})
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}

	eventHandlerFuncs.UpdateFunc(work, &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-work",
			Namespace:   "test-namespace",
			Generation:  1,
			Annotations: map[string]string{"test": "test"},
		},
	})
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}

	time := metav1.Now()
	eventHandlerFuncs.UpdateFunc(work, &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-work",
			Namespace:         "test-namespace",
			DeletionTimestamp: &time,
			Generation:        1,
		},
	})
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}

	eventHandlerFuncs.DeleteFunc(work)
	if !handler.onDeleteCalled {
		t.Errorf("onDelete not called")
	}
}

func TestHandleOnCreateFunc(t *testing.T) {
	cases := []struct {
		name              string
		obj               interface{}
		expectedCallCount int
		expectError       bool
	}{
		{
			name: "successful create",
			obj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace"},
			},
			expectedCallCount: 1,
		},
		{
			name:              "invalid object type",
			obj:               "invalid-object",
			expectedCallCount: 0,
			expectError:       true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler := &workHandler{}
			createFunc := handleOnCreateFunc(context.Background(), handler)
			createFunc(c.obj)
			if handler.onCreateCallCount != c.expectedCallCount {
				t.Errorf("expected %d onCreate calls, got %d", c.expectedCallCount, handler.onCreateCallCount)
			}
		})
	}
}

func TestHandleOnUpdateFunc(t *testing.T) {
	cases := []struct {
		name              string
		oldObj            interface{}
		newObj            interface{}
		expectedCallCount int
		description       string
	}{
		{
			name: "generation increased",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 2},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when generation increases",
		},
		{
			name: "generation same - no update",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			expectedCallCount: 0,
			description:       "should not call OnUpdate when generation stays same and no label/annotation changes",
		},
		{
			name: "generation decreased - no update",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 2},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			expectedCallCount: 0,
			description:       "should not call OnUpdate when generation decreases and no label/annotation changes",
		},
		{
			name: "deletion timestamp set",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-work",
					Namespace:         "test-namespace",
					Generation:        1,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when deletion timestamp is set",
		},
		{
			name: "labels changed",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
					Labels:     map[string]string{"key1": "value1"},
				},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
					Labels:     map[string]string{"key1": "value2"},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when labels change",
		},
		{
			name: "labels added",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
				},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
					Labels:     map[string]string{"key1": "value1"},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when labels are added",
		},
		{
			name: "annotations changed",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-work",
					Namespace:   "test-namespace",
					Generation:  1,
					Annotations: map[string]string{"key1": "value1"},
				},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-work",
					Namespace:   "test-namespace",
					Generation:  1,
					Annotations: map[string]string{"key1": "value2"},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when annotations change",
		},
		{
			name: "annotations added",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
				},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-work",
					Namespace:   "test-namespace",
					Generation:  1,
					Annotations: map[string]string{"key1": "value1"},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when annotations are added",
		},
		{
			name: "labels and generation changed",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 1,
					Labels:     map[string]string{"key1": "value1"},
				},
			},
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-work",
					Namespace:  "test-namespace",
					Generation: 2,
					Labels:     map[string]string{"key1": "value2"},
				},
			},
			expectedCallCount: 1,
			description:       "should call OnUpdate when both labels and generation change",
		},
		{
			name:   "invalid old object type",
			oldObj: "invalid-object",
			newObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			expectedCallCount: 0,
			description:       "should not call OnUpdate when old object is invalid",
		},
		{
			name: "invalid new object type",
			oldObj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace", Generation: 1},
			},
			newObj:            "invalid-object",
			expectedCallCount: 0,
			description:       "should not call OnUpdate when new object is invalid",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler := &workHandler{}
			updateFunc := handleOnUpdateFunc(context.Background(), handler)
			updateFunc(c.oldObj, c.newObj)
			if handler.onUpdateCallCount != c.expectedCallCount {
				t.Errorf("%s: expected %d OnUpdate calls, got %d", c.description, c.expectedCallCount, handler.onUpdateCallCount)
			}
		})
	}
}

func TestHandleOnDeleteFunc(t *testing.T) {
	cases := []struct {
		name              string
		obj               interface{}
		expectedCallCount int
		expectError       bool
	}{
		{
			name: "successful delete",
			obj: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{Name: "test-work", Namespace: "test-namespace"},
			},
			expectedCallCount: 1,
		},
		{
			name:              "invalid object type",
			obj:               "invalid-object",
			expectedCallCount: 0,
			expectError:       true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler := &workHandler{}
			deleteFunc := handleOnDeleteFunc(context.Background(), handler)
			deleteFunc(c.obj)
			if handler.onDeleteCallCount != c.expectedCallCount {
				t.Errorf("expected %d onDelete calls, got %d", c.expectedCallCount, handler.onDeleteCallCount)
			}
		})
	}
}

type workHandler struct {
	onCreateCalled    bool
	onUpdateCalled    bool
	onDeleteCalled    bool
	onCreateCallCount int
	onUpdateCallCount int
	onDeleteCallCount int
}

func (m *workHandler) OnCreate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != payload.ManifestBundleEventDataType {
		return fmt.Errorf("expected %v, got %v", payload.ManifestBundleEventDataType, t)
	}
	if resourceID != "test-namespace/test-work" {
		return fmt.Errorf("expected %v, got %v", "test-namespace/test-work", resourceID)
	}
	m.onCreateCalled = true
	m.onCreateCallCount++
	return nil
}

func (m *workHandler) OnUpdate(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != payload.ManifestBundleEventDataType {
		return fmt.Errorf("expected %v, got %v", payload.ManifestBundleEventDataType, t)
	}
	if resourceID != "test-namespace/test-work" {
		return fmt.Errorf("expected %v, got %v", "test-namespace/test-work", resourceID)
	}
	m.onUpdateCalled = true
	m.onUpdateCallCount++
	return nil
}

func (m *workHandler) OnDelete(ctx context.Context, t types.CloudEventsDataType, resourceID string) error {
	if t != payload.ManifestBundleEventDataType {
		return fmt.Errorf("expected %v, got %v", payload.ManifestBundleEventDataType, t)
	}
	if resourceID != "test-namespace/test-work" {
		return fmt.Errorf("expected %v, got %v", "test-namespace/test-work", resourceID)
	}
	m.onDeleteCalled = true
	m.onDeleteCallCount++
	return nil
}
