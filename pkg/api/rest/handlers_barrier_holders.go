//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"net/http"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Barrier holder REST handlers — officers, share claims, quorum rounds.
//
// go-qrdb v0.47 replaced the share-submission verbs (unseal-share, rekey,
// recovery/recover, root-token) with one officer/quorum model. These handlers
// re-serve go-qrdb's own /v1/barrier wire for the sealed-window listener and
// mirror it under /api/v1. Every request and response is the go-qrdb SDK
// transport type, passed through unchanged: quicbaas owns no holder logic.
// =============================================================================

// barrierOfficerList is the wire shape of an officer listing, matching the
// go-qrdb REST client's decode of GET .../barrier/officers.
type barrierOfficerList struct {
	Officers []*qrdbtransport.OfficerStatus `json:"officers"`
}

// serveHolderVerb decodes the body into req, lets bind fill the fields the
// URL path carries (nil when none do), calls the go-qrdb verb, and answers its
// result as JSON. onErr classifies a failed call.
func serveHolderVerb[Req, Resp any](
	w http.ResponseWriter,
	r *http.Request,
	req *Req,
	bind func(*Req),
	call func(context.Context, *Req) (Resp, error),
	onErr func(http.ResponseWriter, *http.Request, error),
) {
	if !readBarrierJSON(w, r, req) {
		return
	}
	if bind != nil {
		bind(req)
	}
	resp, err := call(holderContext(r), req)
	if err != nil {
		onErr(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// serveHolderClaim serves a share-claim verb (begin or complete) on either
// listener. go-qrdb binds a claim to the caller's certificate and reads a
// context without TLS state as an in-process claim, for which it substitutes
// the holder's enrolled certificate. So a claim arriving without a verified
// peer certificate is refused with 401, and the caller's TLS state rides the
// context to the SDK through qrdbsdk.WithTLSState.
func serveHolderClaim[Req, Resp any](
	w http.ResponseWriter,
	r *http.Request,
	req *Req,
	bind func(*Req),
	call func(context.Context, *Req) (Resp, error),
	onErr func(http.ResponseWriter, *http.Request, error),
) {
	if verifiedPeerCertFromRequest(r) == nil {
		RespondError(w, r, http.StatusUnauthorized, ErrHolderClaimPeerCertRequired)
		return
	}
	serveHolderVerb(w, r, req, bind, call, onErr)
}

// holderContext returns r's context carrying the caller's TLS state when the
// peer certificate was verified, so go-qrdb binds a holder, quorum or
// ceremony call to that certificate. Without a verified certificate the call
// stays anonymous: an unverified chain must never stand in for a holder.
func holderContext(r *http.Request) context.Context {
	if verifiedPeerCertFromRequest(r) == nil {
		return r.Context()
	}
	return qrdbsdk.WithTLSState(r.Context(), r.TLS)
}

// pathUsername reads the {username} path segment of a share-claim route.
func pathUsername(r *http.Request) string {
	return chi.URLParam(r, "username")
}

// requireTenantPath reports 400 and returns false when the {tenant_id} path
// segment is empty.
func requireTenantPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return "", false
	}
	return tenantID, true
}

// HandleBarrierClusterUnseal handles POST /v1/barrier/cluster-unseal.
func (s *Server) HandleBarrierClusterUnseal(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	state, err := s.barrierSvc.BarrierClusterUnseal(holderContext(r))
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, state)
}

// HandleBarrierOfficerList handles GET /v1/barrier/officers.
func (s *Server) HandleBarrierOfficerList(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	officers, err := s.barrierSvc.OfficerList(holderContext(r))
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, barrierOfficerList{Officers: officers})
}

// HandleBarrierOfficerRegister handles POST /v1/barrier/officers.
func (s *Server) HandleBarrierOfficerRegister(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.OfficerRegisterRequest
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.OfficerRegister, s.handleServiceError)
}

// HandleBarrierRecoveryOfficerRegister handles POST /v1/barrier/recovery/officers.
func (s *Server) HandleBarrierRecoveryOfficerRegister(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.OfficerRegisterRequest
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.RecoveryOfficerRegister, s.handleServiceError)
}

// HandleBarrierShareClaimBegin handles
// POST /v1/barrier/officers/{username}/claim/begin.
func (s *Server) HandleBarrierShareClaimBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.ShareClaimBeginRequest
	bind := func(req *qrdbtransport.ShareClaimBeginRequest) { req.Username = pathUsername(r) }
	serveHolderClaim(w, r, &req, bind, s.barrierSvc.ShareClaimBegin, s.handleServiceError)
}

// HandleBarrierShareClaimComplete handles
// POST /v1/barrier/officers/{username}/claim/complete.
func (s *Server) HandleBarrierShareClaimComplete(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.ShareClaimCompleteRequest
	bind := func(req *qrdbtransport.ShareClaimCompleteRequest) { req.Username = pathUsername(r) }
	serveHolderClaim(w, r, &req, bind, s.barrierSvc.ShareClaimComplete, s.handleServiceError)
}

// HandleBarrierRecoveryShareClaimBegin handles
// POST /v1/barrier/recovery/officers/{username}/claim/begin.
func (s *Server) HandleBarrierRecoveryShareClaimBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.ShareClaimBeginRequest
	bind := func(req *qrdbtransport.ShareClaimBeginRequest) { req.Username = pathUsername(r) }
	serveHolderClaim(w, r, &req, bind, s.barrierSvc.RecoveryShareClaimBegin, s.handleServiceError)
}

