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

package iam

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const claimTestTenant = "tenant-claims"

// claimTestResolver hands every tenant its own qrdb in-memory store, which
// performs the guarded writes a claim needs. iamtest.MemoryStoreResolver is
// the same fixture for other packages; this package cannot import iamtest.
type claimTestResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
	wrap   func(qrdbsdk.KVStore) qrdbsdk.KVStore
	err    error
}

func newClaimTestResolver() *claimTestResolver {
	return &claimTestResolver{stores: make(map[string]qrdbsdk.KVStore)}
}

func (r *claimTestResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	if r.wrap != nil {
		return r.wrap(store), nil
	}
	return store, nil
}

// plainKVStore hides the guarded writes of the store it wraps, standing in for
// a tenant store that cannot arbitrate.
type plainKVStore struct {
	qrdbsdk.KVStore
}

type claimTestResolveError struct{}

func (claimTestResolveError) Error() string { return "tenant store unavailable" }

func newTestActivationClaims(t *testing.T) *UserActivationClaims {
	t.Helper()
	claims, err := NewUserActivationClaims(newClaimTestResolver())
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}
	return claims
}

func requireActivationInProgress(t *testing.T, err error) {
	t.Helper()
	var inProgress *UserActivationInProgressError
	if !errors.As(err, &inProgress) {
		t.Fatalf("want *UserActivationInProgressError, got %v", err)
	}
	if !IsUserNotPending(err) {
		t.Fatalf("an activation that lost the claim must read as user-not-pending, got %v", err)
	}
}

func TestNewUserActivationClaims_NilResolver(t *testing.T) {
	claims, err := NewUserActivationClaims(nil)
	if claims != nil {
		t.Fatal("want nil claims for a nil resolver")
	}
	var cfgErr *UserActivationClaimConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *UserActivationClaimConfigError, got %v", err)
	}
}

func TestUserActivationClaims_SecondClaimLoses(t *testing.T) {
	claims := newTestActivationClaims(t)
	ctx := context.Background()
	pendingSince := time.Now().UTC()

	if _, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	_, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	requireActivationInProgress(t, err)

	// The claim is per user: another user of the tenant is unaffected.
	if _, err := claims.Claim(ctx, claimTestTenant, 10002, pendingSince); err != nil {
		t.Fatalf("claim for a different user: %v", err)
	}
}

func TestUserActivationClaims_ReleaseAllowsReclaim(t *testing.T) {
	claims := newTestActivationClaims(t)
	ctx := context.Background()
	pendingSince := time.Now().UTC()

	claim, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := claim.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
}

func TestUserActivationClaims_CompletedClaimYieldsOnlyToLaterReset(t *testing.T) {
	claims := newTestActivationClaims(t)
	ctx := context.Background()
	pendingSince := time.Now().UTC()
	activatedAt := pendingSince.Add(time.Second)

	claim, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := claim.Complete(ctx, activatedAt); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// A record read before the activation, e.g. from a lagging replica, must
	// not reopen the user.
	_, err = claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	requireActivationInProgress(t, err)
	_, err = claims.Claim(ctx, claimTestTenant, 10001, activatedAt)
	requireActivationInProgress(t, err)

	// An administrator resetting the user to pending writes a later record.
	if _, err := claims.Claim(ctx, claimTestTenant, 10001, activatedAt.Add(time.Second)); err != nil {
		t.Fatalf("claim after reset to pending: %v", err)
	}
}

func TestUserActivationClaims_ExpiredLeaseIsTakenOver(t *testing.T) {
	resolver := newClaimTestResolver()
	claims, err := NewUserActivationClaims(resolver)
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}
	ctx := context.Background()
	pendingSince := time.Now().UTC()

	stale, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Age the stored lease, as if its holder died long ago.
	expired, err := json.Marshal(userActivationClaimRecord{Holder: stale.holder, ExpiresAt: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := stale.store.Put(ctx, stale.key, expired); err != nil {
		t.Fatalf("age lease: %v", err)
	}
	stale.value = expired

	if _, err := claims.Claim(ctx, claimTestTenant, 10001, pendingSince); err != nil {
		t.Fatalf("take over expired claim: %v", err)
	}

	// The old holder must not remove or complete the new holder's claim.
	var lost *UserActivationClaimLostError
	if err := stale.Release(ctx); !errors.As(err, &lost) {
		t.Fatalf("release by the old holder: want *UserActivationClaimLostError, got %v", err)
	}
	if err := stale.Complete(ctx, time.Now()); !errors.As(err, &lost) {
		t.Fatalf("complete by the old holder: want *UserActivationClaimLostError, got %v", err)
	}
	_, err = claims.Claim(ctx, claimTestTenant, 10001, pendingSince)
	requireActivationInProgress(t, err)
}

func TestUserActivationClaims_UnsupportedStoreFailsClosed(t *testing.T) {
	resolver := newClaimTestResolver()
	resolver.wrap = func(store qrdbsdk.KVStore) qrdbsdk.KVStore { return plainKVStore{KVStore: store} }
	claims, err := NewUserActivationClaims(resolver)
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}

	_, err = claims.Claim(context.Background(), claimTestTenant, 10001, time.Now())
	var unsupported *UserActivationClaimUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("want *UserActivationClaimUnsupportedError, got %v", err)
	}
}

func TestUserActivationClaims_ResolveErrorPropagates(t *testing.T) {
	resolver := newClaimTestResolver()
	resolver.err = claimTestResolveError{}
	claims, err := NewUserActivationClaims(resolver)
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}

	_, err = claims.Claim(context.Background(), claimTestTenant, 10001, time.Now())
	var claimErr *UserActivationClaimError
	if !errors.As(err, &claimErr) {
		t.Fatalf("want *UserActivationClaimError, got %v", err)
	}
	if !errors.As(err, new(claimTestResolveError)) {
		t.Fatalf("the resolve failure must be preserved, got %v", err)
	}
}

func TestUserActivationClaims_CorruptRecordRefuses(t *testing.T) {
	resolver := newClaimTestResolver()
	claims, err := NewUserActivationClaims(resolver)
	if err != nil {
		t.Fatalf("NewUserActivationClaims: %v", err)
	}
	ctx := context.Background()
	store, err := resolver.Resolve(ctx, claimTestTenant)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := store.Put(ctx, userActivationClaimPrefix+"10001", []byte("not json")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err = claims.Claim(ctx, claimTestTenant, 10001, time.Now())
	requireActivationInProgress(t, err)
}

func TestUserActivationClaimRecord_SupersededBy(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name         string
		record       userActivationClaimRecord
		pendingSince time.Time
		want         bool
	}{
		{"live lease", userActivationClaimRecord{ExpiresAt: now.Add(time.Minute)}, now.Add(time.Hour), false},
		{"expired lease", userActivationClaimRecord{ExpiresAt: now.Add(-time.Minute)}, time.Time{}, true},
		{"completed, stale read", userActivationClaimRecord{CompletedAt: now}, now.Add(-time.Second), false},
		{"completed, same record", userActivationClaimRecord{CompletedAt: now}, now, false},
		{"completed, reset later", userActivationClaimRecord{CompletedAt: now}, now.Add(time.Second), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.record.supersededBy(tc.pendingSince, now); got != tc.want {
				t.Fatalf("supersededBy = %v, want %v", got, tc.want)
			}
		})
	}
}
