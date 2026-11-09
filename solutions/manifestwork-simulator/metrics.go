// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type apiScraper struct {
	client             *http.Client
	baseURL            string
	endpoint           string
	mu                 sync.Mutex
	previous           apiMetricSnapshot
	containerMu        sync.Mutex
	previousContainers containerMetricSnapshot
}

// containerCPUSeries is one container's CPU counter with the timestamp cAdvisor
// last updated it. cAdvisor housekeeping runs per container, so separate
// containers in one scrape can carry different timestamps, and a container can
// replay an older sample between scrapes.
type containerCPUSeries struct {
	target      string
	value       float64
	timestampMS int64
}

type containerMetricSnapshot struct {
	collectedAt time.Time
	// cpu maps namespace/pod/container to that container's latest counter.
	cpu map[string]containerCPUSeries
}

type containerMetrics struct{ Etcd, Kine, Postgres, OCMComponents MetricSet }

type apiMetricSnapshot struct {
	collectedAt         time.Time
	cpuSeconds          float64
	residentMemory      float64
	requests            float64
	latencySum          float64
	latencyCount        float64
	storageLatencySum   float64
	storageLatencyCount float64
}

func newAPIScraper(config *rest.Config) (*apiScraper, error) {
	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, fmt.Errorf("build Kubernetes metrics transport: %w", err)
	}
	return &apiScraper{
		client:   &http.Client{Transport: transport, Timeout: 5 * time.Second},
		baseURL:  strings.TrimRight(config.Host, "/"),
		endpoint: strings.TrimRight(config.Host, "/") + "/metrics",
	}, nil
}

// ScrapeContainers reads cAdvisor through the hosting API server's kubelet proxy, avoiding a Prometheus dependency in kind-based demos.
func (s *apiScraper) ScrapeContainers(ctx context.Context) (containerMetrics, error) {
	result := containerMetrics{}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/v1/nodes", nil)
	if err != nil {
		return result, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return result, fmt.Errorf("list nodes for cAdvisor metrics: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return result, fmt.Errorf("list nodes for cAdvisor metrics returned %s", response.Status)
	}
	var nodes corev1.NodeList
	err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&nodes)
	response.Body.Close()
	if err != nil {
		return result, fmt.Errorf("decode nodes for cAdvisor metrics: %w", err)
	}

	current := containerMetricSnapshot{collectedAt: time.Now(), cpu: map[string]containerCPUSeries{}}
	for _, node := range nodes.Items {
		proxyURL := s.baseURL + "/api/v1/nodes/" + url.PathEscape(node.Name) + "/proxy/metrics/cadvisor"
		proxyRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
		if err != nil {
			return result, err
		}
		proxyResponse, err := s.client.Do(proxyRequest)
		if err != nil {
			return result, fmt.Errorf("scrape cAdvisor on node %s: %w", node.Name, err)
		}
		if proxyResponse.StatusCode != http.StatusOK {
			proxyResponse.Body.Close()
			return result, fmt.Errorf("scrape cAdvisor on node %s returned %s", node.Name, proxyResponse.Status)
		}
		parser := expfmt.NewTextParser(model.UTF8Validation)
		families, parseErr := parser.TextToMetricFamilies(proxyResponse.Body)
		proxyResponse.Body.Close()
		if parseErr != nil {
			return result, fmt.Errorf("parse cAdvisor metrics on node %s: %w", node.Name, parseErr)
		}
		collectContainerMetrics(families, &current, &result, current.collectedAt.UnixMilli())
	}

	s.containerMu.Lock()
	previous := s.previousContainers
	s.previousContainers = current
	s.containerMu.Unlock()
	if previous.collectedAt.IsZero() {
		return result, nil
	}
	// Rates are computed from each container's own counter timestamps: a
	// container whose cAdvisor sample did not advance since the last scrape
	// contributes nothing (reported as unavailable) instead of a fake zero,
	// and the next fresh sample is averaged over its true elapsed time.
	rates := map[string]float64{}
	for key, currentSeries := range current.cpu {
		previousSeries, found := previous.cpu[key]
		if !found || previousSeries.target != currentSeries.target {
			continue
		}
		elapsed := float64(currentSeries.timestampMS-previousSeries.timestampMS) / 1000
		if elapsed <= 0 || currentSeries.value < previousSeries.value {
			continue
		}
		rates[currentSeries.target] += (currentSeries.value - previousSeries.value) / elapsed
	}
	for target, rate := range rates {
		switch target {
		case "etcd":
			result.Etcd.CPUCore = floatPointer(rate)
		case "kine":
			result.Kine.CPUCore = floatPointer(rate)
		case "postgres":
			result.Postgres.CPUCore = floatPointer(rate)
		case "ocm-components":
			result.OCMComponents.CPUCore = floatPointer(rate)
		}
	}
	return result, nil
}