// HandleBarrierRecoveryShareClaimComplete handles
// POST /v1/barrier/recovery/officers/{username}/claim/complete.
func (s *Server) HandleBarrierRecoveryShareClaimComplete(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.ShareClaimCompleteRequest
	bind := func(req *qrdbtransport.ShareClaimCompleteRequest) { req.Username = pathUsername(r) }
	serveHolderClaim(w, r, &req, bind, s.barrierSvc.RecoveryShareClaimComplete, s.handleServiceError)
}

// HandleBarrierQuorumBegin handles POST /v1/barrier/quorum/begin.
func (s *Server) HandleBarrierQuorumBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.QuorumBeginRequest
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.BarrierQuorumBegin, s.handleServiceError)
}

// HandleBarrierQuorumSubmit handles POST /v1/barrier/quorum/submit. The
// submitted share and factor response are key material, wiped when the
// request ends.
func (s *Server) HandleBarrierQuorumSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.QuorumSubmitRequest
	defer wipeCredential(&req.Share)()
	defer wipeCredential(&req.Response)()
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.BarrierQuorumSubmit, s.handleServiceError)
}

// HandleBarrierRekeyStart handles POST /v1/barrier/rekey/start.
func (s *Server) HandleBarrierRekeyStart(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.RekeyStartRequest
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.BarrierRekeyStart, s.handleServiceError)
}

// HandleBarrierOfficerRevoke handles POST /v1/barrier/officers/revoke.
func (s *Server) HandleBarrierOfficerRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}
	var req qrdbtransport.OfficerRevokeRequest
	serveHolderVerb(w, r, &req, nil, s.barrierSvc.BarrierOfficerRevoke, s.handleServiceError)
}

// HandleTenantBarrierOfficerList handles
// GET /api/v1/tenants/{tenant_id}/barrier/officers.
func (s *Server) HandleTenantBarrierOfficerList(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	officers, err := svc.TenantBarrierOfficerList(holderContext(r), tenantID)
	if err != nil {
		s.handleTenantBarrierError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, barrierOfficerList{Officers: officers})
}

// HandleTenantBarrierOfficerRegister handles
// POST /api/v1/tenants/{tenant_id}/barrier/officers.
func (s *Server) HandleTenantBarrierOfficerRegister(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantOfficerRegisterRequest
	bind := func(req *qrdbtransport.TenantOfficerRegisterRequest) { req.TenantID = tenantID }
	serveHolderVerb(w, r, &req, bind, svc.TenantBarrierOfficerRegister, s.handleTenantBarrierError)
}

// HandleTenantBarrierShareClaimBegin handles
// POST /api/v1/tenants/{tenant_id}/barrier/officers/{username}/claim/begin.
func (s *Server) HandleTenantBarrierShareClaimBegin(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantShareClaimBeginRequest
	bind := func(req *qrdbtransport.TenantShareClaimBeginRequest) {
		req.TenantID = tenantID
		req.Username = pathUsername(r)
	}
	serveHolderClaim(w, r, &req, bind, svc.TenantBarrierShareClaimBegin, s.handleTenantBarrierError)
}

// HandleTenantBarrierShareClaimComplete handles
// POST /api/v1/tenants/{tenant_id}/barrier/officers/{username}/claim/complete.
func (s *Server) HandleTenantBarrierShareClaimComplete(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantShareClaimCompleteRequest
	bind := func(req *qrdbtransport.TenantShareClaimCompleteRequest) {
		req.TenantID = tenantID
		req.Username = pathUsername(r)
	}
	serveHolderClaim(w, r, &req, bind, svc.TenantBarrierShareClaimComplete, s.handleTenantBarrierError)
}

// HandleTenantBarrierQuorumBegin handles
// POST /api/v1/tenants/{tenant_id}/barrier/quorum/begin.
func (s *Server) HandleTenantBarrierQuorumBegin(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantQuorumBeginRequest
	bind := func(req *qrdbtransport.TenantQuorumBeginRequest) { req.TenantID = tenantID }
	serveHolderVerb(w, r, &req, bind, svc.TenantBarrierQuorumBegin, s.handleTenantBarrierError)
}

// HandleTenantBarrierQuorumSubmit handles
// POST /api/v1/tenants/{tenant_id}/barrier/quorum/submit.
func (s *Server) HandleTenantBarrierQuorumSubmit(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantQuorumSubmitRequest
	defer wipeCredential(&req.Share)()
	defer wipeCredential(&req.Response)()
	bind := func(req *qrdbtransport.TenantQuorumSubmitRequest) { req.TenantID = tenantID }
	serveHolderVerb(w, r, &req, bind, svc.TenantBarrierQuorumSubmit, s.handleTenantBarrierError)
}

// HandleTenantBarrierRekeyStart handles
// POST /api/v1/tenants/{tenant_id}/barrier/rekey/start.
func (s *Server) HandleTenantBarrierRekeyStart(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantRekeyStartRequest
	bind := func(req *qrdbtransport.TenantRekeyStartRequest) { req.TenantID = tenantID }
	serveHolderVerb(w, r, &req, bind, svc.TenantBarrierRekeyStart, s.handleTenantBarrierError)
}

// HandleTenantBarrierOfficerRevoke handles
// POST /api/v1/tenants/{tenant_id}/barrier/officers/revoke.
func (s *Server) HandleTenantBarrierOfficerRevoke(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID, ok := requireTenantPath(w, r)
	if !ok {
		return
	}
	var req qrdbtransport.TenantOfficerRevokeRequest
	bind := func(req *qrdbtransport.TenantOfficerRevokeRequest) { req.TenantID = tenantID }
	serveHolderVerb(w, r, &req, bind, svc.TenantBarrierOfficerRevoke, s.handleTenantBarrierError)
}
