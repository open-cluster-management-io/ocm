// Copyright Contributors to the Open Cluster Management project

package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	maxClusters           = 1000
	maxWorksPerCluster    = 100
	maxTotalManifestWorks = 10000
	maxUpdatesPerSecond   = 100
)

type SimulatorConfig struct {
	Clusters         int `json:"clusters"`
	WorksPerCluster  int `json:"worksPerCluster"`
	UpdatesPerSecond int `json:"updatesPerSecond"`
}

func (c SimulatorConfig) Validate() error {
	if c.Clusters < 1 || c.Clusters > maxClusters {
		return fmt.Errorf("clusters must be between 1 and %d", maxClusters)
	}
	if c.WorksPerCluster < 1 || c.WorksPerCluster > maxWorksPerCluster {
		return fmt.Errorf("ManifestWorks per cluster must be between 1 and %d", maxWorksPerCluster)
	}
	if c.Clusters*c.WorksPerCluster > maxTotalManifestWorks {
		return fmt.Errorf("total ManifestWorks cannot exceed %d", maxTotalManifestWorks)
	}
	if c.UpdatesPerSecond < 0 || c.UpdatesPerSecond > maxUpdatesPerSecond {
		return fmt.Errorf("updates per second must be between 0 and %d", maxUpdatesPerSecond)
	}
	return nil
}

type MetricSet struct {
	CPUCore                  *float64 `json:"cpuCores,omitempty"`
	MemoryBytes              *float64 `json:"memoryBytes,omitempty"`
	RequestsPerSecond        *float64 `json:"requestsPerSecond,omitempty"`
	AvgLatencySeconds        *float64 `json:"avgLatencySeconds,omitempty"`
	StorageRequestsPerSecond *float64 `json:"storageRequestsPerSecond,omitempty"`
	StorageLatencySeconds    *float64 `json:"storageLatencySeconds,omitempty"`
	Error                    string   `json:"error,omitempty"`
}

type Estimate struct {
	API          MetricSet   `json:"api"`
	Etcd         MetricSet   `json:"etcd"`
	APIRange     MetricRange `json:"apiRange"`
	EtcdRange    MetricRange `json:"etcdRange"`
	Samples      int         `json:"samples"`
	R2           float64     `json:"r2"`
	Calibrated   bool        `json:"calibrated"`
	Provisional  bool        `json:"provisional"`
	Extrapolated bool        `json:"extrapolated"`
	WeakFit      bool        `json:"weakFit"`
	Message      string      `json:"message"`
}

type EstimateAssumptions struct {
	APICPUPerCluster                float64 `json:"apiCpuPerCluster"`
	APIMemoryMiBPerCluster          float64 `json:"apiMemoryMiBPerCluster"`
	APICPUPerWork                   float64 `json:"apiCpuPerWork"`
	APIMemoryMiBPerWork             float64 `json:"apiMemoryMiBPerWork"`
	APICPUPerUpdatePerSecond        float64 `json:"apiCpuPerUpdatePerSecond"`
	APIMemoryMiBPerUpdatePerSecond  float64 `json:"apiMemoryMiBPerUpdatePerSecond"`
	EtcdCPUPerCluster               float64 `json:"etcdCpuPerCluster"`
	EtcdMemoryMiBPerCluster         float64 `json:"etcdMemoryMiBPerCluster"`
	EtcdCPUPerWork                  float64 `json:"etcdCpuPerWork"`
	EtcdMemoryMiBPerWork            float64 `json:"etcdMemoryMiBPerWork"`
	EtcdCPUPerUpdatePerSecond       float64 `json:"etcdCpuPerUpdatePerSecond"`
	EtcdMemoryMiBPerUpdatePerSecond float64 `json:"etcdMemoryMiBPerUpdatePerSecond"`
}

