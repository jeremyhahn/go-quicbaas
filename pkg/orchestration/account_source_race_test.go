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
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Automatic stamps race operator changes (go-quicbaas#1028, round 6).
//
// A token renewal and a secret read both run for as long as a network round
// trip takes, and both end by stamping the account. An operator can switch,
// re-point, re-apply or revoke the account inside that window. Each test
// below runs the operator's change from INSIDE the round trip, so the stamp is
// written after it, and checks the stamp did not undo it.
// =============================================================================

const raceRef = "agents/claude/claude1/oauth_token"

// hookedRefresher runs during first once, inside the provider round trip,
// then answers with renewed (or fails with err).
type hookedRefresher struct {
	during func()
	fired  atomic.Bool
	err    error
}

func (r *hookedRefresher) Refresh(_ context.Context, cred *AccountCredential) (*AccountCredential, error) {
	if r.during != nil && r.fired.CompareAndSwap(false, true) {
		r.during()
	}
	if r.err != nil {
		return nil, r.err
	}
	renewed := *cred
	renewed.AccessToken = "renewed-token"
	renewed.RefreshToken = "renewed-refresh"
	renewed.ExpiresAt = time.Now().UTC().Add(time.Hour)
	return &renewed, nil
}

// hookedSecretResolver runs during once, inside a read of the secret, before
// answering from the wrapped resolver.
type hookedSecretResolver struct {
	*memorySecretResolver
	during func()
	fired  atomic.Bool
}

func (r *hookedSecretResolver) ResolveAccountSecret(ctx context.Context, tenantID, ref string) ([]byte, error) {
	if r.during != nil && r.fired.CompareAndSwap(false, true) {
		r.during()
	}
	return r.memorySecretResolver.ResolveAccountSecret(ctx, tenantID, ref)
}

// rewriteForTest applies change to the stored account with a conditional
// write, as the account service does.
func rewriteForTest(t *testing.T, store AccountStore, tenantID, accountID string, change func(*Account)) *Account {
	t.Helper()
	account, err := store.Get(context.Background(), tenantID, accountID)
	if err != nil {
		t.Errorf("Get() error = %v", err)
		return nil
	}
	change(account)
	if err := store.UpdateIfUnchanged(context.Background(), account, account.UpdatedAt); err != nil {
		t.Errorf("UpdateIfUnchanged() error = %v", err)
		return nil
	}
	return account
}

// expiringOAuthAccount creates an inline OAuth account whose stored access
// token has already expired, so the next Resolve renews it.
func expiringOAuthAccount(t *testing.T, store AccountStore, secretStore *AccountSecretStore) *Account {
	t.Helper()
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old-token", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	return account
}

// TestAccountTokenManager_RenewalOvertakenBySwitchLeavesNoEnvelope: a switch
// to a secret reference commits (and destroys the inline envelope) while the
// renewal's provider round trip is in flight. The renewal's write-back must
// not survive it: no envelope remains, and the record's expiry and refresh
// stamp are the switch's, not the retired credential's.
func TestAccountTokenManager_RenewalOvertakenBySwitchLeavesNoEnvelope(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	account := expiringOAuthAccount(t, store, secretStore)

	var afterSwitch *Account
	mgr.RegisterRefresher(AccountProviderClaude, &hookedRefresher{during: func() {
		afterSwitch = rewriteForTest(t, store, "tenant-a", account.ID, func(a *Account) {
			a.CredentialSecretRef = raceRef
			a.RefreshPolicy = AccountCredentialModeStaticToken
		})
		if err := secretStore.Delete(ctx, "tenant-a", account.ID); err != nil {
			t.Errorf("switch's envelope Delete() error = %v", err)
		}
	}})

	_, err := mgr.Resolve(ctx, account)
	if !errors.Is(err, ErrAccountCredentialSourceChanged) {
		t.Fatalf("Resolve() error = %v, want ErrAccountCredentialSourceChanged", err)
	}
	if afterSwitch == nil {
		t.Fatal("the switch never ran inside the renewal")
	}
	if _, getErr := secretStore.Get(ctx, "tenant-a", account.ID); !errors.Is(getErr, ErrAccountCredentialNotFound) {
		t.Errorf("inline envelope Get() error = %v, want the renewal's write-back removed", getErr)
	}
	got, err := store.Get(ctx, "tenant-a", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.CredentialExpiresAt.Equal(afterSwitch.CredentialExpiresAt) {
		t.Errorf("credential expiry = %v, want %v: the renewal stamped a reference account", got.CredentialExpiresAt, afterSwitch.CredentialExpiresAt)
	}
	if !got.LastRefreshedAt.Equal(afterSwitch.LastRefreshedAt) || got.CredentialSecretRef != raceRef {
		t.Error("the renewal's stamp reached the switched record")
	}
}

// TestAccountTokenManager_RenewalFailureLeavesARevokedAccountRevoked: a
// revoke that lands during a renewal that then fails keeps the account
// revoked; the failure stamp's "error" does not replace it.
func TestAccountTokenManager_RenewalFailureLeavesARevokedAccountRevoked(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	account := expiringOAuthAccount(t, store, secretStore)

	mgr.RegisterRefresher(AccountProviderClaude, &hookedRefresher{
		err: errors.New("invalid_grant"),
		during: func() {
			rewriteForTest(t, store, "tenant-a", account.ID, func(a *Account) {
				a.Status = AccountStatusRevoked
				a.LastError = "revoked by operator"
			})
		},
	})

	if _, err := mgr.Resolve(ctx, account); err == nil {
		t.Fatal("Resolve() succeeded with a failing refresher")
	}
	got, err := store.Get(ctx, "tenant-a", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != AccountStatusRevoked || got.LastError != "revoked by operator" {
		t.Errorf("status = %s, last_error = %q; want the operator's revoke kept", got.Status, got.LastError)
	}
}

// TestAccountTokenManager_StaleSecretFailureSparesARepointedAccount: the
// operator re-points the reference to a valid secret while a read of the old
// one is failing. The failure must not disable the account.
func TestAccountTokenManager_StaleSecretFailureSparesARepointedAccount(t *testing.T) {
	resolver := &hookedSecretResolver{memorySecretResolver: newMemorySecretResolver()}
	resolver.put("tenant-a", "agents/claude/claude2/oauth_token", "sk-fake-valid")
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, raceRef, "")
	resolver.during = func() {
		rewriteForTest(t, store, "tenant-a", account.ID, func(a *Account) {
			a.CredentialSecretRef = "agents/claude/claude2/oauth_token"
			a.LastRefreshedAt = time.Now().UTC()
		})
	}

	if _, err := manager.Resolve(context.Background(), account); !errors.Is(err, ErrAccountCredentialNotFound) {
		t.Fatalf("Resolve() error = %v, want the stale read's own failure", err)
	}
	got := mustGetRefAccount(t, store, account.ID)
	if got.Status != AccountStatusActive || got.LastError != "" {
		t.Errorf("status = %s, last_error = %q; a stale failure disabled the re-pointed account", got.Status, got.LastError)
	}
}

// TestAccountTokenManager_StaleSecretFailureSparesAReappliedAccount: the
// documented recovery — write the secret, re-apply the SAME reference — lands
// while an earlier read is failing. Re-applying re-stamps last_refreshed_at,
// which is what tells the stale failure it has been answered.
func TestAccountTokenManager_StaleSecretFailureSparesAReappliedAccount(t *testing.T) {
	resolver := &hookedSecretResolver{memorySecretResolver: newMemorySecretResolver()}
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, raceRef, "")
	resolver.during = func() {
		rewriteForTest(t, store, "tenant-a", account.ID, func(a *Account) {
			a.LastRefreshedAt = time.Now().UTC()
		})
	}

	if _, err := manager.Resolve(context.Background(), account); err == nil {
		t.Fatal("Resolve() succeeded against a missing secret")
	}
	if got := mustGetRefAccount(t, store, account.ID); got.Status != AccountStatusActive {
		t.Errorf("status = %s; a stale failure undid the re-apply", got.Status)
	}
}

// TestAccountTokenManager_StaleSecretFailureLeavesARevokedAccountRevoked: a
// revoke that lands while a read is failing keeps the account revoked. An
// "error" in its place is one a later re-apply would reactivate.
func TestAccountTokenManager_StaleSecretFailureLeavesARevokedAccountRevoked(t *testing.T) {
	resolver := &hookedSecretResolver{memorySecretResolver: newMemorySecretResolver()}
	manager, store, account := refAccountFixture(t, resolver, AccountProviderClaude, raceRef, "")
	resolver.during = func() {
		rewriteForTest(t, store, "tenant-a", account.ID, func(a *Account) {
			a.Status = AccountStatusRevoked
			a.LastError = "revoked by operator"
		})
	}

	if _, err := manager.Resolve(context.Background(), account); err == nil {
		t.Fatal("Resolve() succeeded against a missing secret")
	}
	got := mustGetRefAccount(t, store, account.ID)
	if got.Status != AccountStatusRevoked || got.LastError != "revoked by operator" {
		t.Errorf("status = %s, last_error = %q; want the operator's revoke kept", got.Status, got.LastError)
	}
}

// TestAccountSecretRefUnresolvedError_DoesNotExposeItsCause proves the cause —
// the secrets backend's own error, possibly carrying its diagnostics — is not
// reachable through the chain a transport walks, while the sentinel is.
func TestAccountSecretRefUnresolvedError_DoesNotExposeItsCause(t *testing.T) {
	cause := errors.New("backend diagnostic sk-fake-marker")
	err := error(&AccountSecretRefUnresolvedError{TenantID: "t", AccountID: "a", Ref: raceRef, Reason: "the secret could not be read", Cause: cause})

	if !errors.Is(err, ErrAccountCredentialNotFound) {
		t.Error("the sentinel is no longer reachable")
	}
	if errors.Is(err, cause) {
		t.Error("the cause is reachable through Unwrap")
	}
}
