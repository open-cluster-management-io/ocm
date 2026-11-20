// Package spiffe serves the hub gRPC server's SPIFFE listener: mTLS with the server's SVID from
// the Workload API, and a registry of client connections that closes each one when its SVID
// expires or when the hub revokes the cluster or add-on.
package spiffe

import (
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"k8s.io/klog/v2"

	ocmauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
)

// stopper is what the registry needs from a timer.
type stopper interface{ Stop() bool }

// Registry tracks open SPIFFE connections and closes them at SVID expiry or on demand. It is
// safe for concurrent use.
type Registry struct {
	mu        sync.Mutex
	conns     map[*trackedConn]struct{}
	now       func() time.Time
	afterFunc func(time.Duration, func()) stopper
}

// NewRegistry returns an empty registry that uses the real clock.
func NewRegistry() *Registry {
	return &Registry{
		conns:     map[*trackedConn]struct{}{},
		now:       time.Now,
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
	}
}

// trackedConn is a client connection with the identity it authenticated with.
type trackedConn struct {
	net.Conn
	id       ocmauthn.OCMIdentity
	spiffeID spiffeid.ID
	serial   *big.Int
	notAfter time.Time
	timer    stopper // guarded by reg.mu
	once     sync.Once
	reg      *Registry
}

// Close closes the connection and removes it from the registry. It is safe to call more than
// once.
func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.reg.mu.Lock()
		timer := c.timer
		delete(c.reg.conns, c)
		c.reg.mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	})
	return c.Conn.Close()
}

// track registers conn and arms a timer that closes it at notAfter.
func (r *Registry) track(conn net.Conn, id ocmauthn.OCMIdentity, sid spiffeid.ID, serial *big.Int, notAfter time.Time) *trackedConn {
	tc := &trackedConn{Conn: conn, id: id, spiffeID: sid, serial: serial, notAfter: notAfter, reg: r}
	r.mu.Lock()
	r.conns[tc] = struct{}{}
	r.mu.Unlock()
	// Arm the timer after the connection is registered, so an already expired SVID is closed
	// and removed rather than left behind.
	timer := r.afterFunc(notAfter.Sub(r.now()), func() {
		klog.Infof("closing connection of %s: SVID expired at %s", sid, notAfter.UTC().Format(time.RFC3339))
		_ = tc.Close()
	})
	r.mu.Lock()
	tc.timer = timer
	_, open := r.conns[tc]
	r.mu.Unlock()
	if !open {
		// Closed before the timer was stored (an already expired SVID).
		timer.Stop()
	}
	return tc
}

// closeWhere closes every connection whose identity matches, outside the lock, and returns how
// many it closed.
func (r *Registry) closeWhere(match func(*trackedConn) bool) int {
	r.mu.Lock()
	var hit []*trackedConn
	for c := range r.conns {
		if match(c) {
			hit = append(hit, c)
		}
	}
	r.mu.Unlock()
	for _, c := range hit {
		_ = c.Close()
	}
	return len(hit)
}

// CloseCluster closes every connection of a cluster's Klusterlet agents and add-on agents.
func (r *Registry) CloseCluster(cluster string) int {
	n := r.closeWhere(func(c *trackedConn) bool { return c.id.ClusterName == cluster })
	if n > 0 {
		klog.Infof("closed %d connection(s) of cluster %s", n, cluster)
	}
	return n
}

// CloseAddOn closes the connections of one add-on's agents on one cluster.
func (r *Registry) CloseAddOn(cluster, addon string) int {
	n := r.closeWhere(func(c *trackedConn) bool {
		return c.id.ClusterName == cluster && c.id.AddOnName == addon
	})
	if n > 0 {
		klog.Infof("closed %d connection(s) of add-on %s on cluster %s", n, addon, cluster)
	}
	return n
}

// CloseTrustDomain closes every connection whose SVID is in td, for when td is removed from the
// accepted agent trust domains.
func (r *Registry) CloseTrustDomain(td spiffeid.TrustDomain) int {
	n := r.closeWhere(func(c *trackedConn) bool { return c.spiffeID.MemberOf(td) })
	if n > 0 {
		klog.Infof("closed %d connection(s) from trust domain %s", n, td)
	}
	return n
}

// Len returns the number of open tracked connections.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns)
}

// PeerSerials returns the certificate serial numbers of the open connections of id, for tests
// and debugging.
func (r *Registry) PeerSerials(id spiffeid.ID) []*big.Int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*big.Int
	for c := range r.conns {
		if c.spiffeID == id {
			out = append(out, c.serial)
		}
	}
	return out
}
