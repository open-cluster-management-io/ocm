package csr

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	certificatesv1informers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificatesv1listers "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authn"

	"open-cluster-management.io/ocm/pkg/server/services"
)

type CSRService struct {
	csrClient   kubernetes.Interface
	csrLister   certificatesv1listers.CertificateSigningRequestLister
	csrInformer certificatesv1informers.CertificateSigningRequestInformer
	codec       *csrce.CSRCodec
}

func NewCSRService(csrClient kubernetes.Interface, csrInformer certificatesv1informers.CertificateSigningRequestInformer) server.Service {
	return &CSRService{
		csrClient:   csrClient,
		csrLister:   csrInformer.Lister(),
		csrInformer: csrInformer,
		codec:       csrce.NewCSRCodec(),
	}
}

func (c *CSRService) List(ctx context.Context, listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	var evts []*cloudevents.Event
	requirement, err := labels.NewRequirement(clusterv1.ClusterNameLabelKey, selection.Equals, []string{listOpts.ClusterName})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*requirement)
	csrs, err := c.csrLister.List(selector)
	if err != nil {
		return nil, err
	}

	for _, csr := range csrs {
		evt, err := c.codec.Encode(services.CloudEventsSourceKube, types.CloudEventsType{CloudEventsDataType: csrce.CSREventDataType}, csr)
		if err != nil {
			return nil, err
		}
		evts = append(evts, evt)
	}

	return evts, nil
}

func (c *CSRService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	csr, err := c.codec.Decode(evt)
	if err != nil {
		return err
	}

	switch eventType.Action {
	case types.CreateRequestAction:
		// The agent requests a CSR, and the gRPC server creates the CSR on the hub. As a result,
		// the username in the csr is the service account of gRPC server rather than the user of agent.
		// The approver controller in the registration will not be able to know where this CSR originates
		// from. Therefore, this annotation with the agent's username is added for CSR approval checks.
		csr.Annotations = map[string]string{operatorv1.CSRUsernameAnnotation: fmt.Sprintf("%v", ctx.Value(authn.ContextUserKey))}
		_, err := c.csrClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{})
		return err
	default:
		return fmt.Errorf("unsupported action %s for csr %s", eventType.Action, csr.Name)
	}
}

func (c *CSRService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	logger := klog.FromContext(ctx)
	if _, err := c.csrInformer.Informer().AddEventHandler(c.EventHandlerFuncs(ctx, handler)); err != nil {
		logger.Error(err, "failed to register csr informer event handler")
	}
}

// TODO handle type check error and event handler error
func (c *CSRService) EventHandlerFuncs(ctx context.Context, handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			csr, ok := obj.(*certificatesv1.CertificateSigningRequest)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "csr add")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: csrce.CSREventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.CreateRequestAction,
			}
			evt, err := c.codec.Encode(services.CloudEventsSourceKube, eventTypes, csr)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode csr", "name", csr.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to create csr", "name", csr.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			csr, ok := newObj.(*certificatesv1.CertificateSigningRequest)
			if !ok {
				utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", newObj), "csr update")
				return
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: csrce.CSREventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.UpdateRequestAction,
			}
			evt, err := c.codec.Encode(services.CloudEventsSourceKube, eventTypes, csr)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode csr", "name", csr.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to update csr", "name", csr.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			csr, ok := obj.(*certificatesv1.CertificateSigningRequest)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "csr delete")
					return
				}

				csr, ok = tombstone.Obj.(*certificatesv1.CertificateSigningRequest)
				if !ok {
					utilruntime.HandleErrorWithContext(ctx, fmt.Errorf("unknown type: %T", obj), "csr delete")
					return
				}
			}

			csr = csr.DeepCopy()
			if csr.DeletionTimestamp.IsZero() {
				csr.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}

			eventTypes := types.CloudEventsType{
				CloudEventsDataType: csrce.CSREventDataType,
				SubResource:         types.SubResourceSpec,
				Action:              types.DeleteRequestAction,
			}
			evt, err := c.codec.Encode(services.CloudEventsSourceKube, eventTypes, csr)
			if err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to encode csr", "name", csr.Name)
				return
			}

			if err := handler.HandleEvent(ctx, evt); err != nil {
				utilruntime.HandleErrorWithContext(ctx, err, "failed to delete csr", "name", csr.Name)
			}

		},
	}
}
