// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	workfake "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workv1 "open-cluster-management.io/api/work/v1"
)

func newTestApp(t *testing.T, allowWrites bool) (*App, *kubefake.Clientset, *workfake.Clientset) {
	t.Helper()
	kube := kubefake.NewSimpleClientset()
	cluster := clusterfake.NewSimpleClientset()
	work := workfake.NewSimpleClientset()
	installDeleteCollectionReactor(t, work)
	app, err := NewApp(appOptions{allowWrites: allowWrites, initialConfig: SimulatorConfig{Clusters: 1, WorksPerCluster: 1}}, kube, cluster.ClusterV1().ManagedClusters(), work.WorkV1(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return app, kube, work
}

func installDeleteCollectionReactor(t *testing.T, client *workfake.Clientset) {
	t.Helper()
	client.PrependReactor("delete-collection", "manifestworks", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteAction := action.(k8stesting.DeleteCollectionAction)
		object, err := client.Tracker().List(workv1.SchemeGroupVersion.WithResource("manifestworks"), workv1.SchemeGroupVersion.WithKind("ManifestWork"), action.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		list := object.(*workv1.ManifestWorkList)
		selector := deleteAction.GetListRestrictions().Labels
		for _, item := range list.Items {
			if selector.Matches(labels.Set(item.Labels)) {
				if err := client.Tracker().Delete(workv1.SchemeGroupVersion.WithResource("manifestworks"), item.Namespace, item.Name); err != nil {
					return true, nil, err
				}
			}
		}
		return true, nil, nil
	})
}

func TestStartRequiresExplicitLoadEnablementAndConfirmation(t *testing.T) {
	ctx := context.Background()
	app, _, _ := newTestApp(t, false)
	if err := app.Start(ctx, "RUN"); err == nil {
		t.Fatal("start should reject load when the process was not opted in")
	}
	app, _, _ = newTestApp(t, true)
	if err := app.Start(ctx, "run"); err == nil {
		t.Fatal("start should require exact confirmation")
	}
	if err := app.Start(ctx, "RUN"); err != nil {
		t.Fatalf("explicitly enabled start failed: %v", err)
	}
	if state := app.State(); !state.Running || !state.Started {
		t.Fatalf("unexpected state after start: %#v", state)
	}
}

func TestReconcileCreatesAndOwnsSimulatorResourcesAndScaleDownRemovesThem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, kube, work := newTestApp(t, true)
	if err := app.Start(ctx, "RUN"); err != nil {
		t.Fatal(err)
	}
	app.reconcile(ctx)
	state := app.State()
	if state.ActiveClusters != 1 {
		t.Fatalf("expected a simulated namespace, got %#v", state)
	}
	namespace := "sim-" + app.runID + "-c0001"
	app.reconcile(ctx)
	if _, err := work.WorkV1().ManifestWorks(namespace).Get(ctx, "sim-work-0001", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected owned work: %v", err)
	}

	foreignNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-cluster"}}
	if _, err := kube.CoreV1().Namespaces().Create(ctx, foreignNamespace, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	foreignWork := &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "keep-me", Namespace: foreignNamespace.Name}}
	if _, err := work.WorkV1().ManifestWorks(foreignNamespace.Name).Create(ctx, foreignWork, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	app.Stop()
	app.reconcile(ctx)
	waitForAppState(t, app, func(state DashboardState) bool { return state.ActiveClusters == 0 && state.TerminatingClusters == 0 })
	if _, err := kube.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err == nil {
		t.Fatal("simulator namespace should be removed on stop")
	}
	if _, err := work.WorkV1().ManifestWorks(namespace).Get(ctx, "sim-work-0001", metav1.GetOptions{}); err == nil {
		t.Fatal("simulator-owned work should be removed on stop")
	}
	if _, err := kube.CoreV1().Namespaces().Get(ctx, foreignNamespace.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("unrelated namespace was touched: %v", err)
	}
	if _, err := work.WorkV1().ManifestWorks(foreignNamespace.Name).Get(ctx, foreignWork.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("unrelated ManifestWork was touched: %v", err)
	}
}

func TestCleanupPreservesForeignManifestWorksInSimulatorNamespace(t *testing.T) {
	ctx := context.Background()
	app, kube, workClient := newTestApp(t, true)
	app.started = true
	app.reconcile(ctx)
	namespace := "sim-" + app.runID + "-c0001"
	app.reconcile(ctx)
	foreign := &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: namespace}}
	if _, err := workClient.WorkV1().ManifestWorks(namespace).Create(ctx, foreign, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	app.Stop()
	app.reconcile(ctx)
	waitForAppState(t, app, func(state DashboardState) bool { return state.LastError != "" })
	if _, err := kube.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace with foreign work must be preserved: %v", err)
	}
	if _, err := workClient.WorkV1().ManifestWorks(namespace).Get(ctx, foreign.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("foreign work was deleted: %v", err)
	}
	if app.State().LastError == "" {
		t.Fatal("blocked cleanup should report why the namespace was retained")
	}
}

