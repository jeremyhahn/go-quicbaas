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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// End-to-end: an operator adds a real provider API key, then spawns.
//
// These tests walk the whole path a pooled account actually takes —
// write-time credential validation, the default-claim on create, strategy
// selection at spawn time, credential decryption, and projection into the
// subprocess environment — and assert on the ENVIRONMENT SLICE the transport
// factory received, not on the resolver's return value. That is the only place
// where "the credential reached the agent" is a fact rather than an intention.
//
// Nothing here touches the network or the host: the agent is the in-process
// mock JSON-RPC responder (mockTransportFactory), credentials live in an
// in-memory SecretsManager, and the OAuth token endpoint is an httptest server.
// =============================================================================

const e2eTenant = "tenant-e2e"

// importAccount performs exactly what service.AccountService.CreateAccount
// does, in the order it does it: validate the credential against the provider's
// effective descriptor, write the secret envelope, then write the metadata
// record. Going through the same two stores (rather than a hand-built Account)
// is what makes the default-claim below the real one and not a test fixture.
//
// pkg/service cannot be imported from an in-package orchestration test — it
// imports this package — so the sequence is restated here; TestAccountService_*
// in pkg/service asserts the service performs it identically.
func importAccount(t *testing.T, store AccountStore, secretStore *AccountSecretStore,
	account *Account, cred *AccountCredential) *Account {
	t.Helper()
	ctx := context.Background()

	if account.RefreshPolicy == "" {
		account.RefreshPolicy = AccountRefreshNone
	}
	if err := DefaultAccountProviderRegistry().ValidateCredential(account.Provider, account.RefreshPolicy, cred); err != nil {
		t.Fatalf("ValidateCredential(%s) error = %v — the credential would be rejected at write time", account.ID, err)
	}
	if err := secretStore.Put(ctx, account.TenantID, account.ID, cred); err != nil {
		t.Fatalf("secrets.Put(%s) error = %v", account.ID, err)
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("store.Create(%s) error = %v", account.ID, err)
	}
	return account
}

// spawnEnvValue returns the value of key in the env slice the last spawn's
// transport factory observed, and whether it was present at all.
func spawnEnvValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

// assertSpawnCredential asserts the spawn selected wantAccountID via
// wantStrategy and that wantEnvKey carried exactly wantToken into the
// subprocess — and that no OTHER account's token came along with it.
func assertSpawnCredential(t *testing.T, sess *Session, env []string,
	wantAccountID, wantStrategy, wantEnvKey, wantToken string) {
	t.Helper()

	if sess.AccountID != wantAccountID {
		t.Fatalf("Session.AccountID = %q, want %q", sess.AccountID, wantAccountID)
	}
	if sess.AccountStrategy != wantStrategy {
		t.Fatalf("Session.AccountStrategy = %q, want %q", sess.AccountStrategy, wantStrategy)
	}
	got, ok := spawnEnvValue(env, wantEnvKey)
	if !ok {
		t.Fatalf("subprocess env has no %s; the account's credential never reached the agent (env = %v)", wantEnvKey, env)
	}
	if got != wantToken {
		t.Fatalf("subprocess %s = %q, want %q", wantEnvKey, got, wantToken)
	}
	if id, _ := spawnEnvValue(env, accountEnvID); id != wantAccountID {
		t.Fatalf("subprocess %s = %q, want %q", accountEnvID, id, wantAccountID)
	}
}

