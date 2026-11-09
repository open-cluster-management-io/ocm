// Copyright Contributors to the Open Cluster Management project

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSimulatorConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    SimulatorConfig
		wantError bool
	}{
		{name: "within limits", config: SimulatorConfig{Clusters: 100, WorksPerCluster: 10, UpdatesPerSecond: 20}},
		{name: "cluster lower bound", config: SimulatorConfig{Clusters: 0, WorksPerCluster: 1}, wantError: true},
		{name: "cluster upper bound", config: SimulatorConfig{Clusters: 1001, WorksPerCluster: 1}, wantError: true},
		{name: "total work safety cap", config: SimulatorConfig{Clusters: 1000, WorksPerCluster: 11}, wantError: true},
		{name: "rate upper bound", config: SimulatorConfig{Clusters: 1, WorksPerCluster: 1, UpdatesPerSecond: 101}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestBuildManifestWorkIsReproducibleAndContainsOnlySyntheticAppKinds(t *testing.T) {
	first := buildManifestWork("abc12345", "sim-abc12345-c0001", 7)
	second := buildManifestWork("abc12345", "sim-abc12345-c0001", 7)
	if first.Name != "sim-work-0007" || first.Namespace != "sim-abc12345-c0001" {
		t.Fatalf("unexpected work identity: %s/%s", first.Namespace, first.Name)
	}
	if len(first.Spec.Workload.Manifests) < 5 || len(first.Spec.Workload.Manifests) > 8 {
		t.Fatalf("unexpected manifest count %d", len(first.Spec.Workload.Manifests))
	}
	kinds := map[string]int{}
	for i, manifest := range first.Spec.Workload.Manifests {
		if string(manifest.Raw) != string(second.Spec.Workload.Manifests[i].Raw) {
			t.Fatal("same run and index should produce the same workload")
		}
		var object map[string]any
		if err := json.Unmarshal(manifest.Raw, &object); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		kind, _ := object["kind"].(string)
		kinds[kind]++
		if kind == "Secret" {
			secret, _ := object["stringData"].(map[string]any)
			if secret["password"] == "" || secret["password"] == nil {
				t.Fatal("synthetic Secret has no fake password")
			}
		}
	}
	for _, required := range []string{"Deployment", "Service", "Ingress", "ConfigMap", "Secret"} {
		if kinds[required] == 0 {
			t.Errorf("missing %s from generated application", required)
		}
	}
}

func TestEstimatorRequiresVariedBaselineAndPredictsUncertainty(t *testing.T) {
	if estimate := NewEstimator(nil).Predict(SimulatorConfig{Clusters: 1, WorksPerCluster: 1}); estimate.Calibrated {
		t.Fatal("empty baseline must remain uncalibrated")
	}
	var points []BaselinePoint
	for clusters := 1; clusters <= 4; clusters++ {
		for works := 1; works <= 3; works++ {
			for tps := 0; tps <= 2; tps++ {
				load := float64(clusters * works)
				points = append(points, BaselinePoint{Clusters: clusters, WorksPerCluster: works, UpdatesPerSecond: float64(tps), APICPU: 0.2 + 0.01*float64(clusters) + 0.001*load + 0.002*float64(tps), APIMemory: 100 + 2*float64(clusters) + 0.1*load, EtcdCPU: 0.1 + 0.005*float64(clusters) + 0.0005*load + 0.001*float64(tps), EtcdMemory: 50 + float64(clusters) + 0.05*load})
			}
		}
	}
	estimate := NewEstimator(points).Predict(SimulatorConfig{Clusters: 2, WorksPerCluster: 2, UpdatesPerSecond: 1})
	if !estimate.Calibrated {
		t.Fatalf("baseline should fit: %s", estimate.Message)
	}
	if estimate.API.CPUCore == nil || *estimate.API.CPUCore <= 0 {
		t.Fatalf("API CPU estimate missing: %#v", estimate.API)
	}
	if estimate.APIRange.CPULow == nil || estimate.APIRange.CPUHigh == nil || *estimate.APIRange.CPUHigh < *estimate.API.CPUCore {
		t.Fatalf("API CPU uncertainty range missing: %#v", estimate.APIRange)
	}
	if estimate.WeakFit {
		t.Fatalf("exact synthetic baseline should not be reported as weak: R²=%f", estimate.R2)
	}
	extrapolated := NewEstimator(points).Predict(SimulatorConfig{Clusters: 5, WorksPerCluster: 1, UpdatesPerSecond: 1})
	if !extrapolated.Extrapolated {
		t.Fatal("prediction outside the measured cluster range must be marked as extrapolated")
	}
}

func TestProvisionalDirectEstimateUsesSelectedLoadAndEditableAssumptions(t *testing.T) {
	config := SimulatorConfig{Clusters: 3, WorksPerCluster: 2, UpdatesPerSecond: 5}
	assumptions := EstimateAssumptions{
		APICPUPerCluster: 0.1, APIMemoryMiBPerCluster: 10, APICPUPerWork: 0.01, APIMemoryMiBPerWork: 1,
		EtcdCPUPerCluster: 0.05, EtcdMemoryMiBPerCluster: 5, EtcdCPUPerWork: 0.005, EtcdMemoryMiBPerWork: 0.5,
	}
	baselineAPI := MetricSet{CPUCore: floatPointer(0.5), MemoryBytes: floatPointer(100 * 1024 * 1024)}
	baselineEtcd := MetricSet{CPUCore: floatPointer(0.25), MemoryBytes: floatPointer(50 * 1024 * 1024)}
	estimate := provisionalDirectEstimate(config, assumptions, baselineAPI, baselineEtcd, true)
	if !estimate.Provisional || estimate.Calibrated {
		t.Fatalf("expected an explicitly provisional estimate: %#v", estimate)
	}
	if got := *estimate.API.CPUCore; math.Abs(got-0.86) > 1e-9 {
		t.Fatalf("API CPU estimate = %f, want 0.86", got)
	}
	if got := *estimate.Etcd.CPUCore; math.Abs(got-0.43) > 1e-9 {
		t.Fatalf("etcd CPU estimate = %f, want 0.43", got)
	}
	if estimate.APIRange.CPUHigh == nil || *estimate.APIRange.CPUHigh <= *estimate.API.CPUCore {
		t.Fatal("provisional estimate should expose an assumption range")
	}
}

func TestProvisionalEstimateIncludesUpdateRateTerm(t *testing.T) {
	config := SimulatorConfig{Clusters: 1, WorksPerCluster: 1, UpdatesPerSecond: 10}
	withRate := DefaultEstimateAssumptions()
	withoutRate := withRate
	withoutRate.APICPUPerUpdatePerSecond = 0
	withoutRate.APIMemoryMiBPerUpdatePerSecond = 0
	withoutRate.EtcdCPUPerUpdatePerSecond = 0
	withoutRate.EtcdMemoryMiBPerUpdatePerSecond = 0
	base := MetricSet{CPUCore: floatPointer(0.1), MemoryBytes: floatPointer(100 * 1024 * 1024)}
	plain := provisionalDirectEstimate(config, withoutRate, base, base, true)
	rated := provisionalDirectEstimate(config, withRate, base, base, true)
	if expected := *plain.API.CPUCore + 10*withRate.APICPUPerUpdatePerSecond; math.Abs(*rated.API.CPUCore-expected) > 1e-9 {
		t.Fatalf("API CPU estimate = %f, want %f", *rated.API.CPUCore, expected)
	}
	if expected := *plain.Etcd.CPUCore + 10*withRate.EtcdCPUPerUpdatePerSecond; math.Abs(*rated.Etcd.CPUCore-expected) > 1e-9 {
		t.Fatalf("etcd CPU estimate = %f, want %f", *rated.Etcd.CPUCore, expected)
	}
}

func TestProvisionalEstimateDefaultsReproduceBothBenchmarkPoints(t *testing.T) {
	assumptions := DefaultEstimateAssumptions()
	base := MetricSet{CPUCore: floatPointer(0), MemoryBytes: floatPointer(0)}
	for _, point := range []struct {
		name                             string
		updatesPerSecond                 int
		apiCPU, apiMem, etcdCPU, etcdMem float64
	}{
		{"4/sec benchmark", 4, 0.175, 497, 0.042, 54},
		{"10/sec benchmark", 10, 0.313, 679, 0.057, 80},
	} {
		config := SimulatorConfig{Clusters: 250, WorksPerCluster: 10, UpdatesPerSecond: point.updatesPerSecond}
		estimate := provisionalDirectEstimate(config, assumptions, base, base, true)
		if math.Abs(*estimate.API.CPUCore-point.apiCPU) > 0.01 {
			t.Fatalf("%s: API CPU estimate = %.3f, want ~%.3f", point.name, *estimate.API.CPUCore, point.apiCPU)
		}
		if math.Abs(*estimate.API.MemoryBytes/1048576-point.apiMem) > 15 {
			t.Fatalf("%s: API memory estimate = %.0f MiB, want ~%.0f MiB", point.name, *estimate.API.MemoryBytes/1048576, point.apiMem)
		}
		if math.Abs(*estimate.Etcd.CPUCore-point.etcdCPU) > 0.005 {
			t.Fatalf("%s: etcd CPU estimate = %.3f, want ~%.3f", point.name, *estimate.Etcd.CPUCore, point.etcdCPU)
		}
		if math.Abs(*estimate.Etcd.MemoryBytes/1048576-point.etcdMem) > 3 {
			t.Fatalf("%s: etcd memory estimate = %.0f MiB, want ~%.0f MiB", point.name, *estimate.Etcd.MemoryBytes/1048576, point.etcdMem)
		}
	}
}

func TestLoadBaselineRejectsWrongHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.csv")
	if err := os.WriteFile(path, []byte("bad,header\n1,2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(path); err == nil {
		t.Fatal("expected malformed baseline error")
	}
}

func TestLoadBaselineAllowsNewRecordingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created-yet.csv")
	estimator, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("new baseline recording path should start uncalibrated: %v", err)
	}
	if estimator.Predict(SimulatorConfig{Clusters: 1, WorksPerCluster: 1}).Calibrated {
		t.Fatal("new baseline must begin uncalibrated")
	}
}
