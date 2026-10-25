//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// claimRecorder records the TLS state each share-claim verb receives on its
// context, which is what go-qrdb binds the claim to.
type claimRecorder struct {
	reached bool
	state   *tls.ConnectionState
}

func (c *claimRecorder) record(ctx context.Context) {
	c.reached = true
	c.state = qrdbsdk.TLSStateFromContext(ctx)
}

// claimBarrierService implements the four system share-claim verbs.
type claimBarrierService struct {
	*fakeBarrierService
	claimRecorder
}

func (c *claimBarrierService) ShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "00ff"}, nil
}

func (c *claimBarrierService) ShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

func (c *claimBarrierService) RecoveryShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "00ff"}, nil
}

func (c *claimBarrierService) RecoveryShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

// claimTenantBarrierService implements the two tenant share-claim verbs.
type claimTenantBarrierService struct {
	*fakeTenantBarrierService
	claimRecorder
}

func (c *claimTenantBarrierService) TenantBarrierShareClaimBegin(ctx context.Context, _ *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "00ff"}, nil
}

func (c *claimTenantBarrierService) TenantBarrierShareClaimComplete(ctx context.Context, _ *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	c.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

// claimRoute is one share-claim handler wired to a fresh recording service.
type claimRoute struct {
	name    string
	handler func(http.ResponseWriter, *http.Request)
	rec     *claimRecorder
}

// claimRoutes returns every holder share-claim route, system and tenant,
// begin and complete.
func claimRoutes(t *testing.T) []claimRoute {
	t.Helper()
	sys := &claimBarrierService{fakeBarrierService: &fakeBarrierService{}}
	s := barrierServer(t, sys)
	ten := &claimTenantBarrierService{fakeTenantBarrierService: &fakeTenantBarrierService{}}
	ts := tenantBarrierServer(t, ten)
	return []claimRoute{
		{"begin", s.HandleBarrierShareClaimBegin, &sys.claimRecorder},
		{"complete", s.HandleBarrierShareClaimComplete, &sys.claimRecorder},
		{"recovery-begin", s.HandleBarrierRecoveryShareClaimBegin, &sys.claimRecorder},
		{"recovery-complete", s.HandleBarrierRecoveryShareClaimComplete, &sys.claimRecorder},
		{"tenant-begin", ts.HandleTenantBarrierShareClaimBegin, &ten.claimRecorder},
		{"tenant-complete", ts.HandleTenantBarrierShareClaimComplete, &ten.claimRecorder},
	}
}

// holderCert is a client certificate naming a barrier holder.
func holderCert(name string) *x509.Certificate {
	return &x509.Certificate{Subject: pkix.Name{CommonName: name}}
}

// claimReq builds a claim request for holder "alice" in tenant "acme" with
// the given TLS state.
func claimReq(state *tls.ConnectionState) *http.Request {
	r := tenantBarrierReq(http.MethodPost, "acme", `{"username":"alice"}`)
	r.TLS = state
	return r
}

// A claim without a verified client certificate never reaches go-qrdb: there
// it would read as an in-process claim and run as the holder named in the
// path, overwriting that holder's outstanding nonce.
func TestHolderClaim_RefusesUnverifiedPeer(t *testing.T) {
	unverified := map[string]*tls.ConnectionState{
		"no-tls":         nil,
		"no-peer-cert":   {},
		"unverified-raw": {PeerCertificates: []*x509.Certificate{holderCert("alice")}},
	}
	for _, route := range claimRoutes(t) {
		for stateName, state := range unverified {
			t.Run(route.name+"/"+stateName, func(t *testing.T) {
				*route.rec = claimRecorder{}
				w := httptest.NewRecorder()

				route.handler(w, claimReq(state))

				if w.Code != http.StatusUnauthorized {
					t.Fatalf("status %d, want 401; body %s", w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), ErrHolderClaimPeerCertRequired.Error()) {
					t.Fatalf("body %s does not name the refusal", w.Body.String())
				}
				if route.rec.reached {
					t.Fatal("an unbound claim reached the barrier service")
				}
			})
		}
	}
}

// A verified caller's TLS state reaches go-qrdb unchanged, so the claim is
// bound to the certificate the caller presented, whichever holder the path
// names: a different holder's certificate is carried as itself for go-qrdb
// to refuse, never replaced by the named holder's enrolled certificate.
func TestHolderClaim_ForwardsCallerTLSState(t *testing.T) {
	callers := map[string]*x509.Certificate{
		"matching-holder":  holderCert("alice"),
		"different-holder": holderCert("mallory"),
	}
	for _, route := range claimRoutes(t) {
		for callerName, cert := range callers {
			t.Run(route.name+"/"+callerName, func(t *testing.T) {
				*route.rec = claimRecorder{}
				state := &tls.ConnectionState{
					PeerCertificates: []*x509.Certificate{cert},
					VerifiedChains:   [][]*x509.Certificate{{cert}},
				}
				w := httptest.NewRecorder()

				route.handler(w, claimReq(state))

				if w.Code != http.StatusOK {
					t.Fatalf("status %d, want 200; body %s", w.Code, w.Body.String())
				}
				if route.rec.state != state {
					t.Fatalf("barrier service saw TLS state %+v, want the caller's", route.rec.state)
				}
			})
		}
	}
}
