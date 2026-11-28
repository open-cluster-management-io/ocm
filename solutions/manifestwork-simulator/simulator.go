// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	runIDLabel         = "simulator.ocm.io/run-id"
	clusterIndexLabel  = "simulator.ocm.io/cluster-index"
	workIndexLabel     = "simulator.ocm.io/work-index"
	clusterMarkerLabel = "simulator.ocm.io/simulated-cluster"
)

var errForeignManifestWork = errors.New("simulator namespace contains a ManifestWork not owned by this run")

type appOptions struct {
	allowWrites   bool
	initialConfig SimulatorConfig
	runID         string
}

// Registration stages, in order. They map onto the phases of
// registerCluster so the dashboard can show where pending registrations sit.
const (
	stageManagedCluster = "managed-cluster"
	stageCSR            = "csr"
	stageCertificate    = "certificate"
	stageHubAcceptance  = "hub-acceptance"
	stageNamespace      = "namespace"
)

// registrationStallSeconds flags a pending registration whose stage has not
// advanced for this long. A hub controller working through a large join
// backlog (for example a Kine-backed API) shows up here as many stalled
// registrations, which distinguishes a slow hub from a stuck simulator.
const registrationStallSeconds = 120

type clusterRuntime struct {
	name             string
	index            int
	cancel           context.CancelFunc
	terminating      bool
	cleanupBlocked   bool
	cleanupStarted   bool
	cleanupAttempts  int
	cleanupRetryAt   time.Time
	workNames        []string
	agentWorkClient  workclient.WorkV1Interface
	csrName          string
	registered       bool
	registrationDone chan struct{}
	// registrationStage/stageSince/registrationStarted track progress for
	// pending registrations; registrationStage is cleared once registered.
	registrationStage   string
	stageSince          time.Time
	registrationStarted time.Time
}

// hostingBaseline is the idle hosting API/etcd measurement that direct-hub
// estimates are projected from. Using the idle baseline instead of the live
// measurement keeps a loaded hub from double-counting its active workload.
// CPU baselines use the quietest idle sample (churn is one-sided) and memory
// baselines use the idle average.
type hostingBaseline struct {
	API  MetricSet
	Etcd MetricSet
}

// idleBaselineSamples is how many idle samples are averaged before the
// baseline is frozen. Once frozen it is never recomputed, so a baseline
// captured before the first run is not skewed by post-load cache warmth.
const idleBaselineSamples = 6

func averageFloats(values []*float64) *float64 {
	total, count := 0.0, 0
	for _, value := range values {
		if value != nil {
			total += *value
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return floatPointer(total / float64(count))
}

// minimumFloats returns the smallest observed value. Idle CPU noise is
// one-sided (background churn only adds to it), so the quietest sample best
// represents the true idle draw.
func minimumFloats(values []*float64) *float64 {
	var minimum *float64
	for _, value := range values {
		if value == nil {
			continue
		}
		if minimum == nil || *value < *minimum {
			minimum = value
		}
	}
	if minimum == nil {
		return nil
	}
	return floatPointer(*minimum)
}

func averageHostingBaseline(window []hostingBaseline) *hostingBaseline {
	result := &hostingBaseline{}
	apiCPU := make([]*float64, 0, len(window))
	apiMemory := make([]*float64, 0, len(window))
	etcdCPU := make([]*float64, 0, len(window))
	etcdMemory := make([]*float64, 0, len(window))
	for _, sample := range window {
		apiCPU = append(apiCPU, sample.API.CPUCore)
		apiMemory = append(apiMemory, sample.API.MemoryBytes)
		etcdCPU = append(etcdCPU, sample.Etcd.CPUCore)
		etcdMemory = append(etcdMemory, sample.Etcd.MemoryBytes)
	}
	result.API.CPUCore = minimumFloats(apiCPU)
	result.API.MemoryBytes = averageFloats(apiMemory)
	result.Etcd.CPUCore = minimumFloats(etcdCPU)
	result.Etcd.MemoryBytes = averageFloats(etcdMemory)
	return result
}

type App struct {
	options                  appOptions
	runID                    string
	core                     kubernetes.Interface
	managedClusters          clusterclient.ManagedClusterInterface
	works                    workclient.WorkV1Interface
	ocmRESTConfig            *rest.Config
	bootstrapClient          kubernetes.Interface
	bootstrapManagedClusters clusterclient.ManagedClusterInterface
	metrics                  *metricCollector
	reconcileDone            chan struct{}

	mu                      sync.RWMutex
	config                  SimulatorConfig
	assumptions             EstimateAssumptions
	started                 bool
	running                 bool
	clusters                map[string]*clusterRuntime
	lastError               string
	history                 []Sample
	fullCleanupRequested    bool
	fullCleanupStarted      bool
	fullCleanupRetryAt      time.Time
	fullCleanupAttempts     int
	nextClusterIndex        int
	pendingNamespaceDeletes map[string]types.UID
	lastPendingDeleteCheck  time.Time
	// cleanupTotal is the peak remaining namespace-deletion count of the
	// current cleanup episode; with the live remaining count it yields the
	// dashboard's finalized/total progress display.
	cleanupTotal int
	idleWindow   []hostingBaseline
	idleBaseline *hostingBaseline

	manifestWorkUpdates         atomic.Uint64
	managedClusterRegistrations atomic.Uint64
	registrationCSRs            atomic.Uint64
	registrationCSRsApproved    atomic.Uint64
	leaseWrites                 atomic.Uint64
	agentStatusUpdates          atomic.Uint64
	agentStatusLagNanos         atomic.Uint64
	errors                      atomic.Uint64
	activeWatches               atomic.Int64
	achievedTPS                 atomic.Uint64
	lastTPSCount                atomic.Uint64
	workCursor                  atomic.Uint64
	reconcileCursor             atomic.Uint64
	updateRevision              atomic.Uint64
	statusLimiter               chan struct{}
	cleanupLimiter              chan struct{}
}

func NewApp(options appOptions, core kubernetes.Interface, managedClusters clusterclient.ManagedClusterInterface, workClient workclient.WorkV1Interface, ocmRESTConfig *rest.Config, metrics *metricCollector) (*App, error) {
	if err := options.initialConfig.Validate(); err != nil {
		return nil, err
	}
	runID := options.runID
	if runID == "" {
		random := make([]byte, 4)
		if _, err := cryptorand.Read(random); err != nil {
			return nil, fmt.Errorf("generate simulator run ID: %w", err)
		}
		runID = hex.EncodeToString(random)
	}
	if len(runID) > 32 || !isDNSLabel(runID) {
		return nil, fmt.Errorf("run ID %q must be a DNS label of at most 32 characters", runID)
	}
	app := &App{
		options: options, runID: runID, core: core, managedClusters: managedClusters, works: workClient, ocmRESTConfig: ocmRESTConfig, metrics: metrics,
		config: options.initialConfig, assumptions: DefaultEstimateAssumptions(), clusters: map[string]*clusterRuntime{}, statusLimiter: make(chan struct{}, 20), cleanupLimiter: make(chan struct{}, 20), pendingNamespaceDeletes: map[string]types.UID{},
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ownedNamespaces, err := core.CoreV1().Namespaces().List(recoveryCtx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + runID})
	if err != nil {
		return nil, fmt.Errorf("find prior simulator resources for run ID %s: %w", runID, err)
	}
	ownedClusters, err := managedClusters.List(recoveryCtx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + runID})
	if err != nil {
		return nil, fmt.Errorf("find prior simulator ManagedClusters for run ID %s: %w", runID, err)
	}
	for _, namespace := range ownedNamespaces.Items {
		if namespace.Labels[runIDLabel] != runID || namespace.Labels[clusterMarkerLabel] != "true" {
			continue
		}
		index, parseErr := strconv.Atoi(namespace.Labels[clusterIndexLabel])
		if parseErr != nil {
			continue
		}
		if index > app.nextClusterIndex {
			app.nextClusterIndex = index
		}
		if namespace.DeletionTimestamp != nil {
			app.pendingNamespaceDeletes[namespace.Name] = namespace.UID
			continue
		}
		_, clusterCancel := context.WithCancel(context.Background())
		app.clusters[namespace.Name] = &clusterRuntime{name: namespace.Name, index: index, cancel: clusterCancel, terminating: true}
	}
	for _, managedCluster := range ownedClusters.Items {
		if managedCluster.Labels[runIDLabel] != runID || managedCluster.Labels[clusterMarkerLabel] != "true" {
			continue
		}
		index, parseErr := strconv.Atoi(managedCluster.Labels[clusterIndexLabel])
		if parseErr != nil {
			continue
		}
		if index > app.nextClusterIndex {
			app.nextClusterIndex = index
		}
		if _, found := app.clusters[managedCluster.Name]; found {
			continue
		}
		_, clusterCancel := context.WithCancel(context.Background())
		app.clusters[managedCluster.Name] = &clusterRuntime{name: managedCluster.Name, index: index, cancel: clusterCancel, terminating: true}
	}
	app.fullCleanupRequested = len(app.clusters) > 0
	return app, nil
}

func (a *App) Run(ctx context.Context) {
	a.reconcileDone = make(chan struct{})
	go func() { defer close(a.reconcileDone); a.reconcileLoop(ctx) }()
	go a.updateLoop(ctx)
	go a.scheduleUpdates(ctx)
	go a.metricsLoop(ctx)
}

func (a *App) WaitForCleanup(timeout time.Duration) bool {
	if a.reconcileDone == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-a.reconcileDone:
		return true
	case <-timer.C:
		return false
	}
}

