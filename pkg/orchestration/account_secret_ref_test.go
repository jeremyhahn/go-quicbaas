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

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// memorySecretResolver is an in-memory AccountSecretResolver keyed by
// tenant + reference, so a test can prove a reference resolves ONLY in the
// tenant the resolver is asked for.
type memorySecretResolver struct {
	mu      sync.Mutex
	secrets map[string][]byte
	// generations is each secret's version stamp, advanced on every put, as
	// a secrets store stamps the version it writes.
	generations map[string]int64
	reads       int
	failErr     error
}

func newMemorySecretResolver() *memorySecretResolver {
	return &memorySecretResolver{secrets: make(map[string][]byte), generations: make(map[string]int64)}
}

func (r *memorySecretResolver) put(tenantID, ref, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets[tenantID+"|"+ref] = []byte(value)
	r.generations[tenantID+"|"+ref]++
}

func (r *memorySecretResolver) ResolveAccountSecret(_ context.Context, tenantID, ref string) (*AccountSecretValue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if r.failErr != nil {
		return nil, r.failErr
	}
	value, ok := r.secrets[tenantID+"|"+ref]
	if !ok {
		return nil, qerrors.New(qerrors.ErrNotFound, "secrets.GetSecret", "secret not found")
	}
	return &AccountSecretValue{Data: value, Generation: r.generations[tenantID+"|"+ref]}, nil
}

// refAccountFixture creates a reference-sourced account in an in-memory store
// and returns a token manager wired to resolver. The manager's inline secret
// store has NO backing manager, so any read of the inline envelope fails —
// which is how these tests prove a reference account never touches it.
func refAccountFixture(t *testing.T, resolver AccountSecretResolver, provider AccountProvider, ref, file string) (*AccountTokenManager, AccountStore, *Account) {
	t.Helper()
	store := NewInMemoryAccountStore()
	account := &Account{
		ID:                   "acct-ref",
		TenantID:             "tenant-a",
		Provider:             provider,
		Status:               AccountStatusActive,
		RefreshPolicy:        AccountCredentialModeStaticToken,
		CredentialSecretRef:  ref,
		CredentialSecretFile: file,
	}
	if err := store.Create(context.Background(), account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	var opts []AccountTokenManagerOption
	if resolver != nil {
		opts = append(opts, WithAccountSecretResolver(resolver))
	}
	manager := NewAccountTokenManager(NewAccountSecretStore(nil), store, 0, opts...)
	return manager, store, account
}

// mustGetRefAccount reads the fixture account back from store.
func mustGetRefAccount(t *testing.T, store AccountStore, accountID string) *Account {
	t.Helper()
	account, err := store.Get(context.Background(), "tenant-a", accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	return account
}

func TestAccountTokenManager_ResolvesSecretRefAsAccessToken(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "sk-fake-<account>\n")
	manager, _, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	cred, err := manager.Resolve(context.Background(), account)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.AccessToken != "sk-fake-<account>" {
		t.Errorf("AccessToken = %q, want the trimmed secret value", cred.AccessToken)
	}
	if cred.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("AccessTokenEnvKey = %q, want the provider default", cred.AccessTokenEnvKey)
	}
	if cred.RefreshToken != "" || len(cred.Files) != 0 {
		t.Errorf("unexpected credential material: %+v", cred)
	}
}

func TestAccountTokenManager_ResolvesSecretRefAsFile(t *testing.T) {
	resolver := newMemorySecretResolver()
	body := "{\"tokens\":{\"access_token\":\"fake\"}}\n"
	resolver.put("tenant-a", "agents/codex/reviewer/auth_json", body)
	manager, _, account := refAccountFixture(t, resolver, AccountProviderCodex, "agents/codex/reviewer/auth_json", ".codex/auth.json")

	cred, err := manager.Resolve(context.Background(), account)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := string(cred.Files[".codex/auth.json"]); got != body {
		t.Errorf("file content = %q, want the secret verbatim", got)
	}
	if cred.AccessToken != "" {
		t.Errorf("a file-projected secret must not also become the access token: %q", cred.AccessToken)
	}
}

// TestAccountTokenManager_SecretWriteRotatesNextResolve is the rotation
// property: writing the secret changes what the NEXT resolution hands out,
// with no account write in between.
func TestAccountTokenManager_SecretWriteRotatesNextResolve(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "sk-fake-old")
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")
	ctx := context.Background()

	first, err := manager.Resolve(ctx, account)
	if err != nil || first.AccessToken != "sk-fake-old" {
		t.Fatalf("first Resolve = %+v, %v", first, err)
	}
	before, err := store.Get(ctx, "tenant-a", account.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "sk-fake-new")

	second, err := manager.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if second.AccessToken != "sk-fake-new" {
		t.Errorf("AccessToken = %q, want the rotated secret", second.AccessToken)
	}
	after, err := store.Get(ctx, "tenant-a", account.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Error("rotation by secret write must not require an account write")
	}
}

