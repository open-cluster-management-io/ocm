package csr

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestList(t *testing.T) {
	cases := []struct {
		name         string
		csrs         []runtime.Object
		clusterName  string
		expectedCSRs int
	}{
		{
			name:         "no csrs",
			csrs:         []runtime.Object{},
			clusterName:  "test-cluster",
			expectedCSRs: 0,
		},
		{
			name: "list csrs",
			csrs: []runtime.Object{
				&certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-csr1",
						Labels: map[string]string{
							"open-cluster-management.io/cluster-name": "test-cluster1",
						},
					},
				},
				&certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-csr2",
					},
				},
			},
			clusterName:  "test-cluster1",
			expectedCSRs: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			csrClient := kubefake.NewSimpleClientset(c.csrs...)
			csrInformers := informers.NewSharedInformerFactory(csrClient, 10*time.Minute)
			csrInformer := csrInformers.Certificates().V1().CertificateSigningRequests()
			for _, obj := range c.csrs {
				if err := csrInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			service := NewCSRService(csrClient, csrInformer)
			evts, err := service.List(context.Background(), types.ListOptions{ClusterName: c.clusterName})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(evts) != c.expectedCSRs {
				t.Errorf("expected %d csrs, got %d", c.expectedCSRs, len(evts))
			}
		})
	}
}

func TestHandleStatusUpdate(t *testing.T) {
	cases := []struct {
		name            string
		csrs            []runtime.Object
		csrEvt          *cloudevents.Event
		validateActions func(t *testing.T, actions []clienttesting.Action)
		expectedError   bool
	}{
		{
			name: "invalid event type",
			csrs: []runtime.Object{},
			csrEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{}).NewEvent()
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "invalid action",
			csrs: []runtime.Object{},
			csrEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: csrce.CSREventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.DeleteRequestAction,
				}).NewEvent()
				csr := &certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test-csr"},
				}
				evt.SetData(cloudevents.ApplicationJSON, csr)
				return &evt
			}(),
			expectedError: true,
		},
		{
			name: "create csr",
			csrs: []runtime.Object{},
			csrEvt: func() *cloudevents.Event {
				evt := types.NewEventBuilder("test", types.CloudEventsType{
					CloudEventsDataType: csrce.CSREventDataType,
					SubResource:         types.SubResourceStatus,
					Action:              types.CreateRequestAction,
				}).NewEvent()
				csr := &certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test-csr"},
				}
				evt.SetData(cloudevents.ApplicationJSON, csr)
				return &evt
			}(),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			csrClient := kubefake.NewSimpleClientset(c.csrs...)
			csrInformers := informers.NewSharedInformerFactory(csrClient, 10*time.Minute)
			csrInformer := csrInformers.Certificates().V1().CertificateSigningRequests()

			service := NewCSRService(csrClient, csrInformer)
			err := service.HandleStatusUpdate(context.Background(), c.csrEvt)
			if c.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, csrClient.Actions())
		})
	}
}

func TestEventHandlerFuncs(t *testing.T) {
	handler := &csrOnHandler{}
	service := &CSRService{}
	eventHandlerFuncs := service.EventHandlerFuncs(context.Background(), handler)

	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-csr",
			Labels: map[string]string{
				"open-cluster-management.io/cluster-name": "test-cluster",
			},
		},
	}
	eventHandlerFuncs.AddFunc(csr)
	if !handler.onCreateCalled {
		t.Errorf("onCreate not called")
	}

	eventHandlerFuncs.UpdateFunc(nil, csr)
	if !handler.onUpdateCalled {
		t.Errorf("onUpdate not called")
	}
}

type csrOnHandler struct {
	onCreateCalled bool
	onUpdateCalled bool
}

func (m *csrOnHandler) HandleEvent(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return err
	}

	if eventType.CloudEventsDataType != csrce.CSREventDataType {
		return fmt.Errorf("expected %v, got %v", csrce.CSREventDataType, eventType.CloudEventsDataType)
	}

	m.onCreateCalled = true
	m.onUpdateCalled = true
	return nil
}
