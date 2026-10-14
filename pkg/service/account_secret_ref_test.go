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
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// These tests run the credential_secret_ref path end to end below the
// transports: a REAL SecretsService over the same context-routed KV the
// account store uses, the account service, and the spawn-path resolver — so
// the tenant a secret is written in, the tenant an account is created for, and
// the tenant a spawn resolves under all have to agree, as on a deployed node.
//
// Every token value here is fake.

const (
	refName  = "agents/claude/claude1/oauth_token"
	refValue = "sk-fake-oat-claude1"
)

// secretRefFixture is the account service, the tenant secrets service it
// resolves references through, and the spawn-path resolver, over one
// context-routed KV.
type secretRefFixture struct {
	kv       *tenantNamespacedKV
	secrets  SecretsService
	inline   *orchestration.AccountSecretStore
	service  AccountService
	resolver orchestration.AccountResolver
}

func newSecretRefFixture(t *testing.T) *secretRefFixture {
	t.Helper()
	return newSecretRefFixtureWrapping(t, nil)
}

// newSecretRefFixtureWrapping builds the fixture with its account store passed
// through wrap, so a test can interpose on store calls (nil wraps nothing).
func newSecretRefFixtureWrapping(t *testing.T, wrap func(orchestration.AccountStore) orchestration.AccountStore) *secretRefFixture {
	t.Helper()
	kv := newTenantNamespacedKV()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewKVBackend(kv)})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	secretsSvc := newSecretsService(mgr, nil)
	refs := NewAccountSecretResolver(secretsSvc)

	store := orchestration.NewAccountStore(kv)
	if wrap != nil {
		store = wrap(store)
	}
	inline := orchestration.NewAccountSecretStore(mgr)
	tokens := orchestration.NewAccountTokenManager(inline, store, 0, orchestration.WithAccountSecretResolver(refs))

	svc := NewAccountService(store, inline)
	wirer, ok := svc.(interface {
		SetSecretResolver(orchestration.AccountSecretResolver)
		SetTokenManager(*orchestration.AccountTokenManager)
	})
	if !ok {
		t.Fatal("account service does not accept a secret resolver and token manager")
	}
	wirer.SetSecretResolver(refs)
	wirer.SetTokenManager(tokens)

	return &secretRefFixture{
		kv:       kv,
		secrets:  secretsSvc,
		inline:   inline,
		service:  svc,
		resolver: orchestration.NewAccountResolver(store, tokens, orchestration.NewAccountStrategyRegistry(nil)),
	}
}

// putTenantSecret writes a raw secret value as tenantID's own principal would.
func (f *secretRefFixture) putTenantSecret(t *testing.T, tenantID, name, value string) {
	t.Helper()
	if _, err := f.secrets.StoreSecret(tenantctx.WithTenant(context.Background(), tenantID), &StoreSecretRequest{
		SecretID: name, Name: name, Data: []byte(value),
	}); err != nil {
		t.Fatalf("StoreSecret(%s in %s) error = %v", name, tenantID, err)
	}
}