// TestAccountTokenManager_SecretRefResolvesOnlyInOwningTenant proves the
// resolver is asked for the ACCOUNT's tenant: a same-named secret in another
// tenant is invisible.
func TestAccountTokenManager_SecretRefResolvesOnlyInOwningTenant(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.put("tenant-b", "agents/claude/claude1/oauth_token", "sk-fake-other-tenant")
	manager, _, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	_, err := manager.Resolve(context.Background(), account)
	var unresolved *AccountSecretRefUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("Resolve error = %v, want AccountSecretRefUnresolvedError", err)
	}
	if unresolved.TenantID != "tenant-a" {
		t.Errorf("resolved in tenant %q, want the account's own", unresolved.TenantID)
	}
}

// TestAccountTokenManager_MissingSecretMarksAccountErrored proves a missing
// secret is a typed error the spawn path parks on AND an account_error an
// operator sees, rather than a silent spawn failure.
func TestAccountTokenManager_MissingSecretMarksAccountErrored(t *testing.T) {
	resolver := newMemorySecretResolver()
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")
	ctx := context.Background()

	_, err := manager.Resolve(ctx, account)
	if !errors.Is(err, ErrAccountCredentialNotFound) {
		t.Fatalf("Resolve error = %v, want it to unwrap to ErrAccountCredentialNotFound", err)
	}
	var unresolved *AccountSecretRefUnresolvedError
	if !errors.As(err, &unresolved) || unresolved.Reason != "the secret does not exist in this tenant" {
		t.Fatalf("Resolve error = %v, want a not-found reason", err)
	}

	stored, getErr := store.Get(ctx, "tenant-a", account.ID)
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if stored.Status != AccountStatusError {
		t.Errorf("status = %s, want error", stored.Status)
	}
	if !strings.Contains(stored.LastError, "agents/claude/claude1/oauth_token") {
		t.Errorf("last_error = %q, want it to name the reference", stored.LastError)
	}
	if stored.Selectable(stored.UpdatedAt) {
		t.Error("an account whose secret is missing must drop out of selection")
	}
}

func TestAccountTokenManager_EmptySecretIsUnresolved(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.put("tenant-a", "agents/claude/claude1/oauth_token", "  \n")
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	_, err := manager.Resolve(context.Background(), account)
	var unresolved *AccountSecretRefUnresolvedError
	if !errors.As(err, &unresolved) || unresolved.Reason != "the secret is empty" {
		t.Fatalf("Resolve error = %v, want an empty-secret reason", err)
	}
	stored := mustGetRefAccount(t, store, account.ID)
	if stored.Status != AccountStatusError {
		t.Errorf("status = %s, want error", stored.Status)
	}
}

// TestAccountTokenManager_UnreadableSecretNeverLeaksCause proves the recorded
// last_error names the failure class, not the resolver's error text.
func TestAccountTokenManager_UnreadableSecretNeverLeaksCause(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.failErr = errors.New("backend said sk-fake-leak")
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	if _, err := manager.Resolve(context.Background(), account); err == nil {
		t.Fatal("Resolve succeeded against a failing resolver")
	}
	stored := mustGetRefAccount(t, store, account.ID)
	if strings.Contains(stored.LastError, "sk-fake-leak") {
		t.Errorf("last_error carries the resolver's error text: %q", stored.LastError)
	}
	if !strings.Contains(stored.LastError, "could not be read") {
		t.Errorf("last_error = %q, want the unreadable class", stored.LastError)
	}
}

func TestAccountTokenManager_SecretRefWithoutResolverFailsClosed(t *testing.T) {
	manager, store, account := refAccountFixture(t, nil, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	_, err := manager.Resolve(context.Background(), account)
	var notConfigured *AccountSecretResolverNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("Resolve error = %v, want AccountSecretResolverNotConfiguredError", err)
	}
	stored := mustGetRefAccount(t, store, account.ID)
	if stored.Status != AccountStatusActive {
		t.Errorf("a wiring fault must not mark the account: status = %s", stored.Status)
	}
}

func TestAccountTokenManager_CanceledContextMarksNothing(t *testing.T) {
	resolver := newMemorySecretResolver()
	resolver.failErr = context.Canceled
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, "agents/claude/claude1/oauth_token", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Resolve(ctx, account); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
	stored := mustGetRefAccount(t, store, account.ID)
	if stored.Status != AccountStatusActive {
		t.Errorf("the caller's own cancellation must not mark the account: status = %s", stored.Status)
	}
}

