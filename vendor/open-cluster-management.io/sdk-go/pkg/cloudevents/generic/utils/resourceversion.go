package utils

import (
	"strconv"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	workpayload "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// SetResourceVersion sets the resourceversion extension on a CloudEvent based on the event type.
// For ManifestBundleEvent, it uses the object's generation number, which represents spec changes.
// For other event types, it uses the object's resource version, which represents all changes to the object.
// If the resource version is empty for non-ManifestBundle types, no extension is set.
func SetResourceVersion(eventType types.CloudEventsType, evt *cloudevents.Event, obj generic.ResourceObject) {
	if eventType.CloudEventsDataType == workpayload.ManifestBundleEventDataType {
		evt.SetExtension(types.ExtensionResourceVersion, obj.GetGeneration())
		return
	}

	rv := obj.GetResourceVersion()
	if rv == "" {
		return
	}

	evt.SetExtension(types.ExtensionResourceVersion, rv)
}

// GetResourceVersionFromObject extracts the resource version from a resource object as an int64.
// For ManifestBundleEvent, it returns the object's generation number directly.
// For other event types, it parses the resource version string to an int64.
// Returns an error if the resource version string cannot be parsed.
func GetResourceVersionFromObject(eventType types.CloudEventsType, obj generic.ResourceObject) (int64, error) {
	if eventType.CloudEventsDataType == workpayload.ManifestBundleEventDataType {
		return obj.GetGeneration(), nil
	}

	return strconv.ParseInt(obj.GetResourceVersion(), 10, 64)
}

// GetResourceVersionFromEvent extracts the resource version from a CloudEvent extension as an int64.
// For ManifestBundleEvent, it converts the extension value to an integer (generation).
// For other event types, it converts the extension value to a string and parses it to int64.
// Returns an error if the extension is missing, has an invalid type, or cannot be parsed.
func GetResourceVersionFromEvent(eventType types.CloudEventsType, evt cloudevents.Event) (int64, error) {
	if eventType.CloudEventsDataType == workpayload.ManifestBundleEventDataType {
		// TODO only use string as the resourceversion extension
		gen, err := cloudeventstypes.ToInteger(evt.Extensions()[types.ExtensionResourceVersion])
		return int64(gen), err
	}

	rv, err := cloudeventstypes.ToString(evt.Extensions()[types.ExtensionResourceVersion])
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(rv, 10, 64)
}
