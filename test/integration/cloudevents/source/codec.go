package source

import (
	"fmt"
	"strconv"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubetypes "k8s.io/apimachinery/pkg/types"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/payload"
)

type ManifestCodec struct{}

func (c *ManifestCodec) EventDataType() types.CloudEventsDataType {
	return payload.ManifestEventDataType
}

func (d *ManifestCodec) Encode(source string, eventType types.CloudEventsType, work *workv1.ManifestWork) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != payload.ManifestEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	if len(work.Spec.Workload.Manifests) != 1 {
		return nil, fmt.Errorf("too many manifests in the work")
	}

	eventBuilder := types.NewEventBuilder(source, eventType).
		WithResourceID(string(work.UID)).
		WithResourceVersion(work.Generation).
		WithClusterName(work.Namespace)

	if !work.GetDeletionTimestamp().IsZero() {
		evt := eventBuilder.WithDeletionTimestamp(work.GetDeletionTimestamp().Time).NewEvent()
		return &evt, nil
	}

	evt := eventBuilder.NewEvent()

	manifest := work.Spec.Workload.Manifests[0]
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to convert manifest to unstructured object: %v", err)
	}

	evtPayload := &payload.Manifest{
		Manifest:     unstructured.Unstructured{Object: unstructuredObj},
		DeleteOption: work.Spec.DeleteOption,
	}

	if len(work.Spec.ManifestConfigs) == 1 {
		evtPayload.ConfigOption = &payload.ManifestConfigOption{
			FeedbackRules:  work.Spec.ManifestConfigs[0].FeedbackRules,
			UpdateStrategy: work.Spec.ManifestConfigs[0].UpdateStrategy,
		}
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, evtPayload); err != nil {
		return nil, fmt.Errorf("failed to encode manifests to cloud event: %v", err)
	}

	return &evt, nil
}

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

	resourceVersionInt, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resourceversion - %v to int64", resourceVersion)
	}

	clusterName, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		return nil, fmt.Errorf("failed to get clustername extension: %v", err)
	}

	manifestStatus := &payload.ManifestStatus{}
	if err := evt.DataAs(manifestStatus); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	work := &workv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			UID:             kubetypes.UID(resourceID),
			ResourceVersion: resourceVersion,
			Generation:      resourceVersionInt,
			Namespace:       clusterName,
		},
		Status: workv1.ManifestWorkStatus{
			Conditions: manifestStatus.Conditions,
			ResourceStatus: workv1.ManifestResourceStatus{
				Manifests: []workv1.ManifestCondition{
					{
						Conditions:      manifestStatus.Status.Conditions,
						StatusFeedbacks: manifestStatus.Status.StatusFeedbacks,
						ResourceMeta:    manifestStatus.Status.ResourceMeta,
					},
				},
			},
		},
	}

	return work, nil
}

type ManifestBundleCodec struct{}

func (c *ManifestBundleCodec) EventDataType() types.CloudEventsDataType {
	return payload.ManifestBundleEventDataType
}

func (d *ManifestBundleCodec) Encode(source string, eventType types.CloudEventsType, work *workv1.ManifestWork) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != payload.ManifestBundleEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	eventBuilder := types.NewEventBuilder(source, eventType).
		WithResourceID(string(work.UID)).
		WithResourceVersion(work.Generation).
		WithClusterName(work.Namespace)

	if !work.GetDeletionTimestamp().IsZero() {
		evt := eventBuilder.WithDeletionTimestamp(work.GetDeletionTimestamp().Time).NewEvent()
		return &evt, nil
	}

	evt := eventBuilder.NewEvent()

	if err := evt.SetData(cloudevents.ApplicationJSON, &payload.ManifestBundle{Manifests: work.Spec.Workload.Manifests}); err != nil {
		return nil, fmt.Errorf("failed to encode manifests to cloud event: %v", err)
	}

	return &evt, nil
}

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

	resourceVersion, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionResourceVersion])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceversion extension: %v", err)
	}

	resourceVersionInt, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resourceversion - %v to int64", resourceVersion)
	}

	clusterName, err := cloudeventstypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		return nil, fmt.Errorf("failed to get clustername extension: %v", err)
	}

	manifestStatus := &payload.ManifestBundleStatus{}
	if err := evt.DataAs(manifestStatus); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	work := &workv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			UID:             kubetypes.UID(resourceID),
			ResourceVersion: resourceVersion,
			Generation:      resourceVersionInt,
			Namespace:       clusterName,
		},
		Status: workv1.ManifestWorkStatus{
			Conditions: manifestStatus.Conditions,
			ResourceStatus: workv1.ManifestResourceStatus{
				Manifests: manifestStatus.ResourceStatus,
			},
		},
	}

	return work, nil
}
