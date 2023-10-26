package metrics

import (
	"time"

	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/utils/clock"
)

const (
	// Constants for metric names.
	SchedulingName        = "placement_scheduling"
	SchedulingSubsystem   = "scheduling"
	SchedulingDurationKey = "scheduling_duration_seconds"
	BindDurationKey       = "bind_duration_seconds"
	PluginDurationKey     = "plugin_duration_seconds"
)

// Metric histograms for tracking various durations.
var (
	schedulingDuration = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
		Subsystem:      SchedulingSubsystem,
		Name:           SchedulingDurationKey,
		StabilityLevel: k8smetrics.ALPHA,
		Help:           "How long in seconds it takes to schedule a placement.",
		Buckets:        k8smetrics.ExponentialBuckets(10e-7, 10, 10),
	}, []string{"name"})

	bindDuration = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
		Subsystem:      SchedulingSubsystem,
		Name:           BindDurationKey,
		StabilityLevel: k8smetrics.ALPHA,
		Help:           "How long in seconds it takes to bind a placement to placementDecisions.",
		Buckets:        k8smetrics.ExponentialBuckets(10e-7, 10, 10),
	}, []string{"name"})

	PluginDuration = k8smetrics.NewHistogramVec(&k8smetrics.HistogramOpts{
		Subsystem:      SchedulingSubsystem,
		Name:           PluginDurationKey,
		StabilityLevel: k8smetrics.ALPHA,
		Help:           "How long in seconds a plugin runs for a placement.",
		Buckets:        k8smetrics.ExponentialBuckets(10e-7, 10, 10),
	}, []string{"name", "plugin_type", "plugin_name"})

	metrics = []k8smetrics.Registerable{
		schedulingDuration, bindDuration, PluginDuration,
	}
)

func init() {
	// Register metrics on initialization.
	for _, m := range metrics {
		legacyregistry.MustRegister(m)
	}
}

// HistogramMetric represents an interface for counting individual observations.
type HistogramMetric interface {
	Observe(float64)
}

// ScheduleMetrics holds the metrics and data related to scheduling and binding.
type ScheduleMetrics struct {
	clock              clock.Clock
	scheduling         HistogramMetric
	binding            HistogramMetric
	scheduleStartTimes map[string]time.Time
	bindStartTimes     map[string]time.Time
}

// NewScheduleMetrics creates a new ScheduleMetrics instance.
func NewScheduleMetrics(clock clock.Clock) *ScheduleMetrics {
	return &ScheduleMetrics{
		clock:              clock,
		scheduling:         schedulingDuration.WithLabelValues(SchedulingName),
		binding:            bindDuration.WithLabelValues(SchedulingName),
		scheduleStartTimes: map[string]time.Time{},
		bindStartTimes:     map[string]time.Time{},
	}
}

// StartSchedule marks the start time of scheduling for a given key.
func (m *ScheduleMetrics) StartSchedule(key string) {
	if m == nil {
		return
	}

	if _, exists := m.scheduleStartTimes[key]; !exists {
		m.scheduleStartTimes[key] = m.clock.Now()
	}
}

// SinceInSeconds returns the time duration in seconds since the provided start time.
func (m *ScheduleMetrics) SinceInSeconds(start time.Time) float64 {
	return m.clock.Since(start).Seconds()
}

// StartBind marks the start time of binding for a given key.
func (m *ScheduleMetrics) StartBind(key string) {
	if m == nil {
		return
	}

	m.bindStartTimes[key] = m.clock.Now()
	if startTime, exists := m.scheduleStartTimes[key]; exists {
		m.scheduling.Observe(m.SinceInSeconds(startTime))
		delete(m.scheduleStartTimes, key)
	}
}

// Done is called when a task is completed to record the duration.
func (m *ScheduleMetrics) Done(key string) {
	if m == nil {
		return
	}

	if startTime, exists := m.bindStartTimes[key]; exists {
		m.binding.Observe(m.SinceInSeconds(startTime))
		delete(m.bindStartTimes, key)
	}
}