// DefaultEstimateAssumptions returns increments calibrated against two
// no-Kine kind benchmarks, both at 250 simulated clusters with CSR-backed
// registration and lease heartbeats and 10 ManifestWorks per cluster:
// 4 fleet updates/sec (measured load-driven deltas above idle: +0.175 API
// CPU cores, +497 MiB API memory, +0.042 etcd CPU cores, +54 MiB etcd
// memory) and 10 fleet updates/sec (+0.313 API CPU cores, +679 MiB API
// memory, +0.057 etcd CPU cores, +80 MiB etcd memory). Two points separate
// a static per-object cost from a per-update-rate cost, but cannot separate
// per-cluster from per-work cost, so the earlier split ratio (about 29%/71%
// at 10 works per cluster) is preserved; more calibration points will refine
// both the magnitudes and that split.
func DefaultEstimateAssumptions() EstimateAssumptions {
	return EstimateAssumptions{
		APICPUPerCluster: 0.0001, APIMemoryMiBPerCluster: 0.44,
		APICPUPerWork: 0.000024, APIMemoryMiBPerWork: 0.11,
		APICPUPerUpdatePerSecond: 0.023, APIMemoryMiBPerUpdatePerSecond: 30,
		EtcdCPUPerCluster: 0.000037, EtcdMemoryMiBPerCluster: 0.043,
		EtcdCPUPerWork: 0.000009, EtcdMemoryMiBPerWork: 0.01,
		EtcdCPUPerUpdatePerSecond: 0.0026, EtcdMemoryMiBPerUpdatePerSecond: 4.3,
	}
}

func (a EstimateAssumptions) Validate() error {
	values := map[string]float64{
		"API CPU per cluster": a.APICPUPerCluster, "API memory per cluster": a.APIMemoryMiBPerCluster,
		"API CPU per ManifestWork": a.APICPUPerWork, "API memory per ManifestWork": a.APIMemoryMiBPerWork,
		"API CPU per update/sec": a.APICPUPerUpdatePerSecond, "API memory per update/sec": a.APIMemoryMiBPerUpdatePerSecond,
		"etcd CPU per cluster": a.EtcdCPUPerCluster, "etcd memory per cluster": a.EtcdMemoryMiBPerCluster,
		"etcd CPU per ManifestWork": a.EtcdCPUPerWork, "etcd memory per ManifestWork": a.EtcdMemoryMiBPerWork,
		"etcd CPU per update/sec": a.EtcdCPUPerUpdatePerSecond, "etcd memory per update/sec": a.EtcdMemoryMiBPerUpdatePerSecond,
	}
	for name, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10000 {
			return fmt.Errorf("%s assumption must be between 0 and 10000", name)
		}
	}
	return nil
}

// projectionConfig selects the workload shape for direct-hub projections.
// While the simulator is started, or idle before a first run, the selected
// slider load is projected immediately. Once a run is stopped and cleanup is
// underway, only the resources that actually remain are projected, so the
// estimate decays to the measured baseline instead of holding the stopped load.
func projectionConfig(started bool, config SimulatorConfig, cleanupUnderway bool, activeClusters, activeWorks int) SimulatorConfig {
	if started || !cleanupUnderway {
		return config
	}
	worksPerCluster := 1
	if activeClusters > 0 && activeWorks > 0 {
		worksPerCluster = (activeWorks + activeClusters - 1) / activeClusters
	}
	return SimulatorConfig{Clusters: activeClusters, WorksPerCluster: worksPerCluster}
}

