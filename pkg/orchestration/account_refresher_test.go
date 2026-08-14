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

package orchestration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingRefresher records how many times Refresh was called and returns a
// fresh, monotonically-numbered access token each time, so a test can prove
// single-flight collapses concurrent callers into one HTTP round trip.
type countingRefresher struct {
	calls atomic.Int64
	delay time.Duration
}

func (r *countingRefresher) Refresh(_ context.Context, cred *AccountCredential) (*AccountCredential, error) {
	n := r.calls.Add(1)
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	renewed := *cred
	renewed.AccessToken = "renewed-token"
	renewed.RefreshToken = "renewed-refresh"
	renewed.ExpiresAt = time.Now().Add(time.Hour)
	_ = n
	return &renewed, nil
}

type failingRefresher struct{ err error }

func (r *failingRefresher) Refresh(_ context.Context, _ *AccountCredential) (*AccountCredential, error) {
	return nil, r.err
}

func setupTokenManager(t *testing.T) (*AccountTokenManager, AccountStore, *AccountSecretStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	mgr := NewAccountTokenManager(secretStore, store, 60*time.Second)
	return mgr, store, secretStore
}

func TestAccountTokenManager_Resolve_StaticCredentialNeverRefreshed(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderCodex, RefreshPolicy: AccountRefreshNone}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{Env: map[string]string{"CODEX_API_KEY": "static-key"}}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.Env["CODEX_API_KEY"] != "static-key" {
		t.Fatalf("Resolve() = %+v, want the static key untouched", cred)
	}
}

func TestAccountTokenManager_Resolve_RefreshesExpiredCredential(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := &countingRefresher{}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute), // already expired
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.AccessToken != "renewed-token" {
		t.Fatalf("Resolve() AccessToken = %s, want renewed-token", cred.AccessToken)
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresher called %d times, want 1", refresher.calls.Load())
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.LastRefreshedAt.IsZero() {
		t.Fatalf("account.LastRefreshedAt not updated after a successful refresh")
	}
}

// TestAccountTokenManager_SingleFlight_PerAccount proves five concurrent
// Resolve calls for the SAME account collapse into exactly one Refresh call,
// and the presented refresh_token is never replayed against the (fake)
// authorization server.
func TestAccountTokenManager_SingleFlight_PerAccount(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := &countingRefresher{delay: 20 * time.Millisecond}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	const concurrency = 5
	var wg sync.WaitGroup
	results := make([]*AccountCredential, concurrency)
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = mgr.Resolve(ctx, account)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Resolve()[%d] error = %v", i, err)
		}
		if results[i].AccessToken != "renewed-token" {
			t.Fatalf("Resolve()[%d].AccessToken = %s, want renewed-token", i, results[i].AccessToken)
		}
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresher called %d times across 5 concurrent spawns, want exactly 1 (single-flight)", refresher.calls.Load())
	}
}

// TestAccountTokenManager_SingleFlight_DifferentAccounts proves two DIFFERENT
// accounts refresh in parallel rather than serializing behind one lock.
func TestAccountTokenManager_SingleFlight_DifferentAccounts(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := &countingRefresher{delay: 30 * time.Millisecond}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	for _, id := range []string{"acct-1", "acct-2"} {
		account := &Account{ID: id, TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
		if err := store.Create(ctx, account); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
		if err := secretStore.Put(ctx, "tenant-a", id, &AccountCredential{
			AccessToken: "old", RefreshToken: "rt-" + id, ExpiresAt: time.Now().Add(-time.Minute),
		}); err != nil {
			t.Fatalf("Put(%s) error = %v", id, err)
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	for _, id := range []string{"acct-1", "acct-2"} {
		wg.Add(1)
		go func(accountID string) {
			defer wg.Done()
			account, _ := store.Get(ctx, "tenant-a", accountID)
			if _, err := mgr.Resolve(ctx, account); err != nil {
				t.Errorf("Resolve(%s) error = %v", accountID, err)
			}
		}(id)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if refresher.calls.Load() != 2 {
		t.Fatalf("refresher called %d times for 2 distinct accounts, want 2", refresher.calls.Load())
	}
	// Serialized, this would take >= 2*delay; parallel should be close to 1*delay.
	if elapsed >= 2*refresher.delay {
		t.Fatalf("elapsed = %v, want well under %v — different accounts appear to have serialized", elapsed, 2*refresher.delay)
	}
}

func TestAccountTokenManager_Resolve_RefreshFailure_MarksAccountError(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	mgr.RegisterRefresher(AccountProviderClaude, &failingRefresher{err: errors.New("token endpoint unreachable")})

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := mgr.Resolve(ctx, account); err == nil {
		t.Fatalf("Resolve() error = nil, want a refresh failure")
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.Status != AccountStatusError {
		t.Fatalf("account.Status = %s, want error after a failed refresh", got.Status)
	}
	if got.LastError == "" {
		t.Fatalf("account.LastError is empty after a failed refresh")
	}
}

func TestAccountTokenManager_Resolve_NoRefresherRegistered(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	_, err := mgr.Resolve(ctx, account)
	var notConfigured *AccountRefresherNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRefresherNotConfiguredError", err, err)
	}
}
