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

package service

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Session reservations — the remote half of the session cap
// =============================================================================
//
// A worker daemon selects its account itself (pkg/orchestration/fleet/account_pool.go), so
// the claim that makes MaxConcurrentSessions hold under concurrency has to be
// made against the control plane's table, not a table of the daemon's own: a
// cap enforced per host would let every host fill the account. These two verbs
// are how a daemon takes and gives back a slot in that one table
// (orchestration.AccountSessionReserver, go-quicbaas#1047).
//
// They are permissioned like the credential release (agent_accounts:
// materialize) because they are its companion: a reservation is taken
// immediately before the release it guards, by the same principal, and a
// principal that may release a credential is exactly the one that may claim the
// slot it will be used in.

// ReserveAccountSessionRequest asks for one session slot on an account.
type ReserveAccountSessionRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`

	// ExpectedLive, when set, makes the claim conditional on the account's
	// live-session count still being the one the caller selected on. A moved
	// count is refused with orchestration.AccountSessionCountChangedError and
	// the caller re-selects. See orchestration.SessionReservationRequest.
	ExpectedLive *int `json:"expected_live,omitempty"`
}

// ReserveAccountSessionResponse carries the reservation. The session record
// that names ReservationID satisfies it.
type ReserveAccountSessionResponse struct {
	ReservationID string    `json:"reservation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// ReleaseAccountSessionRequest gives a reservation back.
type ReleaseAccountSessionRequest struct {
	TenantID      string `json:"tenant_id"`
	AccountID     string `json:"account_id"`
	ReservationID string `json:"reservation_id"`
}

// ReleaseAccountSessionResponse acknowledges a release.
type ReleaseAccountSessionResponse struct {
	ReservationID string    `json:"reservation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// AccountSessionReserverNotConfiguredError indicates this AccountService was
// never wired with the reservation table (orchestration not brought up).
type AccountSessionReserverNotConfiguredError struct{}

// Error implements the error interface.
func (e *AccountSessionReserverNotConfiguredError) Error() string {
	return "account: session reservations not configured"
}

// ReserveAccountSession implements AccountService.
//
// A refusal because the account is at its cap, or because a conditional
// claim's count moved, is returned as the orchestration package's typed error
// unchanged, so every transport maps it to its own status and a daemon
// rebuilds the same typed error on its side. Every account is reserved,
// metered or not: occupancy is counted for balancing independently of whether
// a cap is enforced.
func (s *accountServiceImpl) ReserveAccountSession(ctx context.Context, req *ReserveAccountSessionRequest) (*ReserveAccountSessionResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	if s.sessions == nil {
		return nil, &AccountSessionReserverNotConfiguredError{}
	}
	ctx, err := targetTenantContext(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	// THE CAP IS READ FROM THE STORED RECORD, never from the caller: a daemon
	// could otherwise reserve against a cap of its own choosing.
	account, err := s.store.Get(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}
	claim := orchestration.SessionReservationRequest{Account: account}
	if req.ExpectedLive != nil {
		claim.ExpectLive, claim.ExpectedLive = true, *req.ExpectedLive
	}
	reservationID, err := s.sessions.Reserve(ctx, req.TenantID, claim)
	if err != nil {
		return nil, err
	}
	return &ReserveAccountSessionResponse{ReservationID: reservationID, Timestamp: time.Now().UTC()}, nil
}

// ReleaseAccountSession implements AccountService.
func (s *accountServiceImpl) ReleaseAccountSession(ctx context.Context, req *ReleaseAccountSessionRequest) (*ReleaseAccountSessionResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	if req.ReservationID == "" {
		return nil, &AccountInvalidError{Field: "reservation_id", Message: "reservation ID is required"}
	}
	if s.sessions == nil {
		return nil, &AccountSessionReserverNotConfiguredError{}
	}
	ctx, err := targetTenantContext(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Release(ctx, req.TenantID, req.AccountID, req.ReservationID); err != nil {
		return nil, err
	}
	return &ReleaseAccountSessionResponse{ReservationID: req.ReservationID, Timestamp: time.Now().UTC()}, nil
}
