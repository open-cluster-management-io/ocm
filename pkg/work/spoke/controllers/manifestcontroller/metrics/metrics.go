package metrics

import (
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	WorkSubsystem = "work"
)

var (
	ResourceApplyTotal = k8smetrics.NewCounterVec(
		&k8smetrics.CounterOpts{
			Subsystem:      WorkSubsystem,
			Name:           "resource_apply_total",
			Help:           "Total number of ManifestWork resource apply results.",
			StabilityLevel: k8smetrics.ALPHA,
		},
		[]string{"outcome"},
	)
)

func init() {
	legacyregistry.MustRegister(ResourceApplyTotal)
}
