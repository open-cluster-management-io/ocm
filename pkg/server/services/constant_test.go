package services

import (
	"context"
	"strings"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
)

func TestValidateClusterName(t *testing.T) {
	newEvent := func(clusterName any) *cloudevents.Event {
		evt := cloudevents.NewEvent()
		if clusterName != nil {
			evt.SetExtension(types.ExtensionClusterName, clusterName)
		}
		return &evt
	}

	cases := []struct {
		name                string
		evt                 *cloudevents.Event
		resourceClusterName string
		expectedError       string
	}{
		{
			name:          "nil event",
			expectedError: "event is nil",
		},
		{
			name:                "missing cluster name extension",
			evt:                 newEvent(nil),
			resourceClusterName: "cluster1",
			expectedError:       "failed to get cluster name",
		},
		{
			name:                "non-string cluster name extension",
			evt:                 newEvent(1),
			resourceClusterName: "cluster1",
			expectedError:       "failed to get cluster name",
		},
		{
			name:                "empty cluster name extension",
			evt:                 newEvent(""),
			resourceClusterName: "cluster1",
			expectedError:       "cluster name is empty",
		},
		{
			name:                "cluster name mismatch",
			evt:                 newEvent("cluster1"),
			resourceClusterName: "cluster2",
			expectedError:       `resource cluster name "cluster2" does not match event cluster name "cluster1"`,
		},
		{
			name:                "cluster name matches",
			evt:                 newEvent("cluster1"),
			resourceClusterName: "cluster1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateClusterName(c.evt, c.resourceClusterName)
			if c.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.expectedError) {
				t.Fatalf("expected error containing %q, got %v", c.expectedError, err)
			}
		})
	}
}

func TestValidateClusterIdentity(t *testing.T) {
	cases := []struct {
		name          string
		identity      string
		clusterName   string
		expectedError bool
	}{
		{
			name:          "no identity",
			clusterName:   "cluster1",
			expectedError: true,
		},
		{
			name:        "cluster agent identity matches",
			identity:    "system:open-cluster-management:cluster1:agent1",
			clusterName: "cluster1",
		},
		{
			name:          "cluster agent identity for another cluster",
			identity:      "system:open-cluster-management:cluster2:agent1",
			clusterName:   "cluster1",
			expectedError: true,
		},
		{
			name:        "non cluster identity is not bound to a cluster",
			identity:    "system:serviceaccount:open-cluster-management:agent-registration-bootstrap",
			clusterName: "cluster1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if c.identity != "" {
				ctx = context.WithValue(ctx, authn.ContextUserKey, c.identity)
			}
			err := ValidateClusterIdentity(ctx, c.clusterName)
			if c.expectedError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.expectedError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
