package csr

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	certificatev1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	genericutils "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/utils"
)

var CSREventDataType = types.CloudEventsDataType{
	Group:    certificatev1.SchemeGroupVersion.Group,
	Version:  certificatev1.SchemeGroupVersion.Version,
	Resource: "certificatesigningrequests",
}

// CSRCodec is a codec to encode/decode a CSR/cloudevent for an agent.
type CSRCodec struct{}

func NewCSRCodec() *CSRCodec {
	return &CSRCodec{}
}

// EventDataType always returns the event data type `certificates.k8s.io.v1.certificatesigningrequests`.
func (c *CSRCodec) EventDataType() types.CloudEventsDataType {
	return CSREventDataType
}

// Encode the CSR to a cloudevent
func (c *CSRCodec) Encode(source string, eventType types.CloudEventsType, csr *certificatev1.CertificateSigningRequest) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != CSREventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	if len(csr.Labels) == 0 {
		return nil, fmt.Errorf("no cluster label found for CSR")
	}
	cluster, ok := csr.Labels[v1.ClusterNameLabelKey]

	if !ok {
		return nil, fmt.Errorf("no cluster name found for CSR")
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(string(csr.UID)).
		WithClusterName(cluster).
		NewEvent()

	genericutils.SetResourceVersion(eventType, &evt, csr)

	if !csr.DeletionTimestamp.IsZero() {
		evt.SetExtension(types.ExtensionDeletionTimestamp, csr.DeletionTimestamp.Time)
		return &evt, nil
	}

	newCSR := csr.DeepCopy()
	newCSR.TypeMeta = metav1.TypeMeta{
		APIVersion: certificatev1.SchemeGroupVersion.String(),
		Kind:       "CertificateSigningRequest",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newCSR); err != nil {
		return nil, fmt.Errorf("failed to encode csr to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to a CSR
func (c *CSRCodec) Decode(evt *cloudevents.Event) (*certificatev1.CertificateSigningRequest, error) {
	return utils.DecodeWithDeletionHandling(evt, func() *certificatev1.CertificateSigningRequest {
		return &certificatev1.CertificateSigningRequest{}
	})
}
