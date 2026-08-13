package manifestcontroller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

// TestMain registers the spoke-work feature gates so reconcile's
// SpokeMutableFeatureGate.Enabled(ManifestWorkApplyLatency) check does not panic in tests.
func TestMain(m *testing.M) {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	os.Exit(m.Run())
}

// logEntry is a single captured structured log line.
type logEntry struct {
	msg string
	kv  map[string]any
}

// captureSink is a logr.LogSink that records Info lines and their key/value pairs
// so tests can assert on the structured attributes the emitters produce.
type captureSink struct {
	entries *[]logEntry
}

func (s *captureSink) Init(logr.RuntimeInfo)          {}
func (s *captureSink) Enabled(int) bool               { return true }
func (s *captureSink) Error(error, string, ...any)    {}
func (s *captureSink) WithValues(...any) logr.LogSink { return s }
func (s *captureSink) WithName(string) logr.LogSink   { return s }

func (s *captureSink) Info(_ int, msg string, kv ...any) {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		m[key] = kv[i+1]
	}
	*s.entries = append(*s.entries, logEntry{msg: msg, kv: m})
}

func captureContext() (context.Context, *[]logEntry) {
	entries := &[]logEntry{}
	logger := logr.New(&captureSink{entries: entries})
	return klog.NewContext(context.Background(), logger), entries
}

func TestApplyOutcome(t *testing.T) {
	if got := applyOutcome(nil); got != "applied" {
		t.Errorf("applyOutcome(nil) = %q, want applied", got)
	}
	if got := applyOutcome(errors.New("boom")); got != "failed" {
		t.Errorf("applyOutcome(err) = %q, want failed", got)
	}
}

func TestRollupOutcome(t *testing.T) {
	cases := []struct {
		applied, failed int
		want            string
	}{
		{3, 0, "applied"},
		{0, 2, "failed"},
		{2, 1, "partial"},
		{0, 0, "read_only"},
	}
	for _, c := range cases {
		if got := rollupOutcome(c.applied, c.failed); got != c.want {
			t.Errorf("rollupOutcome(%d,%d) = %q, want %q", c.applied, c.failed, got, c.want)
		}
	}
}

func TestCountApplyResults(t *testing.T) {
	results := []applyResult{
		{Error: nil},             // applied
		{Error: errors.New("x")}, // failed
		{Error: nil},             // applied
		{strategy: workapiv1.UpdateStrategyTypeReadOnly}, // read-only, counted separately
	}
	applied, failed, readOnly := countApplyResults(results)
	if applied != 2 || failed != 1 || readOnly != 1 {
		t.Errorf("countApplyResults = (%d,%d,%d), want (2,1,1)", applied, failed, readOnly)
	}
}

func TestPriorAppliedGeneration(t *testing.T) {
	empty := &workapiv1.ManifestWork{}
	if got := priorAppliedGeneration(empty); got != -1 {
		t.Errorf("priorAppliedGeneration(no condition) = %d, want -1", got)
	}

	mw := &workapiv1.ManifestWork{}
	mw.Status.Conditions = []metav1.Condition{{
		Type:               workapiv1.WorkApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 5,
	}}
	if got := priorAppliedGeneration(mw); got != 5 {
		t.Errorf("priorAppliedGeneration = %d, want 5", got)
	}
}

func testWorkMeta() workMeta {
	return workMeta{
		name:       "demo-mw",
		namespace:  "cluster1",
		generation: 7,
		labels:     map[string]string{"example.com/team": "platform"},
	}
}

func testManifest() orderedManifest {
	return orderedManifest{
		resourceMeta: workapiv1.ManifestResourceMeta{
			Kind:      "ConfigMap",
			Name:      "cm1",
			Namespace: "default",
		},
	}
}

func TestEmitResourceApply_GateOff(t *testing.T) {
	ctx, entries := captureContext()
	// logApply=false: never emits, even on a first apply.
	emitResourceApply(ctx, false, true, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeUpdate, applyResult{}, "")
	if len(*entries) != 0 {
		t.Errorf("gate off: expected no log, got %d", len(*entries))
	}
}