func (a *App) SetConfig(config SimulatorConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	a.config = config
	a.mu.Unlock()
	return nil
}

func (a *App) SetAssumptions(assumptions EstimateAssumptions) error {
	if err := assumptions.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	a.assumptions = assumptions
	a.mu.Unlock()
	return nil
}

// ResetHistory drops collected graph samples on request. The captured idle
// baseline and the in-progress idle window are kept so direct-hub estimates
// stay grounded after a reset.
func (a *App) ResetHistory() {
	a.mu.Lock()
	a.history = nil
	a.mu.Unlock()
}

func (a *App) Start(ctx context.Context, confirmation string) error {
	if !a.options.allowWrites {
		return errors.New("load generation is disabled; restart with --enable-load after verifying the target is disposable")
	}
	if confirmation != "RUN" {
		return errors.New("type RUN to confirm load generation against the configured OCM API")
	}
	a.mu.RLock()
	priorResources := len(a.clusters) > 0 || a.fullCleanupRequested || a.fullCleanupStarted
	a.mu.RUnlock()
	if priorResources {
		return errors.New("prior resources for this run ID are still being cleaned up; wait for cleanup to finish before starting")
	}
	if _, err := a.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("test OCM API access before starting: %w", err)
	}
	if _, err := a.works.ManifestWorks(metav1.NamespaceAll).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("test ManifestWork API access before starting: %w", err)
	}
	var bootstrapClient kubernetes.Interface
	var bootstrapManagedClusters clusterclient.ManagedClusterInterface
	if a.ocmRESTConfig != nil {
		bootstrapConfig, err := a.newBootstrapClient(ctx)
		if err != nil {
			return err
		}
		bootstrapClient, err = kubernetes.NewForConfig(bootstrapConfig)
		if err != nil {
			return fmt.Errorf("create bootstrap identity client: %w", err)
		}
		bootstrapClusterClientset, err := clusterclientset.NewForConfig(bootstrapConfig)
		if err != nil {
			return fmt.Errorf("create bootstrap ManagedCluster client: %w", err)
		}
		bootstrapManagedClusters = bootstrapClusterClientset.ClusterV1().ManagedClusters()
	}
	a.mu.Lock()
	a.started = true
	a.running = true
	a.bootstrapClient = bootstrapClient
	a.bootstrapManagedClusters = bootstrapManagedClusters
	a.lastError = ""
	a.mu.Unlock()
	return nil
}

func (a *App) newBootstrapClient(ctx context.Context) (*rest.Config, error) {
	tokenRequest := &authenticationv1.TokenRequest{}
	token, err := a.core.CoreV1().ServiceAccounts("open-cluster-management").CreateToken(ctx, "agent-registration-bootstrap", tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("request simulator bootstrap token (check the agent-registration-bootstrap ServiceAccount): %w", err)
	}
	config := rest.CopyConfig(a.ocmRESTConfig)
	config.BearerToken = token.Status.Token
	config.BearerTokenFile = ""
	config.Username, config.Password = "", ""
	config.CertFile, config.KeyFile = "", ""
	config.CertData, config.KeyData = nil, nil
	config.ExecProvider, config.AuthProvider = nil, nil
	config.Impersonate = rest.ImpersonationConfig{}
	return config, nil
}

func (a *App) Pause() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

func (a *App) Stop() {
	a.mu.Lock()
	a.started = false
	a.running = false
	a.bootstrapClient = nil
	a.bootstrapManagedClusters = nil
	for _, cluster := range a.clusters {
		cluster.terminating = true
		if cluster.cancel != nil {
			cluster.cancel()
		}
	}
	a.fullCleanupRequested = true
	a.mu.Unlock()
}