func provisionalDirectEstimate(config SimulatorConfig, assumptions EstimateAssumptions, hostingAPI, hostingEtcd MetricSet, idleBaselineCaptured bool) Estimate {
	totalWorks := float64(config.Clusters * config.WorksPerCluster)
	updatesPerSecond := float64(config.UpdatesPerSecond)
	apiCPUDelta := float64(config.Clusters)*assumptions.APICPUPerCluster + totalWorks*assumptions.APICPUPerWork + updatesPerSecond*assumptions.APICPUPerUpdatePerSecond
	apiMemoryDelta := (float64(config.Clusters)*assumptions.APIMemoryMiBPerCluster + totalWorks*assumptions.APIMemoryMiBPerWork + updatesPerSecond*assumptions.APIMemoryMiBPerUpdatePerSecond) * 1024 * 1024
	etcdCPUDelta := float64(config.Clusters)*assumptions.EtcdCPUPerCluster + totalWorks*assumptions.EtcdCPUPerWork + updatesPerSecond*assumptions.EtcdCPUPerUpdatePerSecond
	etcdMemoryDelta := (float64(config.Clusters)*assumptions.EtcdMemoryMiBPerCluster + totalWorks*assumptions.EtcdMemoryMiBPerWork + updatesPerSecond*assumptions.EtcdMemoryMiBPerUpdatePerSecond) * 1024 * 1024
	apiCPUBase, apiMemoryBase := metricBase(hostingAPI.CPUCore), metricBase(hostingAPI.MemoryBytes)
	etcdCPUBase, etcdMemoryBase := metricBase(hostingEtcd.CPUCore), metricBase(hostingEtcd.MemoryBytes)
	result := Estimate{
		Provisional: true,
		API:         MetricSet{CPUCore: floatPointer(apiCPUBase + apiCPUDelta), MemoryBytes: floatPointer(apiMemoryBase + apiMemoryDelta)},
		Etcd:        MetricSet{CPUCore: floatPointer(etcdCPUBase + etcdCPUDelta), MemoryBytes: floatPointer(etcdMemoryBase + etcdMemoryDelta)},
		Message:     "Provisional direct-hub estimate = idle hosting API/etcd baseline + editable per-cluster, per-ManifestWork and per-update-rate increments. Defaults are calibrated from two no-Kine kind benchmarks (250 clusters x 10 works at 4 and 10 updates/sec); the per-cluster/per-work split is still assumed.",
	}
	if !idleBaselineCaptured {
		result.Message += " No idle baseline has been captured yet, so the live hosting measurement (which already includes the active load) is used as the base and the estimate double-counts until an idle baseline exists."
	}
	result.APIRange = assumptionRange(apiCPUBase, apiMemoryBase, apiCPUDelta, apiMemoryDelta)
	result.EtcdRange = assumptionRange(etcdCPUBase, etcdMemoryBase, etcdCPUDelta, etcdMemoryDelta)
	if hostingAPI.CPUCore == nil || hostingAPI.MemoryBytes == nil || hostingEtcd.CPUCore == nil || hostingEtcd.MemoryBytes == nil {
		result.Message += " Hosting API/etcd baseline is not fully measured yet; missing base components are treated as zero."
	}
	return result
}