func (f *secretRefFixture) createRefAccount(t *testing.T, ctx context.Context) *orchestration.Account {
	t.Helper()
	created, err := f.service.CreateAccount(ctx, &CreateAccountRequest{
		TenantID:            nsTenant,
		Provider:            orchestration.AccountProviderClaude,
		CredentialSecretRef: refName,
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return created.Account
}

func (f *secretRefFixture) spawnToken(t *testing.T) (string, error) {
	t.Helper()
	resolved, err := f.resolver.Resolve(spawnContext(nsTenant), orchestration.AccountResolveRequest{
		TenantID: nsTenant, ProjectID: nsProject, Provider: orchestration.AccountProviderClaude, Auto: true,
	})
	if err != nil {
		return "", err
	}
	return resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"], nil
}

func (f *secretRefFixture) getAccount(t *testing.T, accountID string) *orchestration.Account {
	t.Helper()
	got, err := f.service.GetAccount(tenantAdminContext(), &GetAccountRequest{TenantID: nsTenant, AccountID: accountID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	return got.Account
}

// TestAccountSecretRef_TenantAdminCreatedAccountResolvesInTenant is the
// #534/#535 boundary for references: tenant T's admin provisions T's account,
// the secret lives in T's keyspace, and T's spawn gets T's value.
func TestAccountSecretRef_TenantAdminCreatedAccountResolvesInTenant(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue+"\n")

	account := f.createRefAccount(t, tenantAdminContext())
	if account.CredentialSecretRef != refName {
		t.Fatalf("CredentialSecretRef = %q, want %q", account.CredentialSecretRef, refName)
	}

	token, err := f.spawnToken(t)
	if err != nil {
		t.Fatalf("spawn Resolve() error = %v", err)
	}
	if token != refValue {
		t.Fatalf("spawn token = %q, want the tenant secret's value", token)
	}

	// Nothing was copied into an inline envelope.
	_, getErr := f.inline.Get(spawnContext(nsTenant), nsTenant, account.ID)
	if !errors.Is(getErr, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("inline envelope Get() error = %v, want not found: a reference must not be copied", getErr)
	}
}

// TestAccountSecretRef_ResolvesOnlyInOwningTenant proves a same-named secret
// in ANOTHER keyspace — the system tenant's, where a system admin's own write
// lands — is never read for tenant T's account.
func TestAccountSecretRef_ResolvesOnlyInOwningTenant(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, orchestration.SystemTenantID, refName, "sk-fake-system")
	f.putTenantSecret(t, "another-tenant", refName, "sk-fake-other")

	_, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude, CredentialSecretRef: refName,
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "credential_secret_ref" {
		t.Fatalf("CreateAccount() error = %v, want credential_secret_ref refused: the secret is not in the account's tenant", err)
	}
	if !strings.Contains(invalid.Message, nsTenant) {
		t.Errorf("message %q should name the tenant the secret was looked for in", invalid.Message)
	}
}

// TestAccountSecretRef_SecretWriteRotatesNextSpawn proves the rotation
// runbook: writing a new version of the secret changes the next spawn's
// credential with no account write.
func TestAccountSecretRef_SecretWriteRotatesNextSpawn(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, "sk-fake-v1")
	account := f.createRefAccount(t, tenantAdminContext())
	before := f.getAccount(t, account.ID)

	if token, err := f.spawnToken(t); err != nil || token != "sk-fake-v1" {
		t.Fatalf("first spawn = %q, %v", token, err)
	}
	f.putTenantSecret(t, nsTenant, refName, "sk-fake-v2")
	token, err := f.spawnToken(t)
	if err != nil {
		t.Fatalf("second spawn error = %v", err)
	}
	if token != "sk-fake-v2" {
		t.Fatalf("second spawn token = %q, want the rotated value", token)
	}
	if after := f.getAccount(t, account.ID); after.CredentialSecretRef != before.CredentialSecretRef {
		t.Fatal("the account's reference changed; rotation must be a secret write only")
	}
}

// TestAccountSecretRef_MaterializeReleasesResolvedValue proves a worker host
// receives the RESOLVED value through materialize — never the reference.
func TestAccountSecretRef_MaterializeReleasesResolvedValue(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())

	resp, err := f.service.MaterializeAccountCredential(tenantctx.WithTenant(context.Background(), nsTenant),
		&MaterializeAccountCredentialRequest{TenantID: nsTenant, AccountID: account.ID})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.Credential.AccessToken != refValue || resp.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("released credential = %+v, want the resolved token projected into CLAUDE_CODE_OAUTH_TOKEN", resp.Credential)
	}
}