type DashboardState struct {
	RunID                            string              `json:"runId"`
	AllowWrites                      bool                `json:"allowWrites"`
	CanStart                         bool                `json:"canStart"`
	Started                          bool                `json:"started"`
	Running                          bool                `json:"running"`
	Config                           SimulatorConfig     `json:"config"`
	Assumptions                      EstimateAssumptions `json:"assumptions"`
	DesiredClusters                  int                 `json:"desiredClusters"`
	ActiveClusters                   int                 `json:"activeClusters"`
	TerminatingClusters              int                 `json:"terminatingClusters"`
	PendingNamespaceDeletes          int                 `json:"pendingNamespaceDeletes"`
	CleanupTotal                     int                 `json:"cleanupTotal"`
	CleanupFinalized                 int                 `json:"cleanupFinalized"`
	DesiredWorks                     int                 `json:"desiredWorks"`
	ActiveWorks                      int                 `json:"activeWorks"`
	ActiveWatches                    int                 `json:"activeWatches"`
	AchievedUpdatesPerSecond         float64             `json:"achievedUpdatesPerSecond"`
	ManifestWorkUpdates              uint64              `json:"manifestWorkUpdates"`
	AgentStatusUpdates               uint64              `json:"agentStatusUpdates"`
	RegistrationCSRs                 uint64              `json:"registrationCSRs"`
	RegistrationCSRsApproved         uint64              `json:"registrationCSRsApproved"`
	ManagedClusterRegistrations      uint64              `json:"managedClusterRegistrations"`
	PendingRegistrations             int                 `json:"pendingRegistrations"`
	RegistrationStages               map[string]int      `json:"registrationStages"`
	PendingRegistrationAgeSeconds    float64             `json:"pendingRegistrationAgeSeconds"`
	PendingRegistrationOldestSeconds float64             `json:"pendingRegistrationOldestSeconds"`
	StalledRegistrations             int                 `json:"stalledRegistrations"`
	LeaseWrites                      uint64              `json:"leaseWrites"`
	AverageAgentStatusLagSeconds     float64             `json:"averageAgentStatusLagSeconds"`
	Errors                           uint64              `json:"errors"`
	LastError                        string              `json:"lastError,omitempty"`
	History                          []Sample            `json:"history"`
	DirectEstimate                   Estimate            `json:"directEstimate"`
}

// estimateBaselines returns the base the direct-hub estimate is projected
// from: the captured idle baseline when available, otherwise the most recent
// live hosting measurement.
func (a *App) estimateBaselines() (api, etcd MetricSet, idleCaptured bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.idleBaseline != nil {
		return a.idleBaseline.API, a.idleBaseline.Etcd, true
	}
	if len(a.history) > 0 {
		last := a.history[len(a.history)-1]
		return last.HostingAPI, last.HostingEtcd, false
	}
	return MetricSet{}, MetricSet{}, false
}

// recordIdleSampleLocked accumulates idle hosting measurements and freezes
// the baseline once enough of them exist. Callers must hold a.mu.
func (a *App) recordIdleSampleLocked(sample Sample, trulyIdle bool) {
	if !trulyIdle || a.idleBaseline != nil {
		return
	}
	a.idleWindow = append(a.idleWindow, hostingBaseline{API: sample.HostingAPI, Etcd: sample.HostingEtcd})
	if len(a.idleWindow) >= idleBaselineSamples {
		a.idleBaseline = averageHostingBaseline(a.idleWindow)
		a.idleWindow = nil
	}
}

// setRegistrationStage records where a pending registration currently is so
// the dashboard can distinguish a slow hub from a stuck simulator.
func (a *App) setRegistrationStage(name, stage string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cluster := a.clusters[name]; cluster != nil && !cluster.terminating {
		cluster.registrationStage = stage
		cluster.stageSince = time.Now()
	}
}

// registrationProgressLocked summarizes pending registrations: per-stage
// counts, mean and oldest age in seconds, and how many have sat in one stage
// beyond registrationStallSeconds. Callers must hold a.mu.
func (a *App) registrationProgressLocked(now time.Time) (stages map[string]int, meanAge, oldestAge float64, stalled, pending int) {
	if a.bootstrapClient == nil {
		return nil, 0, 0, 0, 0
	}
	var totalAge float64
	for _, cluster := range a.clusters {
		if cluster.terminating || cluster.registered {
			continue
		}
		pending++
		stage := cluster.registrationStage
		if stage == "" {
			stage = "unknown"
		}
		if stages == nil {
			stages = map[string]int{}
		}
		stages[stage]++
		age := now.Sub(cluster.registrationStarted).Seconds()
		totalAge += age
		if age > oldestAge {
			oldestAge = age
		}
		if now.Sub(cluster.stageSince).Seconds() >= registrationStallSeconds {
			stalled++
		}
	}
	if pending > 0 {
		meanAge = totalAge / float64(pending)
	}
	return stages, meanAge, oldestAge, stalled, pending
}

// updateCleanupProgressLocked records the peak remaining cleanup work of the
// current episode so the dashboard can show finalized/total progress. The
// total never shrinks while work remains and resets when an episode ends.
// Callers must hold a.mu.
func (a *App) updateCleanupProgressLocked(terminating, pendingDeletes int) {
	remaining := terminating + pendingDeletes
	if remaining == 0 {
		a.cleanupTotal = 0
	} else if remaining > a.cleanupTotal {
		a.cleanupTotal = remaining
	}
}

