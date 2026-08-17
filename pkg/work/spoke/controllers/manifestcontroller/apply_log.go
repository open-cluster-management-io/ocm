package manifestcontroller

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/logs/json"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

// applyLogSink is the logger used for the apply-latency lines below.
//
// These lines are a machine-consumed telemetry stream, not operator-facing prose: a log
// pipeline joins them to the hub's write-time line on {mw_namespace, mw_name, generation} to
// derive hub->spoke propagation latency, which requires every field to arrive as a first-class
// attribute. A collector can only do that if the whole line is JSON, and klog's format is
// process-global — so the agent's default text output would force every consumer to re-parse
// these fields out of a formatted string.
//
// This sink therefore emits the apply-latency lines as JSON regardless of the process-wide
// logging format, while every other line in the agent keeps that format. It is built from
// component-base's own JSON logger, so the encoding is identical to what
// --logging-format=json produces (ts / caller / msg plus the structured key-values) rather
// than a format private to this package. Writes are serialised by zapcore.Lock, so a JSON
// line can never interleave with concurrent klog output on the same stream.
//
// Emission stays gated behind the ManifestWorkApplyLatency feature gate; when the gate is off
// nothing is written here at all. Overridden in tests to capture the emitted key-values.
var applyLogSink = newApplyLogSink()

func newApplyLogSink() logr.Logger {
	// verbosity 0: these lines are unconditional Info once the feature gate admits them,
	// so they must not be filtered by -v. nil errorStream keeps everything on stdout.
	logger, _ := json.NewJSONLogger(0, zapcore.Lock(zapcore.AddSync(os.Stdout)), nil, nil)
	return logger
}

// Flow discriminators for the spoke apply-latency log lines. Gated behind the
// ManifestWorkApplyLatency feature gate; paired with the hub webhook line (mw_hub_apply) for
// hub->spoke propagation-latency measurement in Datadog.
const (
	flowSpokeApply         = "mw_spoke_apply"          // rollup start, once per generation (latency join key)
	flowResourceSpokeApply = "mw_resource_spoke_apply" // per resource, first apply of a generation (drill-down)
	flowResourceSpokeSync  = "mw_resource_spoke_sync"  // per resource, later reconcile where the outcome changed
	flowSpokeApplyResult   = "mw_spoke_apply_result"   // rollup end, once per generation (outcome)
)

// applyLogTimeFormat is RFC3339 with millisecond precision, UTC.
const applyLogTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Per-resource outcome values. read_only marks a resource tracked by a read-only manifest, which
// the agent observes but never writes, so it is neither applied nor failed.
const (
	outcomeApplied  = "applied"
	outcomeFailed   = "failed"
	outcomeReadOnly = "read_only"
)

// workMeta carries the ManifestWork identity down to the per-resource apply emit, which runs
// deep in applyOneManifest where the ManifestWork object itself is no longer in scope.
type workMeta struct {
	name       string
	namespace  string
	generation int64
	labels     map[string]string
}

// applyCounts is the per-generation apply tally carried by the rollup end line.
type applyCounts struct {
	applied  int
	failed   int
	readOnly int
}

// emittedRollups records the newest generation whose rollup pair this agent process has already
// emitted, keyed by ManifestWork name (the work informer is scoped to the agent's own cluster
// namespace, so the name is unique within a process).
//
// It closes a retry window the persisted guard cannot see. priorAppliedGeneration reads
// WorkApplied.ObservedGeneration, but that value is written by sync *after* reconcile returns.
// When the write fails — a concurrent spec update conflicting on resourceVersion is the common
// case — the controller requeues and reconcile runs again against a status that still names the
// previous generation, so the persisted guard admits the same generation a second time and emits
// a duplicate mw_spoke_apply. That line is the key the propagation-latency join groups on, so a
// duplicate leaves the join with two candidate timestamps for one propagation event.
//
// This layers on top of the persisted guard rather than replacing it: a restarted agent starts
// with an empty ledger and falls back to the status it reads from the API server, which is the
// restart-safe behaviour D4 chose the persisted signal for.
var emittedRollups = newRollupGenerations()

// rollupGenerations is the process-local half of the once-per-generation guard.
type rollupGenerations struct {
	mu   sync.Mutex
	seen map[string]int64
}

func newRollupGenerations() *rollupGenerations {
	return &rollupGenerations{seen: map[string]int64{}}
}

// admit reports whether this process has yet to emit the rollup pair for this generation,
// recording it when so. Because it records, callers must place it last in a short-circuiting
// condition, so a work rejected by an earlier check is not marked as emitted.
func (r *rollupGenerations) admit(name string, generation int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if seen, ok := r.seen[name]; ok && seen >= generation {
		return false
	}
	r.seen[name] = generation
	return true
}

// forget drops a ManifestWork's entry once the work is gone, so the ledger tracks live works
// rather than every work the process has ever reconciled.
func (r *rollupGenerations) forget(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.seen, name)
}

// priorAppliedGeneration returns the ObservedGeneration of the WorkApplied condition, or -1
// when the condition is absent. This persisted value is the restart-safe signal used to emit
// the rollup lines exactly once per generation (a new agent pod reads the same status).
func priorAppliedGeneration(mw *workapiv1.ManifestWork) int64 {
	if c := meta.FindStatusCondition(mw.Status.Conditions, workapiv1.WorkApplied); c != nil {
		return c.ObservedGeneration
	}
	return -1
}

