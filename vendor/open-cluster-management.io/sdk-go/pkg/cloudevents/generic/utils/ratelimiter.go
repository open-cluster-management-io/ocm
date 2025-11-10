package utils

import (
	"time"

	"k8s.io/client-go/util/flowcontrol"
)

// LongThrottleLatency defines threshold for logging requests. All requests being
// throttled (via the provided rateLimiter) for more than LongThrottleLatency will
// be logged.
const LongThrottleLatency = 1 * time.Second

const (
	// TODO we may adjust these after performance test
	DefaultQPS   float32 = 50.0
	DefaultBurst int     = 100
)

// EventRateLimit for limiting the event sending rate.
type EventRateLimit struct {
	// QPS indicates the maximum QPS to send the event.
	// If it's less than or equal to zero, the DefaultQPS (50) will be used.
	QPS float32

	// Maximum burst for throttle.
	// If it's less than or equal to zero, the DefaultBurst (100) will be used.
	Burst int
}

func NewRateLimiter(limit EventRateLimit) flowcontrol.RateLimiter {
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