func (a *App) State() DashboardState {
	a.mu.RLock()
	state := DashboardState{
		RunID: a.runID, AllowWrites: a.options.allowWrites, Started: a.started, Running: a.running,
		Config: a.config, Assumptions: a.assumptions, LastError: a.lastError,
		ManifestWorkUpdates: a.manifestWorkUpdates.Load(), AgentStatusUpdates: a.agentStatusUpdates.Load(),
		RegistrationCSRs: a.registrationCSRs.Load(), RegistrationCSRsApproved: a.registrationCSRsApproved.Load(),
		ManagedClusterRegistrations: a.managedClusterRegistrations.Load(), LeaseWrites: a.leaseWrites.Load(),
		AverageAgentStatusLagSeconds: averageStatusLag(a.agentStatusLagNanos.Load(), a.agentStatusUpdates.Load()),
		Errors:                       a.errors.Load(), ActiveWatches: int(a.activeWatches.Load()),
		AchievedUpdatesPerSecond: math.Float64frombits(a.achievedTPS.Load()),
		History:                  append([]Sample(nil), a.history...),
		PendingNamespaceDeletes:  len(a.pendingNamespaceDeletes),
	}
	for _, cluster := range a.clusters {
		if cluster.terminating {
			state.TerminatingClusters++
			continue
		}
		state.ActiveClusters++
		state.ActiveWorks += len(cluster.workNames)
	}
	state.RegistrationStages, state.PendingRegistrationAgeSeconds, state.PendingRegistrationOldestSeconds, state.StalledRegistrations, state.PendingRegistrations = a.registrationProgressLocked(time.Now())
	state.CleanupTotal = a.cleanupTotal
	if state.CleanupTotal > 0 {
		state.CleanupFinalized = max(0, state.CleanupTotal-state.TerminatingClusters-state.PendingNamespaceDeletes)
	}
	state.CanStart = state.AllowWrites && !state.Started && state.ActiveClusters == 0 && state.TerminatingClusters == 0 && !a.fullCleanupRequested && !a.fullCleanupStarted
	if a.started {
		state.DesiredClusters = a.config.Clusters
		state.DesiredWorks = a.config.Clusters * a.config.WorksPerCluster
	}
	// A stopped run must not keep projecting the load it just deleted: while
	// cleanup or pending deletes exist, project only what actually remains.
	cleanupUnderway := a.fullCleanupRequested || a.fullCleanupStarted ||
		state.TerminatingClusters > 0 || state.ActiveClusters > 0 || len(a.pendingNamespaceDeletes) > 0
	projection := projectionConfig(a.started, a.config, cleanupUnderway, state.ActiveClusters, state.ActiveWorks)
	a.mu.RUnlock()
	estimateAPI, estimateEtcd, idleCaptured := a.estimateBaselines()
	if a.metrics == nil {
		state.DirectEstimate = provisionalDirectEstimate(projection, state.Assumptions, estimateAPI, estimateEtcd, idleCaptured)
		return state
	}
	calibrated := a.metrics.predict(projection)
	if calibrated.Calibrated {
		state.DirectEstimate = calibrated
	} else {
		state.DirectEstimate = provisionalDirectEstimate(projection, state.Assumptions, estimateAPI, estimateEtcd, idleCaptured)
	}
	return state
}

func (a *App) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			a.Stop()
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				a.reconcile(cleanupCtx)
				state := a.State()
				if state.ActiveClusters == 0 && state.TerminatingClusters == 0 {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			return
		case <-ticker.C:
			a.reconcile(ctx)
		}
	}
}

func (a *App) reconcile(ctx context.Context) {
	a.refreshPendingNamespaceDeletes(ctx)
	a.mu.Lock()
	started, config := a.started, a.config
	if !started {
		for _, cluster := range a.clusters {
			if !cluster.terminating {
				cluster.terminating = true
				if cluster.cancel != nil {
					cluster.cancel()
				}
			}
		}
	}
	launchFullCleanup := !started && a.fullCleanupRequested && !a.fullCleanupStarted && !time.Now().Before(a.fullCleanupRetryAt)
	fullCleanupRequested := a.fullCleanupRequested
	if launchFullCleanup {
		a.fullCleanupStarted = true
	}
	activeNames := make([]string, 0, len(a.clusters))
	terminatingNames := make([]string, 0, len(a.clusters))
	for name, cluster := range a.clusters {
		if cluster.terminating {
			terminatingNames = append(terminatingNames, name)
		} else {
			activeNames = append(activeNames, name)
		}
	}
	sort.Strings(activeNames)
	sort.Strings(terminatingNames)
	a.updateCleanupProgressLocked(len(terminatingNames), len(a.pendingNamespaceDeletes))
	cleanupNames := make([]string, 0, len(terminatingNames))
	now := time.Now()
	if !launchFullCleanup && !a.fullCleanupStarted {
		for _, name := range terminatingNames {
			cluster := a.clusters[name]
			if cluster.cleanupBlocked || cluster.cleanupStarted || now.Before(cluster.cleanupRetryAt) {
				continue
			}
			cluster.cleanupStarted = true
			cleanupNames = append(cleanupNames, name)
		}
	}
	a.mu.Unlock()

	if launchFullCleanup {
		go a.cleanupRun(ctx)
		return
	}
	for _, name := range cleanupNames {
		go a.cleanupCluster(ctx, name)
	}
	if !started || fullCleanupRequested {
		return
	}
	if len(activeNames) > config.Clusters {
		for _, name := range activeNames[config.Clusters:] {
			a.beginDeleteCluster(name)
		}
		return
	}
	if len(activeNames) < config.Clusters {
		a.mu.Lock()
		a.nextClusterIndex++
		index := a.nextClusterIndex
		a.mu.Unlock()
		if err := a.createCluster(ctx, index); err != nil {
			a.reportError(err)
		}
		return
	}
	registeredNames := make([]string, 0, len(activeNames))
	for _, name := range activeNames {
		a.mu.RLock()
		cluster := a.clusters[name]
		registered := cluster != nil && cluster.registered
		a.mu.RUnlock()
		if registered {
			registeredNames = append(registeredNames, name)
		}
	}
	if len(registeredNames) > 0 {
		name := registeredNames[int(a.reconcileCursor.Add(1)-1)%len(registeredNames)]
		if err := a.reconcileWorks(ctx, name, config.WorksPerCluster); err != nil {
			a.reportError(err)
		}
	}
}

func (a *App) createCluster(ctx context.Context, index int) error {
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if !started {
		return nil
	}
	name := fmt.Sprintf("sim-%s-c%04d", a.runID, index)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{runIDLabel: a.runID, clusterMarkerLabel: "true", clusterIndexLabel: strconv.Itoa(index)},
	}}
	created, err := a.core.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = a.core.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err == nil && (created.Labels[runIDLabel] != a.runID || created.Labels[clusterMarkerLabel] != "true" || created.Labels[clusterIndexLabel] != strconv.Itoa(index)) {
			return fmt.Errorf("refusing to use namespace %s not owned by simulator run %s", name, a.runID)
		}
	}
	if err != nil {
		return fmt.Errorf("create simulator cluster namespace %s: %w", name, err)
	}
	if created.Labels[runIDLabel] != a.runID || created.Labels[clusterMarkerLabel] != "true" || created.Labels[clusterIndexLabel] != strconv.Itoa(index) {
		return fmt.Errorf("namespace %s is not owned by simulator run %s", name, a.runID)
	}
	a.mu.Lock()
	if index > a.nextClusterIndex {
		a.nextClusterIndex = index
	}
	a.mu.Unlock()
	watchCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	cluster := &clusterRuntime{name: name, index: index, cancel: cancel, terminating: !a.started}
	if a.bootstrapClient == nil {
		cluster.registered = true
	} else {
		cluster.registrationDone = make(chan struct{})
		cluster.registrationStage = stageManagedCluster
		cluster.stageSince = time.Now()
		cluster.registrationStarted = time.Now()
	}
	a.clusters[name] = cluster
	a.mu.Unlock()
	if cluster.terminating {
		cancel()
	} else if cluster.registrationDone != nil {
		go func() {
			defer close(cluster.registrationDone)
			if err := a.registerCluster(watchCtx, name, index); err != nil && watchCtx.Err() == nil {
				a.reportError(err)
			}
		}()
	} else {
		go a.agentLoop(watchCtx, name)
	}
	return nil
}

