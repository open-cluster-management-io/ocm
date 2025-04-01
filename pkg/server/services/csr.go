package services

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	certificatesv1informers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificatesv1listers "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

var _ server.Service = &CSRService{}

type CSRService struct {
	csrClient   kubernetes.Interface
	csrLister   certificatesv1listers.CertificateSigningRequestLister
	csrInformer certificatesv1informers.CertificateSigningRequestInformer
	codec       csrce.CSRCodec
}

func NewCSRService(csrClient kubernetes.Interface, csrInformer certificatesv1informers.CertificateSigningRequestInformer) server.Service {
	return &CSRService{
		csrClient:   csrClient,
		csrLister:   csrInformer.Lister(),
		csrInformer: csrInformer,
	}
}

func (c *CSRService) Get(_ context.Context, resourceID string) (*cloudevents.Event, error) {
	csr, err := c.csrLister.Get(resourceID)
	if err != nil {
		return nil, err
	}

	evt, err := c.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: csrce.CSREventDataType}, csr)
	if err != nil {
		return nil, err
	}

	return evt, nil
}

func (c *CSRService) List(listOpts types.ListOptions) ([]*cloudevents.Event, error) {
	var evts []*cloudevents.Event
	// list csrs with prefix matching cluster name
	requirement, _ := labels.NewRequirement(clusterv1.ClusterNameLabelKey, selection.Equals, []string{listOpts.ClusterName})
	selector := labels.NewSelector().Add(*requirement)
	csrs, err := c.csrLister.List(selector)
	if err != nil {
		return nil, err
	}

	for _, csr := range csrs {
		evt, err := c.codec.Encode(source, types.CloudEventsType{CloudEventsDataType: csrce.CSREventDataType}, csr)
		if err != nil {
			return nil, err
		}
		evts = append(evts, evt)
	}

	return evts, nil
}

// q if there is resourceVersion, this will return directly to the agent as conflict?
func (c *CSRService) HandleStatusUpdate(ctx context.Context, evt *cloudevents.Event) error {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}
	csr, err := c.codec.Decode(evt)
	if err != nil {
		return err
	}

	// only create and update action
	switch eventType.Action {
	case createRequestAction:
		_, err := c.csrClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *CSRService) RegisterHandler(handler server.EventHandler) {
	c.csrInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			accessor, _ := meta.Accessor(obj)
			if err := handler.OnCreate(context.Background(), csrce.CSREventDataType, accessor.GetName()); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			accessor, _ := meta.Accessor(newObj)
			if err := handler.OnUpdate(context.Background(), csrce.CSREventDataType, accessor.GetName()); err != nil {
				klog.Error(err)
			}
		},
		// agent does not need to care about delete event
	})
}