// TestAccountSecretRef_MaterializeIsSingleUse proves a referenced account's
// release tells the caller not to reuse it: a write of the secret moves no
// account timestamp, so a cached copy would outlive the rotation.
func TestAccountSecretRef_MaterializeIsSingleUse(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())
	ctx := tenantctx.WithTenant(context.Background(), nsTenant)

	first, err := f.service.MaterializeAccountCredential(ctx,
		&MaterializeAccountCredentialRequest{TenantID: nsTenant, AccountID: account.ID})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if !first.SingleUse {
		t.Fatal("a referenced account's release is not single-use")
	}

	f.putTenantSecret(t, nsTenant, refName, "sk-fake-oat-rotated")
	second, err := f.service.MaterializeAccountCredential(ctx,
		&MaterializeAccountCredentialRequest{TenantID: nsTenant, AccountID: account.ID, LeaseID: first.LeaseID})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential(after write) error = %v", err)
	}
	if second.Credential.AccessToken != "sk-fake-oat-rotated" {
		t.Error("the release after a secret write did not carry the replacement")
	}
	if !second.LastRefreshedAt.Equal(first.LastRefreshedAt) {
		t.Error("a secret write moved last_refreshed_at; single_use would then be redundant")
	}
}

// TestAccountSecretRef_InlineMaterializeIsReusable is the other side: an inline
// account's release stays cacheable under the last_refreshed_at signal.
func TestAccountSecretRef_InlineMaterializeIsReusable(t *testing.T) {
	f := newSecretRefFixture(t)
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	resp, err := f.service.MaterializeAccountCredential(tenantctx.WithTenant(context.Background(), nsTenant),
		&MaterializeAccountCredentialRequest{TenantID: nsTenant, AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.SingleUse {
		t.Error("an inline account's release is single-use")
	}
}

// TestAccountSecretRef_MissingSecretErrorsAccountAndReapplyRecovers covers the
// operator loop: the secret disappears, the next spawn parks and the account
// goes to "error" with a last_error naming the reference; writing the secret
// and re-applying the reference returns it to service.
func TestAccountSecretRef_MissingSecretErrorsAccountAndReapplyRecovers(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())

	if _, err := f.secrets.DeleteSecret(tenantctx.WithTenant(context.Background(), nsTenant),
		&DeleteSecretRequest{SecretID: refName, PermanentlyDelete: true, AllVersions: true}); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	_, err := f.spawnToken(t)
	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("spawn error = %v, want the missing-credential class", err)
	}
	errored := f.getAccount(t, account.ID)
	if errored.Status != orchestration.AccountStatusError || !strings.Contains(errored.LastError, refName) {
		t.Fatalf("account = status %s, last_error %q; want error naming %s", errored.Status, errored.LastError, refName)
	}

	f.putTenantSecret(t, nsTenant, refName, refValue)
	updated, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: account.ID, CredentialSecretRef: refName,
	})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if updated.Account.Status != orchestration.AccountStatusActive || updated.Account.LastError != "" {
		t.Fatalf("re-applied account = status %s, last_error %q; want active and clear", updated.Account.Status, updated.Account.LastError)
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Fatalf("spawn after recovery = %q, %v", token, err)
	}
}

// TestAccountSecretRef_SwitchInlineAccountToReference proves the update path
// that migrates an existing inline account: the reference takes over and the
// inline envelope is destroyed.
func TestAccountSecretRef_SwitchInlineAccountToReference(t *testing.T) {
	f := newSecretRefFixture(t)
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	f.putTenantSecret(t, nsTenant, refName, refValue)

	if _, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: created.Account.ID, CredentialSecretRef: refName,
	}); err != nil {
		t.Fatalf("UpdateAccount(switch) error = %v", err)
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Fatalf("spawn after switch = %q, %v; want the referenced value", token, err)
	}
	_, getErr := f.inline.Get(spawnContext(nsTenant), nsTenant, created.Account.ID)
	if !errors.Is(getErr, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("inline envelope Get() error = %v, want it destroyed by the switch", getErr)
	}
}

