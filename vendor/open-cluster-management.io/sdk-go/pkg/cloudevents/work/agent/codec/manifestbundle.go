package codec

import (
	"fmt"
	"strconv"

	"github.com/bwmarrin/snowflake"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/apis/work/v1/validator"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/statushash"
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

// ManifestBundleCodec is a codec to encode/decode a ManifestWork/cloudevent with ManifestBundle for an agent.
type ManifestBundleCodec struct{}

func NewManifestBundleCodec() *ManifestBundleCodec {
	return &ManifestBundleCodec{}
}

// EventDataType always returns the event data type `io.open-cluster-management.works.v1alpha1.manifestbundles`.
func (c *ManifestBundleCodec) EventDataType() types.CloudEventsDataType {
	return payload.ManifestBundleEventDataType
}

// Encode the status of a ManifestWork to a cloudevent with ManifestBundleStatus.
func (c *ManifestBundleCodec) Encode(source string, eventType types.CloudEventsType, work *workv1.ManifestWork) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != payload.ManifestBundleEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	resourceVersion, err := strconv.ParseInt(work.ResourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the resourceversion of the work %s, %v", work.UID, err)
	}

	originalSource, ok := work.Labels[common.CloudEventsOriginalSourceLabelKey]
	if !ok {
		return nil, fmt.Errorf("failed to find originalsource from the work %s", work.UID)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(string(work.UID)).
		WithStatusUpdateSequenceID(sequenceGenerator.Generate().String()).
		WithResourceVersion(resourceVersion).
		WithClusterName(work.Namespace).
		WithOriginalSource(originalSource).
		NewEvent()

	statusHash, err := statushash.ManifestWorkStatusHash(work)
	if err != nil {
		return nil, err
	}

	evt.SetExtension(types.ExtensionStatusHash, statusHash)

	manifestBundleStatus := &payload.ManifestBundleStatus{
		Conditions:     work.Status.Conditions,
		ResourceStatus: work.Status.ResourceStatus.Manifests,
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, manifestBundleStatus); err != nil {
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

	clusterName, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		return nil, fmt.Errorf("failed to get clustername extension: %v", err)
	}

	work := &workv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			UID:             kubetypes.UID(resourceID),
			ResourceVersion: fmt.Sprintf("%d", resourceVersion),
			Name:            resourceID,
			Namespace:       clusterName,
			Labels: map[string]string{
				common.CloudEventsOriginalSourceLabelKey: evt.Source(),
			},
			Annotations: map[string]string{
				common.CloudEventsDataTypeAnnotationKey: eventType.CloudEventsDataType.String(),
			},
		},
	}

	if _, ok := evtExtensions[types.ExtensionDeletionTimestamp]; ok {
		deletionTimestamp, err := cloudeventstypes.ToTime(evtExtensions[types.ExtensionDeletionTimestamp])
		if err != nil {
			return nil, fmt.Errorf("failed to get deletiontimestamp, %v", err)
		}

		// In the case of an agent restart, the manifestwork finalizer is cleared.
		// Explicitly re-add the finalizer to ensure proper cleanup of the manifestwork.
		work.Finalizers = []string{workv1.ManifestWorkFinalizer}
		work.DeletionTimestamp = &metav1.Time{Time: deletionTimestamp}
		return work, nil
	}

	manifests := &payload.ManifestBundle{}
	if err := evt.DataAs(manifests); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	work.Spec = workv1.ManifestWorkSpec{
		Workload: workv1.ManifestsTemplate{
			Manifests: manifests.Manifests,
		},
		DeleteOption:    manifests.DeleteOption,
		ManifestConfigs: manifests.ManifestConfigs,
	}

	// validate the manifests
	if err := validator.ManifestValidator.ValidateManifests(work.Spec.Workload.Manifests); err != nil {
		return nil, fmt.Errorf("manifests are invalid, %v", err)
	}

	return work, nil
}