func metricBase(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func assumptionRange(cpuBase, memoryBase, cpuDelta, memoryDelta float64) MetricRange {
	return MetricRange{
		CPULow: floatPointer(cpuBase + cpuDelta*0.5), CPUHigh: floatPointer(cpuBase + cpuDelta*1.5),
		MemoryLow: floatPointer(memoryBase + memoryDelta*0.5), MemoryHigh: floatPointer(memoryBase + memoryDelta*1.5),
	}
}

type MetricRange struct {
	CPULow     *float64 `json:"cpuLow,omitempty"`
	CPUHigh    *float64 `json:"cpuHigh,omitempty"`
	MemoryLow  *float64 `json:"memoryLow,omitempty"`
	MemoryHigh *float64 `json:"memoryHigh,omitempty"`
}

type Sample struct {
	Timestamp                        timeString      `json:"timestamp"`
	Config                           SimulatorConfig `json:"config"`
	HostingAPI                       MetricSet       `json:"hostingApi"`
	HostingEtcd                      MetricSet       `json:"hostingEtcd"`
	OCMComponents                    MetricSet       `json:"ocmComponents"`
	OCMAPI                           MetricSet       `json:"ocmApi"`
	Kine                             MetricSet       `json:"kine"`
	Postgres                         MetricSet       `json:"postgres"`
	DirectEstimate                   Estimate        `json:"directEstimate"`
	AchievedUpdatesPerSecond         float64         `json:"achievedUpdatesPerSecond"`
	ManifestWorkUpdates              uint64          `json:"manifestWorkUpdates"`
	AgentStatusUpdates               uint64          `json:"agentStatusUpdates"`
	RegistrationCSRs                 uint64          `json:"registrationCSRs"`
	RegistrationCSRsApproved         uint64          `json:"registrationCSRsApproved"`
	ManagedClusterRegistrations      uint64          `json:"managedClusterRegistrations"`
	LeaseWrites                      uint64          `json:"leaseWrites"`
	PendingRegistrations             int             `json:"pendingRegistrations"`
	RegistrationStages               map[string]int  `json:"registrationStages,omitempty"`
	PendingRegistrationAgeSeconds    float64         `json:"pendingRegistrationAgeSeconds,omitempty"`
	PendingRegistrationOldestSeconds float64         `json:"pendingRegistrationOldestSeconds,omitempty"`
	StalledRegistrations             int             `json:"stalledRegistrations,omitempty"`
	AverageAgentStatusLagSeconds     float64         `json:"averageAgentStatusLagSeconds"`
	BaselineStatus                   string          `json:"baselineStatus,omitempty"`
	Errors                           uint64          `json:"errors"`
	ActiveWatches                    int             `json:"activeWatches"`
}

// timeString keeps the wire format stable without requiring custom frontend date parsing.
type timeString string

type BaselinePoint struct {
	Clusters         int
	WorksPerCluster  int
	UpdatesPerSecond float64
	APICPU           float64
	APIMemory        float64
	EtcdCPU          float64
	EtcdMemory       float64
}

type linearModel struct {
	coefficients   []float64
	residualStdDev float64
	r2             float64
}

type Estimator struct {
	points                                 []BaselinePoint
	apiCPU, apiMemory, etcdCPU, etcdMemory *linearModel
	message                                string
	minClusters, maxClusters               int
	minWorks, maxWorks                     int
	minUpdates, maxUpdates                 float64
}

func LoadBaseline(path string) (*Estimator, error) {
	if path == "" {
		return NewEstimator(nil), nil
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewEstimator(nil), nil
		}
		return nil, fmt.Errorf("open baseline CSV: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat baseline CSV: %w", err)
	}
	if info.Size() == 0 {
		return NewEstimator(nil), nil
	}

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read baseline CSV header: %w", err)
	}
	want := []string{"clusters", "works_per_cluster", "updates_per_second", "api_cpu_cores", "api_memory_bytes", "etcd_cpu_cores", "etcd_memory_bytes"}
	if len(header) != len(want) {
		return nil, fmt.Errorf("baseline CSV header must be: %s", strings.Join(want, ","))
	}
	for i := range want {
		if strings.TrimSpace(header[i]) != want[i] {
			return nil, fmt.Errorf("baseline CSV header must be: %s", strings.Join(want, ","))
		}
	}
	var points []BaselinePoint
	for line := 2; ; line++ {
		row, readErr := r.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read baseline CSV line %d: %w", line, readErr)
		}
		values := make([]float64, len(row))
		for i := range row {
			values[i], err = strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
			if err != nil || math.IsNaN(values[i]) || math.IsInf(values[i], 0) {
				return nil, fmt.Errorf("invalid numeric value on baseline CSV line %d, column %d", line, i+1)
			}
		}
		if math.Trunc(values[0]) != values[0] || math.Trunc(values[1]) != values[1] || math.Trunc(values[2]) != values[2] || values[0] < 1 || values[0] > maxClusters || values[1] < 1 || values[1] > maxWorksPerCluster || values[2] < 0 || values[2] > maxUpdatesPerSecond || values[0]*values[1] > maxTotalManifestWorks || values[3] < 0 || values[4] < 0 || values[5] < 0 || values[6] < 0 {
			return nil, fmt.Errorf("baseline CSV line %d contains values outside simulator or metric bounds", line)
		}
		points = append(points, BaselinePoint{
			Clusters: int(values[0]), WorksPerCluster: int(values[1]), UpdatesPerSecond: values[2],
			APICPU: values[3], APIMemory: values[4], EtcdCPU: values[5], EtcdMemory: values[6],
		})
	}
	return NewEstimator(points), nil
}

