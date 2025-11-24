package pubsub

import (
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding/spec"
	"github.com/cloudevents/sdk-go/v2/types"
)

const (
	// CloudEvents attribute prefix for Pub/Sub message attributes
	ceAttrPrefix = "ce-"
	// ContentType attribute key
	contentType = "Content-Type"
)

// Encode encodes a CloudEvent into a Pub/Sub message.
// It serializes the CloudEvent data as JSON and adds CloudEvent attributes
// as Pub/Sub message attributes with "ce-" prefix.
//
// CloudEvents required attributes (added with "ce-" prefix):
//   - ce-specversion: CloudEvents specification version
//   - ce-type: Event type
//   - ce-source: Event source
//   - ce-id: Event ID
//
// CloudEvents special attribute:
//   - Content-Type: Data content type
//
// CloudEvents optional attributes (added with "ce-" prefix if present):
//   - ce-dataschema: Schema of the data
//   - ce-subject: Subject of the event
//   - ce-time: Timestamp of the event
//
// CloudEvents extensions are also added as attributes with "ce-" prefix.
func Encode(evt cloudevents.Event) (*pubsub.Message, error) {
	msg := &pubsub.Message{
		Data:       evt.Data(),
		Attributes: make(map[string]string),
	}

	// Get the spec version to access attributes
	version := spec.WithPrefix(ceAttrPrefix).Version(evt.SpecVersion())

	// Add all CloudEvent attributes using the spec
	for _, attr := range version.Attributes() {
		value := attr.Get(evt.Context)
		if value != nil {
			// Convert value to string
			strValue, err := types.Format(value)
			if err != nil {
				return nil, fmt.Errorf("failed to format attribute %s: %v", attr.Kind().String(), err)
			}
			if strValue != "" {
				// Special handling for datacontenttype - use "Content-Type" without "ce-" prefix
				// This follows the CloudEvents HTTP protocol binding convention
				if attr.Kind() == spec.DataContentType {
					msg.Attributes[contentType] = strValue
				} else {
					msg.Attributes[attr.PrefixedName()] = strValue
				}
			}
		}
	}

	// Add all CloudEvent extensions as attributes
	for key, value := range evt.Extensions() {
		attrKey := ceAttrPrefix + key
		strValue, err := types.Format(value)
		if err != nil {
			return nil, fmt.Errorf("failed to format extension %s: %v", key, err)
		}
		msg.Attributes[attrKey] = strValue
	}

	return msg, nil
}

// Decode decodes a Pub/Sub message into a CloudEvent.
// It reconstructs the CloudEvent from Pub/Sub message data and attributes.
//
// The function expects the Pub/Sub message to contain:
//   - Data: CloudEvent data payload
//   - Attributes: CloudEvent context attributes with "ce-" prefix and extensions
//   - Required: ce-specversion, ce-type, ce-source, ce-id
//   - Optional: ce-subject, ce-dataschema, ce-time
//   - Special: Content-Type (for datacontenttype, without "ce-" prefix)
//   - Extensions: Any attribute with "ce-" prefix not listed above
//
// Returns an error if:
//   - The message is nil or has empty data
//   - Required CloudEvent attributes are missing
//   - Attribute values cannot be set on the CloudEvent
func Decode(msg *pubsub.Message) (cloudevents.Event, error) {
	if msg == nil {
		return cloudevents.Event{}, fmt.Errorf("cannot decode nil Pub/Sub message")
	}

	specs := spec.WithPrefix(ceAttrPrefix)
	// Get spec version from message attributes to create the event with the correct version
	specVersionAttr := specs.PrefixedSpecVersionName()
	specVersion := msg.Attributes[specVersionAttr]
	if specVersion == "" {
		specVersion = cloudevents.VersionV1 // Default to V1 if not specified
	}

	// Create a new CloudEvent with the spec version
	evt := cloudevents.NewEvent(specVersion)

	// Get the spec version to access attributes
	version := specs.Version(specVersion)

	var dataContentType string
	// Set all CloudEvent context attributes from message attributes
	for _, attr := range version.Attributes() {
		var value string
		var ok bool

		// Special handling for datacontenttype - use "Content-Type" without "ce-" prefix
		if attr.Kind() == spec.DataContentType {
			value, ok = msg.Attributes[contentType]
			dataContentType = value
		} else {
			value, ok = msg.Attributes[attr.PrefixedName()]
		}

		if ok && value != "" {
			// Convert string value to appropriate type and set on the event
			if err := attr.Set(evt.Context, value); err != nil {
				return cloudevents.Event{}, fmt.Errorf("failed to set attribute %s: %v", attr.Kind().String(), err)
			}
		}
	}

	// Set CloudEvent extensions from message attributes
	// Extensions are attributes with "ce-" prefix that are not standard CloudEvents attributes
	standardAttrs := make(map[string]bool)
	for _, attr := range version.Attributes() {
		standardAttrs[attr.PrefixedName()] = true
	}

	for key, value := range msg.Attributes {
		// Skip Content-Type and standard attributes
		if key == contentType || standardAttrs[key] {
			continue
		}

		// Check if it's a "ce-" prefixed attribute (extension)
		if len(key) > len(ceAttrPrefix) && key[:len(ceAttrPrefix)] == ceAttrPrefix {
			extName := key[len(ceAttrPrefix):]
			evt.SetExtension(extName, value)
		}
	}

	if dataContentType == "" {
		// default data content type be "application/JSON"
		dataContentType = cloudevents.ApplicationJSON
	}
	// Set the data from the message
	if err := evt.SetData(dataContentType, msg.Data); err != nil {
		return cloudevents.Event{}, fmt.Errorf("failed to set event data: %v", err)
	}

	// Validate required CloudEvent attributes
	for _, attr := range version.Attributes() {
		if attr.Kind().IsRequired() {
			value := attr.Get(evt.Context)
			if value == nil || types.IsZero(value) {
				return cloudevents.Event{}, fmt.Errorf("CloudEvent missing required attribute: %s", attr.Kind().String())
			}
		}
	}

	return evt, nil
}
