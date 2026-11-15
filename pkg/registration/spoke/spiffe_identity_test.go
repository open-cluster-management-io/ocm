package spoke

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/features"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
)

func TestWaitForIdentity(t *testing.T) {
	identityPollInterval = 10 * time.Millisecond
	defer func() { identityPollInterval = 5 * time.Second }()

	// The identity shows up on the third check: the agent goes on.
	calls := 0
	stop, err := waitForIdentity(context.Background(), func(context.Context) (bool, error) {
		calls++
		return calls == 3, nil
	})
	if stop || err != nil || calls != 3 {
		t.Errorf("stop %v, err %v after %d checks", stop, err, calls)
	}

	// The agent is stopped while waiting: it stops without an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if stop, err := waitForIdentity(ctx, func(context.Context) (bool, error) { return false, nil }); !stop || err != nil {
		t.Errorf("stop %v, err %v when the context ends", stop, err)
	}

	// The check fails: the agent stops with the error.
	stop, err = waitForIdentity(context.Background(), func(context.Context) (bool, error) {
		return false, errors.New("workload API broken")
	})
	if !stop || err == nil || !strings.Contains(err.Error(), "workload API broken") {
		t.Errorf("stop %v, err %v when the check fails", stop, err)
	}
}

// MultipleHubs picks one of several bootstrap kubeconfigs; a spiffe agent has none.
func TestValidateMultipleHubsWithSPIFFE(t *testing.T) {
	defer func() {
		_ = features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{string(ocmfeature.MultipleHubs): false})
	}()
	_ = features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{string(ocmfeature.MultipleHubs): true})
	o := NewSpokeAgentOptions()
	o.RegisterDriverOption = registerfactory.NewOptions()
	o.RegisterDriverOption.RegistrationAuth = operatorv1.SPIFFEAuthType
	if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "MultipleHubs") {
		t.Errorf("Validate: %v", err)
	}
}
