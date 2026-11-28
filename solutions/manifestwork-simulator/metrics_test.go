// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestAPIScraperCalculatesCounterRatesAndReadsProcessMetrics(t *testing.T) {
	var scrape atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := scrape.Add(1) * 10
		fmt.Fprintf(w, `# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total %d
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes %d
# TYPE apiserver_request_total counter
apiserver_request_total{verb="GET"} %d
# TYPE apiserver_request_duration_seconds histogram
apiserver_request_duration_seconds_bucket{le="+Inf"} %d
apiserver_request_duration_seconds_sum %d
apiserver_request_duration_seconds_count %d
# TYPE etcd_request_duration_seconds histogram
etcd_request_duration_seconds_bucket{le="+Inf"} %d
etcd_request_duration_seconds_sum %d
etcd_request_duration_seconds_count %d
`, count/10, 1000+count, count, count, count/5, count, count, count/10, count)
	}))
	defer server.Close()
	scraper, err := newAPIScraper(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	first := scraper.Scrape(context.Background())
	if first.MemoryBytes == nil || *first.MemoryBytes != 1010 {
		t.Fatalf("process memory sample missing: %#v", first)
	}
	time.Sleep(10 * time.Millisecond)
	second := scraper.Scrape(context.Background())
	if second.CPUCore == nil || *second.CPUCore <= 0 {
		t.Fatalf("CPU rate was not calculated: %#v", second)
	}
	if second.RequestsPerSecond == nil || *second.RequestsPerSecond <= 0 {
		t.Fatalf("API request rate was not calculated: %#v", second)
	}
	if second.AvgLatencySeconds == nil || *second.AvgLatencySeconds <= 0 {
		t.Fatalf("request latency was not calculated: %#v", second)
	}
	if second.StorageRequestsPerSecond == nil || *second.StorageRequestsPerSecond <= 0 || second.StorageLatencySeconds == nil {
		t.Fatalf("storage operation rate and latency were not calculated: %#v", second)
	}
}

func TestPrometheusQueryUsesBearerTokenAndParsesVectorValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("query path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token")
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("query = %q", r.URL.Query().Get("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1700000000,"42.5"]}]}}`))
	}))
	defer server.Close()
	client, err := newPromClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.query(context.Background(), "test-token", "up")
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || *value != 42.5 {
		t.Fatalf("query value = %v", value)
	}
}