// TestAccountSecretRef_RetriedSwitchDestroysTheInlineEnvelope proves the
// switch's cleanup is retryable. The first attempt commits the record and then
// fails to destroy the inline envelope; the SAME update, retried once the
// backend is back, must remove it and report success — the retry no longer
// looks like a switch, which is what used to leave the envelope behind.
func TestAccountSecretRef_RetriedSwitchDestroysTheInlineEnvelope(t *testing.T) {
	f := newSecretRefFixture(t)
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	f.putTenantSecret(t, nsTenant, refName, refValue)
	envelopePath, err := orchestration.AccountSecretPath(nsTenant, created.Account.ID)
	if err != nil {
		t.Fatalf("AccountSecretPath() error = %v", err)
	}
	switchReq := &UpdateAccountRequest{TenantID: nsTenant, AccountID: created.Account.ID, CredentialSecretRef: refName}

	f.kv.failDeletesContaining(envelopePath)
	_, err = f.service.UpdateAccount(tenantAdminContext(), switchReq)
	var cleanup *AccountCredentialCleanupError
	if !errors.As(err, &cleanup) {
		t.Fatalf("UpdateAccount(switch, backend down) error = %v, want an AccountCredentialCleanupError", err)
	}
	if _, getErr := f.inline.Get(spawnContext(nsTenant), nsTenant, created.Account.ID); getErr != nil {
		t.Fatalf("inline envelope Get() error = %v; the failed cleanup should have left it in place", getErr)
	}

	f.kv.failDeletesContaining("")
	if _, err := f.service.UpdateAccount(tenantAdminContext(), switchReq); err != nil {
		t.Fatalf("UpdateAccount(retry) error = %v, want success", err)
	}
	_, getErr := f.inline.Get(spawnContext(nsTenant), nsTenant, created.Account.ID)
	if !errors.Is(getErr, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("inline envelope Get() error = %v, want it destroyed by the retried switch", getErr)
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Fatalf("spawn after retried switch = %q, %v; want the referenced value", token, err)
	}
}

// interleavingStore runs `between` once, the first time a conditional write
// arrives: after that request read the account, before its write lands. It is
// how a test places a concurrent change exactly inside another update's
// read-modify-write.
type interleavingStore struct {
	orchestration.AccountStore
	between func()
	fired   bool
}

func (s *interleavingStore) UpdateIfUnchanged(ctx context.Context, account *orchestration.Account, expected time.Time) error {
	if !s.fired && s.between != nil {
		s.fired = true
		s.between()
	}
	return s.AccountStore.UpdateIfUnchanged(ctx, account, expected)
}

// TestAccountSecretRef_ARenameOvertakenByASwitchKeepsTheReference is the
// round-4 review finding on go-quicbaas#1028: a rename that read an inline
// account, and was overtaken by a switch to a secret reference that destroyed
// the inline envelope, used to save its stale snapshot with no reference,
// leaving an account with NO credential source while both requests succeeded.
// The rename's write is now conditional; it loses the race, re-reads and
// re-applies onto the switched account.
func TestAccountSecretRef_ARenameOvertakenByASwitchKeepsTheReference(t *testing.T) {
	var f *secretRefFixture
	var accountID string
	wrapper := &interleavingStore{}
	f = newSecretRefFixtureWrapping(t, func(inner orchestration.AccountStore) orchestration.AccountStore {
		wrapper.AccountStore = inner
		return wrapper
	})
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	accountID = created.Account.ID
	f.putTenantSecret(t, nsTenant, refName, refValue)

	wrapper.between = func() {
		if _, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
			TenantID: nsTenant, AccountID: accountID, CredentialSecretRef: refName,
		}); err != nil {
			t.Errorf("the overtaking switch failed: %v", err)
		}
	}
	if _, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: accountID, DisplayName: "renamed",
	}); err != nil {
		t.Fatalf("UpdateAccount(rename) error = %v", err)
	}

	got, err := f.service.GetAccount(tenantAdminContext(), &GetAccountRequest{TenantID: nsTenant, AccountID: accountID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.Account.CredentialSecretRef != refName {
		t.Fatalf("reference = %q, want %q: the stale rename erased the switch", got.Account.CredentialSecretRef, refName)
	}
	if got.Account.DisplayName != "renamed" {
		t.Fatalf("display name = %q, want the rename re-applied", got.Account.DisplayName)
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Fatalf("spawn after the race = %q, %v; want the referenced value", token, err)
	}
}

// TestAccountStore_UpdateIfUnchangedRefusesAStaleSnapshot: the store refuses a
// conditional write whose snapshot predates the stored record, writing nothing.
func TestAccountStore_UpdateIfUnchangedRefusesAStaleSnapshot(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())
	stale := account.UpdatedAt.Add(-time.Second)

	store := orchestration.NewAccountStore(f.kv)
	ctx := tenantctx.WithTenant(tenantAdminContext(), nsTenant)
	current, err := store.Get(ctx, nsTenant, account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	current.DisplayName = "should-not-land"
	err = store.UpdateIfUnchanged(ctx, current, stale)
	if !errors.Is(err, orchestration.ErrAccountVersionConflict) {
		t.Fatalf("UpdateIfUnchanged(stale) error = %v, want ErrAccountVersionConflict", err)
	}
	after, err := store.Get(ctx, nsTenant, account.ID)
	if err != nil || after.DisplayName == "should-not-land" {
		t.Fatalf("a refused conditional write landed: %+v, %v", after, err)
	}
}

// TestAccountSecretRef_UpdateOfAReferenceAccountWithNoEnvelopeSucceeds proves
// the per-update cleanup is idempotent: an ordinary update of an account that
// never had an inline envelope does not fail on the absent one.
func TestAccountSecretRef_UpdateOfAReferenceAccountWithNoEnvelopeSucceeds(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())

	for i := 0; i < 2; i++ {
		if _, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
			TenantID: nsTenant, AccountID: account.ID, DisplayName: "renamed",
		}); err != nil {
			t.Fatalf("UpdateAccount() %d error = %v", i, err)
		}
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Fatalf("spawn after update = %q, %v", token, err)
	}
}

