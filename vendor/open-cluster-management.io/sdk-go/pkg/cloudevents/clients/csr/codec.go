package csr

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	certificatev1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
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

// EventDataType always returns the event data type `io.k8s.certificates.v1.certificatesigningrequests`.
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
		WithResourceID(csr.Name).
		WithClusterName(cluster).
		NewEvent()

	if csr.ResourceVersion != "" {
		evt.SetExtension(types.ExtensionResourceVersion, csr.ResourceVersion)
	}

	newCSR := csr.DeepCopy()
	newCSR.TypeMeta = metav1.TypeMeta{
		APIVersion: certificatev1.SchemeGroupVersion.String(),
		Kind:       "CSR",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newCSR); err != nil {
		return nil, fmt.Errorf("failed to encode csr to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to a CSR
func (c *CSRCodec) Decode(evt *cloudevents.Event) (*certificatev1.CertificateSigningRequest, error) {
	csr := &certificatev1.CertificateSigningRequest{}
	if err := evt.DataAs(csr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	return csr, nil
}
