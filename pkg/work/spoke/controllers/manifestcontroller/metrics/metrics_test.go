package metrics

import (
	"testing"

	"k8s.io/component-base/metrics/legacyregistry"
)

func TestResourceApplyTotal(t *testing.T) {
	ResourceApplyTotal.WithLabelValues("applied").Inc()
	ResourceApplyTotal.WithLabelValues("failed").Inc()
	ResourceApplyTotal.WithLabelValues("read_only").Inc()

	mfs, err := legacyregistry.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	expected := map[string]bool{
		"applied":   false,
		"failed":    false,
		"read_only": false,
	}

	for _, mf := range mfs {
		if mf.GetName() != "work_resource_apply_total" {
			continue
		}

		for _, metric := range mf.GetMetric() {
			outcome := ""

			for _, label := range metric.GetLabel() {
				if label.GetName() == "outcome" {
					outcome = label.GetValue()
					break
				}
			}

			if _, ok := expected[outcome]; !ok {
				continue
			}

			if metric.GetCounter().GetValue() < 1 {
				t.Errorf("outcome %q counter was not incremented", outcome)
			}

			expected[outcome] = true
		}
	}

	for outcome, found := range expected {
		if !found {
			t.Errorf("metric for outcome %q was not found", outcome)
		}
	}
}
func TestManifestWorkApplyTotal(t *testing.T) {
	ManifestWorkApplyTotal.WithLabelValues("applied").Inc()
	ManifestWorkApplyTotal.WithLabelValues("failed").Inc()

	mfs, err := legacyregistry.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	expected := map[string]bool{
		"applied": false,
		"failed":  false,
	}

	for _, mf := range mfs {
		if mf.GetName() != "work_manifestwork_apply_total" {
			continue
		}

		for _, metric := range mf.GetMetric() {
			outcome := ""

			for _, label := range metric.GetLabel() {
				if label.GetName() == "outcome" {
					outcome = label.GetValue()
					break
				}
			}

			if _, ok := expected[outcome]; !ok {
				continue
			}

			if metric.GetCounter().GetValue() < 1 {
				t.Errorf("outcome %q counter was not incremented", outcome)
			}

			expected[outcome] = true
		}
	}

	for outcome, found := range expected {
		if !found {
			t.Errorf("metric for outcome %q was not found", outcome)
		}
	}
}