func (a *App) beginDeleteCluster(name string) {
	a.mu.Lock()
	cluster := a.clusters[name]
	if cluster == nil || cluster.terminating {
		a.mu.Unlock()
		return
	}
	cluster.terminating = true
	if cluster.cancel != nil {
		cluster.cancel()
	}
	a.mu.Unlock()
}

func (a *App) cleanupCluster(parent context.Context, name string) {
	select {
	case a.cleanupLimiter <- struct{}{}:
		defer func() { <-a.cleanupLimiter }()
	case <-parent.Done():
		a.finishCleanup(name, parent.Err())
		return
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	uid, err := a.cleanupClusterResources(ctx, name)
	if err != nil {
		a.finishCleanup(name, err)
		return
	}
	a.mu.Lock()
	delete(a.clusters, name)
	if uid != "" {
		a.pendingNamespaceDeletes[name] = uid
	}
	a.mu.Unlock()
}

func (a *App) cleanupRun(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	selector := runIDLabel + "=" + a.runID + "," + clusterMarkerLabel + "=true"
	namespaces, err := a.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		a.finishFullCleanup(err)
		return
	}
	if err := a.cleanupOrphanRegistrations(ctx, namespaces.Items); err != nil {
		a.finishFullCleanup(err)
		return
	}
	if len(namespaces.Items) == 0 {
		a.mu.Lock()
		for name := range a.clusters {
			delete(a.clusters, name)
		}
		a.fullCleanupRequested, a.fullCleanupStarted = false, false
		a.fullCleanupAttempts, a.fullCleanupRetryAt, a.lastError = 0, time.Time{}, ""
		a.mu.Unlock()
		return
	}
	ownedNamespaces := make(map[string]*corev1.Namespace, len(namespaces.Items))
	for i := range namespaces.Items {
		namespace := &namespaces.Items[i]
		if namespace.Labels[runIDLabel] == a.runID && namespace.Labels[clusterMarkerLabel] == "true" {
			ownedNamespaces[namespace.Name] = namespace
		}
	}
	blocked := map[string]bool{}
	var blockedMu sync.Mutex
	var deleteErrors []error
	var workers sync.WaitGroup
	var deleted sync.Map
	for name, namespace := range ownedNamespaces {
		if namespace.DeletionTimestamp != nil {
			if err := a.cleanupRegistration(ctx, name); err != nil {
				a.mu.Lock()
				deleteErrors = append(deleteErrors, fmt.Errorf("clean simulator registration for %s: %w", name, err))
				a.mu.Unlock()
				continue
			}
			deleted.Store(name, namespace.UID)
			continue
		}
		workers.Add(1)
		go func(name string, namespace *corev1.Namespace) {
			defer workers.Done()
			select {
			case a.cleanupLimiter <- struct{}{}:
				defer func() { <-a.cleanupLimiter }()
			case <-ctx.Done():
				a.mu.Lock()
				deleteErrors = append(deleteErrors, ctx.Err())
				a.mu.Unlock()
				return
			}
			works, err := a.works.ManifestWorks(name).List(ctx, metav1.ListOptions{})
			if err != nil {
				a.mu.Lock()
				deleteErrors = append(deleteErrors, fmt.Errorf("inspect ManifestWorks in %s: %w", name, err))
				a.mu.Unlock()
				return
			}
			for _, work := range works.Items {
				if work.Labels[runIDLabel] != a.runID {
					blockedMu.Lock()
					blocked[name] = true
					blockedMu.Unlock()
					break
				}
			}
			if err := a.works.ManifestWorks(name).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID}); err != nil && !apierrors.IsNotFound(err) {
				a.mu.Lock()
				deleteErrors = append(deleteErrors, fmt.Errorf("delete owned ManifestWorks in %s: %w", name, err))
				a.mu.Unlock()
				return
			}
			blockedMu.Lock()
			isBlocked := blocked[name]
			blockedMu.Unlock()
			if isBlocked {
				return
			}
			if err := a.cleanupRegistration(ctx, name); err != nil {
				a.mu.Lock()
				deleteErrors = append(deleteErrors, fmt.Errorf("clean simulator registration for %s: %w", name, err))
				a.mu.Unlock()
				return
			}
			err = a.core.CoreV1().Namespaces().Delete(ctx, name, deleteByUID(namespace.UID))
			if err != nil && !apierrors.IsNotFound(err) {
				a.mu.Lock()
				deleteErrors = append(deleteErrors, fmt.Errorf("delete simulator namespace %s: %w", name, err))
				a.mu.Unlock()
				return
			}
			deleted.Store(name, namespace.UID)
		}(name, namespace)
	}
	workers.Wait()
	a.mu.Lock()
	deleted.Range(func(key, value any) bool {
		name := key.(string)
		delete(a.clusters, name)
		a.pendingNamespaceDeletes[name] = value.(types.UID)
		return true
	})
	for name := range blocked {
		if cluster := a.clusters[name]; cluster != nil {
			cluster.cleanupBlocked = true
			cluster.cleanupStarted = false
		}
	}
	a.mu.Unlock()
	if len(deleteErrors) > 0 {
		a.finishFullCleanup(errors.Join(deleteErrors...))
		return
	}
	a.mu.Lock()
	a.fullCleanupRequested, a.fullCleanupStarted = false, false
	a.fullCleanupAttempts, a.fullCleanupRetryAt = 0, time.Time{}
	if len(blocked) == 0 {
		a.lastError = ""
	}
	a.mu.Unlock()
	if len(blocked) > 0 {
		a.reportError(fmt.Errorf("preserving %d simulator namespace(s) containing ManifestWorks not owned by run %s", len(blocked), a.runID))
	}
}

func (a *App) refreshPendingNamespaceDeletes(ctx context.Context) {
	a.mu.Lock()
	if len(a.pendingNamespaceDeletes) == 0 || time.Now().Before(a.lastPendingDeleteCheck) {
		a.mu.Unlock()
		return
	}
	a.lastPendingDeleteCheck = time.Now().Add(5 * time.Second)
	a.mu.Unlock()
	namespaces, err := a.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID + "," + clusterMarkerLabel + "=true"})
	if err != nil {
		a.reportError(fmt.Errorf("refresh pending simulator namespace deletions: %w", err))
		return
	}
	present := make(map[string]struct{}, len(namespaces.Items))
	for _, namespace := range namespaces.Items {
		present[namespace.Name] = struct{}{}
	}
	a.mu.Lock()
	for name := range a.pendingNamespaceDeletes {
		if _, found := present[name]; !found {
			delete(a.pendingNamespaceDeletes, name)
		}
	}
	a.mu.Unlock()
}