// TestAccountPool_Anthropic_StaticToken_EndToEnd is the owner-facing scenario
// for `claude`: import an API-key-style account, spawn without naming one, and
// see that account's token inside the agent process.
func TestAccountPool_Anthropic_StaticToken_EndToEnd(t *testing.T) {
	backend, store, secretStore, lastEnv, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()

	const envKey = "CLAUDE_CODE_OAUTH_TOKEN"

	// 1. The first Anthropic account an operator adds becomes the default.
	//    Its credential omits access_token_env_key: the built-in claude
	//    descriptor supplies CLAUDE_CODE_OAUTH_TOKEN.
	primary := importAccount(t, store, secretStore,
		&Account{ID: "anthropic-primary", TenantID: e2eTenant, Provider: AccountProviderClaude,
			DisplayName: "team-max-1", RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessToken: "sk-ant-primary"})
	if !primary.Default {
		t.Fatalf("the first anthropic account is not the default; an unpinned spawn would have nothing to select")
	}

	// 2. A second account joins the pool without displacing the default.
	secondary := importAccount(t, store, secretStore,
		&Account{ID: "anthropic-secondary", TenantID: e2eTenant, Provider: AccountProviderClaude,
			DisplayName: "team-max-2", RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessToken: "sk-ant-secondary"})
	if secondary.Default {
		t.Fatalf("the second anthropic account displaced the default without being asked to")
	}

	ctx := ContextWithTenant(context.Background(), e2eTenant)

	// 3. A spawn that names NO account gets the default's credential.
	spawnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto) error = %v", err)
	}
	assertSpawnCredential(t, sess, lastEnv(), "anthropic-primary", StaticAccountStrategyName, envKey, "sk-ant-primary")
	if err := backend.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// 4. Switching the default is atomic: the incumbent loses the flag in the
	//    same operation the challenger gains it, so the pool is never briefly
	//    two-defaults or briefly none.
	if err := store.SetDefault(ctx, e2eTenant, "anthropic-secondary"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	if got := defaultsInScope(t, store, e2eTenant, AccountProviderClaude, ""); len(got) != 1 || got[0] != "anthropic-secondary" {
		t.Fatalf("defaults after the switch = %v, want exactly [anthropic-secondary]", got)
	}

	spawnCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	sess2, err := backend.Spawn(spawnCtx2, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto, after switch) error = %v", err)
	}
	assertSpawnCredential(t, sess2, lastEnv(), "anthropic-secondary", StaticAccountStrategyName, envKey, "sk-ant-secondary")
	if err := backend.Close(context.Background(), sess2.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// 5. An explicit pin beats the default.
	spawnCtx3, cancel3 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel3()
	sess3, err := backend.Spawn(spawnCtx3, &SpawnRequest{WorkDir: "/tmp", AccountID: "anthropic-primary"})
	if err != nil {
		t.Fatalf("Spawn(pinned) error = %v", err)
	}
	assertSpawnCredential(t, sess3, lastEnv(), "anthropic-primary", pinnedAccountStrategyName, envKey, "sk-ant-primary")
	if err := backend.Close(context.Background(), sess3.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestAccountPool_OpenAI_StaticToken_EndToEnd is the same scenario for `codex`.
//
// The built-in codex descriptor ships NO AccessTokenEnvKey — the codex CLI
// reads OAuth material from its own auth.json, so there is no honest
// equivalent of CLAUDE_CODE_OAUTH_TOKEN to ship — which means an operator
// pooling OpenAI API keys names the variable on the account. This test pins
// that requirement down so it stays a documented step rather than a surprise.
func TestAccountPool_OpenAI_StaticToken_EndToEnd(t *testing.T) {
	backend, store, secretStore, lastEnv, cleanup := newTestACPBackendForProvider(t, string(AccountProviderCodex))
	defer cleanup()

	const envKey = "OPENAI_API_KEY"

	if descriptor, ok := DefaultAccountProviderRegistry().Descriptor(AccountProviderCodex); !ok || descriptor.AccessTokenEnvKey != "" {
		t.Fatalf("codex descriptor AccessTokenEnvKey = %q, want empty — an OpenAI account must name its own env var", descriptor.AccessTokenEnvKey)
	}

	primary := importAccount(t, store, secretStore,
		&Account{ID: "openai-primary", TenantID: e2eTenant, Provider: AccountProviderCodex,
			DisplayName: "openai-prod", RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessTokenEnvKey: envKey, AccessToken: "sk-openai-primary"})
	if !primary.Default {
		t.Fatalf("the first openai account is not the default")
	}

	secondary := importAccount(t, store, secretStore,
		&Account{ID: "openai-secondary", TenantID: e2eTenant, Provider: AccountProviderCodex,
			DisplayName: "openai-staging", RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessTokenEnvKey: envKey, AccessToken: "sk-openai-secondary"})
	if secondary.Default {
		t.Fatalf("the second openai account displaced the default without being asked to")
	}

	ctx := ContextWithTenant(context.Background(), e2eTenant)

	spawnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto) error = %v", err)
	}
	assertSpawnCredential(t, sess, lastEnv(), "openai-primary", StaticAccountStrategyName, envKey, "sk-openai-primary")
	if err := backend.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Switching the default is atomic here too: one operation, one holder.
	if err := store.SetDefault(ctx, e2eTenant, "openai-secondary"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	if got := defaultsInScope(t, store, e2eTenant, AccountProviderCodex, ""); len(got) != 1 || got[0] != "openai-secondary" {
		t.Fatalf("defaults after the switch = %v, want exactly [openai-secondary]", got)
	}

	spawnCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	sess2, err := backend.Spawn(spawnCtx2, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto, after switch) error = %v", err)
	}
	assertSpawnCredential(t, sess2, lastEnv(), "openai-secondary", StaticAccountStrategyName, envKey, "sk-openai-secondary")
	if err := backend.Close(context.Background(), sess2.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// And an explicit pin still beats the (now switched) default.
	spawnCtx3, cancel3 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel3()
	sess3, err := backend.Spawn(spawnCtx3, &SpawnRequest{WorkDir: "/tmp", AccountID: "openai-primary"})
	if err != nil {
		t.Fatalf("Spawn(pinned) error = %v", err)
	}
	assertSpawnCredential(t, sess3, lastEnv(), "openai-primary", pinnedAccountStrategyName, envKey, "sk-openai-primary")
	if err := backend.Close(context.Background(), sess3.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestAccountPool_CrossProviderDefaults_DoNotInterfere proves the two
// providers' defaults are independent: adding an OpenAI account never changes
// what an unpinned Anthropic spawn resolves to.
func TestAccountPool_CrossProviderDefaults_DoNotInterfere(t *testing.T) {
	claudeBackend, store, secretStore, claudeEnv, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()

	// One backend per CLI, but one shared account pool — the deployment shape.
	codexBackend, codexEnv, codexCleanup := newBackendSharingAccountPool(t, string(AccountProviderCodex), store, secretStore)
	defer codexCleanup()

	importAccount(t, store, secretStore,
		&Account{ID: "anthropic-only", TenantID: e2eTenant, Provider: AccountProviderClaude,
			RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessToken: "sk-ant"})
	importAccount(t, store, secretStore,
		&Account{ID: "openai-only", TenantID: e2eTenant, Provider: AccountProviderCodex,
			RefreshPolicy: AccountCredentialModeStaticToken},
		&AccountCredential{AccessTokenEnvKey: "OPENAI_API_KEY", AccessToken: "sk-oai"})

	ctx := ContextWithTenant(context.Background(), e2eTenant)

	claudeCtx, cancelClaude := context.WithTimeout(ctx, 5*time.Second)
	defer cancelClaude()
	claudeSess, err := claudeBackend.Spawn(claudeCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(claude) error = %v", err)
	}
	assertSpawnCredential(t, claudeSess, claudeEnv(), "anthropic-only", StaticAccountStrategyName, "CLAUDE_CODE_OAUTH_TOKEN", "sk-ant")
	if _, leaked := spawnEnvValue(claudeEnv(), "OPENAI_API_KEY"); leaked {
		t.Fatalf("the OpenAI key leaked into a claude spawn")
	}
	if err := claudeBackend.Close(context.Background(), claudeSess.ID); err != nil {
		t.Fatalf("Close(claude) error = %v", err)
	}

	codexCtx, cancelCodex := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCodex()
	codexSess, err := codexBackend.Spawn(codexCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(codex) error = %v", err)
	}
	assertSpawnCredential(t, codexSess, codexEnv(), "openai-only", StaticAccountStrategyName, "OPENAI_API_KEY", "sk-oai")
	if _, leaked := spawnEnvValue(codexEnv(), "CLAUDE_CODE_OAUTH_TOKEN"); leaked {
		t.Fatalf("the Anthropic token leaked into a codex spawn")
	}
	if err := codexBackend.Close(context.Background(), codexSess.ID); err != nil {
		t.Fatalf("Close(codex) error = %v", err)
	}
}

// newBackendSharingAccountPool wires a second provider's backend onto an
// EXISTING account pool, which is how a real deployment runs: one store, one
// secret store, one resolver per backend.
func newBackendSharingAccountPool(t *testing.T, command string, store AccountStore, secretStore *AccountSecretStore) (
	backend *ACPBackend, lastEnv func() []string, cleanup func()) {
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

	backend = NewACPBackend(ACPBackendConfig{Command: command, DefaultModel: "default-model"})
	backend.transportFactory = wrapped
	backend.SetAccountResolver(NewAccountResolver(
		store,
		NewAccountTokenManager(secretStore, store, 0),
		NewAccountStrategyRegistry(NewCostTracker(nil, nil)),
	))

	lastEnv = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
	return backend, lastEnv, transportCleanup
}

// TestAccountPool_TwoUndesignatedAccounts_FailLoudly is the counter-case the
// default exists to prevent, kept so the auto-claim can never be "fixed" into a
// silent pick: a pool whose default was deleted and that holds more than one
// survivor refuses to guess.
func TestAccountPool_TwoUndesignatedAccounts_FailLoudly(t *testing.T) {
	backend, store, secretStore, _, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()
	ctx := ContextWithTenant(context.Background(), e2eTenant)

	for _, id := range []string{"a", "b", "c"} {
		importAccount(t, store, secretStore,
			&Account{ID: id, TenantID: e2eTenant, Provider: AccountProviderClaude,
				RefreshPolicy: AccountCredentialModeStaticToken},
			&AccountCredential{AccessToken: "tok-" + id})
	}
	// "a" claimed the default on create; deleting it leaves two undesignated.
	if err := store.Delete(ctx, e2eTenant, "a"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	spawnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err == nil {
		t.Fatalf("Spawn(auto) succeeded with two undesignated accounts; selection must never guess which login to bill")
	}
	var unavailable *NoAccountAvailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Spawn(auto) error = %v (%T), want *NoAccountAvailableError", err, err)
	}

	// Naming a default resolves the ambiguity without any other change.
	if err := store.SetDefault(ctx, e2eTenant, "c"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	spawnCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	sess, err := backend.Spawn(spawnCtx2, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto, after set-default) error = %v", err)
	}
	if err := backend.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if sess.AccountID != "c" {
		t.Fatalf("Session.AccountID = %q, want c", sess.AccountID)
	}
}

// =============================================================================
// oauth_refresh with operator-supplied endpoint + client id
// =============================================================================

// TestAccountPool_OAuthRefresh_EndToEnd proves the oauth_refresh mode is
// complete code, blocked only on values QuicBaaS deliberately does not ship.
//
// The account supplies token_endpoint and client_id itself — exactly the two
// fields BuiltinAccountProviders leaves empty for Anthropic and OpenAI, because
// neither vendor documents the OAuth application its CLI authenticates with.
// With an operator's real values in those fields, this is the production path:
// nothing else is stubbed, and the httptest server stands in only for the
// vendor's HTTPS endpoint.
func TestAccountPool_OAuthRefresh_EndToEnd(t *testing.T) {
	var (
		mu       sync.Mutex
		gotForm  url.Values
		callSeen int
	)
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		gotForm = r.PostForm
		callSeen++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "sk-ant-renewed",
			"refresh_token": "rt-rotated",
			"expires_in":    3600,
			"token_type":    "Bearer",
		}); err != nil {
			t.Errorf("token endpoint encode error = %v", err)
		}
	}))
	defer tokenEndpoint.Close()

	backend, store, secretStore, lastEnv, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()

	// The refresher is registered for every oauth-capable provider, exactly as
	// server bring-up does it.
	tokens := NewAccountTokenManager(secretStore, store, 0)
	registered := tokens.RegisterOAuthRefreshers(NewOAuth2TokenRefresher(OAuth2RefresherConfig{
		HTTPClient: tokenEndpoint.Client(),
	}))
	if len(registered) == 0 {
		t.Fatalf("RegisterOAuthRefreshers() registered nothing; no oauth-capable provider is declared")
	}
	backend.SetAccountResolver(NewAccountResolver(store, tokens, NewAccountStrategyRegistry(NewCostTracker(nil, nil))))

	// An access token already past its expiry: the spawn must renew before it
	// hands anything to the agent.
	importAccount(t, store, secretStore,
		&Account{ID: "anthropic-oauth", TenantID: e2eTenant, Provider: AccountProviderClaude,
			RefreshPolicy: AccountCredentialModeOAuthRefresh},
		&AccountCredential{
			AccessToken:   "sk-ant-stale",
			RefreshToken:  "rt-original",
			ExpiresAt:     time.Now().Add(-time.Minute),
			TokenEndpoint: tokenEndpoint.URL,
			ClientID:      "operator-supplied-client-id",
		})

	ctx := ContextWithTenant(context.Background(), e2eTenant)
	spawnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn(auto) error = %v", err)
	}
	defer func() {
		if closeErr := backend.Close(context.Background(), sess.ID); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()

	assertSpawnCredential(t, sess, lastEnv(), "anthropic-oauth", StaticAccountStrategyName,
		"CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-renewed")

	mu.Lock()
	form, calls := gotForm, callSeen
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls)
	}
	if got := form.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q, want refresh_token", got)
	}
	if got := form.Get("refresh_token"); got != "rt-original" {
		t.Fatalf("refresh_token = %q, want rt-original", got)
	}
	if got := form.Get("client_id"); got != "operator-supplied-client-id" {
		t.Fatalf("client_id = %q, want the operator-supplied value", got)
	}

	// The rotated refresh token must be durable: presenting rt-original again
	// would be replay, which an authorization server rejects.
	stored, err := secretStore.Get(context.Background(), e2eTenant, "anthropic-oauth")
	if err != nil {
		t.Fatalf("secrets.Get() error = %v", err)
	}
	if stored.RefreshToken != "rt-rotated" {
		t.Fatalf("stored refresh token = %q, want rt-rotated", stored.RefreshToken)
	}
	if stored.AccessToken != "sk-ant-renewed" {
		t.Fatalf("stored access token = %q, want sk-ant-renewed", stored.AccessToken)
	}

	// The renewal's expiry is mirrored onto the non-secret metadata record, so
	// an operator listing the pool sees the renewed credential's remaining life
	// rather than the stale "expired" the spawn just healed.
	record, err := store.Get(context.Background(), e2eTenant, "anthropic-oauth")
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if record.Status != AccountStatusActive {
		t.Fatalf("Status = %q, want active after a successful renewal", record.Status)
	}
	// expires_in was 3600s, so the mirrored timestamp must sit roughly an hour
	// out — replacing the expiry that was in the PAST when the spawn started.
	untilExpiry := time.Until(record.CredentialExpiresAt)
	if untilExpiry < 55*time.Minute || untilExpiry > time.Hour {
		t.Fatalf("CredentialExpiresAt = %v (%s from now), want ~1h from the renewal's expires_in",
			record.CredentialExpiresAt, untilExpiry)
	}
	// Under the 24h warning window a one-hour OAuth token reads as "expiring";
	// what matters is that it is no longer reported as expired.
	if got := record.CredentialHealth(); got != AccountCredentialHealthExpiring {
		t.Fatalf("CredentialHealth() = %q, want expiring for a freshly renewed 1h token", got)
	}
	if record.LastValidatedAt.IsZero() {
		t.Fatalf("LastValidatedAt is zero; a provider-honored renewal is proof of life")
	}
}

// TestAccountPool_OAuthRefresh_WithoutEndpoint_RejectedAtWriteTime is the
// tripwire's operational half: because the built-in Anthropic descriptor ships
// no token endpoint, an oauth_refresh account that supplies none is refused
// when it is created — never at 3am when a live session's token expires.
func TestAccountPool_OAuthRefresh_WithoutEndpoint_RejectedAtWriteTime(t *testing.T) {
	err := DefaultAccountProviderRegistry().ValidateCredential(
		AccountProviderClaude, AccountCredentialModeOAuthRefresh,
		&AccountCredential{AccessToken: "a", RefreshToken: "r"})
	var unresolved *AccountProviderEndpointUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("ValidateCredential() error = %v (%T), want *AccountProviderEndpointUnresolvedError", err, err)
	}
	if unresolved.Provider != AccountProviderClaude {
		t.Fatalf("error names provider %q, want claude", unresolved.Provider)
	}
}
