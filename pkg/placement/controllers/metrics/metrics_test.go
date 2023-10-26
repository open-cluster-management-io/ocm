package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/component-base/metrics/legacyregistry"
	testingclock "k8s.io/utils/clock/testing"
)

func TestMetrics(t *testing.T) {
	t0 := time.Unix(0, 0)
	c := testingclock.NewFakeClock(t0)

	metrics := NewScheduleMetrics(c)

	metrics.StartSchedule("test")
	c.Step(50 * time.Second)
	metrics.StartBind("test")
	c.Step(30 * time.Second)
	metrics.Done("test")

	metrics.StartSchedule("test1")
	c.Step(50 * time.Second)
	metrics.StartBind("test1")
	c.Step(30 * time.Second)
	metrics.Done("test1")

	startTime := c.Now()
	c.Step(50 * time.Second)
	PluginDuration.With(prometheus.Labels{
		"name":        SchedulingName,
		"plugin_type": "filter",
		"plugin_name": "fakePlugin1",
	}).Observe(metrics.SinceInSeconds(startTime))
	c.Step(30 * time.Second)
	PluginDuration.With(prometheus.Labels{
		"name":        SchedulingName,
		"plugin_type": "prioritizer",
		"plugin_name": "fakePlugin2",
	}).Observe(metrics.SinceInSeconds(startTime))

	mfs, err := legacyregistry.DefaultGatherer.Gather()
	if err != nil {
		t.Errorf("failed to gather metrics")
	}

	for _, mf := range mfs {
		if *mf.Name == SchedulingSubsystem+"_"+BindDurationKey {
			mfMetric := mf.GetMetric()
			for _, m := range mfMetric {
				if m.GetHistogram().GetSampleCount() != 2 {
					t.Errorf("sample count is not correct")
				}
			}
		}
		if *mf.Name == SchedulingSubsystem+"_"+PluginDurationKey {
			mfMetric := mf.GetMetric()
			if len(mfMetric) != 2 {
				t.Errorf("plugin metrics count is not correct")
			}
			for _, m := range mfMetric {
				if m.GetHistogram().GetSampleCount() != 1 {
					t.Errorf("plugin sample count is not correct")
				}
			}
		}
	}
}
