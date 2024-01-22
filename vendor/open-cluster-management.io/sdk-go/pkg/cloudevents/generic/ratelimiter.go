package generic

import (
	"time"

	"k8s.io/client-go/util/flowcontrol"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
)

// longThrottleLatency defines threshold for logging requests. All requests being
// throttled (via the provided rateLimiter) for more than longThrottleLatency will
// be logged.
const longThrottleLatency = 1 * time.Second

const (
	// TODO we may adjust these after performance test
	DefaultQPS   float32 = 50.0
	DefaultBurst int     = 100
)

func NewRateLimiter(limit options.EventRateLimit) flowcontrol.RateLimiter {
	qps := limit.QPS
	if qps <= 0.0 {
		qps = DefaultQPS
	}

	burst := limit.Burst
	if burst <= 0 {
		burst = DefaultBurst
	}

	return flowcontrol.NewTokenBucketRateLimiter(qps, burst)
}
