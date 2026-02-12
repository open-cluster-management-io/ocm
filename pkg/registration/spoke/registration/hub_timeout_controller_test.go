package registration

import (
	"context"
	"testing"
	"time"

	kubefake "k8s.io/client-go/kubernetes/fake"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestHubTimeoutController_Sync(t *testing.T) {
	cases := []struct {
		name            string
		leaseAgeSeconds int
		timeoutSeconds  int32
		expect          bool
	}{
		{
			name:            "not timeout",
			leaseAgeSeconds: 2,
			timeoutSeconds:  5,
			expect:          false,
		},
		{
			name:            "timeout",
			leaseAgeSeconds: 6,
			timeoutSeconds:  5,
			expect:          true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Set lease renew time in the past to simulate aging
			leaseRenewTime := time.Now().Add(-time.Duration(c.leaseAgeSeconds) * time.Second)

			lease := testinghelpers.NewManagedClusterLease("managed-cluster-lease", leaseRenewTime)
			leaseClient := kubefake.NewClientset(lease)

			handled := false
			controller := &hubTimeoutController{
				leaseClient: leaseClient.CoordinationV1().Leases(testinghelpers.TestManagedClusterName),
				handleTimeout: func(ctx context.Context) error {
					handled = true
					return nil
				},
				clusterName:    testinghelpers.TestManagedClusterName,
				timeoutSeconds: c.timeoutSeconds,
				// Set startTime in the past to bypass the 10s grace period
				startTime: time.Now().Add(-time.Second * 15),
			}

			err := controller.sync(context.Background(), testingcommon.NewFakeSyncContext(t, ""), "")
			if err != nil {
				t.Fatal(err)
			}

			if handled != c.expect {
				t.Errorf("expect %v, but got %v", c.expect, handled)
			}
		})
	}
}

func TestLeaseUpdater_isTimeout(t *testing.T) {
	leaseRenewTime := time.Now()
	testcases := []struct {
		name           string
		reconcileTime  time.Time
		timeoutSeconds int32
		expect         bool
	}{
		{
			name:           "not timeout yet",
			timeoutSeconds: 3,
			reconcileTime:  leaseRenewTime.Add(time.Second * 2),
			expect:         false,
		},
		{
			name:           "timeout",
			timeoutSeconds: 3,
			reconcileTime:  leaseRenewTime.Add(time.Second * 4),
			expect:         true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if expect := isTimeout(tc.reconcileTime, leaseRenewTime, tc.timeoutSeconds); expect != tc.expect {
				t.Errorf("expect %v, but got %v", tc.expect, expect)
			}
		})
	}
}