func collectContainerMetrics(families map[string]*dto.MetricFamily, snapshot *containerMetricSnapshot, result *containerMetrics, wallClockMS int64) {
	if family := families["container_cpu_usage_seconds_total"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetCounter() == nil {
				continue
			}
			cpu := metricLabel(metric, "cpu")
			if cpu != "" && cpu != "total" {
				continue
			}
			target, ok := containerTarget(metric)
			if !ok {
				continue
			}
			timestampMS := metric.GetTimestampMs()
			if timestampMS <= 0 {
				timestampMS = wallClockMS
			}
			key := metricLabel(metric, "namespace") + "/" + metricLabel(metric, "pod") + "/" + metricLabel(metric, "container")
			// cAdvisor can emit a container under several cgroup paths; keep
			// the most recent observation for the key.
			if existing, found := snapshot.cpu[key]; !found || timestampMS >= existing.timestampMS {
				snapshot.cpu[key] = containerCPUSeries{target: target, value: metric.GetCounter().GetValue(), timestampMS: timestampMS}
			}
		}
	}
	if family := families["container_memory_working_set_bytes"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetGauge() == nil {
				continue
			}
			target, ok := containerTarget(metric)
			if !ok {
				continue
			}
			value := metric.GetGauge().GetValue()
			switch target {
			case "etcd":
				result.Etcd.MemoryBytes = addMetricValue(result.Etcd.MemoryBytes, value)
			case "kine":
				result.Kine.MemoryBytes = addMetricValue(result.Kine.MemoryBytes, value)
			case "postgres":
				result.Postgres.MemoryBytes = addMetricValue(result.Postgres.MemoryBytes, value)
			case "ocm-components":
				result.OCMComponents.MemoryBytes = addMetricValue(result.OCMComponents.MemoryBytes, value)
			}
		}
	}
}

func containerTarget(metric *dto.Metric) (string, bool) {
	namespace, pod, container := metricLabel(metric, "namespace"), metricLabel(metric, "pod"), metricLabel(metric, "container")
	if container == "" || container == "POD" {
		return "", false
	}
	if namespace == "kube-system" && (container == "etcd" || strings.HasPrefix(pod, "etcd-")) {
		return "etcd", true
	}
	if namespace == "ocm-sql-api" && container == "kine" {
		return "kine", true
	}
	if namespace == "ocm-sql-api" && container == "postgres" {
		return "postgres", true
	}
	if (namespace == "cluster-manager" || strings.HasPrefix(namespace, "open-cluster-management")) && container != "POD" {
		return "ocm-components", true
	}
	return "", false
}

func metricLabel(metric *dto.Metric, name string) string {
	for _, pair := range metric.GetLabel() {
		if pair.GetName() == name {
			return pair.GetValue()
		}
	}
	return ""
}

func isLongRunningRequest(metric *dto.Metric) bool {
	switch strings.ToUpper(metricLabel(metric, "verb")) {
	case "WATCH", "WATCHLIST", "CONNECT":
		return true
	default:
		return false
	}
}

func addMetricValue(current *float64, value float64) *float64 {
	if current == nil {
		return floatPointer(value)
	}
	return floatPointer(*current + value)
}

