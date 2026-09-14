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
	stderrors "errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// The IN-PROCESS hand-over is leased too.
//
// A local spawn puts the credential in a subprocess's environment and in a
// per-spawn materialization directory. That is a hand-over, so it takes a grant
// exactly as the remote release does — through the SAME authority, so one
// revocation covers both.
// =============================================================================

// seedSubscriptionAccount creates an account whose credential is a SUBSCRIPTION
// OAuth envelope materialized as a JSON file. There is deliberately no API-key
// variant: the platform does not use one.
func seedSubscriptionAccount(t *testing.T, store AccountStore, secrets *AccountSecretStore, tenantID, accountID string) {
	t.Helper()
	ctx := context.Background()
	if err := store.Create(ctx, &Account{
		ID:            accountID,
		TenantID:      tenantID,
		Provider:      AccountProviderClaude,
		RefreshPolicy: AccountCredentialModeOAuthRefresh,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secrets.Put(ctx, tenantID, accountID, &AccountCredential{
		AccessToken:       "oauth-access-token-value",
		RefreshToken:      "oauth-refresh-token-value",
		ExpiresAt:         time.Now().Add(8 * time.Hour),
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		Files: map[string][]byte{
			".claude/.credentials.json": []byte(`{"access_token":"oauth-access-token-value"}`),
		},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
}

// setupLeasedResolver builds a resolver with a delivery leaser wired.
func setupLeasedResolver(t *testing.T, leaser AccountCredentialLeaser) (AccountResolver, AccountStore, *AccountSecretStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(nil)
	resolver := NewAccountResolver(store, tokens, strategies, WithAccountDeliveryGrantor(leaser))
	return resolver, store, secretStore
}

func TestAccountResolver_LeasesTheDelivery(t *testing.T) {
	leaser, mgr, _ := newTestLeaser(t)
	resolver, store, secrets := setupLeasedResolver(t, leaser)
	seedSubscriptionAccount(t, store, secrets, "tenant-a", "acct-1")
	ctx := context.Background()

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID:  "tenant-a",
		ProjectID: "proj-1",
		JobID:     "job-1",
		Provider:  AccountProviderClaude,
		AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.LeaseID == "" {
		t.Fatal("Resolve() handed over credential material with no delivery grant")
	}
	if resolved.LeaseExpiresAt.Before(time.Now()) {
		t.Fatalf("LeaseExpiresAt = %v, already past", resolved.LeaseExpiresAt)
	}

	// The grant is scoped to the job the spawn named, so the job's deliveries
	// are revocable as a set.
	held, err := mgr.GetLease(ctx, resolved.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	wantPath, _ := AccountLeasePath(AccountLeaseScope{
		TenantID: "tenant-a", AccountID: "acct-1", ProjectID: "proj-1", JobID: "job-1",
	})
	if held.SecretPath != wantPath {
		t.Fatalf("grant path = %q, want %q", held.SecretPath, wantPath)
	}

	// And the credential is still delivered: leasing governs the hand-over, it
	// does not replace it.
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "oauth-access-token-value" {
		t.Fatalf("Resolve().Env = %v, want the OAuth access token projected", resolved.Env)
	}
	if len(resolved.Files) != 1 {
		t.Fatalf("Resolve().Files = %v, want the subscription credentials file", resolved.Files)
	}
}

// TestAccountResolver_LeaseDoesNotTruncateTheCredential pins the relationship
// the owner asked to be explicit: the grant's lifetime and the OAuth
// credential's are INDEPENDENT, and a short grant never shortens a token the
// refresher would have kept alive.
func TestAccountResolver_LeaseDoesNotTruncateTheCredential(t *testing.T) {
	mgr := newTestLeaseManager(t)
	// A grant far shorter than the credential's own 8h expiry.
	leaser := NewAccountCredentialLeaser(mgr, time.Minute, nil)
	resolver, store, secrets := setupLeasedResolver(t, leaser)
	seedSubscriptionAccount(t, store, secrets, "tenant-a", "acct-1")

	resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// The delivered credential's own expiry is untouched by the grant's.
	if resolved.LeaseExpiresAt.After(time.Now().Add(2 * time.Minute)) {
		t.Fatalf("LeaseExpiresAt = %v, want the one-minute grant", resolved.LeaseExpiresAt)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] == "" {
		t.Fatal("a short grant must not withhold the credential")
	}
	held, err := mgr.GetLease(context.Background(), resolved.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if !held.Renewable {
		t.Fatal("a delivery grant must be renewable, or a long job is cut off at one TTL")
	}
}

// TestAccountResolver_FailsClosedWhenTheGrantCannotBeIssued is the bad-input
// twin, and it is the security-relevant direction: a credential handed over with
// no grant is a credential nothing can revoke.
func TestAccountResolver_FailsClosedWhenTheGrantCannotBeIssued(t *testing.T) {
	mgr := newTestLeaseManager(t)
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	resolver, store, secrets := setupLeasedResolver(t, NewAccountCredentialLeaser(mgr, 0, nil))
	seedSubscriptionAccount(t, store, secrets, "tenant-a", "acct-1")

	resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err == nil {
		t.Fatalf("Resolve() succeeded with an unavailable lease store, handing over %+v", resolved.Env)
	}
	if resolved != nil {
		t.Fatal("Resolve() returned credential material alongside its failure")
	}
	var issueErr *AccountLeaseIssueError
	if !stderrors.As(err, &issueErr) {
		t.Fatalf("Resolve() error = %v, want AccountLeaseIssueError", err)
	}
	// The refusal names the account, never the credential.
	for _, secret := range []string{"oauth-access-token-value", "oauth-refresh-token-value"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the refusal leaked credential material: %q", err)
		}
	}
}

// TestAccountResolver_UnleasedWhenNoLeaserWired keeps the pre-existing
// behaviour reachable: a deployment with no lease manager resolves exactly as it
// did before grants existed rather than losing the capability.
func TestAccountResolver_UnleasedWhenNoLeaserWired(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	seedSubscriptionAccount(t, store, secrets, "tenant-a", "acct-1")

	resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.LeaseID != "" {
		t.Fatalf("LeaseID = %q with no leaser wired, want empty", resolved.LeaseID)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] == "" {
		t.Fatal("an unleased deployment must still deliver the credential")
	}
}
