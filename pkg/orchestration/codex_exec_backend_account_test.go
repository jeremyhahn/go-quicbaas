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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestCodexExecBackendWithAccountPool builds a CodexExecBackend over a real
// resolver, store and secret store, the way newTestACPBackendForProvider does.
// The command is a path, as it often is in a deployment.
func newTestCodexExecBackendWithAccountPool(t *testing.T) (*CodexExecBackend, AccountStore, *AccountSecretStore) {
	t.Helper()
	backend := NewCodexExecBackend(CodexExecBackendConfig{Command: filepath.Join(t.TempDir(), "codex")})
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(NewCostTracker(nil, nil))
	backend.SetAccountResolver(NewAccountResolver(store, tokens, strategies))
	return backend, store, secretStore
}

func codexTestSpawnContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), "system"), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func codexSessionEnv(t *testing.T, b *CodexExecBackend, id string) []string {
	t.Helper()
	sess, ok := b.loadSession(id)
	if !ok {
		t.Fatalf("session %s not registered", id)
	}
	return sess.env
}

func TestCodexExecBackend_Spawn_WithoutAccountFields_IsUnaffected(t *testing.T) {
	// No resolver at all: a spawn naming no account never consults one.
	backend := NewCodexExecBackend(CodexExecBackendConfig{Command: "codex"})
	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)
	if sess.AccountID != "" || sess.AccountStrategy != "" || sess.CredentialLeaseID != "" {
		t.Fatalf("Session account fields = %+v, want empty", sess)
	}
	if envValue(codexSessionEnv(t, backend, sess.ID), accountEnvID) != "" {
		t.Fatalf("session env carries an account id without an account request")
	}
}

func TestCodexExecBackend_Spawn_AccountRequestedButNoResolver_FailsClosed(t *testing.T) {
	backend := NewCodexExecBackend(CodexExecBackendConfig{Command: "codex"})
	_, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir(), AccountAuto: true})
	var notConfigured *AccountResolverNotConfiguredError
	if !errors.As(err, &notConfigured) || notConfigured.Backend != "codex" {
		t.Fatalf("Spawn() error = %v (%T), want *AccountResolverNotConfiguredError for codex", err, err)
	}
}

