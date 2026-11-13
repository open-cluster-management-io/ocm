package spiffe

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	sdkevents "open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/patcher"
)

// recordingQueue records AddAfter; nothing else is used by the guard.
type recordingQueue struct {
	workqueue.TypedRateLimitingInterface[string]
	afters []time.Duration
}

func (q *recordingQueue) AddAfter(_ string, d time.Duration) { q.afters = append(q.afters, d) }

type syncContext struct{ queue *recordingQueue }

func (s syncContext) Queue() workqueue.TypedRateLimitingInterface[string] { return s.queue }
func (s syncContext) Recorder() sdkevents.Recorder {
	return sdkevents.NewContextualLoggingEventRecorder("test")
}

var (
	t1040 = time.Date(2026, 11, 13, 10, 40, 0, 0, time.UTC)
	pin   = map[string]string{clusterv1.SPIFFETrustDomainAnnotationKey: "ocm-hub.example.org"}
)

func cluster(annotations map[string]string, deletedAt *time.Time, finalizers ...string) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "c1", Annotations: annotations, Finalizers: finalizers}}
	if deletedAt != nil {
		ts := metav1.NewTime(*deletedAt)
		c.DeletionTimestamp = &ts
	}
	return c
}

type guardResult struct {
	actions []clienttesting.Action
	afters  []time.Duration
	client  *clusterfake.Clientset
}

func runGuard(t *testing.T, c *clusterv1.ManagedCluster, now time.Time, lifetime, skew time.Duration, all bool) guardResult {
	t.Helper()
	client := clusterfake.NewSimpleClientset(c)
	informers := clusterinformers.NewSharedInformerFactory(client, 0)
	require.NoError(t, informers.Cluster().V1().ManagedClusters().Informer().GetStore().Add(c))
	g := &deleteGuard{
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			client.ClusterV1().ManagedClusters()),
		lister:      informers.Cluster().V1().ManagedClusters().Lister(),
		lifetime:    lifetime,
		skew:        skew,
		allClusters: all,
		now:         func() time.Time { return now },
	}
	q := &recordingQueue{}
	require.NoError(t, g.sync(context.Background(), syncContext{queue: q}, c.Name))
	return guardResult{actions: client.Actions(), afters: q.afters, client: client}
}

func patchedFinalizers(t *testing.T, a clienttesting.Action) string {
	t.Helper()
	require.Equal(t, "patch", a.GetVerb())
	return string(a.(clienttesting.PatchAction).GetPatch())
}

func TestDeleteGuardAddsFinalizer(t *testing.T) {
	r := runGuard(t, cluster(pin, nil), t1040, time.Hour, 5*time.Minute, false)
	require.Len(t, r.actions, 1)
	require.Contains(t, patchedFinalizers(t, r.actions[0]), RevocationFinalizer)
}

func TestDeleteGuardSkipsOtherDrivers(t *testing.T) {
	r := runGuard(t, cluster(nil, nil), t1040, time.Hour, 5*time.Minute, false)
	require.Empty(t, r.actions, "an unpinned cluster is not guarded next to another driver")

	r = runGuard(t, cluster(nil, nil), t1040, time.Hour, 5*time.Minute, true)
	require.Len(t, r.actions, 1, "every cluster is guarded when spiffe is the only driver")
	require.Contains(t, patchedFinalizers(t, r.actions[0]), RevocationFinalizer)
}

func TestDeleteGuardWaits(t *testing.T) {
	deleted := t1040
	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer), t1040.Add(time.Minute), time.Hour, 5*time.Minute, false)
	require.Empty(t, r.actions, "no patch")
	require.Equal(t, []time.Duration{64 * time.Minute}, r.afters)
}

func TestDeleteGuardReleases(t *testing.T) {
	deleted := t1040
	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer, "other"),
		t1040.Add(65*time.Minute+time.Second), time.Hour, 5*time.Minute, false)
	require.Len(t, r.actions, 1)
	p := patchedFinalizers(t, r.actions[0])
	require.NotContains(t, p, RevocationFinalizer)
	require.Contains(t, p, "other", "the other finalizers stay")
	require.Empty(t, r.afters)
}

func TestDeleteGuardBoundary(t *testing.T) {
	deleted := t1040
	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer), t1040.Add(65*time.Minute), time.Hour, 5*time.Minute, false)
	require.Len(t, r.actions, 1, "released at exactly 11:45")
	require.Empty(t, r.afters)
}

func TestDeleteGuardDefault(t *testing.T) {
	deleted := t1040
	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer), t1040, 24*time.Hour, 5*time.Minute, false)
	require.Empty(t, r.actions)
	// Released at 10:45 the next day.
	require.Equal(t, []time.Duration{24*time.Hour + 5*time.Minute}, r.afters)
}

// TestDeleteGuardSPIREClockAhead: SPIRE's clock runs 4m ahead and issues an SVID with a 1h TTL
// at 10:39:30 hub time, so its NotAfter is 11:43:30 hub time. The 11:45 release still comes
// after that, because maxClockSkew (5m) covers the 4m.
func TestDeleteGuardSPIREClockAhead(t *testing.T) {
	deleted := t1040
	issuedHubTime := t1040.Add(-30 * time.Second)
	notAfterHubTime := issuedHubTime.Add(4 * time.Minute).Add(time.Hour)
	release := deleted.Add(time.Hour + 5*time.Minute)
	require.True(t, release.After(notAfterHubTime))

	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer), notAfterHubTime, time.Hour, 5*time.Minute, false)
	require.Empty(t, r.actions, "still held when the SVID expires")
	require.Equal(t, []time.Duration{release.Sub(notAfterHubTime)}, r.afters)
}

func TestDeleteGuardRecreateBlocked(t *testing.T) {
	deleted := t1040
	r := runGuard(t, cluster(pin, &deleted, RevocationFinalizer), t1040.Add(time.Minute), time.Hour, 5*time.Minute, false)
	_, err := r.client.ClusterV1().ManagedClusters().Create(context.Background(),
		&clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "c1"}}, metav1.CreateOptions{})
	require.True(t, apierrors.IsAlreadyExists(err), "got %v", err)
}

func TestDeleteGuardGone(t *testing.T) {
	client := clusterfake.NewSimpleClientset()
	informers := clusterinformers.NewSharedInformerFactory(client, 0)
	g := &deleteGuard{lister: informers.Cluster().V1().ManagedClusters().Lister(), now: time.Now}
	require.NoError(t, g.sync(context.Background(), syncContext{queue: &recordingQueue{}}, "c1"))
	require.Empty(t, client.Actions())
	var _ runtime.Object = &clusterv1.ManagedCluster{}
}