func (s *apiScraper) Scrape(ctx context.Context) MetricSet {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return MetricSet{Error: err.Error()}
	}
	response, err := s.client.Do(request)
	if err != nil {
		return MetricSet{Error: fmt.Sprintf("scrape API server metrics: %v", err)}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return MetricSet{Error: fmt.Sprintf("API server metrics endpoint returned %s", response.Status)}
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(response.Body)
	if err != nil {
		return MetricSet{Error: fmt.Sprintf("parse API server metrics: %v", err)}
	}

	current := apiMetricSnapshot{collectedAt: time.Now()}
	if family := families["process_cpu_seconds_total"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetCounter() != nil {
				current.cpuSeconds += metric.GetCounter().GetValue()
			}
		}
	}
	if family := families["process_resident_memory_bytes"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetGauge() != nil {
				current.residentMemory += metric.GetGauge().GetValue()
			}
		}
	}
	if family := families["apiserver_request_total"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetCounter() != nil {
				current.requests += metric.GetCounter().GetValue()
			}
		}
	}
	if family := families["apiserver_request_duration_seconds"]; family != nil {
		for _, metric := range family.GetMetric() {
			if isLongRunningRequest(metric) {
				continue
			}
			if metric.GetHistogram() != nil {
				current.latencySum += metric.GetHistogram().GetSampleSum()
				current.latencyCount += float64(metric.GetHistogram().GetSampleCount())
			}
		}
	}
	if family := families["etcd_request_duration_seconds"]; family != nil {
		for _, metric := range family.GetMetric() {
			if metric.GetHistogram() != nil {
				current.storageLatencySum += metric.GetHistogram().GetSampleSum()
				current.storageLatencyCount += float64(metric.GetHistogram().GetSampleCount())
			}
		}
	}

	s.mu.Lock()
	previous := s.previous
	s.previous = current
	s.mu.Unlock()
	result := MetricSet{MemoryBytes: floatPointer(current.residentMemory)}
	if previous.collectedAt.IsZero() {
		return result
	}
	seconds := current.collectedAt.Sub(previous.collectedAt).Seconds()
	if seconds <= 0 {
		return result
	}
	if current.cpuSeconds >= previous.cpuSeconds {
		result.CPUCore = floatPointer((current.cpuSeconds - previous.cpuSeconds) / seconds)
	}
	if current.requests >= previous.requests {
		result.RequestsPerSecond = floatPointer((current.requests - previous.requests) / seconds)
	}
	latencyCount := current.latencyCount - previous.latencyCount
	if latencyCount > 0 && current.latencySum >= previous.latencySum {
		result.AvgLatencySeconds = floatPointer((current.latencySum - previous.latencySum) / latencyCount)
	}
	storageCount := current.storageLatencyCount - previous.storageLatencyCount
	if storageCount > 0 && current.storageLatencySum >= previous.storageLatencySum {
		result.StorageRequestsPerSecond = floatPointer(storageCount / seconds)
		result.StorageLatencySeconds = floatPointer((current.storageLatencySum - previous.storageLatencySum) / storageCount)
	}
	return result
}

type promClient struct {
	baseURL string
	client  *http.Client
}

func newPromClient(baseURL string) (*promClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("Prometheus URL must be an absolute http(s) URL")
	}
	return &promClient{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (p *promClient) query(ctx context.Context, bearerToken, promQL string) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	endpoint := p.baseURL + "/api/v1/query?query=" + url.QueryEscape(promQL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prometheus query returned %s", response.Status)
	}
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("Prometheus query failed: %s", payload.Error)
	}
	if len(payload.Data.Result) == 0 || len(payload.Data.Result[0].Value) < 2 {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(payload.Data.Result[0].Value[1], &value); err != nil {
		return nil, err
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, err
	}
	return floatPointer(parsed), nil
}

type metricCollector struct {
	hostingProm, ocmProm   *promClient
	hostingToken, ocmToken string
	hostingAPI, ocmAPI     *apiScraper
	estimator              *Estimator
	estimatorMu            sync.RWMutex
	baselineFile           string
	recordDirect           bool
	baselineMu             sync.Mutex
}

