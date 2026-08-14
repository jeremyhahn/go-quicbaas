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
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAccountResolver_PinnedAccount_Errored proves the pinned path and the
// strategy path agree about an errored account. The strategy path excludes it
// through Account.Selectable; naming it outright must not be a way to use a
// credential the pool has already judged broken.
func TestAccountResolver_PinnedAccount_Errored(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()

	account := &Account{
		ID:        "acct-1",
		TenantID:  "tenant-a",
		Provider:  AccountProviderClaude,
		Status:    AccountStatusError,
		LastError: "refresh rejected: invalid_grant",
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1",
		&AccountCredential{AccessToken: "x"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	var errored *AccountErroredError
	if !errors.As(err, &errored) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountErroredError", err, err)
	}
	if errored.AccountID != "acct-1" {
		t.Errorf("errored.AccountID = %q, want acct-1", errored.AccountID)
	}
	if !errors.Is(err, ErrAccountErrored) {
		t.Error("expected the error to unwrap to ErrAccountErrored")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("Error() = %q, want the recorded failure reason", err.Error())
	}

	// The same account, once an operator clears the status, resolves normally —
	// the check is on the status, not on a permanent mark.
	if touchErr := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		SetStatusValid: true, SetStatus: AccountStatusActive, ClearLastError: true,
	}); touchErr != nil {
		t.Fatalf("Touch() error = %v", touchErr)
	}
	if _, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	}); err != nil {
		t.Fatalf("Resolve() after recovery error = %v", err)
	}
}

// touchFailingAccountStore fails every Touch while behaving normally otherwise.
// It stands in for a store outage during the usage stamp.
type touchFailingAccountStore struct {
	AccountStore

	mu    sync.Mutex
	calls int
	err   error
}

func (s *touchFailingAccountStore) Touch(
	_ context.Context, _ string, _ string, _ TouchAccountOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.err
}

func (s *touchFailingAccountStore) touchCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestAccountResolver_TouchFailureIsReported proves a failed usage stamp is
// surfaced instead of discarded. The stamp is what LeastUsageAccountStrategy
// balances on: silently losing it leaves the account looking perpetually
// least-used, so every subsequent selection lands on it again.
func TestAccountResolver_TouchFailureIsReported(t *testing.T) {
	ctx := context.Background()
	base := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	store := &touchFailingAccountStore{
		AccountStore: base,
		err:          &AccountStoreError{Message: "kv unavailable"},
	}
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	logger := newRecordingLogger()
	resolver := NewAccountResolver(store, tokens,
		NewAccountStrategyRegistry(nil), WithStoreLogger(logger))

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := base.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1",
		&AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// The resolution still succeeds: the credential is already resolved and
	// refusing to hand it over would turn a bookkeeping fault into an outage.
	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" {
		t.Fatalf("Resolve() = %+v, want acct-1", resolved)
	}
	if store.touchCalls() == 0 {
		t.Fatal("Touch was never attempted")
	}
	warnings := logger.atLevel("WARN")
	if !strings.Contains(warnings, "acct-1") || !strings.Contains(warnings, "kv unavailable") {
		t.Fatalf("a failed usage stamp was discarded instead of reported; warnings = %q", warnings)
	}
}

// TestAccountResolver_TouchFailureWithoutLogger keeps the no-logger deployment
// working: the report is lost but the resolution is not.
func TestAccountResolver_TouchFailureWithoutLogger(t *testing.T) {
	ctx := context.Background()
	base := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	store := &touchFailingAccountStore{
		AccountStore: base,
		err:          &AccountStoreError{Message: "kv unavailable"},
	}
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	resolver := NewAccountResolver(store, tokens, NewAccountStrategyRegistry(nil))

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := base.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1",
		&AccountCredential{AccessToken: "tok"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}
