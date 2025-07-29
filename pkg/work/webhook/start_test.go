package webhook

import (
	"testing"

	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
)

func TestOptions_SetupWebhookServer(t *testing.T) {
	opts := commonoptions.NewWebhookOptions()
	err := features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates)
	if err != nil {
		t.Fatal(err)
	}
	o := NewOptions()
	err = o.SetupWebhookServer(opts)
	if err != nil {
		t.Errorf("SetupWebhookServer() error = %v, wantErr %v", err, nil)
	}
}
