package utils

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// DecodeWithDeletionHandling decodes a cloudevent into a resource object, handling deletion events.
// For deletion events without data, it extracts UID and DeletionTimestamp from event extensions.
// For other events, it unmarshals the event data into the resource object.
//
// The factory function creates a new instance of type T.
func DecodeWithDeletionHandling[T metav1.Object](evt *cloudevents.Event, factory func() T) (T, error) {
	var zero T
	obj := factory()

	if _, ok := evt.Extensions()[types.ExtensionDeletionTimestamp]; ok {
		if len(evt.Data()) == 0 {
			resourceID, err := cloudeventstypes.ToString(evt.Extensions()[types.ExtensionResourceID])
			if err != nil {
				return zero, err
			}

			deletionTimestamp, err := cloudeventstypes.ToTime(evt.Extensions()[types.ExtensionDeletionTimestamp])
			if err != nil {
				return zero, err
			}

			obj.SetUID(kubetypes.UID(resourceID))
			obj.SetDeletionTimestamp(&metav1.Time{Time: deletionTimestamp})
			return obj, nil
		}

		if err := evt.DataAs(obj); err != nil {
			return zero, fmt.Errorf("failed to unmarshal event, %v", err)
		}

		return obj, nil
	}

	if err := evt.DataAs(obj); err != nil {
		return zero, fmt.Errorf("failed to unmarshal event, %v", err)
	}

	return obj, nil
}
