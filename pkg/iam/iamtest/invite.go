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

// Package iamtest provides in-memory test doubles for the IAM seams.
//
// It lives in a dedicated test-support package because pkg/server and
// pkg/api/rest both need it; quicbaas keeps NO invite store implementation in
// production code: token generation, digesting, single-use
// redemption and tenant routing all belong to go-qrdb, and this repository
// previously kept a parallel copy of each. The double exists only so tests of
// the code AROUND invitations (PIV activation, first-boot) can run without a
// qrdb node.
//
// It reproduces the two properties callers depend on and nothing else:
// tenant scoping (a token issued for one tenant is absent from another, not
// found-then-rejected), and single-winner redemption.

package iamtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// MemoryInviteStore is an in-memory InviteStore for tests.
type MemoryInviteStore struct {
	mu sync.Mutex
	// byTenant maps tenantID -> token -> record. Keying by tenant FIRST is
	// what makes cross-tenant lookup return "not found" rather than a record
	// that then has to be rejected.
	byTenant map[string]map[string]*iam.InviteRecord
}

// compile-time proof the double tracks the real interface.
var _ iam.InviteStore = (*MemoryInviteStore)(nil)

// NewMemoryInviteStore constructs an empty in-memory invite store.
func NewMemoryInviteStore() *MemoryInviteStore {
	return &MemoryInviteStore{byTenant: make(map[string]map[string]*iam.InviteRecord)}
}

// Create issues an invitation and returns the plaintext token exactly once.
func (s *MemoryInviteStore) Create(
	_ context.Context,
	tenantID string,
	userID uint64,
	ttl time.Duration,
) (string, *iam.InviteRecord, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(buf)

	record := &iam.InviteRecord{
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byTenant[tenantID] == nil {
		s.byTenant[tenantID] = make(map[string]*iam.InviteRecord)
	}
	s.byTenant[tenantID][token] = record
	return token, record, nil
}

// Lookup resolves a token within a tenant without consuming it, and REFUSES a
// token that is spent or expired.
//
// Refusing rather than returning the record is what qrdb does: its
// DAOStore.Lookup runs checkUsable and answers ErrAlreadyRedeemed / ErrExpired,
// which translateInviteError maps onto iam.ErrInviteUsed / iam.ErrInviteExpired.
// This double used to return the spent record with Used=true and a nil error
// instead. That divergence was not cosmetic: it meant every handler test
// exercised the `if record.Used` branch, which the real store can never reach,
// while production took the error path and rendered a Forbidden QBError as
// "access denied: invite has already been used" — the confusing message in
// #272. A double that is wrong about which branch runs makes the tests agree
// with each other and disagree with the deployment.
func (s *MemoryInviteStore) Lookup(_ context.Context, tenantID, token string) (*iam.InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byTenant[tenantID][token]
	if !ok {
		return nil, iam.ErrInviteNotFound
	}
	if record.Used {
		return nil, iam.ErrInviteUsed
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		return nil, iam.ErrInviteExpired
	}
	copied := *record
	return &copied, nil
}

// Redeem consumes a token within a tenant exactly once. A second redemption —
// including a concurrent one, since the whole check-and-set is under the lock —
// gets iam.ErrInviteUsed.
func (s *MemoryInviteStore) Redeem(_ context.Context, tenantID, token string) (*iam.InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byTenant[tenantID][token]
	if !ok {
		return nil, iam.ErrInviteNotFound
	}
	if record.Used {
		return nil, iam.ErrInviteUsed
	}
	// qrdb's Redeem runs the same usability check as its Lookup, so an
	// expired token is refused here too rather than being consumable.
	if time.Now().UTC().After(record.ExpiresAt) {
		return nil, iam.ErrInviteExpired
	}
	record.Used = true
	copied := *record
	return &copied, nil
}
