package tokenrequest

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	sace "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/serviceaccount"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestNewTokenRequestService(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient)

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

	if tokenService.store == nil {
		t.Errorf("store should not be nil")
	}
}

func TestGet(t *testing.T) {
	cases := []struct {
		name          string
		resourceID    string
		setupStore    func(*TokenRequestService)
		expectedError bool
		errorCheck    func(error) bool
	}{
		{
			name:       "token not found",
			resourceID: "non-existent-token",
			setupStore: func(s *TokenRequestService) {
				// Empty store
			},
			expectedError: true,
			errorCheck: func(err error) bool {
				return errors.IsNotFound(err)
			},
		},
		{
			name:       "token found",
			resourceID: "test-token-uid",
			setupStore: func(s *TokenRequestService) {
				tokenRequest := &authenticationv1.TokenRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "test-namespace",
					},
					Spec: authenticationv1.TokenRequestSpec{
						Audiences: []string{"test-audience"},
					},
					Status: authenticationv1.TokenRequestStatus{
						Token:               "test-token-value",
						ExpirationTimestamp: metav1.NewTime(time.Now().Add(1 * time.Hour)),
					},
				}
				tokenRequest.UID = "test-token-uid"
				s.store.Add(tokenRequest)
			},
			expectedError: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset()
			service := NewTokenRequestService(kubeClient).(*TokenRequestService)

			if c.setupStore != nil {
				c.setupStore(service)
			}

			evt, err := service.Get(context.Background(), c.resourceID)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if c.errorCheck != nil && !c.errorCheck(err) {
					t.Errorf("error check failed for error: %v", err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if evt == nil {
				t.Errorf("expected event, got nil")
			}
		})
	}
}

func TestList(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient)

	evts, err := service.List(types.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if evts != nil {
		t.Errorf("expected nil events, got %v", evts)
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name              string
		serviceAccounts   []runtime.Object
		tokenRequestEvt   *cloudevents.Event
		validateActions   func(t *testing.T, actions []clienttesting.Action)
		validateCache     func(t *testing.T, service *TokenRequestService)
		validateHandler   func(t *testing.T, handler *mockEventHandler)
		reactorError      error
		expectedError     bool
		expectedErrorText string
	}{
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
				}).NewEvent()
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
				}).NewEvent()
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
			validateCache: func(t *testing.T, service *TokenRequestService) {
				obj, exists, err := service.store.GetByKey("test-request-uid")
				if err != nil {
					t.Errorf("unexpected error getting from cache: %v", err)
				}
				if !exists {
					t.Errorf("expected token to be cached")
				}
				tokenResponse, ok := obj.(*authenticationv1.TokenRequest)
				if !ok {
					t.Errorf("expected TokenRequest, got %T", obj)
				}
				if tokenResponse.UID != "test-request-uid" {
					t.Errorf("expected UID %s, got %s", "test-request-uid", tokenResponse.UID)
				}
			},
			validateHandler: func(t *testing.T, handler *mockEventHandler) {
				if !handler.onCreateCalled {
					t.Errorf("expected OnCreate to be called")
				}
				if handler.lastResourceID != "test-request-uid" {
					t.Errorf("expected resourceID %s, got %s", "test-request-uid", handler.lastResourceID)
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
				}).NewEvent()
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
			expectedErrorText: "failed to create token for service account",
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

			service := NewTokenRequestService(kubeClient).(*TokenRequestService)

			// Create and register a mock handler
			mockHandler := &mockEventHandler{}
			service.RegisterHandler(context.Background(), mockHandler)

			err := service.HandleStatusUpdate(context.Background(), c.tokenRequestEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if c.expectedErrorText != "" && err.Error()[:len(c.expectedErrorText)] != c.expectedErrorText {
					t.Errorf("expected error to contain %q, got %q", c.expectedErrorText, err.Error())
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

			if c.validateCache != nil {
				c.validateCache(t, service)
			}

			if c.validateHandler != nil {
				c.validateHandler(t, mockHandler)
			}
		})
	}
}

func TestRegisterHandler(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	service := NewTokenRequestService(kubeClient).(*TokenRequestService)

	mockHandler := &mockEventHandler{}
	service.RegisterHandler(context.Background(), mockHandler)

	if service.handler == nil {
		t.Errorf("handler should not be nil after registration")
	}
}

func TestTokenCacheTTL(t *testing.T) {
	// Save original TTL and restore after test
	originalTTL := TokenCacheTTL
	defer func() {
		TokenCacheTTL = originalTTL
	}()

	// Use a shorter TTL for faster test
	TokenCacheTTL = 2 * time.Second

	kubeClient := kubefake.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "test-namespace",
		},
	})

	service := NewTokenRequestService(kubeClient).(*TokenRequestService)
	mockHandler := &mockEventHandler{}
	service.RegisterHandler(context.Background(), mockHandler)

	// Create a token request event
	evt := types.NewEventBuilder("test", types.CloudEventsType{
		CloudEventsDataType: sace.TokenRequestDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}).NewEvent()
	tokenRequest := &authenticationv1.TokenRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "test-namespace",
		},
		Spec: authenticationv1.TokenRequestSpec{
			Audiences: []string{"test-audience"},
		},
	}
	tokenRequest.UID = "test-ttl-uid"
	evt.SetData(cloudevents.ApplicationJSON, tokenRequest)

	// Handle the event to cache the token
	err := service.HandleStatusUpdate(context.Background(), &evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify token is in cache
	_, exists, err := service.store.GetByKey("test-ttl-uid")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !exists {
		t.Errorf("expected token to be in cache")
	}

	// Wait for TTL to expire
	time.Sleep(TokenCacheTTL + 1*time.Second)

	// Verify token is removed from cache
	_, exists, err = service.store.GetByKey("test-ttl-uid")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if exists {
		t.Errorf("expected token to be removed from cache after TTL")
	}
}

// mockEventHandler is a mock implementation of server.EventHandler for testing
type mockEventHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
	onDeleteCalled bool
	lastDataType   types.CloudEventsDataType
	lastResourceID string
}

func (m *mockEventHandler) OnCreate(ctx context.Context, dataType types.CloudEventsDataType, resourceID string) error {
	m.onCreateCalled = true
	m.lastDataType = dataType
	m.lastResourceID = resourceID
	return nil
}

func (m *mockEventHandler) OnUpdate(ctx context.Context, dataType types.CloudEventsDataType, resourceID string) error {
	m.onUpdateCalled = true
	m.lastDataType = dataType
	m.lastResourceID = resourceID
	return nil
}

func (m *mockEventHandler) OnDelete(ctx context.Context, dataType types.CloudEventsDataType, resourceID string) error {
	m.onDeleteCalled = true
	m.lastDataType = dataType
	m.lastResourceID = resourceID
	return nil
}
