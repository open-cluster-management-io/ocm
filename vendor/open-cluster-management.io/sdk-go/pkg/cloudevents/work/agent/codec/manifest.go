package codec

import (
	"fmt"
	"strconv"

	"github.com/bwmarrin/snowflake"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"open-cluster-management.io/api/utils/work/v1/utils"
	"open-cluster-management.io/api/utils/work/v1/workvalidator"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/payload"
)

const (
	// CloudEventsDataTypeAnnotationKey is the key of the cloudevents data type annotation.
	CloudEventsDataTypeAnnotationKey = "cloudevents.open-cluster-management.io/datatype"

	// CloudEventsDataTypeAnnotationKey is the key of the cloudevents original source annotation.
	CloudEventsOriginalSourceAnnotationKey = "cloudevents.open-cluster-management.io/originalsource"
)

var sequenceGenerator *snowflake.Node

func init() {
	// init the snowflake id generator with node id 1 for each single agent. Each single agent has its own consumer id
	// to be identified, and we can ensure the order of status update event from the same agent via sequence id. The
	// events from different agents are independent, hence the ordering among them needs not to be guaranteed.
	//
	// The snowflake `NewNode` returns error only when the snowflake node id is less than 1 or great than 1024, so the
	// error can be ignored here.
	sequenceGenerator, _ = snowflake.NewNode(1)
}

// ManifestCodec is a codec to encode/decode a ManifestWork/cloudevent with ManifestBundle for an agent.
type ManifestCodec struct {
	restMapper meta.RESTMapper
}

func NewManifestCodec(restMapper meta.RESTMapper) *ManifestCodec {
	return &ManifestCodec{
		restMapper: restMapper,
	}
}

// EventDataType returns the event data type for `io.open-cluster-management.works.v1alpha1.manifests`.
func (c *ManifestCodec) EventDataType() types.CloudEventsDataType {
	return payload.ManifestEventDataType
}

// Encode the status of a ManifestWork to a cloudevent with ManifestStatus.
func (c *ManifestCodec) Encode(source string, eventType types.CloudEventsType, work *workv1.ManifestWork) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != payload.ManifestEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	resourceVersion, err := strconv.ParseInt(work.ResourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the resourceversion of the work %s, %v", work.UID, err)
	}

	originalSource, ok := work.Annotations[CloudEventsOriginalSourceAnnotationKey]
	if !ok {
		return nil, fmt.Errorf("failed to find originalsource from the work %s", work.UID)
	}

	if len(work.Spec.Workload.Manifests) != 1 {
		return nil, fmt.Errorf("too many manifests in the work %s", work.UID)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(string(work.UID)).
		WithStatusUpdateSequenceID(sequenceGenerator.Generate().String()).
		WithResourceVersion(resourceVersion).
		WithClusterName(work.Namespace).
		WithOriginalSource(originalSource).
		NewEvent()

	statusPayload := &payload.ManifestStatus{
		Conditions: work.Status.Conditions,
	}

	if len(work.Status.ResourceStatus.Manifests) != 0 {
		statusPayload.Status = &work.Status.ResourceStatus.Manifests[0]
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, statusPayload); err != nil {
		return nil, fmt.Errorf("failed to encode manifestwork status to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent whose data is Manifest to a ManifestWork.
func (c *ManifestCodec) Decode(evt *cloudevents.Event) (*workv1.ManifestWork, error) {
	eventType, err := types.ParseCloudEventsType(evt.Type())
	if err != nil {
		return nil, fmt.Errorf("failed to parse cloud event type %s, %v", evt.Type(), err)
	}

	if eventType.CloudEventsDataType != payload.ManifestEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evtExtensions := evt.Context.GetExtensions()

	resourceID, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionResourceID])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceid extension: %v", err)
	}

	resourceVersion, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionResourceVersion])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceversion extension: %v", err)
	}

	clusterName, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		return nil, fmt.Errorf("failed to get clustername extension: %v", err)
	}

	work := &workv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			UID:             kubetypes.UID(resourceID),
			ResourceVersion: resourceVersion,
			Name:            resourceID,
			Namespace:       clusterName,
			Annotations: map[string]string{
				CloudEventsDataTypeAnnotationKey:       eventType.CloudEventsDataType.String(),
				CloudEventsOriginalSourceAnnotationKey: evt.Source(),
			},
		},
	}

	if _, ok := evtExtensions[types.ExtensionDeletionTimestamp]; ok {
		deletionTimestamp, err := cloudeventstypes.ToTime(evtExtensions[types.ExtensionDeletionTimestamp])
		if err != nil {
			return nil, fmt.Errorf("failed to get deletiontimestamp, %v", err)
		}

		work.DeletionTimestamp = &metav1.Time{Time: deletionTimestamp}
		return work, nil
	}

	manifestPayload := &payload.Manifest{}
	if err := evt.DataAs(manifestPayload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	unstructuredObj := manifestPayload.Manifest
	rawJson, err := unstructuredObj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest GVR from event %s, %v", string(evt.Data()), err)
	}

	work.Spec = workv1.ManifestWorkSpec{
		Workload: workv1.ManifestsTemplate{
			Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{Raw: rawJson}}},
		},
		DeleteOption: manifestPayload.DeleteOption,
	}

	if manifestPayload.ConfigOption != nil {
		_, gvr, err := utils.BuildResourceMeta(0, &unstructuredObj, c.restMapper)
		if err != nil {
			return nil, fmt.Errorf("failed to get manifest GVR from event %s, %v", string(evt.Data()), err)
		}

		work.Spec.ManifestConfigs = []workv1.ManifestConfigOption{
			{
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:     gvr.Group,
					Resource:  gvr.Resource,
					Name:      unstructuredObj.GetName(),
					Namespace: unstructuredObj.GetNamespace(),
				},
				FeedbackRules:  manifestPayload.ConfigOption.FeedbackRules,
				UpdateStrategy: manifestPayload.ConfigOption.UpdateStrategy,
			},
		}
	}

	// validate the manifest
	if err := workvalidator.ManifestValidator.ValidateManifests(work.Spec.Workload.Manifests); err != nil {
		return nil, fmt.Errorf("manifest is invalid, %v", err)
	}

	return work, nil
}