func TestAccountSecretRef_SwitchRefusesUnresolvableReference(t *testing.T) {
	f := newSecretRefFixture(t)
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}

	_, err = f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: created.Account.ID, CredentialSecretRef: refName,
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "credential_secret_ref" {
		t.Fatalf("UpdateAccount() error = %v, want the unresolvable reference refused", err)
	}
	// The refused switch left the inline credential in service.
	if token, err := f.spawnToken(t); err != nil || token != "sk-fake-inline" {
		t.Fatalf("spawn after refused switch = %q, %v; want the inline credential", token, err)
	}
}

func TestAccountSecretRef_RotateIsRefusedForReferenceAccount(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())

	_, err := f.service.RotateAccountCredential(tenantAdminContext(), &RotateAccountCredentialRequest{
		TenantID: nsTenant, AccountID: account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, refName) {
		t.Fatalf("RotateAccountCredential() error = %v, want a refusal naming the secret to write", err)
	}
}

func TestAccountSecretRef_UpdateRefusesOAuthModeOnReferenceAccount(t *testing.T) {
	f := newSecretRefFixture(t)
	f.putTenantSecret(t, nsTenant, refName, refValue)
	account := f.createRefAccount(t, tenantAdminContext())

	_, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: account.ID, RefreshPolicy: orchestration.AccountRefreshOAuth2,
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "refresh_policy" {
		t.Fatalf("UpdateAccount() error = %v, want refresh_policy refused", err)
	}
}

func TestAccountSecretRef_UpdateRefusesFileWithoutReference(t *testing.T) {
	f := newSecretRefFixture(t)
	_, err := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
		TenantID: nsTenant, AccountID: "acct-any", CredentialSecretFile: ".codex/auth.json",
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "credential_secret_file" {
		t.Fatalf("UpdateAccount() error = %v, want credential_secret_file refused", err)
	}
}

func TestAccountSecretRef_CreateRequiresExactlyOneSource(t *testing.T) {
	f := newSecretRefFixture(t)
	cases := map[string]*CreateAccountRequest{
		"neither": {TenantID: nsTenant, Provider: orchestration.AccountProviderClaude},
		"both": {
			TenantID: nsTenant, Provider: orchestration.AccountProviderClaude, CredentialSecretRef: refName,
			Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
		},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.service.CreateAccount(tenantAdminContext(), req)
			var invalid *AccountInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("CreateAccount() error = %v, want AccountInvalidError", err)
			}
		})
	}
}