func (a *App) finishFullCleanup(err error) {
	a.mu.Lock()
	a.fullCleanupStarted = false
	if err != nil && !errors.Is(err, context.Canceled) {
		a.fullCleanupAttempts++
		delay := time.Second << min(a.fullCleanupAttempts-1, 5)
		a.fullCleanupRetryAt = time.Now().Add(delay)
	}
	a.mu.Unlock()
	if err != nil && !errors.Is(err, context.Canceled) {
		a.reportError(err)
	}
}

func (a *App) cleanupClusterResources(ctx context.Context, name string) (types.UID, error) {
	namespace, err := a.core.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		namespace = nil
	} else if err != nil {
		return "", fmt.Errorf("get simulator namespace %s: %w", name, err)
	}
	if namespace != nil && (namespace.Labels[runIDLabel] != a.runID || namespace.Labels[clusterMarkerLabel] != "true") {
		return "", nil
	}

	selector := metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID}
	if err := a.works.ManifestWorks(name).DeleteCollection(ctx, metav1.DeleteOptions{}, selector); err != nil && !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("delete owned ManifestWorks in %s: %w", name, err)
	}
	for {
		owned, err := a.works.ManifestWorks(name).List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID, Limit: 1})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("wait for owned ManifestWorks in %s to delete: %w", name, err)
		}
		if len(owned.Items) == 0 {
			break
		}
		if err := waitContext(ctx, 100*time.Millisecond); !err {
			return "", ctx.Err()
		}
	}
	remaining, err := a.works.ManifestWorks(name).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("verify simulator namespace %s contents: %w", name, err)
	}
	if remaining != nil && len(remaining.Items) > 0 {
		return "", fmt.Errorf("%w: preserving namespace %s", errForeignManifestWork, name)
	}
	if err := a.cleanupRegistration(ctx, name); err != nil {
		return "", err
	}
	if namespace == nil {
		return "", nil
	}
	if namespace.DeletionTimestamp == nil {
		if err := a.core.CoreV1().Namespaces().Delete(ctx, name, deleteByUID(namespace.UID)); err != nil && !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("delete simulator namespace %s: %w", name, err)
		}
	}
	return namespace.UID, nil
}

func (a *App) finishCleanup(name string, err error) {
	a.mu.Lock()
	cluster := a.clusters[name]
	if cluster == nil {
		a.mu.Unlock()
		return
	}
	cluster.cleanupStarted = false
	if errors.Is(err, errForeignManifestWork) {
		cluster.cleanupBlocked = true
	} else if err != nil && !errors.Is(err, context.Canceled) {
		cluster.cleanupAttempts++
		delay := time.Second << min(cluster.cleanupAttempts-1, 5)
		cluster.cleanupRetryAt = time.Now().Add(delay)
	}
	a.mu.Unlock()
	if err != nil && !errors.Is(err, context.Canceled) {
		a.reportError(err)
	}
}

func (a *App) reconcileWorks(ctx context.Context, namespace string, desired int) error {
	list, err := a.works.ManifestWorks(namespace).List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID})
	if err != nil {
		return fmt.Errorf("list simulator ManifestWorks in %s: %w", namespace, err)
	}
	owned := make([]workv1.ManifestWork, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Labels[runIDLabel] == a.runID {
			owned = append(owned, item)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].Name < owned[j].Name })
	if len(owned) > desired {
		item := owned[len(owned)-1]
		if item.DeletionTimestamp == nil {
			if err := a.works.ManifestWorks(namespace).Delete(ctx, item.Name, deleteByUID(item.UID)); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete simulator ManifestWork %s/%s: %w", namespace, item.Name, err)
			}
		}
		owned = owned[:len(owned)-1]
		a.setClusterWorks(namespace, owned)
		return nil
	}
	if len(owned) < desired {
		index := len(owned) + 1
		work := buildManifestWork(a.runID, namespace, index)
		_, err := a.works.ManifestWorks(namespace).Create(ctx, work, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, getErr := a.works.ManifestWorks(namespace).Get(ctx, work.Name, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("check existing simulator ManifestWork %s/%s: %w", namespace, work.Name, getErr)
			}
			if existing.Labels[runIDLabel] != a.runID {
				return fmt.Errorf("refusing to use ManifestWork %s/%s not owned by simulator run %s", namespace, work.Name, a.runID)
			}
			owned = append(owned, *existing)
		} else if err != nil {
			return fmt.Errorf("create simulator ManifestWork %s/%s: %w", namespace, work.Name, err)
		} else {
			owned = append(owned, *work)
		}
	}
	workNames := make([]string, len(owned))
	for i := range owned {
		workNames[i] = owned[i].Name
	}
	a.mu.Lock()
	if cluster := a.clusters[namespace]; cluster != nil && !cluster.terminating {
		cluster.workNames = workNames
	}
	a.mu.Unlock()
	return nil
}

func (a *App) setClusterWorks(namespace string, works []workv1.ManifestWork) {
	names := make([]string, len(works))
	for i := range works {
		names[i] = works[i].Name
	}
	a.mu.Lock()
	if cluster := a.clusters[namespace]; cluster != nil && !cluster.terminating {
		cluster.workNames = names
	}
	a.mu.Unlock()
}

func deleteByUID(uid types.UID) metav1.DeleteOptions {
	return metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
}

