package tokenrequest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	sace "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/serviceaccount"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestNewTokenRequestService(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient, nil)

	tokenService, ok := service.(*TokenRequestService)
	if !ok {
		t.Errorf("expected TokenRequestService, got %T", service)
	}

	if tokenService.client == nil {
		t.Errorf("client should not be nil")
	}

	if tokenService.codec == nil {
		t.Errorf("codec should not be nil")
	}
}

func TestList(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient, nil)

	evts, err := service.List(context.Background(), types.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if evts != nil {
		t.Errorf("expected nil events, got %v", evts)
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	newTokenRequestEvent := func(subResource types.EventSubResource, clusterName, namespace, serviceAccount string) *cloudevents.Event {
		evt := types.NewEventBuilder("test", types.CloudEventsType{
			CloudEventsDataType: sace.TokenRequestDataType,
			SubResource:         subResource,
			Action:              types.CreateRequestAction,
		}).WithClusterName(clusterName).NewEvent()
		tokenRequest := &authenticationv1.TokenRequest{
			ObjectMeta: metav1.ObjectMeta{Name: serviceAccount, Namespace: namespace},
			Spec:       authenticationv1.TokenRequestSpec{Audiences: []string{"test-audience"}},
		}
		if err := evt.SetData(cloudevents.ApplicationJSON, tokenRequest); err != nil {
			t.Fatal(err)
		}
		return &evt
	}
	serviceAccount := func(name string) runtime.Object {
		return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-namespace"}}
	}
	testAddon := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "test-namespace"}}
	assertTokenCreated := func(t *testing.T, actions []clienttesting.Action) {
		testingcommon.AssertActions(t, actions, "create")
		if actions[0].GetSubresource() != "token" {
			t.Errorf("expected subresource %s, got %s", "token", actions[0].GetSubresource())
		}
	}

	cases := []struct {
		name            string
		serviceAccounts []runtime.Object
		// addons enables the addon lister based service account validation when set.
		addons            []runtime.Object
		tokenRequestEvt   *cloudevents.Event
		validateActions   func(t *testing.T, actions []clienttesting.Action)
		validateHandler   func(t *testing.T, handler *mockEventHandler)
		reactorError      error
		expectedError     bool
		expectedErrorText string
	}{
		{
			name:              "token request namespace does not match the event cluster name",
			serviceAccounts:   []runtime.Object{serviceAccount("test-sa")},
			tokenRequestEvt:   newTokenRequestEvent(types.SubResourceSpec, "other-namespace", "test-namespace", "test-sa"),
			expectedError:     true,
			expectedErrorText: "resource cluster name",
		},
		{
			name:              "create token request with status subresource",
			serviceAccounts:   []runtime.Object{serviceAccount("test-sa")},
			tokenRequestEvt:   newTokenRequestEvent(types.SubResourceStatus, "test-namespace", "test-namespace", "test-sa"),
			expectedError:     true,
			expectedErrorText: "unsupported subresource",
		},
		{
			name:            "create token request for the addon service account",
			serviceAccounts: []runtime.Object{serviceAccount("test-addon")},
			addons:          []runtime.Object{testAddon},
			tokenRequestEvt: newTokenRequestEvent(types.SubResourceSpec, "test-namespace", "test-namespace", "test-addon"),
			validateActions: assertTokenCreated,
		},
		{
			name:            "create token request for the addon agent service account",
			serviceAccounts: []runtime.Object{serviceAccount("test-addon-agent")},
			addons:          []runtime.Object{testAddon},
			tokenRequestEvt: newTokenRequestEvent(types.SubResourceSpec, "test-namespace", "test-namespace", "test-addon-agent"),
			validateActions: assertTokenCreated,
		},
		{
			name:              "create token request for a non addon service account",
			serviceAccounts:   []runtime.Object{serviceAccount("test-sa")},
			addons:            []runtime.Object{testAddon},
			tokenRequestEvt:   newTokenRequestEvent(types.SubResourceSpec, "test-namespace", "test-namespace", "test-sa"),
			expectedError:     true,
			expectedErrorText: "serviceaccount test-namespace/test-sa is not an addon agent",
		},
		{
			name:            "create token request for an addon of another cluster",
			serviceAccounts: []runtime.Object{serviceAccount("test-addon-agent")},
			addons: []runtime.Object{
				&addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "test-addon", Namespace: "other-namespace"}},
			},
			tokenRequestEvt:   newTokenRequestEvent(types.SubResourceSpec, "test-namespace", "test-namespace", "test-addon-agent"),
			expectedError:     true,
			expectedErrorText: "serviceaccount test-namespace/test-addon-agent is not an addon agent",
		},
		{
			name:            "invalid event type",
			serviceAccounts: []runtime.Object{},
			tokenRequestEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError:     true,
			expectedErrorText: "failed to parse cloud event type",
		},
		{
			name:            "invalid action",
			serviceAccounts: []runtime.Object{},
			tokenRequestEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: sace.TokenRequestDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.DeleteRequestAction,
				}).WithClusterName("test-namespace").NewEvent()
				tokenRequest := &authenticationv1.TokenRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "test-namespace",
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, tokenRequest)
				return &evt
			}(),
			expectedError:     true,
			expectedErrorText: "unsupported action",
		},
		{
			name: "create token request successfully",
			serviceAccounts: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "test-namespace",
					},
				},
			},
			tokenRequestEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: sace.TokenRequestDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.CreateRequestAction,
				}).WithClusterName("test-namespace").NewEvent()
				tokenRequest := &authenticationv1.TokenRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "test-namespace",
					},
					Spec: authenticationv1.TokenRequestSpec{
						Audiences: []string{"test-audience"},
					},
				}
				tokenRequest.UID = "test-request-uid"
				evt.SetData(cloudevents.ApplicationJSON, tokenRequest)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				if actions[0].GetSubresource() != "token" {
					t.Errorf("expected subresource %s, got %s", "token", actions[0].GetSubresource())
				}
			},
			validateHandler: func(t *testing.T, handler *mockEventHandler) {
				if !handler.onCreateCalled {
					t.Errorf("expected OnCreate to be called")
				}
				if handler.lastDataType != sace.TokenRequestDataType {
					t.Errorf("expected data type %s, got %s", sace.TokenRequestDataType, handler.lastDataType)
				}
			},
		},
		{
			name:            "create token request with client error",
			serviceAccounts: []runtime.Object{},
			tokenRequestEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: sace.TokenRequestDataType,
					SubResource:         types.SubResourceSpec,
					Action:              types.CreateRequestAction,
				}).WithClusterName("test-namespace").NewEvent()
				tokenRequest := &authenticationv1.TokenRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "test-namespace",
					},
					Spec: authenticationv1.TokenRequestSpec{
						Audiences: []string{"test-audience"},
					},
				}
				evt.SetData(cloudevents.ApplicationJSON, tokenRequest)
				return &evt
			}(),
			reactorError:      fmt.Errorf("simulated error"),
			expectedError:     true,
			expectedErrorText: "simulated error",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.serviceAccounts...)

			// Add reactor for error simulation
			if c.reactorError != nil {
				kubeClient.PrependReactor("create", "serviceaccounts", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, c.reactorError
				})
			}

			service := NewTokenRequestService(kubeClient, newAddonLister(t, c.addons)).(*TokenRequestService)

			// Create and register a mock handler
			mockHandler := &mockEventHandler{}
			service.RegisterHandler(context.Background(), mockHandler)

			err := service.HandleStatusUpdate(context.Background(), c.tokenRequestEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if c.expectedErrorText != "" && !strings.HasPrefix(err.Error(), c.expectedErrorText) {
					t.Errorf("expected error to start with %q, got %q", c.expectedErrorText, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if c.validateActions != nil {
				c.validateActions(t, kubeClient.Actions())
			}

			if c.validateHandler != nil {
				c.validateHandler(t, mockHandler)
			}
		})
	}
}