func TestAccountSecretRef_CreateWithoutResolverFailsClosed(t *testing.T) {
	svc := NewAccountService(orchestration.NewInMemoryAccountStore(), nil)
	_, err := svc.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude, CredentialSecretRef: refName,
	})
	var notConfigured *orchestration.AccountSecretResolverNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("CreateAccount() error = %v, want AccountSecretResolverNotConfiguredError", err)
	}
}

func TestNewAccountSecretResolver_NoSecretsService(t *testing.T) {
	_, err := NewAccountSecretResolver(nil).ResolveAccountSecret(context.Background(), nsTenant, refName)
	var notConfigured *AccountSecretsServiceNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("ResolveAccountSecret() error = %v, want AccountSecretsServiceNotConfiguredError", err)
	}
}

// pausingGetStore runs `after` once, right after the first Get it serves
// returns: the caller has read the account and not yet acted on it. The hook
// is disarmed before it runs, since the writer it runs reads through this same
// store.
type pausingGetStore struct {
	orchestration.AccountStore
	after func()
	armed atomic.Bool
}

func (s *pausingGetStore) Get(ctx context.Context, tenantID, accountID string) (*orchestration.Account, error) {
	account, err := s.AccountStore.Get(ctx, tenantID, accountID)
	if err == nil && s.after != nil && s.armed.CompareAndSwap(true, false) {
		s.after()
	}
	return account, err
}

// TestAccountSecretRef_RotationOvertakenByASwitchLeavesNoEnvelope is the
// round-5 rotation race on go-quicbaas#1028: an inline rotation reads the
// account, a switch to a secret reference commits and destroys the inline
// envelope, and the rotation then resumes. It must fail with a typed error,
// leave no inline envelope behind, and leave the account's expiry alone.
func TestAccountSecretRef_RotationOvertakenByASwitchLeavesNoEnvelope(t *testing.T) {
	wrapper := &pausingGetStore{}
	f := newSecretRefFixtureWrapping(t, func(inner orchestration.AccountStore) orchestration.AccountStore {
		wrapper.AccountStore = inner
		return wrapper
	})
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	f.putTenantSecret(t, nsTenant, refName, refValue)

	var afterSwitch *orchestration.Account
	wrapper.after = func() {
		switched, switchErr := f.service.UpdateAccount(tenantAdminContext(), &UpdateAccountRequest{
			TenantID: nsTenant, AccountID: created.Account.ID, CredentialSecretRef: refName,
		})
		if switchErr != nil {
			t.Errorf("the overtaking switch failed: %v", switchErr)
			return
		}
		afterSwitch = switched.Account
	}
	wrapper.armed.Store(true)

	_, err = f.service.RotateAccountCredential(tenantAdminContext(), &RotateAccountCredentialRequest{
		TenantID: nsTenant, AccountID: created.Account.ID,
		Credential: &orchestration.AccountCredential{
			AccessToken: "sk-fake-rotated", ExpiresAt: time.Now().UTC().Add(90 * 24 * time.Hour),
		},
	})
	if !errors.Is(err, orchestration.ErrAccountCredentialSourceChanged) {
		t.Fatalf("RotateAccountCredential() error = %v, want ErrAccountCredentialSourceChanged", err)
	}
	if afterSwitch == nil {
		t.Fatal("the switch never ran inside the rotation")
	}

	_, getErr := f.inline.Get(spawnContext(nsTenant), nsTenant, created.Account.ID)
	if !errors.Is(getErr, orchestration.ErrAccountCredentialNotFound) {
		t.Errorf("inline envelope Get() error = %v, want the rotation's envelope removed", getErr)
	}
	got := f.getAccount(t, created.Account.ID)
	if got.CredentialSecretRef != refName {
		t.Errorf("reference = %q, want %q", got.CredentialSecretRef, refName)
	}
	if !got.CredentialExpiresAt.Equal(afterSwitch.CredentialExpiresAt) {
		t.Errorf("credential expiry = %v, want %v unchanged by the refused rotation",
			got.CredentialExpiresAt, afterSwitch.CredentialExpiresAt)
	}
	if token, err := f.spawnToken(t); err != nil || token != refValue {
		t.Errorf("spawn after the race = %q, %v; want the referenced value", token, err)
	}
}

