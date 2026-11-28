// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
)

func main() {
	var hostingKubeconfig, ocmKubeconfig string
	var hostingPrometheusURL, ocmPrometheusURL string
	var hostingPrometheusTokenFile, ocmPrometheusTokenFile string
	var baselineCSV, runID, listenAddress string
	var enableLoad, recordDirectBaseline bool
	var initialConfig SimulatorConfig
	flag.StringVar(&hostingKubeconfig, "hosting-kubeconfig", "", "kubeconfig for the hosting cluster API server")
	flag.StringVar(&ocmKubeconfig, "ocm-kubeconfig", "", "kubeconfig for the OCM API server that receives simulator resources")
	flag.StringVar(&hostingPrometheusURL, "hosting-prometheus-url", "", "Prometheus base URL for hosting-cluster etcd container metrics")
	flag.StringVar(&ocmPrometheusURL, "ocm-prometheus-url", "", "Prometheus base URL for Kine and PostgreSQL container metrics")
	flag.StringVar(&hostingPrometheusTokenFile, "hosting-prometheus-token-file", "", "optional file containing a bearer token for the hosting Prometheus API")
	flag.StringVar(&ocmPrometheusTokenFile, "ocm-prometheus-token-file", "", "optional file containing a bearer token for the OCM Prometheus API")
	flag.StringVar(&baselineCSV, "baseline-csv", "", "optional CSV with direct-hub calibration samples; used for estimates and optional recording")
	flag.BoolVar(&recordDirectBaseline, "record-direct-baseline", false, "append samples to baseline-csv; use only when --ocm-kubeconfig points to an etcd-backed direct hub")
	flag.BoolVar(&enableLoad, "enable-load", false, "allow the dashboard to create simulator namespaces and ManifestWorks after explicit UI confirmation")
	flag.StringVar(&runID, "run-id", "", "optional stable DNS-label ownership ID (otherwise a random ID is generated)")
	flag.StringVar(&listenAddress, "listen-address", "127.0.0.1:8080", "local dashboard listen address; non-loopback addresses are rejected")
	flag.IntVar(&initialConfig.Clusters, "initial-clusters", 1, "initial desired simulated clusters (1-1000)")
	flag.IntVar(&initialConfig.WorksPerCluster, "initial-works-per-cluster", 1, "initial ManifestWorks per simulated cluster")
	flag.IntVar(&initialConfig.UpdatesPerSecond, "initial-updates-per-second", 0, "initial fleet-wide logical ManifestWork update rate")
	flag.Parse()

	if hostingKubeconfig == "" || ocmKubeconfig == "" {
		fatalf("both --hosting-kubeconfig and --ocm-kubeconfig are required")
	}
	if !isLoopbackAddress(listenAddress) {
		fatalf("dashboard must bind to loopback; non-loopback --listen-address is not allowed")
	}
	if recordDirectBaseline && baselineCSV == "" {
		fatalf("--record-direct-baseline requires --baseline-csv")
	}
	if recordDirectBaseline && !enableLoad {
		fatalf("--record-direct-baseline requires --enable-load so calibration is collected under simulated load")
	}
	if err := initialConfig.Validate(); err != nil {
		fatalf("invalid initial simulator controls: %v", err)
	}

	hostingRESTConfig, err := clientcmd.BuildConfigFromFlags("", hostingKubeconfig)
	if err != nil {
		fatalf("load hosting kubeconfig: %v", err)
	}
	ocmRESTConfig, err := clientcmd.BuildConfigFromFlags("", ocmKubeconfig)
	if err != nil {
		fatalf("load OCM kubeconfig: %v", err)
	}
	// Cleanup may reconcile hundreds of simulator-owned namespaces at once, while the logical update-rate slider remains separately bounded.
	ocmRESTConfig.QPS = 100
	ocmRESTConfig.Burst = 200
	coreClient, err := kubernetes.NewForConfig(ocmRESTConfig)
	if err != nil {
		fatalf("create OCM core client: %v", err)
	}
	clusterClient, err := clusterclientset.NewForConfig(ocmRESTConfig)
	if err != nil {
		fatalf("create OCM ManagedCluster client: %v", err)
	}
	workClient, err := workclientset.NewForConfig(ocmRESTConfig)
	if err != nil {
		fatalf("create OCM ManifestWork client: %v", err)
	}
	hostingScraper, err := newAPIScraper(hostingRESTConfig)
	if err != nil {
		fatalf("configure hosting API metrics: %v", err)
	}
	ocmScraper, err := newAPIScraper(ocmRESTConfig)
	if err != nil {
		fatalf("configure OCM API metrics: %v", err)
	}
	hostingToken, err := readToken(hostingPrometheusTokenFile)
	if err != nil {
		fatalf("read hosting Prometheus token: %v", err)
	}
	ocmToken, err := readToken(ocmPrometheusTokenFile)
	if err != nil {
		fatalf("read OCM Prometheus token: %v", err)
	}
	hostingPrometheus, err := newPromClient(hostingPrometheusURL)
	if err != nil {
		fatalf("configure hosting Prometheus: %v", err)
	}
	ocmPrometheus, err := newPromClient(ocmPrometheusURL)
	if err != nil {
		fatalf("configure OCM Prometheus: %v", err)
	}
	estimator, err := LoadBaseline(baselineCSV)
	if err != nil {
		fatalf("load direct-hub baseline: %v", err)
	}
	metricSource := &metricCollector{
		hostingProm: hostingPrometheus, ocmProm: ocmPrometheus,
		hostingToken: hostingToken, ocmToken: ocmToken,
		hostingAPI: hostingScraper, ocmAPI: ocmScraper,
		estimator: estimator, baselineFile: baselineCSV, recordDirect: recordDirectBaseline,
	}
	app, err := NewApp(appOptions{allowWrites: enableLoad, initialConfig: initialConfig, runID: runID}, coreClient, clusterClient.ClusterV1().ManagedClusters(), workClient.WorkV1(), ocmRESTConfig, metricSource)
	if err != nil {
		fatalf("initialize simulator: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app.Run(ctx)
	server := &http.Server{
		Addr: listenAddress, Handler: (&dashboardServer{app: app}).Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		waitForShutdown(server, 10*time.Second)
	}()
	fmt.Printf("ManifestWork simulator run %s listening at http://%s\n", app.runID, listenAddress)
	fmt.Printf("Load generation enabled: %t (requires typing RUN in the dashboard)\n", enableLoad)
	if recordDirectBaseline {
		fmt.Println("WARNING: direct baseline recording is enabled; ensure the OCM kubeconfig targets an etcd-backed direct hub")
	}
	serveErr := server.ListenAndServe()
	cancel()
	if !app.WaitForCleanup(11 * time.Second) {
		log.Printf("WARNING: simulator cleanup did not finish before shutdown timeout; reuse --run-id %s to clean up", app.runID)
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		fatalf("dashboard server: %v", serveErr)
	}
}

func readToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func fatalf(format string, args ...any) { log.Fatalf("ERROR: "+format, args...) }

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: manifestwork-simulator --hosting-kubeconfig FILE --ocm-kubeconfig FILE [flags]\n\n")
		flag.PrintDefaults()
	}
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