func TestRunIDRecoveryCleansPreviousResourcesBeforeRestart(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "sim-recovery-c0001", Labels: map[string]string{runIDLabel: "recovery", clusterMarkerLabel: "true", clusterIndexLabel: "1"}}})
	clusterClient := clusterfake.NewSimpleClientset()
	workClient := workfake.NewSimpleClientset()
	installDeleteCollectionReactor(t, workClient)
	priorWork := buildManifestWork("recovery", "sim-recovery-c0001", 1)
	if _, err := workClient.WorkV1().ManifestWorks(priorWork.Namespace).Create(ctx, priorWork, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(appOptions{allowWrites: true, runID: "recovery", initialConfig: SimulatorConfig{Clusters: 1, WorksPerCluster: 1}}, kube, clusterClient.ClusterV1().ManagedClusters(), workClient.WorkV1(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(ctx, "RUN"); err == nil {
		t.Fatal("start should wait for recovered resources to be cleaned")
	}
	for i := 0; i < 5; i++ {
		app.reconcile(ctx)
		time.Sleep(5 * time.Millisecond)
	}
	waitForAppState(t, app, func(state DashboardState) bool { return state.TerminatingClusters == 0 })
	if state := app.State(); state.TerminatingClusters != 0 {
		t.Fatalf("recovered resources remain: %#v", state)
	}
	if err := app.Start(ctx, "RUN"); err != nil {
		t.Fatalf("start after recovered cleanup: %v", err)
	}
}

func TestStopImmediatelyClearsActiveCountsAndAllowsRestartAfterBulkCleanup(t *testing.T) {
	ctx := context.Background()
	app, kube, workClient := newTestApp(t, true)
	kube.PrependReactor("delete", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteAction := action.(k8stesting.DeleteAction)
		object, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("namespaces"), "", deleteAction.GetName())
		if err != nil {
			return true, nil, err
		}
		namespace := object.(*corev1.Namespace).DeepCopy()
		now := metav1.Now()
		namespace.DeletionTimestamp = &now
		if err := kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("namespaces"), namespace, ""); err != nil {
			return true, nil, err
		}
		return true, nil, nil
	})
	app.started = true
	for index := 1; index <= 30; index++ {
		name := fmt.Sprintf("sim-%s-c%04d", app.runID, index)
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{runIDLabel: app.runID, clusterMarkerLabel: "true", clusterIndexLabel: strconv.Itoa(index)}}}
		if _, err := kube.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		_, cancel := context.WithCancel(ctx)
		app.clusters[name] = &clusterRuntime{name: name, index: index, cancel: cancel}
		app.nextClusterIndex = index
		for workIndex := 1; workIndex <= 2; workIndex++ {
			if _, err := workClient.WorkV1().ManifestWorks(name).Create(ctx, buildManifestWork(app.runID, name, workIndex), metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	app.Stop()
	state := app.State()
	if state.DesiredClusters != 0 || state.ActiveClusters != 0 || state.TerminatingClusters != 30 {
		t.Fatalf("Stop should immediately separate active and terminating counts: %#v", state)
	}
	app.reconcile(ctx)
	waitForAppState(t, app, func(state DashboardState) bool { return state.CanStart && state.PendingNamespaceDeletes == 30 })
	if err := app.Start(ctx, "RUN"); err != nil {
		t.Fatalf("start after cleanup should work: %v", err)
	}
	app.reconcile(ctx)
	if _, err := kube.CoreV1().Namespaces().Get(ctx, fmt.Sprintf("sim-%s-c0031", app.runID), metav1.GetOptions{}); err != nil {
		t.Fatalf("restart should use a new cluster index while prior namespaces finalize: %v", err)
	}
}

func TestAgentAcknowledgesGenerationOnce(t *testing.T) {
	ctx := context.Background()
	app, _, workClient := newTestApp(t, true)
	work := buildManifestWork(app.runID, "sim-test", 1)
	created, err := workClient.WorkV1().ManifestWorks("sim-test").Create(ctx, work, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.acknowledgeWork(ctx, created); err != nil {
		t.Fatalf("acknowledge work: %v", err)
	}
	updated, err := workClient.WorkV1().ManifestWorks("sim-test").Get(ctx, created.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !workStatusCurrent(updated) {
		t.Fatal("agent status should observe the work generation")
	}
	if len(updated.Status.ResourceStatus.Manifests) != len(updated.Spec.Workload.Manifests) {
		t.Fatalf("agent should report each manifest status: got %d for %d manifests", len(updated.Status.ResourceStatus.Manifests), len(updated.Spec.Workload.Manifests))
	}
	if app.agentStatusUpdates.Load() != 1 {
		t.Fatalf("status update count = %d", app.agentStatusUpdates.Load())
	}
}

func TestUpdateOneChangesOnlyOwnedWork(t *testing.T) {
	ctx := context.Background()
	app, _, workClient := newTestApp(t, true)
	namespace := "sim-" + app.runID + "-c0001"
	work := buildManifestWork(app.runID, namespace, 1)
	if _, err := workClient.WorkV1().ManifestWorks(namespace).Create(ctx, work, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	foreign := buildManifestWork("different", namespace, 2)
	foreign.Name = "foreign-work"
	if _, err := workClient.WorkV1().ManifestWorks(namespace).Create(ctx, foreign, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	app.clusters[namespace] = &clusterRuntime{name: namespace, index: 1, workNames: []string{work.Name}}
	app.updateOne(ctx)
	updated, err := workClient.WorkV1().ManifestWorks(namespace).Get(ctx, work.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(updated.Spec.Workload.Manifests[3].Raw, &object); err != nil {
		t.Fatal(err)
	}
	data := object["data"].(map[string]any)
	if data["simulationRevision"] == "0" {
		t.Fatal("owned ConfigMap was not updated")
	}
	if app.manifestWorkUpdates.Load() != 1 {
		t.Fatalf("logical update count = %d", app.manifestWorkUpdates.Load())
	}
}

func TestStoppedSimulatorProjectionFallsToMeasuredBaseline(t *testing.T) {
	ctx := context.Background()
	app, _, _ := newTestApp(t, true)
	if err := app.SetConfig(SimulatorConfig{Clusters: 4, WorksPerCluster: 3, UpdatesPerSecond: 2}); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(ctx, "RUN"); err != nil {
		t.Fatal(err)
	}
	app.clusters["sim-"+app.runID+"-c0001"] = &clusterRuntime{name: "sim-" + app.runID + "-c0001", workNames: []string{"sim-work-0001"}}
	started := app.State()
	if started.DirectEstimate.API.CPUCore == nil || *started.DirectEstimate.API.CPUCore <= 0 {
		t.Fatalf("selected-load projection should be visible while started: %#v", started.DirectEstimate)
	}
	app.Stop()
	stopped := app.State()
	for name, value := range map[string]*float64{
		"API CPU":     stopped.DirectEstimate.API.CPUCore,
		"etcd CPU":    stopped.DirectEstimate.Etcd.CPUCore,
		"API memory":  stopped.DirectEstimate.API.MemoryBytes,
		"etcd memory": stopped.DirectEstimate.Etcd.MemoryBytes,
	} {
		if value == nil || *value != 0 {
			t.Fatalf("stopped %s projection should fall to the measured baseline instead of holding the stopped load: %#v", name, stopped.DirectEstimate)
		}
	}
}

func TestEstimateUsesIdleBaselineInsteadOfLoadedMeasurement(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	app.idleBaseline = &hostingBaseline{
		API:  MetricSet{CPUCore: floatPointer(0.05), MemoryBytes: floatPointer(389 << 20)},
		Etcd: MetricSet{CPUCore: floatPointer(0.02), MemoryBytes: floatPointer(43 << 20)},
	}
	app.history = append(app.history, Sample{
		HostingAPI:  MetricSet{CPUCore: floatPointer(0.4), MemoryBytes: floatPointer(1000 << 20)},
		HostingEtcd: MetricSet{CPUCore: floatPointer(0.09), MemoryBytes: floatPointer(120 << 20)},
	})
	state := app.State()
	if state.DirectEstimate.API.CPUCore == nil {
		t.Fatalf("expected a projected API cost: %#v", state.DirectEstimate)
	}
	if got := *state.DirectEstimate.API.CPUCore; got > 0.1 {
		t.Fatalf("estimate must use the idle baseline, not the loaded live measurement: %f", got)
	}
	if state.DirectEstimate.Provisional {
		// The message documents that the idle baseline is the base.
		if got := state.DirectEstimate.Message; !strings.Contains(got, "idle hosting API/etcd baseline") {
			t.Fatalf("estimate message should describe the idle-baseline base: %s", got)
		}
	}
}

func TestRegistrationProgressStagesAndStall(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	app.bootstrapClient = kubefake.NewSimpleClientset()
	now := time.Now()
	app.mu.Lock()
	app.clusters["sim-a"] = &clusterRuntime{name: "sim-a", registrationStage: stageHubAcceptance, stageSince: now.Add(-3 * time.Minute), registrationStarted: now.Add(-4 * time.Minute)}
	app.clusters["sim-b"] = &clusterRuntime{name: "sim-b", registrationStage: stageCSR, stageSince: now.Add(-10 * time.Second), registrationStarted: now.Add(-20 * time.Second)}
	app.clusters["sim-c"] = &clusterRuntime{name: "sim-c", registered: true}
	app.clusters["sim-d"] = &clusterRuntime{name: "sim-d", terminating: true, registrationStage: stageCSR}
	app.mu.Unlock()

	stages, meanAge, oldest, stalled, pending := app.registrationProgressLocked(now)
	if pending != 2 {
		t.Fatalf("pending = %d, want 2 (registered and terminating clusters are excluded)", pending)
	}
	if stages[stageHubAcceptance] != 1 || stages[stageCSR] != 1 {
		t.Fatalf("stages = %v, want one hub-acceptance and one csr", stages)
	}
	if stalled != 1 {
		t.Fatalf("stalled = %d, want 1 (hub-acceptance older than the stall threshold)", stalled)
	}
	if oldest != 240 {
		t.Fatalf("oldest = %f, want 240", oldest)
	}
	if meanAge != 130 {
		t.Fatalf("meanAge = %f, want 130", meanAge)
	}

	// A stage transition must reset the stall clock.
	app.setRegistrationStage("sim-a", stageNamespace)
	app.mu.RLock()
	cluster := app.clusters["sim-a"]
	app.mu.RUnlock()
	if cluster.registrationStage != stageNamespace || now.Sub(cluster.stageSince) > time.Second {
		t.Fatalf("stage transition not recorded: stage=%q stageSince=%v", cluster.registrationStage, cluster.stageSince)
	}
	if _, _, _, stalled, _ = app.registrationProgressLocked(time.Now()); stalled != 0 {
		t.Fatalf("stalled after transition = %d, want 0", stalled)
	}

	// Completing registration removes the cluster from pending counts.
	app.mu.Lock()
	app.clusters["sim-a"].registered = true
	app.clusters["sim-a"].registrationStage = ""
	app.mu.Unlock()
	if _, _, _, _, pending = app.registrationProgressLocked(time.Now()); pending != 1 {
		t.Fatalf("pending after registration = %d, want 1", pending)
	}
}

func TestCleanupProgressShowsFinalizedOverTotal(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	app.mu.Lock()
	app.clusters["sim-t-c0001"] = &clusterRuntime{name: "sim-t-c0001", terminating: true}
	app.clusters["sim-t-c0002"] = &clusterRuntime{name: "sim-t-c0002", terminating: true}
	app.pendingNamespaceDeletes["sim-t-c0003"] = "uid-3"
	app.updateCleanupProgressLocked(2, 1)
	app.mu.Unlock()
	state := app.State()
	if state.CleanupTotal != 3 || state.CleanupFinalized != 0 {
		t.Fatalf("cleanup progress = %d/%d, want 0 finalized of 3", state.CleanupFinalized, state.CleanupTotal)
	}

	// Two clusters finished resource cleanup and moved to namespace
	// finalization; the total must not shrink while work remains.
	app.mu.Lock()
	delete(app.clusters, "sim-t-c0001")
	delete(app.clusters, "sim-t-c0002")
	app.pendingNamespaceDeletes["sim-t-c0001"] = "uid-1"
	app.pendingNamespaceDeletes["sim-t-c0002"] = "uid-2"
	app.updateCleanupProgressLocked(0, 3)
	app.mu.Unlock()
	state = app.State()
	if state.CleanupTotal != 3 || state.CleanupFinalized != 0 {
		t.Fatalf("cleanup progress = %d/%d, want a stable total of 3", state.CleanupFinalized, state.CleanupTotal)
	}

	// Kubernetes finished finalizing two namespaces.
	app.mu.Lock()
	delete(app.pendingNamespaceDeletes, "sim-t-c0001")
	delete(app.pendingNamespaceDeletes, "sim-t-c0002")
	app.updateCleanupProgressLocked(0, 1)
	app.mu.Unlock()
	state = app.State()
	if state.CleanupFinalized != 2 || state.CleanupTotal != 3 {
		t.Fatalf("cleanup progress = %d/%d, want 2 finalized of 3", state.CleanupFinalized, state.CleanupTotal)
	}

	// The episode ends when nothing remains; the display resets.
	app.mu.Lock()
	delete(app.pendingNamespaceDeletes, "sim-t-c0003")
	app.updateCleanupProgressLocked(0, 0)
	app.mu.Unlock()
	state = app.State()
	if state.CleanupTotal != 0 || state.CleanupFinalized != 0 {
		t.Fatalf("cleanup progress must reset when the episode ends: %d/%d", state.CleanupFinalized, state.CleanupTotal)
	}
}

func TestResetHistoryKeepsIdleBaseline(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	app.mu.Lock()
	app.history = []Sample{{Timestamp: "t1"}, {Timestamp: "t2"}}
	app.idleBaseline = &hostingBaseline{API: MetricSet{CPUCore: floatPointer(0.05)}}
	app.mu.Unlock()
	app.ResetHistory()
	app.mu.RLock()
	defer app.mu.RUnlock()
	if len(app.history) != 0 {
		t.Fatal("history should be empty after reset")
	}
	if app.idleBaseline == nil || app.idleBaseline.API.CPUCore == nil {
		t.Fatal("the idle baseline must survive a history reset")
	}
}

func TestIdleBaselineFreezesAfterSixIdleSamples(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	for i := 0; i < idleBaselineSamples; i++ {
		app.recordIdleSampleLocked(Sample{
			HostingAPI:  MetricSet{CPUCore: floatPointer(0.05), MemoryBytes: floatPointer(400 << 20)},
			HostingEtcd: MetricSet{CPUCore: floatPointer(0.02), MemoryBytes: floatPointer(40 << 20)},
		}, true)
	}
	if app.idleBaseline == nil {
		t.Fatal("expected the idle baseline to be captured")
	}
	if got := *app.idleBaseline.API.CPUCore; math.Abs(got-0.05) > 1e-9 {
		t.Fatalf("API CPU baseline = %f, want 0.05", got)
	}
	// CPU baselines ignore one-sided churn: a spike must not raise the floor.
	app.idleBaseline = nil
	app.idleWindow = nil
	for i := 0; i < idleBaselineSamples; i++ {
		cpu := 0.04
		if i == 3 {
			cpu = 0.30
		}
		app.recordIdleSampleLocked(Sample{
			HostingAPI:  MetricSet{CPUCore: floatPointer(cpu), MemoryBytes: floatPointer(400 << 20)},
			HostingEtcd: MetricSet{CPUCore: floatPointer(0.02), MemoryBytes: floatPointer(40 << 20)},
		}, true)
	}
	if got := *app.idleBaseline.API.CPUCore; math.Abs(got-0.04) > 1e-9 {
		t.Fatalf("API CPU baseline must use the quietest sample, got %f", got)
	}
	app.recordIdleSampleLocked(Sample{HostingAPI: MetricSet{CPUCore: floatPointer(9)}}, true)
	if got := *app.idleBaseline.API.CPUCore; math.Abs(got-0.04) > 1e-9 {
		t.Fatalf("baseline must stay frozen after capture: %f", got)
	}
	app.recordIdleSampleLocked(Sample{HostingAPI: MetricSet{CPUCore: floatPointer(9)}}, false)
	if app.idleWindow != nil {
		t.Fatal("non-idle samples must not accumulate in the idle window")
	}
}

func TestHTTPRejectsUnsafeCrossOriginAndInvalidControls(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	handler := (&dashboardServer{app: app}).Handler()
	request := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"clusters":1000,"worksPerCluster":100,"updatesPerSecond":0}`))
	request.Header.Set("Origin", "http://evil.example")
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request status %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"clusters":1000,"worksPerCluster":100,"updatesPerSecond":0}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsafe total-work request status %d", response.Code)
	}
}

func TestDashboardServesEmbeddedPageAndState(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	handler := (&dashboardServer{app: app}).Handler()
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "ManifestWork load simulator") || !strings.Contains(page.Body.String(), "directCPU") {
		t.Fatalf("dashboard page was not served as expected: status=%d", page.Code)
	}
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"allowWrites":false`) {
		t.Fatalf("dashboard state was not served as expected: status=%d body=%s", state.Code, state.Body.String())
	}
}

func TestAssumptionsEndpointUpdatesTheDirectHubProjection(t *testing.T) {
	app, _, _ := newTestApp(t, false)
	handler := (&dashboardServer{app: app}).Handler()
	body := `{"apiCpuPerCluster":0.1,"apiMemoryMiBPerCluster":10,"apiCpuPerWork":0.01,"apiMemoryMiBPerWork":1,"etcdCpuPerCluster":0.05,"etcdMemoryMiBPerCluster":5,"etcdCpuPerWork":0.005,"etcdMemoryMiBPerWork":0.5}`
	request := httptest.NewRequest(http.MethodPut, "/api/assumptions", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("assumption update status=%d body=%s", response.Code, response.Body.String())
	}
	state := app.State()
	if !state.DirectEstimate.Provisional || state.DirectEstimate.API.CPUCore == nil {
		t.Fatalf("expected projected API cost: %#v", state.DirectEstimate)
	}
	if got := *state.DirectEstimate.API.CPUCore; math.Abs(got-0.11) > 1e-9 {
		t.Fatalf("API projection = %f, want 0.11", got)
	}
}

func TestWatchCancellationDoesNotLeak(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app, _, _ := newTestApp(t, true)
	go app.agentLoop(ctx, "sim-test")
	time.Sleep(10 * time.Millisecond)
	cancel()
	deadline := time.Now().Add(time.Second)
	for app.activeWatches.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if app.activeWatches.Load() != 0 {
		t.Fatal("agent watch did not stop after context cancellation")
	}
}

func waitForAppState(t *testing.T, app *App, ready func(DashboardState) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := app.State()
		if ready(state) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("simulator state did not reach expected condition: %#v", app.State())
}
