package codec

import (
	"encoding/json"
	"fmt"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ManifestBundleCodec is a codec to encode/decode a ManifestWork/cloudevent with ManifestBundle for a source.
type ManifestBundleCodec struct{}

func NewManifestBundleCodec() *ManifestBundleCodec {
	return &ManifestBundleCodec{}
}

// EventDataType always returns the event data type `io.open-cluster-management.works.v1alpha1.manifestbundles`.
func (c *ManifestBundleCodec) EventDataType() types.CloudEventsDataType {
	return payload.ManifestBundleEventDataType
}

// Encode the spec of a ManifestWork to a cloudevent with ManifestBundle.
func (c *ManifestBundleCodec) Encode(source string, eventType types.CloudEventsType, work *workv1.ManifestWork) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != payload.ManifestBundleEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithClusterName(work.Namespace).
		WithResourceID(string(work.UID)).
		WithResourceVersion(work.Generation).
		NewEvent()

	// set the work's meta data to its cloud event
	metaJson, err := json.Marshal(work.ObjectMeta)
	if err != nil {
		return nil, err
	}
	evt.SetExtension(types.ExtensionWorkMeta, string(metaJson))

	if !work.DeletionTimestamp.IsZero() {
		evt.SetExtension(types.ExtensionDeletionTimestamp, work.DeletionTimestamp.Time)
		return &evt, nil
	}

	manifests := &payload.ManifestBundle{
		Manifests:       work.Spec.Workload.Manifests,
		DeleteOption:    work.Spec.DeleteOption,
		ManifestConfigs: work.Spec.ManifestConfigs,
		Executer:        work.Spec.Executor,
	}
	if err := evt.SetData(cloudevents.ApplicationJSON, manifests); err != nil {
		return nil, fmt.Errorf("failed to encode manifestwork status to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent whose data is ManifestBundle to a ManifestWork.
func (c *ManifestBundleCodec) Decode(evt *cloudevents.Event) (*workv1.ManifestWork, error) {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return nil, fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}

	if eventType.CloudEventsDataType != payload.ManifestBundleEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evtExtensions := evt.Context.GetExtensions()

	resourceID, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionResourceID])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceid extension: %v", err)
	}

	resourceVersion, err := cloudeventstypes.ToInteger(evtExtensions[types.ExtensionResourceVersion])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceversion extension: %v", err)
	}

	sequenceID, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionStatusUpdateSequenceID])
	if err != nil {
		return nil, fmt.Errorf("failed to get sequenceid extension: %v", err)
	}

	metaObj := metav1.ObjectMeta{}

	// the agent sends the work meta data back, restore the meta to the received work, otherwise only set the
	// UID and ResourceVersion to the received work, for the work's other meta data will be got from the work
	// client local cache.
	if workMetaExtension, ok := evtExtensions[types.ExtensionWorkMeta]; ok {
		metaJson, err := cloudeventstypes.ToString(workMetaExtension)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(metaJson), &metaObj); err != nil {
			return nil, err
		}
	}

	metaObj.UID = kubetypes.UID(resourceID)
	if len(metaObj.Name) == 0 {
		metaObj.Name = resourceID
	}
	metaObj.Generation = int64(resourceVersion)
	if metaObj.Annotations == nil {
		metaObj.Annotations = map[string]string{}
	}
	metaObj.Annotations[common.CloudEventsSequenceIDAnnotationKey] = sequenceID

	work := &workv1.ManifestWork{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metaObj,
	}

	manifestStatus := &payload.ManifestBundleStatus{}
	if err := evt.DataAs(manifestStatus); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	// the agent sends the work spec back, restore it
	if manifestStatus.ManifestBundle != nil {
		work.Spec.Workload.Manifests = manifestStatus.ManifestBundle.Manifests
		work.Spec.DeleteOption = manifestStatus.ManifestBundle.DeleteOption
		work.Spec.ManifestConfigs = manifestStatus.ManifestBundle.ManifestConfigs
		work.Spec.Executor = manifestStatus.ManifestBundle.Executer
	}

	work.Status = workv1.ManifestWorkStatus{
		Conditions: manifestStatus.Conditions,
		ResourceStatus: workv1.ManifestResourceStatus{
			Manifests: manifestStatus.ResourceStatus,
		},
	}

	return work, nil
}