func NewEstimator(points []BaselinePoint) *Estimator {
	e := &Estimator{points: append([]BaselinePoint(nil), points...), message: "No direct-hub baseline data loaded; estimate is uncalibrated."}
	if len(points) < 5 {
		return e
	}
	e.minClusters, e.maxClusters = points[0].Clusters, points[0].Clusters
	e.minWorks, e.maxWorks = points[0].Clusters*points[0].WorksPerCluster, points[0].Clusters*points[0].WorksPerCluster
	e.minUpdates, e.maxUpdates = points[0].UpdatesPerSecond, points[0].UpdatesPerSecond
	features := make([][]float64, 0, len(points))
	apiCPU, apiMemory, etcdCPU, etcdMemory := make([]float64, 0, len(points)), make([]float64, 0, len(points)), make([]float64, 0, len(points)), make([]float64, 0, len(points))
	for _, point := range points {
		clusters, totalWorks := point.Clusters, point.Clusters*point.WorksPerCluster
		if clusters < e.minClusters {
			e.minClusters = clusters
		}
		if clusters > e.maxClusters {
			e.maxClusters = clusters
		}
		if totalWorks < e.minWorks {
			e.minWorks = totalWorks
		}
		if totalWorks > e.maxWorks {
			e.maxWorks = totalWorks
		}
		if point.UpdatesPerSecond < e.minUpdates {
			e.minUpdates = point.UpdatesPerSecond
		}
		if point.UpdatesPerSecond > e.maxUpdates {
			e.maxUpdates = point.UpdatesPerSecond
		}
		features = append(features, baselineFeatures(point.Clusters, point.WorksPerCluster, point.UpdatesPerSecond))
		apiCPU = append(apiCPU, point.APICPU)
		apiMemory = append(apiMemory, point.APIMemory)
		etcdCPU = append(etcdCPU, point.EtcdCPU)
		etcdMemory = append(etcdMemory, point.EtcdMemory)
	}
	var err error
	if e.apiCPU, err = fitLinear(features, apiCPU); err != nil {
		e.message = "Baseline is not sufficiently varied to fit an estimate."
		return e
	}
	if e.apiMemory, err = fitLinear(features, apiMemory); err != nil {
		e.message = "Baseline is not sufficiently varied to fit an estimate."
		return e
	}
	if e.etcdCPU, err = fitLinear(features, etcdCPU); err != nil {
		e.message = "Baseline is not sufficiently varied to fit an estimate."
		return e
	}
	if e.etcdMemory, err = fitLinear(features, etcdMemory); err != nil {
		e.message = "Baseline is not sufficiently varied to fit an estimate."
		return e
	}
	e.message = "Calibrated from direct-hub measurements; ranges show approximately 95% residual uncertainty."
	return e
}

func (e *Estimator) Predict(config SimulatorConfig) Estimate {
	result := Estimate{Samples: len(e.points), Message: e.message}
	if e.apiCPU == nil || e.apiMemory == nil || e.etcdCPU == nil || e.etcdMemory == nil {
		return result
	}
	x := baselineFeatures(config.Clusters, config.WorksPerCluster, float64(config.UpdatesPerSecond))
	result.API.CPUCore = prediction(e.apiCPU, x)
	result.API.MemoryBytes = prediction(e.apiMemory, x)
	result.Etcd.CPUCore = prediction(e.etcdCPU, x)
	result.Etcd.MemoryBytes = prediction(e.etcdMemory, x)
	result.APIRange = metricRange(e.apiCPU, e.apiMemory, x)
	result.EtcdRange = metricRange(e.etcdCPU, e.etcdMemory, x)
	result.R2 = math.Min(math.Min(e.apiCPU.r2, e.apiMemory.r2), math.Min(e.etcdCPU.r2, e.etcdMemory.r2))
	result.Calibrated = true
	totalWorks := config.Clusters * config.WorksPerCluster
	result.Extrapolated = config.Clusters < e.minClusters || config.Clusters > e.maxClusters || totalWorks < e.minWorks || totalWorks > e.maxWorks || float64(config.UpdatesPerSecond) < e.minUpdates || float64(config.UpdatesPerSecond) > e.maxUpdates
	if result.Extrapolated {
		result.Message += " Current slider values are outside the measured range; this is an extrapolation and should not be treated as a reliable prediction."
	}
	result.WeakFit = result.R2 < 0.7
	if result.WeakFit {
		result.Message += " The calibration fit is weak (minimum R² below 0.7)."
	}
	return result
}