// TestSecretRefUnresolved_ParksAsMissingCredential proves the spawn path's
// classifier parks an unresolvable reference uncharged, as a missing
// credential, and that the remote-spawn wire carries the same class.
func TestSecretRefUnresolved_ParksAsMissingCredential(t *testing.T) {
	err := &AccountSecretRefUnresolvedError{TenantID: "t", AccountID: "a", Ref: "agents/x", Reason: "the secret is empty"}
	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAccountCredentialMissing {
		t.Fatalf("classifyEnvironmentFault = %+v, %v; want %s", fault, ok, FaultAccountCredentialMissing)
	}
	if got := spawnFaultClassOf(err); got != spawnFaultCredentialNotFound {
		t.Errorf("spawnFaultClassOf = %q, want %q", got, spawnFaultCredentialNotFound)
	}
}

func TestValidateAccountCredentialSource(t *testing.T) {
	registry := DefaultAccountProviderRegistry()
	cases := []struct {
		name      string
		provider  AccountProvider
		mode      AccountCredentialMode
		hasInline bool
		ref       string
		file      string
		wantField string // empty: valid
	}{
		{"inline only", AccountProviderClaude, AccountRefreshNone, true, "", "", ""},
		{"ref only, env provider", AccountProviderClaude, AccountRefreshNone, false, "agents/claude/c1/oauth_token", "", ""},
		{"ref with file", AccountProviderCodex, AccountRefreshNone, false, "agents/codex/r/auth_json", ".codex/auth.json", ""},
		{"both", AccountProviderClaude, AccountRefreshNone, true, "agents/x", "", "credential_secret_ref"},
		{"neither", AccountProviderClaude, AccountRefreshNone, false, "", "", "credential"},
		{"file without ref", AccountProviderClaude, AccountRefreshNone, true, "", ".codex/auth.json", "credential_secret_file"},
		{"ref on oauth account", AccountProviderClaude, AccountRefreshOAuth2, false, "agents/x", "", "refresh_policy"},
		{"ref for provider with no env key", AccountProviderCodex, AccountRefreshNone, false, "agents/x", "", "credential_secret_file"},
		{"ref for unknown provider", AccountProvider("unknown"), AccountRefreshNone, false, "agents/x", "", "credential_secret_file"},
		{"tenant-prefixed ref", AccountProviderClaude, AccountRefreshNone, false, "tenant/t1/agents/x", "", "credential_secret_ref"},
		{"empty segment", AccountProviderClaude, AccountRefreshNone, false, "agents//x", "", "credential_secret_ref"},
		{"leading slash", AccountProviderClaude, AccountRefreshNone, false, "/agents/x", "", "credential_secret_ref"},
		{"dot-dot segment", AccountProviderClaude, AccountRefreshNone, false, "agents/../x", "", "credential_secret_ref"},
		{"whitespace", AccountProviderClaude, AccountRefreshNone, false, "agents/x y", "", "credential_secret_ref"},
		{"absolute file", AccountProviderCodex, AccountRefreshNone, false, "agents/x", "/root/.codex/auth.json", "credential_secret_file"},
		{"escaping file", AccountProviderCodex, AccountRefreshNone, false, "agents/x", "../auth.json", "credential_secret_file"},
		{"unclean file", AccountProviderCodex, AccountRefreshNone, false, "agents/x", ".codex//auth.json", "credential_secret_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.ValidateAccountCredentialSource(tc.provider, tc.mode, tc.hasInline, tc.ref, tc.file)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var sourceErr *AccountCredentialSourceError
			if !errors.As(err, &sourceErr) {
				t.Fatalf("error = %v, want AccountCredentialSourceError", err)
			}
			if sourceErr.Field != tc.wantField {
				t.Errorf("field = %q, want %q", sourceErr.Field, tc.wantField)
			}
			if !errors.Is(err, ErrAccountInvalid) {
				t.Error("a credential source error must be a validation error")
			}
		})
	}
}

func TestValidateAccountCredentialSource_UnrecognizedMode(t *testing.T) {
	err := DefaultAccountProviderRegistry().ValidateAccountCredentialSource(
		AccountProviderClaude, AccountCredentialMode("bogus"), false, "agents/x", "")
	var modeErr *AccountCredentialModeInvalidError
	if !errors.As(err, &modeErr) {
		t.Fatalf("error = %v, want AccountCredentialModeInvalidError", err)
	}
}
