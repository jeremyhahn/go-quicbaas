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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// holderBarrierService overrides the holder verbs these tests drive; every
// other BarrierServicer method falls through to fakeBarrierService.
type holderBarrierService struct {
	*fakeBarrierService
	err        error
	quorumReq  *qrdbtransport.QuorumBeginRequest
	claimReq   *qrdbtransport.ShareClaimBeginRequest
	officers   []*qrdbtransport.OfficerStatus
	quorumSeen bool
}

func (h *holderBarrierService) BarrierQuorumBegin(_ context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	h.quorumSeen = true
	if h.err != nil {
		return nil, h.err
	}
	h.quorumReq = req
	return &qrdbtransport.QuorumChallenge{ChallengeID: "ch-1", Domain: req.Domain, Op: req.Op, Username: req.Username}, nil
}

func (h *holderBarrierService) ShareClaimBegin(_ context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	h.claimReq = req
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "00ff"}, nil
}

func (h *holderBarrierService) OfficerList(context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return h.officers, h.err
}

func newHolderBarrierService() *holderBarrierService {
	return &holderBarrierService{fakeBarrierService: &fakeBarrierService{}}
}

// A quorum round begun through go-qrdb's own REST client reaches the service
// with every field intact and the challenge decodes back unchanged.
func TestBarrierWire_QuorumBeginRoundTrips(t *testing.T) {
	svc := newHolderBarrierService()
	client := newQRDBRESTClient(t, barrierServer(t, svc))

	challenge, err := client.BarrierQuorumBegin(context.Background(), &qrdbtransport.QuorumBeginRequest{
		Domain: "system", Op: "unseal", Username: "alice", Factor: "passphrase",
	})
	if err != nil {
		t.Fatalf("BarrierQuorumBegin over the qrdb REST transport: %v", err)
	}
	if svc.quorumReq == nil || svc.quorumReq.Username != "alice" || svc.quorumReq.Factor != "passphrase" {
		t.Fatalf("request reached the service as %+v", svc.quorumReq)
	}
	if challenge.ChallengeID != "ch-1" || challenge.Op != "unseal" {
		t.Fatalf("challenge decoded as %+v", challenge)
	}
}

// The officer named in the URL is the one whose claim begins, whatever the
// body says.
func TestHandleBarrierShareClaimBegin_BindsPathUsername(t *testing.T) {
	svc := newHolderBarrierService()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/barrier/officers/alice/claim/begin",
		strings.NewReader(`{"username":"mallory"}`))
	r.Header.Set("Content-Type", "application/json")
	cert := holderCert("alice")
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	w := httptest.NewRecorder()

	barrierServer(t, svc).router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if svc.claimReq == nil || svc.claimReq.Username != "alice" {
		t.Fatalf("claim reached the service as %+v", svc.claimReq)
	}
}

// The listing uses the {"officers":[...]} envelope go-qrdb's client decodes.
func TestHandleBarrierOfficerList_Envelope(t *testing.T) {
	svc := newHolderBarrierService()
	svc.officers = []*qrdbtransport.OfficerStatus{{Username: "alice", ShareIndex: 1}}

	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/officers", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var got barrierOfficerList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Officers) != 1 || got.Officers[0].Username != "alice" {
		t.Fatalf("officers decoded as %+v", got.Officers)
	}
}

// A malformed body is refused before the service is reached; a service
// failure is answered as an error, never as a challenge.
func TestHandleBarrierQuorumBegin_Errors(t *testing.T) {
	t.Run("malformed body", func(t *testing.T) {
		svc := newHolderBarrierService()
		w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/quorum/begin", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
		if svc.quorumSeen {
			t.Fatal("a malformed body reached the service")
		}
	})
	t.Run("service failure", func(t *testing.T) {
		svc := newHolderBarrierService()
		svc.err = errBarrierBoom
		w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/quorum/begin", `{"op":"unseal"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status %d, want 500", w.Code)
		}
	})
}

// holderTenantBarrierService records the tenant quorum request it is handed.
type holderTenantBarrierService struct {
	*fakeTenantBarrierService
	req *qrdbtransport.TenantQuorumBeginRequest
}

func (h *holderTenantBarrierService) TenantBarrierQuorumBegin(_ context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	h.req = req
	return &qrdbtransport.QuorumChallenge{ChallengeID: "ch-t"}, nil
}

// The tenant in the URL scopes the round; a body naming another tenant is
// overridden.
func TestHandleTenantBarrierQuorumBegin_BindsPathTenant(t *testing.T) {
	svc := &holderTenantBarrierService{fakeTenantBarrierService: &fakeTenantBarrierService{}}
	w := httptest.NewRecorder()

	tenantBarrierServer(t, svc).HandleTenantBarrierQuorumBegin(w,
		tenantBarrierReq(http.MethodPost, "acme", `{"tenant_id":"other","op":"unseal","username":"alice"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if svc.req == nil || svc.req.TenantID != "acme" || svc.req.Username != "alice" {
		t.Fatalf("request reached the service as %+v", svc.req)
	}
}

func TestHandleTenantBarrierQuorumBegin_MissingTenant(t *testing.T) {
	svc := &holderTenantBarrierService{fakeTenantBarrierService: &fakeTenantBarrierService{}}
	w := httptest.NewRecorder()

	tenantBarrierServer(t, svc).HandleTenantBarrierQuorumBegin(w,
		tenantBarrierReq(http.MethodPost, "", `{"op":"unseal"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if svc.req != nil {
		t.Fatal("a request without a tenant reached the service")
	}
}