func TestKubeletProxyProvidesContainerMetricsWithoutPrometheus(t *testing.T) {
	var scrape atomic.Int64
	nodes, err := json.Marshal(corev1.NodeList{Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/nodes" {
			_, _ = w.Write(nodes)
			return
		}
		if r.URL.Path != "/api/v1/nodes/node1/proxy/metrics/cadvisor" {
			http.NotFound(w, r)
			return
		}
		value := scrape.Add(10)
		fmt.Fprintf(w, `# TYPE container_cpu_usage_seconds_total counter
container_cpu_usage_seconds_total{namespace="kube-system",pod="etcd-node1",container="etcd",cpu="total"} %d
container_cpu_usage_seconds_total{namespace="kube-system",pod="etcd-node1",container="etcd",cpu="cpu00"} %d
container_cpu_usage_seconds_total{namespace="ocm-sql-api",pod="ocm-sql-apiserver-abc",container="kine",cpu="total"} %d
container_cpu_usage_seconds_total{namespace="ocm-sql-api",pod="ocm-sql-postgres-abc",container="postgres",cpu="total"} %d
container_cpu_usage_seconds_total{namespace="cluster-manager",pod="cluster-manager-abc",container="registration-operator",cpu="total"} %d
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{namespace="kube-system",pod="etcd-node1",container="etcd"} 100
container_memory_working_set_bytes{namespace="ocm-sql-api",pod="ocm-sql-apiserver-abc",container="kine"} 200
container_memory_working_set_bytes{namespace="ocm-sql-api",pod="ocm-sql-postgres-abc",container="postgres"} 300

container_memory_working_set_bytes{namespace="cluster-manager",pod="cluster-manager-abc",container="registration-operator"} 400
`, value, value*100, value*2, value*3, value*4)
	}))
	defer server.Close()
	scraper, err := newAPIScraper(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	first, err := scraper.ScrapeContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Etcd.MemoryBytes == nil || *first.Etcd.MemoryBytes != 100 || first.Kine.MemoryBytes == nil || *first.Kine.MemoryBytes != 200 || first.Postgres.MemoryBytes == nil || *first.Postgres.MemoryBytes != 300 || first.OCMComponents.MemoryBytes == nil || *first.OCMComponents.MemoryBytes != 400 {
		t.Fatalf("cAdvisor memory was not classified: %#v", first)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := scraper.ScrapeContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Etcd.CPUCore == nil || second.Kine.CPUCore == nil || second.Postgres.CPUCore == nil || second.OCMComponents.CPUCore == nil {
		t.Fatalf("cAdvisor CPU rates were not calculated: %#v", second)
	}
}

func TestKubeletProxyStaleCAdvisorSamplesReportNoCPU(t *testing.T) {
	var scrape atomic.Int64
	nodes, err := json.Marshal(corev1.NodeList{Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/nodes" {
			_, _ = w.Write(nodes)
			return
		}
		if r.URL.Path != "/api/v1/nodes/node1/proxy/metrics/cadvisor" {
			http.NotFound(w, r)
			return
		}
		count := scrape.Add(1)
		// Scrapes 1 and 2 replay the same cAdvisor sample (cAdvisor
		// housekeeping did not run); scrape 3 carries a fresh counter.
		value, timestampMS := int64(100), int64(1000)
		if count == 3 {
			value, timestampMS = 110, 4000
		}
		fmt.Fprintf(w, `# TYPE container_cpu_usage_seconds_total counter
container_cpu_usage_seconds_total{namespace="ocm-sql-api",pod="ocm-sql-apiserver-abc",container="kine",cpu="total"} %d %d
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{namespace="ocm-sql-api",pod="ocm-sql-apiserver-abc",container="kine"} 200
`, value, timestampMS)
	}))
	defer server.Close()
	scraper, err := newAPIScraper(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	first, err := scraper.ScrapeContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Kine.CPUCore != nil {
		t.Fatalf("first scrape has no previous sample and must not report CPU: %#v", first)
	}
	stale, err := scraper.ScrapeContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stale.Kine.CPUCore != nil {
		t.Fatalf("replayed cAdvisor sample must not report a fake zero CPU rate: %#v", stale)
	}
	if stale.Kine.MemoryBytes == nil || *stale.Kine.MemoryBytes != 200 {
		t.Fatalf("memory must still be read from a replayed scrape: %#v", stale)
	}
	fresh, err := scraper.ScrapeContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Kine.CPUCore == nil {
		t.Fatalf("fresh sample should report a CPU rate: %#v", fresh)
	}
	// 10 counter seconds over the 3 seconds the cAdvisor timestamp advanced.
	if got := *fresh.Kine.CPUCore; got < 3.32 || got > 3.34 {
		t.Fatalf("CPU rate = %f, want the rate over the cAdvisor timestamp delta (10/3)", got)
	}
}

func TestDirectBaselineRecordingSkipsUnderachievedRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calibration", "direct.csv")
	collector := &metricCollector{baselineFile: path, estimator: NewEstimator(nil)}
	sample := Sample{
		Config:                   SimulatorConfig{Clusters: 2, WorksPerCluster: 2, UpdatesPerSecond: 10},
		OCMAPI:                   MetricSet{CPUCore: floatPointer(0.4), MemoryBytes: floatPointer(1024)},
		HostingEtcd:              MetricSet{CPUCore: floatPointer(0.2), MemoryBytes: floatPointer(512)},
		AchievedUpdatesPerSecond: 8,
	}
	if recorded, err := collector.record(sample); err != nil || recorded {
		t.Fatalf("underachieved sample should be skipped: recorded=%t err=%v", recorded, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("skipped sample created baseline file: %v", err)
	}
	sample.AchievedUpdatesPerSecond = 10
	if recorded, err := collector.record(sample); err != nil || !recorded {
		t.Fatalf("valid sample was not recorded: recorded=%t err=%v", recorded, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("baseline CSV was not created: %v", err)
	}
}