func (a *App) agentLoop(ctx context.Context, namespace string) {
	a.mu.RLock()
	cluster := a.clusters[namespace]
	agentWorks := a.works
	if cluster != nil && cluster.agentWorkClient != nil {
		agentWorks = cluster.agentWorkClient
	}
	a.mu.RUnlock()
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		list, err := agentWorks.ManifestWorks(namespace).List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.reportError(fmt.Errorf("list work for simulated agent in %s: %w", namespace, err))
			if !waitContext(ctx, backoff) {
				return
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		for i := range list.Items {
			work := &list.Items[i]
			if work.Labels[runIDLabel] == a.runID && !workStatusCurrent(work) {
				if err := a.acknowledgeWorkWithClient(ctx, agentWorks, work); err != nil {
					if ctx.Err() != nil {
						return
					}
					a.reportError(fmt.Errorf("simulated agent status for %s/%s: %w", namespace, work.Name, err))
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
		timeout := int64(30)
		watcher, err := agentWorks.ManifestWorks(namespace).Watch(ctx, metav1.ListOptions{
			ResourceVersion: list.ResourceVersion, TimeoutSeconds: &timeout, AllowWatchBookmarks: true,
			LabelSelector: runIDLabel + "=" + a.runID,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.reportError(fmt.Errorf("simulated agent watch in %s: %w", namespace, err))
			if !waitContext(ctx, backoff) {
				return
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 250 * time.Millisecond
		a.activeWatches.Add(1)
		watching := true
		for watching {
			var event watch.Event
			select {
			case <-ctx.Done():
				watching = false
				continue
			case event, watching = <-watcher.ResultChan():
				if !watching {
					continue
				}
			}
			if event.Type == watch.Error {
				a.reportError(fmt.Errorf("simulated agent watch event in %s: %v", namespace, event.Object))
				continue
			}
			work, ok := event.Object.(*workv1.ManifestWork)
			if !ok || work.Labels[runIDLabel] != a.runID {
				continue
			}
			if event.Type == watch.Deleted {
				continue
			}
			if workStatusCurrent(work) {
				continue
			}
			if err := a.acknowledgeWorkWithClient(ctx, agentWorks, work); err != nil {
				if ctx.Err() != nil {
					break
				}
				a.reportError(fmt.Errorf("simulated agent status for %s/%s: %w", namespace, work.Name, err))
			}
			if ctx.Err() != nil {
				break
			}
		}
		watcher.Stop()
		a.activeWatches.Add(-1)
		if !waitContext(ctx, 100*time.Millisecond) {
			return
		}
	}
}

func (a *App) acknowledgeWork(ctx context.Context, work *workv1.ManifestWork) error {
	return a.acknowledgeWorkWithClient(ctx, a.works, work)
}

func (a *App) acknowledgeWorkWithClient(ctx context.Context, client workclient.WorkV1Interface, work *workv1.ManifestWork) error {
	select {
	case a.statusLimiter <- struct{}{}:
		defer func() { <-a.statusLimiter }()
	case <-ctx.Done():
		return ctx.Err()
	}
	startedAt := time.Now()
	copy := work.DeepCopy()
	now := metav1.Now()
	copy.Status.Conditions = []metav1.Condition{
		{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "SimulatedApply", Message: "Synthetic work agent acknowledged the manifests.", ObservedGeneration: copy.Generation, LastTransitionTime: now},
		{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "SimulatedAvailable", Message: "Synthetic workload is available in the simulator.", ObservedGeneration: copy.Generation, LastTransitionTime: now},
	}
	copy.Status.ResourceStatus.Manifests = make([]workv1.ManifestCondition, 0, len(copy.Spec.Workload.Manifests))
	for ordinal, manifest := range copy.Spec.Workload.Manifests {
		var object struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(manifest.Raw, &object); err != nil {
			return fmt.Errorf("decode manifest %d: %w", ordinal, err)
		}
		groupVersion, err := schema.ParseGroupVersion(object.APIVersion)
		if err != nil {
			return fmt.Errorf("parse manifest %d apiVersion %q: %w", ordinal, object.APIVersion, err)
		}
		conditions := []metav1.Condition{
			{Type: workv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "SimulatedApply", Message: "Synthetic work agent acknowledged the manifest.", ObservedGeneration: copy.Generation, LastTransitionTime: now},
			{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "SimulatedAvailable", Message: "Synthetic manifest is available in the simulator.", ObservedGeneration: copy.Generation, LastTransitionTime: now},
		}
		copy.Status.ResourceStatus.Manifests = append(copy.Status.ResourceStatus.Manifests, workv1.ManifestCondition{
			ResourceMeta: workv1.ManifestResourceMeta{Ordinal: int32(ordinal), Group: groupVersion.Group, Version: groupVersion.Version, Kind: object.Kind, Name: object.Metadata.Name, Namespace: object.Metadata.Namespace},
			Conditions:   conditions,
		})
	}
	_, err := client.ManifestWorks(work.Namespace).UpdateStatus(ctx, copy, metav1.UpdateOptions{})
	if err == nil {
		a.agentStatusUpdates.Add(1)
		a.agentStatusLagNanos.Add(uint64(time.Since(startedAt).Nanoseconds()))
	}
	return err
}

func averageStatusLag(totalNanos, updates uint64) float64 {
	if updates == 0 {
		return 0
	}
	return float64(totalNanos) / float64(updates) / float64(time.Second)
}

func workStatusCurrent(work *workv1.ManifestWork) bool {
	for _, condition := range work.Status.Conditions {
		if condition.Type == workv1.WorkApplied && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == work.Generation {
			return true
		}
	}
	return false
}

func (a *App) updateLoop(ctx context.Context) {
	second := time.NewTicker(time.Second)
	defer second.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-second.C:
			current := a.manifestWorkUpdates.Load()
			previous := a.lastTPSCount.Swap(current)
			a.achievedTPS.Store(math.Float64bits(float64(current - previous)))
		}
	}
}

func (a *App) scheduleUpdates(ctx context.Context) {
	for ctx.Err() == nil {
		a.mu.RLock()
		running, tps := a.running, a.config.UpdatesPerSecond
		a.mu.RUnlock()
		if !running || tps == 0 {
			if !waitContext(ctx, 100*time.Millisecond) {
				return
			}
			continue
		}
		interval := time.Second / time.Duration(tps)
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		if !waitContext(ctx, interval) {
			return
		}
		a.mu.RLock()
		running = a.running
		a.mu.RUnlock()
		if running {
			a.updateOne(ctx)
		}
	}
}

func (a *App) updateOne(ctx context.Context) {
	a.mu.RLock()
	var works []struct{ namespace, name string }
	for namespace, cluster := range a.clusters {
		if cluster.terminating {
			continue
		}
		for _, name := range cluster.workNames {
			works = append(works, struct{ namespace, name string }{namespace, name})
		}
	}
	a.mu.RUnlock()
	if len(works) == 0 {
		return
	}
	index := int(a.workCursor.Add(1)-1) % len(works)
	selected := works[index]
	work, err := a.works.ManifestWorks(selected.namespace).Get(ctx, selected.name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			a.reportError(fmt.Errorf("get simulator ManifestWork %s/%s: %w", selected.namespace, selected.name, err))
		}
		return
	}
	if work.Labels[runIDLabel] != a.runID {
		return
	}
	updated := work.DeepCopy()
	updatedResource := false
	revision := strconv.FormatUint(a.updateRevision.Add(1), 10)
	for index, manifest := range updated.Spec.Workload.Manifests {
		var object map[string]any
		if err := json.Unmarshal(manifest.Raw, &object); err != nil {
			continue
		}
		if object["kind"] != "ConfigMap" {
			continue
		}
		data, ok := object["data"].(map[string]any)
		if !ok {
			data = map[string]any{}
		}
		data["simulationRevision"] = revision
		object["data"] = data
		raw, err := json.Marshal(object)
		if err != nil {
			continue
		}
		updated.Spec.Workload.Manifests[index].RawExtension = runtime.RawExtension{Raw: raw}
		updatedResource = true
		break
	}
	if !updatedResource {
		a.reportError(fmt.Errorf("ManifestWork %s/%s has no updateable ConfigMap", selected.namespace, selected.name))
		return
	}
	if _, err := a.works.ManifestWorks(selected.namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		a.reportError(fmt.Errorf("update simulator ManifestWork %s/%s: %w", selected.namespace, selected.name, err))
		return
	}
	a.manifestWorkUpdates.Add(1)
}

func (a *App) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.RLock()
			config, assumptions, running, started := a.config, a.assumptions, a.running, a.started
			cleanupUnderway := a.fullCleanupRequested || a.fullCleanupStarted || len(a.pendingNamespaceDeletes) > 0
			activeClusters, activeWorks, registeredClusters := 0, 0, 0
			for _, cluster := range a.clusters {
				if cluster.terminating {
					cleanupUnderway = true
					continue
				}
				activeClusters++
				activeWorks += len(cluster.workNames)
				if cluster.registered {
					registeredClusters++
				}
			}
			projection := projectionConfig(started, config, cleanupUnderway, activeClusters, activeWorks)
			trulyIdle := !started && !cleanupUnderway && activeClusters == 0
			a.mu.RUnlock()
			if a.metrics == nil {
				continue
			}
			recordBaseline := started && running && activeClusters == config.Clusters && registeredClusters == config.Clusters && activeWorks == config.Clusters*config.WorksPerCluster
			achievedTPS := math.Float64frombits(a.achievedTPS.Load())
			sample := a.metrics.collect(ctx, config, recordBaseline, achievedTPS)
			estimateAPI, estimateEtcd, idleCaptured := a.estimateBaselines()
			calibrated := a.metrics.predict(projection)
			if calibrated.Calibrated {
				sample.DirectEstimate = calibrated
			} else {
				sample.DirectEstimate = provisionalDirectEstimate(projection, assumptions, estimateAPI, estimateEtcd, idleCaptured)
			}
			sample.ManifestWorkUpdates = a.manifestWorkUpdates.Load()
			sample.AgentStatusUpdates = a.agentStatusUpdates.Load()
			sample.RegistrationCSRs = a.registrationCSRs.Load()
			sample.RegistrationCSRsApproved = a.registrationCSRsApproved.Load()
			sample.ManagedClusterRegistrations = a.managedClusterRegistrations.Load()
			sample.LeaseWrites = a.leaseWrites.Load()
			a.mu.RLock()
			sample.RegistrationStages, sample.PendingRegistrationAgeSeconds, sample.PendingRegistrationOldestSeconds, sample.StalledRegistrations, sample.PendingRegistrations = a.registrationProgressLocked(time.Now())
			a.mu.RUnlock()
			sample.AverageAgentStatusLagSeconds = averageStatusLag(a.agentStatusLagNanos.Load(), sample.AgentStatusUpdates)
			sample.Errors = a.errors.Load()
			sample.ActiveWatches = int(a.activeWatches.Load())
			a.mu.Lock()
			a.history = append(a.history, sample)
			if len(a.history) > 180 {
				a.history = append([]Sample(nil), a.history[len(a.history)-180:]...)
			}
			a.recordIdleSampleLocked(sample, trulyIdle)
			a.mu.Unlock()
		}
	}
}

