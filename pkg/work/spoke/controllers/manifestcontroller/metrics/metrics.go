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
			Name:           "manifestwork_resource_apply_total",
			Help:           "Total number of ManifestWork resource apply results.",
			StabilityLevel: k8smetrics.ALPHA,
		},
		[]string{"outcome"},
	)

	ManifestWorkApplyTotal = k8smetrics.NewCounterVec(
		&k8smetrics.CounterOpts{
			Subsystem:      WorkSubsystem,
			Name:           "manifestwork_apply_total",
			Help:           "Total number of ManifestWork apply results recorded for generations not yet observed in persisted WorkApplied status; once a generation is persisted, later outcomes for the same generation are not counted, while status persistence failures may cause retries to be counted.",
			StabilityLevel: k8smetrics.ALPHA,
		},
		[]string{"outcome"},
	)
)

func init() {
	legacyregistry.MustRegister(
		ResourceApplyTotal,
		ManifestWorkApplyTotal,
	)
}
