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
	"testing"
	"time"
)

// newTestACPBackendWithAccountPool is newTestACPBackendForProvider for the
// claude backend, which is what most account tests exercise.
func newTestACPBackendWithAccountPool(t *testing.T) (backend *ACPBackend, store AccountStore, secretStore *AccountSecretStore, lastEnv func() []string, cleanup func()) {
	t.Helper()
	return newTestACPBackendForProvider(t, string(AccountProviderClaude))
}

// newTestACPBackendForProvider wires an ACPBackend for one provider's CLI to
// the mock JSON-RPC transport (same fixture as acp_backend_test.go) plus a real
// AccountResolver backed by in-memory stores, and returns a function that
// reports the env slice the most recent spawn's transport factory observed —
// this is how the tests below prove a resolved account credential actually
// reached the subprocess environment, not just the resolver's return value.
//
// command doubles as the AccountProvider: ACPBackend.accountProvider derives the
// provider from the command's basename, so "claude" pools Anthropic accounts and
// "codex" pools OpenAI ones.
func newTestACPBackendForProvider(t *testing.T, command string) (backend *ACPBackend, store AccountStore, secretStore *AccountSecretStore, lastEnv func() []string, cleanup func()) {
	t.Helper()

	factory, transportCleanup := mockTransportFactory(t)

	var mu sync.Mutex
	var captured []string
	wrapped := func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		mu.Lock()
		captured = append([]string(nil), env...)
		mu.Unlock()
		return factory(command, args, workDir, env)
	}

	backend = NewACPBackend(ACPBackendConfig{Command: command, Args: nil, DefaultModel: "default-model"})
	backend.transportFactory = wrapped

	store = NewInMemoryAccountStore()
	secretStore = newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(NewCostTracker(nil, nil))
	backend.SetAccountResolver(NewAccountResolver(store, tokens, strategies))

	lastEnv = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
	cleanup = transportCleanup
	return
}

func envContains(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

func TestACPBackend_Spawn_WithoutAccountFields_IsUnaffected(t *testing.T) {
	backend, _, _, lastEnv, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	if sess.AccountID != "" {
		t.Fatalf("Session.AccountID = %q, want empty when the spawn never asked for account selection", sess.AccountID)
	}
	if envContains(lastEnv(), "QUICBAAS_ACCOUNT_ID=") {
		t.Fatalf("account env vars leaked into a spawn that did not request account selection")
	}
}

func TestACPBackend_Spawn_PinnedAccount_InjectsEnv(t *testing.T) {
	backend, store, secretStore, lastEnv, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "system", Provider: AccountProviderClaude}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-1", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "secret-<account>",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	spawnCtx, cancel := context.WithTimeout(ContextWithTenant(ctx, "system"), 2*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	if sess.AccountID != "acct-1" || sess.AccountStrategy != "pinned" {
		t.Fatalf("Session = {AccountID:%s Strategy:%s}, want acct-1/pinned", sess.AccountID, sess.AccountStrategy)
	}
	if !envContains(lastEnv(), "CLAUDE_CODE_OAUTH_TOKEN=secret-<account>") {
		t.Fatalf("subprocess env = %v, want CLAUDE_CODE_OAUTH_TOKEN=secret-<account>", lastEnv())
	}
}

func TestACPBackend_Spawn_RevokedAccount_FailsClosed(t *testing.T) {
	backend, store, secretStore, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "system", Provider: AccountProviderClaude, Status: AccountStatusRevoked}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "system", "acct-1", &AccountCredential{AccessToken: "x"})

	spawnCtx, cancel := context.WithTimeout(ContextWithTenant(ctx, "system"), 2*time.Second)
	defer cancel()
	_, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-1"})
	var revoked *AccountRevokedError
	if !errors.As(err, &revoked) {
		t.Fatalf("Spawn() error = %v (%T), want *AccountRevokedError — a revoked account must never be handed to a subprocess", err, err)
	}
}

func TestACPBackend_Spawn_AccountRequestedButNoResolverConfigured_FailsClosed(t *testing.T) {
	factory, cleanup := mockTransportFactory(t)
	defer cleanup()

	backend := NewACPBackend(ACPBackendConfig{Command: "claude"})
	backend.transportFactory = factory
	// Deliberately no SetAccountResolver call.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-1"})
	var notConfigured *AccountResolverNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("Spawn() error = %v (%T), want *AccountResolverNotConfiguredError (fail closed, never silently fall back)", err, err)
	}
}

func TestACPBackend_Spawn_AccountAuto_LeastUsage(t *testing.T) {
	backend, store, secretStore, lastEnv, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()
	ctx := context.Background()

	busy := &Account{ID: "acct-busy", TenantID: "system", Provider: AccountProviderClaude}
	idle := &Account{ID: "acct-idle", TenantID: "system", Provider: AccountProviderClaude}
	for _, a := range []*Account{busy, idle} {
		if err := store.Create(ctx, a); err != nil {
			t.Fatalf("Create(%s) error = %v", a.ID, err)
		}
		if err := secretStore.Put(ctx, "system", a.ID, &AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-" + a.ID}); err != nil {
			t.Fatalf("Put(%s) error = %v", a.ID, err)
		}
	}
	tracker := NewCostTracker(newCostTestKVStore(), &costTestLogger{})
	if err := tracker.RecordTurnUsage(ctx, TurnUsage{TenantID: "system", AccountID: "acct-busy", Tokens: 100000}); err != nil {
		t.Fatalf("RecordTurnUsage() error = %v", err)
	}
	backend.SetAccountResolver(NewAccountResolver(store, NewAccountTokenManager(secretStore, store, 60*time.Second), NewAccountStrategyRegistry(tracker)))

	spawnCtx, cancel := context.WithTimeout(ContextWithTenant(ctx, "system"), 2*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{
		WorkDir: "/tmp", AccountAuto: true, AccountStrategy: LeastUsageAccountStrategyName,
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	if sess.AccountID != "acct-idle" {
		t.Fatalf("Spawn() selected %s, want acct-idle (lower usage)", sess.AccountID)
	}
	if !envContains(lastEnv(), "CLAUDE_CODE_OAUTH_TOKEN=tok-acct-idle") {
		t.Fatalf("subprocess env = %v, want the idle account's token", lastEnv())
	}
}
