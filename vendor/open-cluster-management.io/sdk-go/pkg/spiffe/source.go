// Package spiffe holds the SPIFFE Workload API helpers shared by the OCM hub gRPC server and
// the agents.
package spiffe

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// ErrSVIDNotFound is returned when the Workload API has no SVID with the expected ID.
type ErrSVIDNotFound struct {
	Want spiffeid.ID
	Got  []spiffeid.ID
}

func (e *ErrSVIDNotFound) Error() string {
	return fmt.Sprintf("no SVID with ID %s from the Workload API; received %v", e.Want, e.Got)
}

// X509Source is a Workload API source that always presents the SVID with one expected ID. It
// keeps the SVID, its key and the trust bundles in memory only, and follows rotation.
type X509Source struct {
	src     *workloadapi.X509Source
	want    spiffeid.ID
	missing atomic.Pointer[ErrSVIDNotFound]
}

var (
	_ x509svid.Source   = &X509Source{}
	_ x509bundle.Source = &X509Source{}
)

// NewX509Source opens the Workload API at addr (for example
// unix:///spiffe-workload-api/spire-agent.sock) and picks the SVID whose ID is want, never by
// position: the platform may give the same pod other SVIDs too. It blocks until the first update
// arrives or ctx ends, and fails with *ErrSVIDNotFound if that update has no SVID with ID want.
func NewX509Source(ctx context.Context, addr string, want spiffeid.ID) (*X509Source, error) {
	s := &X509Source{want: want}
	picker := func(svids []*x509svid.SVID) *x509svid.SVID {
		got := make([]spiffeid.ID, 0, len(svids))
		for _, sv := range svids {
			if sv.ID == want {
				s.missing.Store(nil)
				return sv
			}
			got = append(got, sv.ID)
		}
		s.missing.Store(&ErrSVIDNotFound{Want: want, Got: got})
		return nil
	}
	src, err := workloadapi.NewX509Source(ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(addr)),
		workloadapi.WithDefaultX509SVIDPicker(picker))
	if err != nil {
		return nil, err
	}
	s.src = src
	if e := s.missing.Load(); e != nil {
		_ = src.Close()
		return nil, e
	}
	return s, nil
}

// ID is the SPIFFE ID this source presents.
func (s *X509Source) ID() spiffeid.ID { return s.want }

// GetX509SVID returns the current SVID with the expected ID. If a later update from the
// Workload API no longer has it, it returns *ErrSVIDNotFound, so a readiness check can report
// what was expected and what arrived.
func (s *X509Source) GetX509SVID() (*x509svid.SVID, error) {
	if e := s.missing.Load(); e != nil {
		return nil, e
	}
	return s.src.GetX509SVID()
}

// GetX509BundleForTrustDomain returns the trust bundle for td: the source's own trust domain or
// one its SPIRE federates with.
func (s *X509Source) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return s.src.GetX509BundleForTrustDomain(td)
}

// Ready returns nil when the source has its SVID, else the reason it hasn't.
func (s *X509Source) Ready() error {
	_, err := s.GetX509SVID()
	return err
}

// Close closes the connection to the Workload API.
func (s *X509Source) Close() error { return s.src.Close() }