func baselineFeatures(clusters, worksPerCluster int, updatesPerSecond float64) []float64 {
	return []float64{1, float64(clusters), float64(clusters * worksPerCluster), updatesPerSecond}
}

func fitLinear(x [][]float64, y []float64) (*linearModel, error) {
	const dimensions = 4
	if len(x) != len(y) || len(x) < dimensions+1 {
		return nil, errors.New("insufficient observations")
	}
	gram := make([][]float64, dimensions)
	for i := range gram {
		gram[i] = make([]float64, dimensions+1)
	}
	for row := range x {
		for i := 0; i < dimensions; i++ {
			for j := 0; j < dimensions; j++ {
				gram[i][j] += x[row][i] * x[row][j]
			}
			gram[i][dimensions] += x[row][i] * y[row]
		}
	}
	coefficients, err := solve(gram)
	if err != nil {
		return nil, err
	}
	mean := 0.0
	for _, value := range y {
		mean += value
	}
	mean /= float64(len(y))
	var sse, sst float64
	for row := range x {
		predicted := dot(x[row], coefficients)
		sse += (y[row] - predicted) * (y[row] - predicted)
		sst += (y[row] - mean) * (y[row] - mean)
	}
	degrees := float64(len(y) - dimensions)
	if degrees <= 0 {
		return nil, errors.New("insufficient degrees of freedom")
	}
	r2 := 1.0
	if sst > 0 {
		r2 = 1 - sse/sst
	} else if sse > 0 {
		r2 = 0
	}
	return &linearModel{coefficients: coefficients, residualStdDev: math.Sqrt(sse / degrees), r2: r2}, nil
}

func solve(matrix [][]float64) ([]float64, error) {
	n := len(matrix)
	for col := 0; col < n; col++ {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(matrix[row][col]) > math.Abs(matrix[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(matrix[pivot][col]) < 1e-10 {
			return nil, errors.New("singular calibration matrix")
		}
		matrix[col], matrix[pivot] = matrix[pivot], matrix[col]
		divisor := matrix[col][col]
		for j := col; j <= n; j++ {
			matrix[col][j] /= divisor
		}
		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := matrix[row][col]
			for j := col; j <= n; j++ {
				matrix[row][j] -= factor * matrix[col][j]
			}
		}
	}
	result := make([]float64, n)
	for i := range result {
		result[i] = matrix[i][n]
	}
	return result, nil
}

func prediction(model *linearModel, x []float64) *float64 {
	value := dot(x, model.coefficients)
	if value < 0 {
		value = 0
	}
	return &value
}

func metricRange(cpu, memory *linearModel, x []float64) MetricRange {
	centerCPU, centerMemory := *prediction(cpu, x), *prediction(memory, x)
	marginCPU, marginMemory := 1.96*cpu.residualStdDev, 1.96*memory.residualStdDev
	return MetricRange{
		CPULow: floatPointer(math.Max(0, centerCPU-marginCPU)), CPUHigh: floatPointer(centerCPU + marginCPU),
		MemoryLow: floatPointer(math.Max(0, centerMemory-marginMemory)), MemoryHigh: floatPointer(centerMemory + marginMemory),
	}
}

func dot(a, b []float64) float64 {
	var result float64
	for i := range a {
		result += a[i] * b[i]
	}
	return result
}
