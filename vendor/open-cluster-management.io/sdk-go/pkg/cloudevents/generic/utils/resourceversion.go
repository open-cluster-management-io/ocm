package utils

import (
	"strconv"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	workpaylaod "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

func SetResourceVersion(eventType types.CloudEventsType, evt *cloudevents.Event, obj generic.ResourceObject) {
	if eventType.CloudEventsDataType == workpaylaod.ManifestBundleEventDataType {
		evt.SetExtension(types.ExtensionResourceVersion, obj.GetGeneration())
		return
	}

	rv := obj.GetResourceVersion()
	if rv == "" {
		return
	}

	evt.SetExtension(types.ExtensionResourceVersion, obj.GetResourceVersion())
}

func GetResourceVersionFromObject(eventType types.CloudEventsType, obj generic.ResourceObject) (int64, error) {
	if eventType.CloudEventsDataType == workpaylaod.ManifestBundleEventDataType {
		return obj.GetGeneration(), nil
	}

	return strconv.ParseInt(obj.GetResourceVersion(), 10, 64)
}

func GetResourceVersionFromEvent(eventType types.CloudEventsType, evt cloudevents.Event) (int64, error) {
	if eventType.CloudEventsDataType == workpaylaod.ManifestBundleEventDataType {
		gen, err := cloudeventstypes.ToInteger(evt.Extensions()[types.ExtensionResourceVersion])
		return int64(gen), err
	}

	rv, err := cloudeventstypes.ToString(evt.Extensions()[types.ExtensionResourceVersion])
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(rv, 10, 64)
}
