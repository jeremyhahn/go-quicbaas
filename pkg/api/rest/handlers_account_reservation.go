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
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ReserveAccountSessionRESTRequest is the JSON body for
// POST /api/v1/agent-accounts/{id}/reservations.
type ReserveAccountSessionRESTRequest struct {
	// ExpectedLive, when present, makes the claim conditional on the
	// account's live-session count still being the one the caller selected
	// on; a moved count answers 412 and the caller re-selects.
	ExpectedLive *int `json:"expected_live,omitempty"`
}

// ReserveAccountSessionRESTResponse carries the reservation. The session
// record that names it satisfies it.
type ReserveAccountSessionRESTResponse struct {
	ReservationID string    `json:"reservation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// ReleaseAccountSessionRESTResponse acknowledges a release.
type ReleaseAccountSessionRESTResponse struct {
	ReservationID string    `json:"reservation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// HandleReserveAccountSession handles
// POST /api/v1/agent-accounts/{id}/reservations.
//
// The tenant is the principal's, exactly as for the credential release this
// verb accompanies: a tenant_id query naming another tenant is refused. An
// account at its session cap answers 409; a conditional claim whose count
// moved answers 412.
func (s *Server) HandleReserveAccountSession(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")
	if accountID == "" {
		RespondBadRequest(w, r, "account id is required")
		return
	}
	subject, ok := requireTenantSubject(w, r)
	if !ok {
		return
	}
	if !queryIdentityMatches(w, r, subject, s.lazyCallerAccount(w, r, subject)) {
		return
	}
	var req ReserveAccountSessionRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}
	resp, err := accountSvc.ReserveAccountSession(r.Context(), &service.ReserveAccountSessionRequest{
		TenantID: subject.TenantID, AccountID: accountID, ExpectedLive: req.ExpectedLive,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &ReserveAccountSessionRESTResponse{
		ReservationID: resp.ReservationID, Timestamp: resp.Timestamp,
	})
}

// HandleReleaseAccountSession handles
// DELETE /api/v1/agent-accounts/{id}/reservations/{rid}.
func (s *Server) HandleReleaseAccountSession(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")
	reservationID := chi.URLParam(r, "rid")
	if accountID == "" || reservationID == "" {
		RespondBadRequest(w, r, "account id and reservation id are required")
		return
	}
	subject, ok := requireTenantSubject(w, r)
	if !ok {
		return
	}
	if !queryIdentityMatches(w, r, subject, s.lazyCallerAccount(w, r, subject)) {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}
	resp, err := accountSvc.ReleaseAccountSession(r.Context(), &service.ReleaseAccountSessionRequest{
		TenantID: subject.TenantID, AccountID: accountID, ReservationID: reservationID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &ReleaseAccountSessionRESTResponse{
		ReservationID: resp.ReservationID, Timestamp: resp.Timestamp,
	})
}
