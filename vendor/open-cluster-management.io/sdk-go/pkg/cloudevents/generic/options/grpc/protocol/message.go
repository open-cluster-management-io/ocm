package protocol

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/binding/format"
	"github.com/cloudevents/sdk-go/v2/binding/spec"
	"github.com/cloudevents/sdk-go/v2/types"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
)

const (
	prefix      = "ce-"
	contenttype = "contenttype"
	// dataSchema  = "dataschema"
	subject = "subject"
	time    = "time"
)

var specs = spec.WithPrefix(prefix)

// Message represents a gRPC message.
// This message *can* be read several times safely
type Message struct {
	internal *pbv1.CloudEvent
	version  spec.Version
	format   format.Format
}

// Check if Message implements binding.Message
var (
	_ binding.Message               = (*Message)(nil)
	_ binding.MessageMetadataReader = (*Message)(nil)
)

func NewMessage(msg *pbv1.CloudEvent) *Message {
	var f format.Format
	var v spec.Version
	if msg.Attributes != nil {
		if contentType, ok := msg.Attributes[contenttype]; ok && format.IsFormat(contentType.GetCeString()) {
			f = format.Lookup(contentType.GetCeString())
		} else if s := msg.SpecVersion; s != "" {
			v = specs.Version(s)
		}
	}
	return &Message{
		internal: msg,
		version:  v,
		format:   f,
	}
}

func (m *Message) ReadEncoding() binding.Encoding {
	if m.version != nil {
		return binding.EncodingBinary
	}
	if m.format != nil {
		return binding.EncodingStructured
	}

	return binding.EncodingUnknown
}

func (m *Message) ReadStructured(ctx context.Context, encoder binding.StructuredWriter) error {
	if m.format == nil {
		return binding.ErrNotStructured
	}

	return encoder.SetStructuredEvent(ctx, m.format, bytes.NewReader(m.internal.GetBinaryData()))
}

func (m *Message) ReadBinary(ctx context.Context, encoder binding.BinaryWriter) error {
	if m.version == nil {
		return binding.ErrNotBinary
	}

	if m.format != nil {
		return binding.ErrNotBinary
	}

	if m.internal.SpecVersion != "" {
		err := encoder.SetAttribute(m.version.AttributeFromKind(spec.SpecVersion), m.internal.SpecVersion)
		if err != nil {
			return err
		}
	}
	if m.internal.Id != "" {
		err := encoder.SetAttribute(m.version.AttributeFromKind(spec.ID), m.internal.Id)
		if err != nil {
			return err
		}
	}
	if m.internal.Source != "" {
		err := encoder.SetAttribute(m.version.AttributeFromKind(spec.Source), m.internal.Source)
		if err != nil {
			return err
		}
	}
	if m.internal.Type != "" {
		err := encoder.SetAttribute(m.version.AttributeFromKind(spec.Type), m.internal.Type)
		if err != nil {
			return err
		}
	}

	for name, value := range m.internal.Attributes {
		attrVal, err := valueFrom(value)
		if err != nil {
			return fmt.Errorf("failed to convert attribute %s: %s", name, err)
		}

		if strings.HasPrefix(name, prefix) {
			attr := m.version.Attribute(name)
			if attr != nil {
				err = encoder.SetAttribute(attr, attrVal)
				if err != nil {
					return err
				}
			} else {
				err = encoder.SetExtension(strings.TrimPrefix(name, prefix), attrVal)
				if err != nil {
					return err
				}
			}
		} else if name == contenttype {
			err = encoder.SetAttribute(m.version.AttributeFromKind(spec.DataContentType), attrVal)
			if err != nil {
				return err
			}
		}
	}

	if m.internal.GetBinaryData() != nil {
		return encoder.SetData(bytes.NewBuffer(m.internal.GetBinaryData()))
	}

	return nil
}

func (m *Message) Finish(error) error {
	return nil
}

func (m *Message) GetAttribute(k spec.Kind) (spec.Attribute, interface{}) {
	attr := m.version.AttributeFromKind(k)
	if attr != nil {
		switch attr.Kind() {
		case spec.SpecVersion:
			return attr, m.internal.SpecVersion
		case spec.Type:
			return attr, m.internal.Type
		case spec.Source:
			return attr, m.internal.Source
		case spec.ID:
			return attr, m.internal.Id
		// case spec.DataContentType:
		// 	return attr, m.internal.Attributes[contenttype].GetCeString()
		default:
			return attr, m.internal.Attributes[prefix+attr.Name()]
		}
	}

	return nil, nil
}

func (m *Message) GetExtension(name string) interface{} {
	return m.internal.Attributes[prefix+name]
}

func valueFrom(attr *pbv1.CloudEventAttributeValue) (interface{}, error) {
	var v interface{}
	switch vt := attr.Attr.(type) {
	case *pbv1.CloudEventAttributeValue_CeBoolean:
		v = vt.CeBoolean
	case *pbv1.CloudEventAttributeValue_CeInteger:
		v = vt.CeInteger
	case *pbv1.CloudEventAttributeValue_CeString:
		v = vt.CeString
	case *pbv1.CloudEventAttributeValue_CeBytes:
		v = vt.CeBytes
	case *pbv1.CloudEventAttributeValue_CeUri:
		uri, err := url.Parse(vt.CeUri)
		if err != nil {
			return nil, fmt.Errorf("failed to parse URI value %s: %s", vt.CeUri, err.Error())
		}
		v = uri
	case *pbv1.CloudEventAttributeValue_CeUriRef:
		uri, err := url.Parse(vt.CeUriRef)
		if err != nil {
			return nil, fmt.Errorf("failed to parse URIRef value %s: %s", vt.CeUriRef, err.Error())
		}
		v = types.URIRef{URL: *uri}
	case *pbv1.CloudEventAttributeValue_CeTimestamp:
		v = vt.CeTimestamp.AsTime()
	default:
		return nil, fmt.Errorf("unsupported attribute type: %T", vt)
	}
	return types.Validate(v)
}