func (m *metricCollector) collect(ctx context.Context, config SimulatorConfig, recordBaseline bool, achievedTPS float64) Sample {
	result := Sample{Timestamp: timeString(time.Now().UTC().Format(time.RFC3339)), Config: config, AchievedUpdatesPerSecond: achievedTPS}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if m.hostingAPI != nil {
			result.HostingAPI = m.hostingAPI.Scrape(ctx)
		} else {
			result.HostingAPI.Error = "hosting kubeconfig not configured"
		}
	}()
	go func() {
		defer wg.Done()
		if m.ocmAPI != nil {
			result.OCMAPI = m.ocmAPI.Scrape(ctx)
		} else {
			result.OCMAPI.Error = "OCM kubeconfig not configured"
		}
	}()
	wg.Wait()
	var cadvisor containerMetrics
	var cadvisorErr error
	if m.hostingAPI != nil {
		cadvisor, cadvisorErr = m.hostingAPI.ScrapeContainers(ctx)
	}

	var metricErrors []string
	query := func(client *promClient, token, expression string) *float64 {
		value, err := client.query(ctx, token, expression)
		if err != nil {
			metricErrors = append(metricErrors, err.Error())
		}
		return value
	}
	if m.hostingProm != nil {
		result.HostingEtcd.CPUCore = query(m.hostingProm, m.hostingToken, `sum(rate(container_cpu_usage_seconds_total{namespace="kube-system",pod=~".*etcd.*",container!="",container!="POD"}[5m]))`)
		result.HostingEtcd.MemoryBytes = query(m.hostingProm, m.hostingToken, `sum(container_memory_working_set_bytes{namespace="kube-system",pod=~".*etcd.*",container!="",container!="POD"})`)
	} else {
		result.HostingEtcd.Error = "Prometheus is not configured; etcd container CPU and memory are unavailable."
	}
	if m.ocmProm != nil {
		result.Kine.CPUCore = query(m.ocmProm, m.ocmToken, `sum(rate(container_cpu_usage_seconds_total{namespace="ocm-sql-api",pod=~"ocm-sql-apiserver.*",container="kine"}[5m]))`)
		result.Kine.MemoryBytes = query(m.ocmProm, m.ocmToken, `sum(container_memory_working_set_bytes{namespace="ocm-sql-api",pod=~"ocm-sql-apiserver.*",container="kine"})`)
		result.Postgres.CPUCore = query(m.ocmProm, m.ocmToken, `sum(rate(container_cpu_usage_seconds_total{namespace="ocm-sql-api",pod=~"ocm-sql-postgres.*",container="postgres"}[5m]))`)
		result.Postgres.MemoryBytes = query(m.ocmProm, m.ocmToken, `sum(container_memory_working_set_bytes{namespace="ocm-sql-api",pod=~"ocm-sql-postgres.*",container="postgres"})`)
		result.Postgres.RequestsPerSecond = query(m.ocmProm, m.ocmToken, `sum(rate(pg_stat_database_xact_commit{datname="ocm_kine"}[5m])) + sum(rate(pg_stat_database_xact_rollback{datname="ocm_kine"}[5m]))`)
		result.OCMComponents.CPUCore = query(m.ocmProm, m.ocmToken, `sum(rate(container_cpu_usage_seconds_total{namespace=~"cluster-manager|open-cluster-management.*",container!="",container!="POD"}[5m]))`)
		result.OCMComponents.MemoryBytes = query(m.ocmProm, m.ocmToken, `sum(container_memory_working_set_bytes{namespace=~"cluster-manager|open-cluster-management.*",container!="",container!="POD"})`)
	} else {
		result.Kine.Error = "Prometheus is not configured; Kine and PostgreSQL container metrics are unavailable."
		result.Postgres.Error = result.Kine.Error
	}
	if result.HostingEtcd.CPUCore == nil {
		result.HostingEtcd.CPUCore = cadvisor.Etcd.CPUCore
	}
	if result.HostingEtcd.MemoryBytes == nil {
		result.HostingEtcd.MemoryBytes = cadvisor.Etcd.MemoryBytes
	}
	if result.Kine.CPUCore == nil {
		result.Kine.CPUCore = cadvisor.Kine.CPUCore
	}
	if result.Kine.MemoryBytes == nil {
		result.Kine.MemoryBytes = cadvisor.Kine.MemoryBytes
	}
	if result.Postgres.CPUCore == nil {
		result.Postgres.CPUCore = cadvisor.Postgres.CPUCore
	}
	if result.Postgres.MemoryBytes == nil {
		result.Postgres.MemoryBytes = cadvisor.Postgres.MemoryBytes
	}
	if result.OCMComponents.CPUCore == nil {
		result.OCMComponents.CPUCore = cadvisor.OCMComponents.CPUCore
	}
	if result.OCMComponents.MemoryBytes == nil {
		result.OCMComponents.MemoryBytes = cadvisor.OCMComponents.MemoryBytes
	}
	if result.HostingEtcd.CPUCore != nil || result.HostingEtcd.MemoryBytes != nil {
		result.HostingEtcd.Error = ""
	}
	if result.Kine.CPUCore != nil || result.Kine.MemoryBytes != nil {
		result.Kine.Error = ""
	}
	if result.Postgres.CPUCore != nil || result.Postgres.MemoryBytes != nil {
		result.Postgres.Error = ""
	}
	if result.OCMComponents.CPUCore != nil || result.OCMComponents.MemoryBytes != nil {
		result.OCMComponents.Error = ""
	}
	if cadvisorErr != nil {
		message := "container metrics unavailable through hosting API kubelet proxy: " + cadvisorErr.Error()
		if result.HostingEtcd.CPUCore == nil && result.HostingEtcd.MemoryBytes == nil {
			result.HostingEtcd.Error = message
		}
		if result.Kine.CPUCore == nil && result.Kine.MemoryBytes == nil {
			result.Kine.Error = message
		}
		if result.Postgres.CPUCore == nil && result.Postgres.MemoryBytes == nil {
			result.Postgres.Error = message
		}
		if result.OCMComponents.CPUCore == nil && result.OCMComponents.MemoryBytes == nil {
			result.OCMComponents.Error = message
		}
	}
	if len(metricErrors) > 0 {
		if result.HostingEtcd.CPUCore == nil && result.HostingEtcd.MemoryBytes == nil {
			result.HostingEtcd.Error = "Prometheus: " + strings.Join(metricErrors, "; ")
		}
		if result.Kine.CPUCore == nil && result.Postgres.CPUCore == nil {
			result.Kine.Error = "Prometheus: " + strings.Join(metricErrors, "; ")
		}
	}
	if m.recordDirect {
		switch {
		case m.baselineFile == "":
			result.BaselineStatus = "Baseline recording is enabled but no CSV path was configured."
		case !recordBaseline:
			result.BaselineStatus = "Baseline recording is armed; start the simulator and wait for the requested resources to be provisioned."
		default:
			recorded, err := m.record(result)
			if err != nil {
				result.BaselineStatus = "Baseline write failed: " + err.Error()
			} else if recorded {
				result.BaselineStatus = "Direct-hub baseline sample recorded."
			} else {
				result.BaselineStatus = "Baseline sample skipped: required metrics are missing or achieved update rate is below 90% of the requested rate."
			}
		}
	}
	result.DirectEstimate = m.predict(config)
	return result
}

