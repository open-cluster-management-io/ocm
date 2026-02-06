package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/utils"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	genericutils "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/utils"
)

var ManagedClusterAddOnEventDataType = types.CloudEventsDataType{
	Group:    addonapiv1alpha1.GroupVersion.Group,
	Version:  addonapiv1alpha1.GroupVersion.Version,
	Resource: "managedclusteraddons",
}

// ManagedClusterAddOnCodec is a codec to encode/decode a ManagedClusterAddOn/cloudevent for an agent.
type ManagedClusterAddOnCodec struct{}

func NewManagedClusterAddOnCodec() *ManagedClusterAddOnCodec {
	return &ManagedClusterAddOnCodec{}
}

// EventDataType always returns the event data type `addon.open-cluster-management.io.v1alpha1.managedclusteraddons`.
func (c *ManagedClusterAddOnCodec) EventDataType() types.CloudEventsDataType {
	return ManagedClusterAddOnEventDataType
}

// Encode the ManagedClusterAddOn to a cloudevent
func (c *ManagedClusterAddOnCodec) Encode(source string, eventType types.CloudEventsType, addon *addonapiv1alpha1.ManagedClusterAddOn) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != ManagedClusterAddOnEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(string(addon.UID)).
		WithClusterName(addon.Namespace).
		NewEvent()

	genericutils.SetResourceVersion(eventType, &evt, addon)

	if !addon.DeletionTimestamp.IsZero() {
		evt.SetExtension(types.ExtensionDeletionTimestamp, addon.DeletionTimestamp.Time)
		return &evt, nil
	}

	newAddon := addon.DeepCopy()
	newAddon.TypeMeta = metav1.TypeMeta{
		APIVersion: addonapiv1alpha1.GroupVersion.String(),
		Kind:       "ManagedClusterAddOn",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newAddon); err != nil {
		return nil, fmt.Errorf("failed to encode managedclusteraddon to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to a ManagedClusterAddOn
func (c *ManagedClusterAddOnCodec) Decode(evt *cloudevents.Event) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	return utils.DecodeWithDeletionHandling(evt, func() *addonapiv1alpha1.ManagedClusterAddOn {
		return &addonapiv1alpha1.ManagedClusterAddOn{}
	})
}