func TestEmitResourceApply_ReadOnlyFirstApply(t *testing.T) {
	ctx, entries := captureContext()
	emitResourceApply(ctx, true, true, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeReadOnly, applyResult{Result: &unstructured.Unstructured{}}, "")
	if len(*entries) != 1 {
		t.Fatalf("read-only first apply: expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowResourceSpokeApply)
	assertKV(t, kv, "outcome", outcomeReadOnly)
	if _, ok := kv["prev_outcome"]; ok {
		t.Error("read-only apply line must not carry prev_outcome")
	}
}

func TestEmitResourceApply_ReadOnlyNoSyncOnResync(t *testing.T) {
	ctx, entries := captureContext()
	// firstApply=false with a differing prevOutcome: a read-only manifest must NOT emit a sync line
	// (its outcome cannot change).
	emitResourceApply(ctx, true, false, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeReadOnly, applyResult{Result: &unstructured.Unstructured{}}, "applied")
	if len(*entries) != 0 {
		t.Errorf("read-only resync: expected no log, got %d", len(*entries))
	}
}

func TestEmitResourceApply_FirstApplySuccess(t *testing.T) {
	ctx, entries := captureContext()
	obj := &unstructured.Unstructured{}
	obj.SetResourceVersion("123")
	emitResourceApply(ctx, true, true, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeUpdate, applyResult{Result: obj}, "")

	if len(*entries) != 1 {
		t.Fatalf("expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowResourceSpokeApply)
	assertKV(t, kv, "mw_name", "demo-mw")
	assertKV(t, kv, "mw_namespace", "cluster1")
	assertKV(t, kv, "generation", int64(7))
	assertKV(t, kv, "applied_kind", "ConfigMap")
	assertKV(t, kv, "applied_name", "cm1")
	assertKV(t, kv, "applied_namespace", "default")
	assertKV(t, kv, "applied_resource_version", "123")
	assertKV(t, kv, "outcome", "applied")
	if _, ok := kv["prev_outcome"]; ok {
		t.Error("first-apply line must not carry prev_outcome")
	}
	if _, ok := kv["ts_epoch_ms"]; !ok {
		t.Error("missing ts_epoch_ms")
	}
}

func TestEmitResourceApply_FirstApplyFailure(t *testing.T) {
	ctx, entries := captureContext()
	emitResourceApply(ctx, true, true, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeServerSideApply, applyResult{Error: errors.New("denied")}, "")

	if len(*entries) != 1 {
		t.Fatalf("expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowResourceSpokeApply)
	assertKV(t, kv, "outcome", "failed")
	assertKV(t, kv, "applied_resource_version", "")
}

// Resync (firstApply=false) where the resource's outcome changed emits the sync flow.
func TestEmitResourceApply_SyncOutcomeChanged(t *testing.T) {
	ctx, entries := captureContext()
	obj := &unstructured.Unstructured{}
	obj.SetResourceVersion("456")
	emitResourceApply(ctx, true, false, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeUpdate, applyResult{Result: obj}, "failed")

	if len(*entries) != 1 {
		t.Fatalf("expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowResourceSpokeSync)
	assertKV(t, kv, "outcome", "applied")
	assertKV(t, kv, "prev_outcome", "failed")
}

// Resync where the outcome is unchanged emits nothing.
func TestEmitResourceApply_SyncNoChange(t *testing.T) {
	ctx, entries := captureContext()
	obj := &unstructured.Unstructured{}
	emitResourceApply(ctx, true, false, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeUpdate, applyResult{Result: obj}, "applied")
	if len(*entries) != 0 {
		t.Errorf("unchanged resync: expected no log, got %d", len(*entries))
	}
}

// Resync with no known prior outcome emits nothing (cannot determine a change).
func TestEmitResourceApply_SyncUnknownPrev(t *testing.T) {
	ctx, entries := captureContext()
	emitResourceApply(ctx, true, false, testWorkMeta(), testManifest(),
		workapiv1.UpdateStrategyTypeUpdate, applyResult{}, "")
	if len(*entries) != 0 {
		t.Errorf("unknown prev: expected no log, got %d", len(*entries))
	}
}

func TestPriorManifestOutcome(t *testing.T) {
	if got := priorManifestOutcome(nil); got != "" {
		t.Errorf("nil condition = %q, want empty", got)
	}
	applied := &workapiv1.ManifestCondition{Conditions: []metav1.Condition{{
		Type: workapiv1.ManifestApplied, Status: metav1.ConditionTrue,
	}}}
	if got := priorManifestOutcome(applied); got != "applied" {
		t.Errorf("applied condition = %q, want applied", got)
	}
	failed := &workapiv1.ManifestCondition{Conditions: []metav1.Condition{{
		Type: workapiv1.ManifestApplied, Status: metav1.ConditionFalse,
	}}}
	if got := priorManifestOutcome(failed); got != "failed" {
		t.Errorf("failed condition = %q, want failed", got)
	}
}

func TestEmitApplyRollup_Start(t *testing.T) {
	ctx, entries := captureContext()
	emitApplyRollup(ctx, testWorkMeta(), flowSpokeApply, 3, nil)

	if len(*entries) != 1 {
		t.Fatalf("expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowSpokeApply)
	assertKV(t, kv, "generation", int64(7))
	assertKV(t, kv, "resource_count", 3)
	if _, ok := kv["outcome"]; ok {
		t.Error("start line must not carry outcome")
	}
	if _, ok := kv["applied_count"]; ok {
		t.Error("start line must not carry applied_count")
	}
}

func TestEmitApplyRollup_End(t *testing.T) {
	ctx, entries := captureContext()
	emitApplyRollup(ctx, testWorkMeta(), flowSpokeApplyResult, 6,
		&applyCounts{applied: 2, failed: 1, readOnly: 3})

	if len(*entries) != 1 {
		t.Fatalf("expected 1 log, got %d", len(*entries))
	}
	kv := (*entries)[0].kv
	assertKV(t, kv, "flow", flowSpokeApplyResult)
	assertKV(t, kv, "resource_count", 6)
	assertKV(t, kv, "applied_count", 2)
	assertKV(t, kv, "failed_count", 1)
	assertKV(t, kv, "read_only_count", 3)
	assertKV(t, kv, "outcome", "partial")
}

func assertKV(t *testing.T, kv map[string]any, key string, want any) {
	t.Helper()
	got, ok := kv[key]
	if !ok {
		t.Errorf("missing key %q", key)
		return
	}
	if got != want {
		t.Errorf("key %q = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

func flowCounts(entries *[]logEntry) map[string]int {
	counts := map[string]int{}
	for _, e := range *entries {
		if f, ok := e.kv["flow"].(string); ok {
			counts[f]++
		}
	}
	return counts
}

func setApplyLatencyGate(t *testing.T, enabled bool) {
	t.Helper()
	if err := features.SpokeMutableFeatureGate.Set(
		fmt.Sprintf("%s=%t", ocmfeature.ManifestWorkApplyLatency, enabled)); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileEmitsApplyLatencyLines drives a full reconcile with the gate on for a new
// generation and asserts all three lines fire exactly once.
func TestReconcileEmitsApplyLatencyLines(t *testing.T) {
	setApplyLatencyGate(t, true)
	defer setApplyLatencyGate(t, false)

	work, workKey := newTestCase("emit").
		withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
		newManifestWork()
	controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
		withKubeObject().withUnstructuredObject()
	syncContext := testingcommon.NewFakeSyncContext(t, workKey)

	ctx, entries := captureContext()
	if err := controller.toController().sync(ctx, syncContext, work.Name); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got := flowCounts(entries)
	if got[flowSpokeApply] != 1 || got[flowResourceSpokeApply] != 1 || got[flowSpokeApplyResult] != 1 {
		t.Errorf("flow counts = %v, want each of mw_spoke_apply/mw_resource_spoke_apply/mw_spoke_apply_result == 1", got)
	}
}

// TestReconcileDedupsRollupBySameGeneration verifies that when the generation has already been
// applied (WorkApplied.ObservedGeneration == generation), no apply-latency lines are emitted —
// the once-per-generation guard suppresses a resync.
func TestReconcileDedupsRollupBySameGeneration(t *testing.T) {
	setApplyLatencyGate(t, true)
	defer setApplyLatencyGate(t, false)

	work, workKey := newTestCase("dedup").
		withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
		withExistingWorkCondition(newCondition(workapiv1.WorkApplied, "True", "", "", 0, nil)).
		newManifestWork()
	controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
		withKubeObject().withUnstructuredObject()
	syncContext := testingcommon.NewFakeSyncContext(t, workKey)

	ctx, entries := captureContext()
	if err := controller.toController().sync(ctx, syncContext, work.Name); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := flowCounts(entries); len(got) != 0 {
		t.Errorf("flow counts = %v, want none (generation already applied)", got)
	}
}

// TestReconcileNoEmitWhenGateOff confirms a full reconcile emits no apply-latency lines with the
// gate disabled.
func TestReconcileNoEmitWhenGateOff(t *testing.T) {
	setApplyLatencyGate(t, false)

	work, workKey := newTestCase("gate-off").
		withWorkManifest(testingcommon.NewUnstructured("v1", "Secret", "ns1", "test")).
		newManifestWork()
	controller := newController(t, work, nil, spoketesting.NewFakeRestMapper()).
		withKubeObject().withUnstructuredObject()
	syncContext := testingcommon.NewFakeSyncContext(t, workKey)

	ctx, entries := captureContext()
	if err := controller.toController().sync(ctx, syncContext, work.Name); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := flowCounts(entries); len(got) != 0 {
		t.Errorf("flow counts = %v, want none (gate off)", got)
	}
}