func TestRegisterHandler(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient, nil).(*TokenRequestService)

	mockHandler := &mockEventHandler{}
	service.RegisterHandler(context.Background(), mockHandler)

	if service.handler == nil {
		t.Errorf("handler should not be nil after registration")
	}
}

// newAddonLister returns nil when addons is nil so the service account validation stays disabled.
func newAddonLister(t *testing.T, addons []runtime.Object) addonlisterv1beta1.ManagedClusterAddOnLister {
	if addons == nil {
		return nil
	}
	addonClient := addonfake.NewSimpleClientset(addons...)
	addonInformer := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute).Addon().V1beta1().ManagedClusterAddOns()
	for _, obj := range addons {
		if err := addonInformer.Informer().GetStore().Add(obj); err != nil {
			t.Fatal(err)
		}
	}
	return addonInformer.Lister()
}

// mockEventHandler is a mock implementation of server.EventHandler for testing
type mockEventHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
	onDeleteCalled bool
	lastDataType   types.CloudEventsDataType
	lastResourceID string
}

func (m *mockEventHandler) HandleEvent(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return err
	}

	m.lastDataType = eventType.CloudEventsDataType
	m.lastResourceID = evt.ID()

	// Determine what kind of event it is based on the action type
	switch eventType.Action {
	case types.CreateRequestAction:
		m.onCreateCalled = true
	case types.UpdateRequestAction:
		m.onUpdateCalled = true
	case types.DeleteRequestAction:
		m.onDeleteCalled = true
	}

	return nil
}