func TestCodexExecBackend_Spawn_PinnedEnvAccount_InjectsEnv(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-cx", TenantID: "system", Provider: AccountProviderCodex}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-cx", &AccountCredential{
		AccessTokenEnvKey: "OPENAI_API_KEY", AccessToken: "sk-test",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	sess, err := backend.Spawn(codexTestSpawnContext(t), &SpawnRequest{WorkDir: t.TempDir(), AccountID: "acct-cx"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	if sess.AccountID != "acct-cx" || sess.AccountStrategy != "pinned" {
		t.Fatalf("Session = {AccountID:%s Strategy:%s}, want acct-cx/pinned", sess.AccountID, sess.AccountStrategy)
	}
	env := codexSessionEnv(t, backend, sess.ID)
	if !envContains(env, "OPENAI_API_KEY=sk-test") {
		t.Fatalf("session env = %v, want OPENAI_API_KEY=sk-test", env)
	}
	if !envContains(env, accountEnvID+"=acct-cx") {
		t.Fatalf("session env = %v, want %s=acct-cx", env, accountEnvID)
	}
}

func TestCodexExecBackend_Spawn_FileAccount_SetsCodexHome(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-file", TenantID: "system", Provider: AccountProviderCodex}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-file", &AccountCredential{
		Files: map[string][]byte{".codex/auth.json": []byte(`{"tokens":"test"}`)},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	sess, err := backend.Spawn(codexTestSpawnContext(t), &SpawnRequest{WorkDir: t.TempDir(), AccountID: "acct-file"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	codexHome := envValue(codexSessionEnv(t, backend, sess.ID), codexAccountHomeEnv)
	if codexHome == "" {
		t.Fatalf("session env has no %s", codexAccountHomeEnv)
	}
	if _, statErr := os.Stat(filepath.Join(codexHome, "auth.json")); statErr != nil {
		t.Fatalf("%s=%s holds no auth.json: %v", codexAccountHomeEnv, codexHome, statErr)
	}

	if err := backend.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, statErr := os.Stat(codexHome); !os.IsNotExist(statErr) {
		t.Fatalf("credential directory %s survives Close (stat err = %v)", codexHome, statErr)
	}
}

func TestCodexExecBackend_Spawn_FileAccountOutsideCodexDir_IsCredentialDirMissing(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-root", TenantID: "system", Provider: AccountProviderCodex}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Keyed at the ROOT rather than under ".codex/": codex would never find it.
	if err := secretStore.Put(ctx, "system", "acct-root", &AccountCredential{
		Files: map[string][]byte{"auth.json": []byte(`{"tokens":"test"}`)},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	req := &SpawnRequest{WorkDir: t.TempDir(), AccountID: "acct-root"}
	_, err := backend.Spawn(codexTestSpawnContext(t), req)
	var missing *CredentialDirMissingError
	if !errors.As(err, &missing) || missing.Rel != codexAccountHomeRel {
		t.Fatalf("Spawn() error = %v (%T), want *CredentialDirMissingError for %s", err, err, codexAccountHomeRel)
	}
	if req.AccountHomeDir != "" {
		if _, statErr := os.Stat(req.AccountHomeDir); !os.IsNotExist(statErr) {
			t.Fatalf("refused spawn left its credential directory %s behind", req.AccountHomeDir)
		}
	}
}

func TestCodexExecBackend_Spawn_AutoWithNoCodexAccount_IsNoAccountAvailable(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-claude", TenantID: "system", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-claude", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "sk-test",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	_, err := backend.Spawn(codexTestSpawnContext(t), &SpawnRequest{WorkDir: t.TempDir(), AccountAuto: true})
	if !errors.Is(err, ErrNoAccountAvailable) {
		t.Fatalf("Spawn() error = %v (%T), want ErrNoAccountAvailable", err, err)
	}
}

func TestCodexExecBackend_Spawn_AccountCannotOverrideTheSpawnsEnvironment(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-cx", TenantID: "system", Provider: AccountProviderCodex}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-cx", &AccountCredential{
		AccessTokenEnvKey: "OPENAI_API_KEY", AccessToken: "sk-test",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	req := &SpawnRequest{WorkDir: t.TempDir(), AccountAuto: true, Env: map[string]string{"OPENAI_API_KEY": "sk-other"}}
	_, err := backend.Spawn(codexTestSpawnContext(t), req)
	var conflict *AccountEnvConflictError
	if !errors.As(err, &conflict) || conflict.Key != "OPENAI_API_KEY" {
		t.Fatalf("Spawn() error = %v (%T), want *AccountEnvConflictError naming OPENAI_API_KEY", err, err)
	}
	if req.Env["OPENAI_API_KEY"] != "sk-other" {
		t.Fatalf("the refused spawn's environment was rewritten to %q", req.Env["OPENAI_API_KEY"])
	}
}

func TestCodexExecBackend_Spawn_StatedCodexHomeConflictsWithFileAccount(t *testing.T) {
	backend, store, secretStore := newTestCodexExecBackendWithAccountPool(t)
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-file", TenantID: "system", Provider: AccountProviderCodex}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-file", &AccountCredential{
		Files: map[string][]byte{".codex/auth.json": []byte(`{"tokens":"test"}`)},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	stated := t.TempDir()
	req := &SpawnRequest{WorkDir: t.TempDir(), AccountID: "acct-file", Env: map[string]string{codexAccountHomeEnv: stated}}
	_, err := backend.Spawn(codexTestSpawnContext(t), req)
	var conflict *AccountEnvConflictError
	if !errors.As(err, &conflict) || conflict.Key != codexAccountHomeEnv {
		t.Fatalf("Spawn() error = %v (%T), want *AccountEnvConflictError naming %s", err, err, codexAccountHomeEnv)
	}
	if req.Env[codexAccountHomeEnv] != stated {
		t.Fatalf("the refused spawn's %s was rewritten to %q", codexAccountHomeEnv, req.Env[codexAccountHomeEnv])
	}
}
