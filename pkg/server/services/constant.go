package services

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
)

const (
	CloudEventsSourceKube = "kube"
)

func ValidateClusterName(evt *cloudevents.Event, resourceClusterName string) error {
	if evt == nil {
		return fmt.Errorf("event is nil")
	}

	clusterName, err := cloudeventstypes.ToString(evt.Extensions()[types.ExtensionClusterName])
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %v", err)
	}
	if clusterName == "" {
		return fmt.Errorf("cluster name is empty")
	}
	if resourceClusterName != clusterName {
		return fmt.Errorf("resource cluster name %q does not match event cluster name %q", resourceClusterName, clusterName)
	}

	return nil
}

func ValidateClusterIdentity(ctx context.Context, resourceClusterName string) error {
	return authn.ValidateClusterIdentity(ctx, resourceClusterName)
}