// applyOutcome maps an apply error to the per-resource outcome value.
func applyOutcome(err error) string {
	if err != nil {
		return outcomeFailed
	}
	return outcomeApplied
}

// priorManifestOutcome returns the outcome recorded by the resource's last-persisted
// ManifestApplied condition ("applied"/"failed"), or "" when unknown. It is the restart-safe
// signal used to detect a per-resource outcome change on a later reconcile.
func priorManifestOutcome(mc *workapiv1.ManifestCondition) string {
	if mc == nil {
		return ""
	}
	c := meta.FindStatusCondition(mc.Conditions, workapiv1.ManifestApplied)
	if c == nil {
		return ""
	}
	if c.Status == metav1.ConditionTrue {
		return outcomeApplied
	}
	return outcomeFailed
}

// countApplyResults tallies results by strategy: read-only manifests apply nothing (counted
// separately), otherwise a non-nil error is a failure and success is an apply.
func countApplyResults(results []applyResult) (applied, failed, readOnly int) {
	for _, r := range results {
		switch {
		case r.strategy == workapiv1.UpdateStrategyTypeReadOnly:
			readOnly++
		case r.Error != nil:
			failed++
		default:
			applied++
		}
	}
	return applied, failed, readOnly
}

// rollupOutcome maps the applying-manifest counts to the work-level outcome value. A work with no
// applying manifests (all read-only) reports read_only.
func rollupOutcome(applied, failed int) string {
	switch {
	case applied == 0 && failed == 0:
		return outcomeReadOnly
	case failed == 0:
		return outcomeApplied
	case applied == 0:
		return outcomeFailed
	default:
		return "partial"
	}
}

// emitResourceApply logs one per-resource apply-latency line. It is a noop when the feature is
// disabled. Otherwise it emits:
//   - flowResourceSpokeApply, on the first apply of a generation (firstApply), for every resource —
//     read-only manifests get outcome=read_only (observed, not applied), others applied/failed; or
//   - flowResourceSpokeSync, on a later reconcile of the same generation, only when a non-read-only
//     resource's outcome changed vs prevOutcome (its last-persisted ManifestApplied result).
//
// Read-only manifests never emit a sync line (their outcome cannot change), and a steady-state resync
// with no outcome change emits nothing — this bounds the per-resource stream to the propagation event
// plus genuine recoveries/regressions.
func emitResourceApply(ctx context.Context, logApply, firstApply bool, wm workMeta, om orderedManifest,
	strategy workapiv1.UpdateStrategyType, result applyResult, prevOutcome string) {
	if !logApply {
		return
	}

	readOnly := strategy == workapiv1.UpdateStrategyTypeReadOnly
	outcome := applyOutcome(result.Error)
	if readOnly {
		outcome = outcomeReadOnly
	}

	flow := ""
	switch {
	case firstApply:
		flow = flowResourceSpokeApply
	case !readOnly && prevOutcome != "" && prevOutcome != outcome:
		flow = flowResourceSpokeSync
	default:
		return
	}

	now := time.Now().UTC()
	resourceVersion := ""
	if result.Error == nil && result.Result != nil {
		if accessor, err := meta.Accessor(result.Result); err == nil {
			resourceVersion = accessor.GetResourceVersion()
		}
	}

	kv := []any{
		"flow", flow,
		"mw_name", wm.name,
		"mw_namespace", wm.namespace,
		"generation", wm.generation,
		"applied_kind", om.resourceMeta.Kind,
		"applied_name", om.resourceMeta.Name,
		"applied_namespace", om.resourceMeta.Namespace,
		"applied_resource_version", resourceVersion,
		"ts_utc", now.Format(applyLogTimeFormat),
		"ts_epoch_ms", now.UnixMilli(),
		"outcome", outcome,
		"labels", wm.labels,
	}
	if flow == flowResourceSpokeSync {
		kv = append(kv, "prev_outcome", prevOutcome)
	}
	applyLogSink.Info("manifestwork resource applied", kv...)
}

// emitApplyRollup logs one work-level rollup line. With counts == nil it is the start line
// (mw_spoke_apply, no outcome, stamped before the apply loop); with counts != nil it is the end
// line (mw_spoke_apply_result, carrying applied/failed/outcome, stamped after the apply loop).
func emitApplyRollup(ctx context.Context, wm workMeta, flow string, resourceCount int, counts *applyCounts) {
	now := time.Now().UTC()
	kv := []any{
		"flow", flow,
		"mw_name", wm.name,
		"mw_namespace", wm.namespace,
		"generation", wm.generation,
		"resource_count", resourceCount,
		"ts_utc", now.Format(applyLogTimeFormat),
		"ts_epoch_ms", now.UnixMilli(),
		"labels", wm.labels,
	}
	if counts != nil {
		kv = append(kv,
			"applied_count", counts.applied,
			"failed_count", counts.failed,
			"read_only_count", counts.readOnly,
			"outcome", rollupOutcome(counts.applied, counts.failed),
		)
	}
	applyLogSink.Info("manifestwork apply", kv...)
}