func (m *metricCollector) predict(config SimulatorConfig) Estimate {
	m.estimatorMu.RLock()
	estimator := m.estimator
	m.estimatorMu.RUnlock()
	if estimator == nil {
		return Estimate{Message: "No direct-hub baseline data loaded; estimate is uncalibrated."}
	}
	return estimator.Predict(config)
}

func (m *metricCollector) record(sample Sample) (bool, error) {
	apiCPU, apiMemory := sample.OCMAPI.CPUCore, sample.OCMAPI.MemoryBytes
	etcdCPU, etcdMemory := sample.HostingEtcd.CPUCore, sample.HostingEtcd.MemoryBytes
	if apiCPU == nil || apiMemory == nil || etcdCPU == nil || etcdMemory == nil {
		return false, nil
	}
	if sample.Config.UpdatesPerSecond > 0 && sample.AchievedUpdatesPerSecond < 0.9*float64(sample.Config.UpdatesPerSecond) {
		return false, nil
	}
	m.baselineMu.Lock()
	defer m.baselineMu.Unlock()
	newFile := false
	if info, err := os.Stat(m.baselineFile); os.IsNotExist(err) || (err == nil && info.Size() == 0) {
		newFile = true
	} else if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(m.baselineFile), 0700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(m.baselineFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if newFile {
		_ = w.Write([]string{"clusters", "works_per_cluster", "updates_per_second", "api_cpu_cores", "api_memory_bytes", "etcd_cpu_cores", "etcd_memory_bytes"})
	}
	_ = w.Write([]string{
		strconv.Itoa(sample.Config.Clusters), strconv.Itoa(sample.Config.WorksPerCluster), strconv.Itoa(sample.Config.UpdatesPerSecond),
		strconv.FormatFloat(*apiCPU, 'f', 6, 64), strconv.FormatFloat(*apiMemory, 'f', 2, 64),
		strconv.FormatFloat(*etcdCPU, 'f', 6, 64), strconv.FormatFloat(*etcdMemory, 'f', 2, 64),
	})
	w.Flush()
	if w.Error() != nil {
		return false, w.Error()
	}
	point := BaselinePoint{Clusters: sample.Config.Clusters, WorksPerCluster: sample.Config.WorksPerCluster, UpdatesPerSecond: float64(sample.Config.UpdatesPerSecond), APICPU: *apiCPU, APIMemory: *apiMemory, EtcdCPU: *etcdCPU, EtcdMemory: *etcdMemory}
	m.estimatorMu.Lock()
	points := []BaselinePoint{}
	if m.estimator != nil {
		points = append(points, m.estimator.points...)
	}
	points = append(points, point)
	m.estimator = NewEstimator(points)
	m.estimatorMu.Unlock()
	return true, nil
}

func floatPointer(value float64) *float64 { return &value }
