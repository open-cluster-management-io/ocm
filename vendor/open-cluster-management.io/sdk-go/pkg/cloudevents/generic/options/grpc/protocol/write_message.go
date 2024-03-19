package protocol

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/binding/format"
	"github.com/cloudevents/sdk-go/v2/binding/spec"
	"github.com/cloudevents/sdk-go/v2/types"
	"google.golang.org/protobuf/types/known/timestamppb"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
)

// WritePBMessage fills the provided pubMessage with the message m.
// Using context you can tweak the encoding processing (more details on binding.Write documentation).
func WritePBMessage(ctx context.Context, m binding.Message, pbEvt *pbv1.CloudEvent, transformers ...binding.Transformer) error {
	structuredWriter := (*pbEventWriter)(pbEvt)
	binaryWriter := (*pbEventWriter)(pbEvt)

	_, err := binding.Write(
		ctx,
		m,
		structuredWriter,
		binaryWriter,
		transformers...,
	)
	return err
}

type pbEventWriter pbv1.CloudEvent

var (
	_ binding.StructuredWriter = (*pbEventWriter)(nil)
	_ binding.BinaryWriter     = (*pbEventWriter)(nil)
)

func (b *pbEventWriter) SetStructuredEvent(ctx context.Context, f format.Format, event io.Reader) error {
	if b.Attributes == nil {
		b.Attributes = make(map[string]*pbv1.CloudEventAttributeValue)
	}

	b.Attributes[contenttype], _ = attributeFor(f.MediaType())

	var buf bytes.Buffer
	_, err := io.Copy(&buf, event)
	if err != nil {
		return err
	}

	// TODO: check the data content type and set the right data format
	b.Data = &pbv1.CloudEvent_BinaryData{
		BinaryData: buf.Bytes(),
	}

	return nil
}

func (b *pbEventWriter) Start(ctx context.Context) error {
	if b.Attributes == nil {
		b.Attributes = make(map[string]*pbv1.CloudEventAttributeValue)
	}

	return nil
}

func (b *pbEventWriter) End(ctx context.Context) error {
	return nil
}

func (b *pbEventWriter) SetData(reader io.Reader) error {
	buf, ok := reader.(*bytes.Buffer)
	if !ok {
		buf = new(bytes.Buffer)
		_, err := io.Copy(buf, reader)
		if err != nil {
			return err
		}
	}

	b.Data = &pbv1.CloudEvent_BinaryData{
		BinaryData: buf.Bytes(),
	}

	return nil
}

func (b *pbEventWriter) SetAttribute(attribute spec.Attribute, value interface{}) error {
	switch attribute.Kind() {
	case spec.SpecVersion:
		val, ok := value.(string)
		if !ok {
			return fmt.Errorf("invalid SpecVersion type, expected string got %T", value)
		}
		b.SpecVersion = val
	case spec.ID:
		val, ok := value.(string)
		if !ok {
			return fmt.Errorf("invalid ID type, expected string got %T", value)
		}
		b.Id = val
	case spec.Source:
		val, ok := value.(string)
		if !ok {
			return fmt.Errorf("invalid Source type, expected string got %T", value)
		}
		b.Source = val
	case spec.Type:
		val, ok := value.(string)
		if !ok {
			return fmt.Errorf("invalid Type type, expected string got %T", value)
		}
		b.Type = val
	case spec.DataContentType:
		if value == nil {
			delete(b.Attributes, contenttype)
		} else {
			attrVal, err := attributeFor(value)
			if err != nil {
				return err
			}
			b.Attributes[contenttype] = attrVal
		}
	case spec.Subject:
		if value == nil {
			delete(b.Attributes, prefix+subject)
		} else {
			attrVal, err := attributeFor(value)
			if err != nil {
				return err
			}
			b.Attributes[prefix+subject] = attrVal
		}
	case spec.Time:
		if value == nil {
			delete(b.Attributes, prefix+time)
		} else {
			attrVal, err := attributeFor(value)
			if err != nil {
				return err
			}
			b.Attributes[prefix+time] = attrVal
		}
	default:
		if value == nil {
			delete(b.Attributes, prefix+attribute.Name())
		} else {
			attrVal, err := attributeFor(value)
			if err != nil {
				return err
			}
			b.Attributes[prefix+attribute.Name()] = attrVal
		}
	}

	return nil
}

func (b *pbEventWriter) SetExtension(name string, value interface{}) error {
	if value == nil {
		delete(b.Attributes, prefix+name)
	} else {
		attrVal, err := attributeFor(value)
		if err != nil {
			return err
		}
		b.Attributes[prefix+name] = attrVal
	}

	return nil
}

func attributeFor(v interface{}) (*pbv1.CloudEventAttributeValue, error) {
	vv, err := types.Validate(v)
	if err != nil {
		return nil, err
	}
	attr := &pbv1.CloudEventAttributeValue{}
	switch vt := vv.(type) {
	case bool:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeBoolean{
			CeBoolean: vt,
		}
	case int32:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeInteger{
			CeInteger: vt,
		}
	case string:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeString{
			CeString: vt,
		}
	case []byte:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeBytes{
			CeBytes: vt,
		}
	case types.URI:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeUri{
			CeUri: vt.String(),
		}
	case types.URIRef:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeUriRef{
			CeUriRef: vt.String(),
		}
	case types.Timestamp:
		attr.Attr = &pbv1.CloudEventAttributeValue_CeTimestamp{
			CeTimestamp: timestamppb.New(vt.Time),
		}
	default:
		return nil, fmt.Errorf("unsupported attribute type: %T", v)
	}
	return attr, nil
}
