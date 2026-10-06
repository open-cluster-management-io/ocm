package authn

import (
	"context"
	"fmt"
	"strings"
)

const clusterIdentityPrefix = "system:open-cluster-management:"

func ValidateClusterIdentity(ctx context.Context, clusterName string) error {
	identity, ok := ctx.Value(ContextUserKey).(string)
	if !ok || identity == "" {
		return fmt.Errorf("no authenticated identity in context")
	}
	if !strings.HasPrefix(identity, clusterIdentityPrefix) {
		return nil
	}

	parts := strings.Split(strings.TrimPrefix(identity, clusterIdentityPrefix), ":")
	var authenticatedCluster string
	switch {
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		authenticatedCluster = parts[0]
	case len(parts) == 6 && parts[0] == "cluster" && parts[1] != "" &&
		parts[2] == "addon" && parts[3] != "" && parts[4] == "agent" && parts[5] != "":
		authenticatedCluster = parts[1]
	default:
		return fmt.Errorf("identity %q does not encode a cluster name", identity)
	}
	if authenticatedCluster != clusterName {
		return fmt.Errorf("identity %q is not allowed to act on cluster %q", identity, clusterName)
	}
	return nil
}
