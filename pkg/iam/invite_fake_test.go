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

// invite_fake_test.go provides the in-memory InviteStore test double.
//
// It lives in a _test file deliberately. quicbaas keeps NO invite store
// implementation in production code: token generation, digesting, single-use
// redemption and tenant routing all belong to go-qrdb, and this repository
// previously kept a parallel copy of each. The double exists only so tests of
// the code AROUND invitations (PIV activation, first-boot) can run without a
// qrdb node.
//
// It reproduces the two properties callers depend on and nothing else:
// tenant scoping (a token issued for one tenant is absent from another, not
// found-then-rejected), and single-winner redemption.

package iam

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// fakeInviteStore is an in-memory InviteStore for tests.
type fakeInviteStore struct {
	mu sync.Mutex
	// byTenant maps tenantID -> token -> record. Keying by tenant FIRST is
	// what makes cross-tenant lookup return "not found" rather than a record
	// that then has to be rejected.
	byTenant map[string]map[string]*InviteRecord
}

// compile-time proof the double tracks the real interface.
var _ InviteStore = (*fakeInviteStore)(nil)

// newFakeInviteStore constructs an empty in-memory invite store.
func newFakeInviteStore() *fakeInviteStore {
	return &fakeInviteStore{byTenant: make(map[string]map[string]*InviteRecord)}
}

// Create issues an invitation and returns the plaintext token exactly once.
func (s *fakeInviteStore) Create(
	_ context.Context,
	tenantID, userID string,
	ttl time.Duration,
) (string, *InviteRecord, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(buf)

	record := &InviteRecord{
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byTenant[tenantID] == nil {
		s.byTenant[tenantID] = make(map[string]*InviteRecord)
	}
	s.byTenant[tenantID][token] = record
	return token, record, nil
}

// Lookup resolves a token within a tenant without consuming it.
func (s *fakeInviteStore) Lookup(_ context.Context, tenantID, token string) (*InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byTenant[tenantID][token]
	if !ok {
		return nil, ErrInviteNotFound
	}
	copied := *record
	return &copied, nil
}

// Redeem consumes a token within a tenant exactly once. A second redemption —
// including a concurrent one, since the whole check-and-set is under the lock —
// gets ErrInviteUsed.
func (s *fakeInviteStore) Redeem(_ context.Context, tenantID, token string) (*InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byTenant[tenantID][token]
	if !ok {
		return nil, ErrInviteNotFound
	}
	if record.Used {
		return nil, ErrInviteUsed
	}
	record.Used = true
	copied := *record
	return &copied, nil
}