func (a *App) reportError(err error) {
	a.errors.Add(1)
	a.mu.Lock()
	a.lastError = err.Error()
	a.mu.Unlock()
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func buildManifestWork(runID, namespace string, index int) *workv1.ManifestWork {
	name := fmt.Sprintf("sim-work-%04d", index)
	seed := int64(index)
	for i, char := range runID {
		seed += int64(char) << uint((i%8)*8)
	}
	random := mathrand.New(mathrand.NewSource(seed))
	appName := fmt.Sprintf("app-%04d", index)
	appLabels := map[string]any{"app.kubernetes.io/name": appName, "simulator.ocm.io/run-id": runID}

	manifests := []map[string]any{
		{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]any{"name": appName, "namespace": "default", "labels": appLabels},
			"spec": map[string]any{
				"replicas": random.Intn(3) + 1,
				"selector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": appName}},
				"template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app.kubernetes.io/name": appName}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "registry.k8s.io/pause:3.10", "resources": map[string]any{"requests": map[string]any{"cpu": fmt.Sprintf("%dm", 10+random.Intn(90)), "memory": fmt.Sprintf("%dMi", 16+random.Intn(112))}}}}}},
			},
		},
		{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]any{"name": appName, "namespace": "default", "labels": appLabels},
			"spec":     map[string]any{"selector": map[string]any{"app.kubernetes.io/name": appName}, "ports": []any{map[string]any{"name": "http", "port": 8080, "targetPort": 8080}}},
		},
		{
			"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
			"metadata": map[string]any{"name": appName, "namespace": "default", "labels": appLabels},
			"spec":     map[string]any{"rules": []any{map[string]any{"host": appName + ".example.invalid", "http": map[string]any{"paths": []any{map[string]any{"path": "/", "pathType": "Prefix", "backend": map[string]any{"service": map[string]any{"name": appName, "port": map[string]any{"number": 8080}}}}}}}}},
		},
		{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": appName + "-config", "namespace": "default", "labels": appLabels},
			"data":     map[string]any{"mode": randomChoice(random, []string{"steady", "burst", "quiet"}), "generated": randomToken(random, 48), "simulationRevision": "0"},
		},
		{
			"apiVersion": "v1", "kind": "Secret",
			"metadata": map[string]any{"name": appName + "-credentials", "namespace": "default", "labels": appLabels},
			"type":     "Opaque", "stringData": map[string]any{"username": "simulated-user", "password": randomToken(random, 32)},
		},
	}
	for extra := 0; extra < random.Intn(4); extra++ {
		manifests = append(manifests, map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": fmt.Sprintf("%s-extra-%d", appName, extra), "namespace": "default", "labels": appLabels},
			"data":     map[string]any{"payload": randomToken(random, 64+random.Intn(512)), "scenario": randomChoice(random, []string{"small", "medium", "large"})},
		})
	}
	encoded := make([]workv1.Manifest, 0, len(manifests))
	for _, manifest := range manifests {
		content, _ := json.Marshal(manifest)
		encoded = append(encoded, workv1.Manifest{RawExtension: runtime.RawExtension{Raw: content}})
	}
	return &workv1.ManifestWork{
		TypeMeta:   metav1.TypeMeta{APIVersion: workv1.GroupVersion.String(), Kind: "ManifestWork"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{runIDLabel: runID, workIndexLabel: strconv.Itoa(index)}},
		Spec:       workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{Manifests: encoded}},
	}
}

func randomChoice(random *mathrand.Rand, choices []string) string {
	return choices[random.Intn(len(choices))]
}

func randomToken(random *mathrand.Rand, size int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	value := make([]byte, size)
	for i := range value {
		value[i] = alphabet[random.Intn(len(alphabet))]
	}
	return string(value)
}

func isDNSLabel(value string) bool {
	if value == "" || len(value) > 32 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