// TestAccountSecretRef_InlineRotationStillSucceeds is the other side: with no
// switch in the way the guarded rotation lands and records the new expiry.
func TestAccountSecretRef_InlineRotationStillSucceeds(t *testing.T) {
	f := newSecretRefFixture(t)
	created, err := f.service.CreateAccount(tenantAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-inline"},
	})
	if err != nil {
		t.Fatalf("CreateAccount(inline) error = %v", err)
	}
	expiry := time.Now().UTC().Add(90 * 24 * time.Hour).Truncate(time.Second)
	if _, err := f.service.RotateAccountCredential(tenantAdminContext(), &RotateAccountCredentialRequest{
		TenantID: nsTenant, AccountID: created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-fake-rotated", ExpiresAt: expiry},
	}); err != nil {
		t.Fatalf("RotateAccountCredential() error = %v", err)
	}
	if got := f.getAccount(t, created.Account.ID); !got.CredentialExpiresAt.Equal(expiry) {
		t.Errorf("credential expiry = %v, want %v", got.CredentialExpiresAt, expiry)
	}
	if token, err := f.spawnToken(t); err != nil || token != "sk-fake-rotated" {
		t.Errorf("spawn after rotation = %q, %v; want the rotated value", token, err)
	}
}

// TestAccountSecretResolver_TheSecretVersionIsTheGeneration: every write of a
// referenced secret is a newer copy of the account's credential, which is how
// a live session learns that its secret was rewritten under it
// (go-quicbaas#997). The version is used, not the response's CreatedAt, which
// the secrets service fills with the time of the READ when the version carries
// no creation stamp — every read would then look like a rotation.
func TestAccountSecretResolver_TheSecretVersionIsTheGeneration(t *testing.T) {
	f := newSecretRefFixture(t)
	refs := NewAccountSecretResolver(f.secrets)

	f.putTenantSecret(t, nsTenant, refName, "sk-fake-<account>")
	first, err := refs.ResolveAccountSecret(context.Background(), nsTenant, refName)
	if err != nil {
		t.Fatalf("ResolveAccountSecret() error = %v", err)
	}
	again, err := refs.ResolveAccountSecret(context.Background(), nsTenant, refName)
	if err != nil {
		t.Fatalf("ResolveAccountSecret() error = %v", err)
	}
	if first.Generation <= 0 || again.Generation != first.Generation {
		t.Fatalf("generations = %d then %d, want one positive generation for an unchanged secret", first.Generation, again.Generation)
	}
	if string(first.Data) != "sk-fake-<account>" {
		t.Errorf("Data = %q, want the secret's value", first.Data)
	}

	f.putTenantSecret(t, nsTenant, refName, "sk-fake-<account>")
	rewritten, err := refs.ResolveAccountSecret(context.Background(), nsTenant, refName)
	if err != nil {
		t.Fatalf("ResolveAccountSecret() error = %v", err)
	}
	if rewritten.Generation <= first.Generation {
		t.Errorf("generation after a rewrite = %d, want past %d", rewritten.Generation, first.Generation)
	}
}

// TestSecretVersionGeneration_AnUnorderableVersionIsUnknown: a version that
// does not fit a generation cannot be ordered against the others and is
// reported as unknown rather than wrapped into a negative number.
func TestSecretVersionGeneration_AnUnorderableVersionIsUnknown(t *testing.T) {
	if got := secretVersionGeneration(7); got != 7 {
		t.Errorf("secretVersionGeneration(7) = %d, want 7", got)
	}
	if got := secretVersionGeneration(math.MaxUint64); got != 0 {
		t.Errorf("secretVersionGeneration(MaxUint64) = %d, want 0 (unknown)", got)
	}
}
