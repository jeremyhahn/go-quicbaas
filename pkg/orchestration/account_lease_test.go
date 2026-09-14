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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Harness
// =============================================================================

// recordingAuditor captures the audit trail so a test can assert BOTH that an
// issuance was recorded and that no credential material reached the record.
type recordingAuditor struct {
	mu     sync.Mutex
	events []StoreAuditEvent
}

func (a *recordingAuditor) RecordStoreEvent(_ context.Context, event StoreAuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *recordingAuditor) all() []StoreAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]StoreAuditEvent(nil), a.events...)
}

// newTestLeaseManager returns a real lease manager over qrdb's memory engine.
// The production type is used deliberately: the tombstone semantics revocation
// depends on are the manager's, and a stand-in would let this package's tests
// pass against semantics production does not have.
func newTestLeaseManager(t *testing.T) lease.LeaseManager {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}
	mgr, err := lease.NewManager(&lease.ManagerConfig{
		Store:  kvstore.NewMemory(),
		Logger: logger.NewAdapter(log),
	})
	if err != nil {
		t.Fatalf("lease.NewManager() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

func newTestLeaser(t *testing.T) (AccountCredentialLeaser, lease.LeaseManager, *recordingAuditor) {
	t.Helper()
	mgr := newTestLeaseManager(t)
	auditor := &recordingAuditor{}
	return NewAccountCredentialLeaser(mgr, 0, auditor), mgr, auditor
}

func testScope() AccountLeaseScope {
	return AccountLeaseScope{
		TenantID:  "tenant-a",
		AccountID: "acct-1",
		Provider:  AccountProviderClaude,
		ProjectID: "proj-1",
		JobID:     "job-1",
		AgentID:   "agent-1",
	}
}

// =============================================================================
// AccountLeasePath
// =============================================================================

func TestAccountLeasePath(t *testing.T) {
	path, err := AccountLeasePath(testScope())
	if err != nil {
		t.Fatalf("AccountLeasePath() error = %v", err)
	}
	want := "accounts/delivery/tenant-a/proj-1/job-1/acct-1"
	if path != want {
		t.Fatalf("AccountLeasePath() = %q, want %q", path, want)
	}
}

func TestAccountLeasePath_SpellsAbsentSegments(t *testing.T) {
	// A tenant-wide, job-less delivery must not collide with a project one, so
	// the absence is spelled rather than skipped.
	path, err := AccountLeasePath(AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("AccountLeasePath() error = %v", err)
	}
	want := "accounts/delivery/tenant-a/_none/_none/acct-1"
	if path != want {
		t.Fatalf("AccountLeasePath() = %q, want %q", path, want)
	}
}

func TestAccountLeasePath_BadInput(t *testing.T) {
	tests := []struct {
		name  string
		scope AccountLeaseScope
	}{
		{name: "no tenant", scope: AccountLeaseScope{AccountID: "acct-1"}},
		{name: "no account", scope: AccountLeaseScope{TenantID: "tenant-a"}},
		{name: "separator in tenant", scope: AccountLeaseScope{TenantID: "tenant/a", AccountID: "acct-1"}},
		{name: "separator in account", scope: AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct/1"}},
		{name: "separator in project", scope: AccountLeaseScope{TenantID: "t", AccountID: "a", ProjectID: "p/1"}},
		{name: "separator in job", scope: AccountLeaseScope{TenantID: "t", AccountID: "a", JobID: "j/1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AccountLeasePath(tc.scope); err == nil {
				t.Fatalf("AccountLeasePath(%+v) succeeded, want a key-layout refusal", tc.scope)
			}
		})
	}
}

// =============================================================================
// Issue
// =============================================================================

func TestAccountCredentialLeaser_GrantDelivery(t *testing.T) {
	leaser, mgr, auditor := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	if issued.LeaseID == "" {
		t.Fatal("GrantDelivery() returned no lease ID")
	}
	if issued.ExpiresAt.Before(time.Now()) {
		t.Fatalf("GrantDelivery() expires in the past: %v", issued.ExpiresAt)
	}

	// The grant is durable and lives in the platform's one lease keyspace, so
	// Security().DynamicSecrets().ListLeases finds it.
	held, err := mgr.GetLease(ctx, issued.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	wantPath, _ := AccountLeasePath(testScope())
	if held.SecretPath != wantPath {
		t.Fatalf("lease path = %q, want %q", held.SecretPath, wantPath)
	}
	if !held.Renewable {
		t.Fatal("a delivery grant must be renewable: a long job must not be cut off at one TTL")
	}
	if held.MaxRenewals != 0 {
		t.Fatalf("MaxRenewals = %d, want 0 (unbounded in count, bounded in time)", held.MaxRenewals)
	}

	events := auditor.all()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want exactly 1 per issuance", len(events))
	}
	if !events[0].Success || events[0].TenantID != "tenant-a" {
		t.Fatalf("audit event = %+v, want a successful tenant-a record", events[0])
	}
	if !strings.Contains(events[0].ResourceID, issued.LeaseID) {
		t.Fatalf("audit subject %q does not name the grant", events[0].ResourceID)
	}
}

func TestAccountCredentialLeaser_Issue_DefaultTTL(t *testing.T) {
	leaser, mgr, _ := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	held, err := mgr.GetLease(ctx, issued.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if held.TTL != DefaultAccountDeliveryTTL {
		t.Fatalf("TTL = %v, want the default %v", held.TTL, DefaultAccountDeliveryTTL)
	}
}

func TestAccountCredentialLeaser_Issue_HonoursExplicitTTL(t *testing.T) {
	mgr := newTestLeaseManager(t)
	leaser := NewAccountCredentialLeaser(mgr, 5*time.Minute, nil)

	issued, err := leaser.GrantDelivery(context.Background(), testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	held, err := mgr.GetLease(context.Background(), issued.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if held.TTL != 5*time.Minute {
		t.Fatalf("TTL = %v, want 5m", held.TTL)
	}
}

func TestAccountCredentialLeaser_Issue_BadInput(t *testing.T) {
	leaser, _, auditor := newTestLeaser(t)
	ctx := context.Background()

	t.Run("unaddressable scope", func(t *testing.T) {
		if _, err := leaser.GrantDelivery(ctx, AccountLeaseScope{AccountID: "acct-1"}); err == nil {
			t.Fatal("GrantDelivery() with no tenant succeeded, want a refusal")
		}
	})

	t.Run("no leaser configured", func(t *testing.T) {
		var unwired *accountCredentialLeaser
		_, err := unwired.GrantDelivery(ctx, testScope())
		var notConfigured *AccountLeaseNotConfiguredError
		if !asErr(err, &notConfigured) {
			t.Fatalf("GrantDelivery() on an unwired leaser error = %v, want AccountLeaseNotConfiguredError", err)
		}
	})

	t.Run("closed lease manager fails the issuance", func(t *testing.T) {
		mgr := newTestLeaseManager(t)
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		failing := NewAccountCredentialLeaser(mgr, 0, auditor)

		_, err := failing.GrantDelivery(ctx, testScope())
		var issueErr *AccountLeaseIssueError
		if !asErr(err, &issueErr) {
			t.Fatalf("GrantDelivery() against a closed manager error = %v, want AccountLeaseIssueError", err)
		}
		if issueErr.AccountID != "acct-1" {
			t.Fatalf("AccountLeaseIssueError.AccountID = %q, want acct-1", issueErr.AccountID)
		}
		// A refused issuance is still audited: repeated failures are what an
		// operator needs to see.
		var refused bool
		for _, event := range auditor.all() {
			if !event.Success {
				refused = true
			}
		}
		if !refused {
			t.Fatal("a refused issuance was not audited")
		}
	})
}

// =============================================================================
// Validate — the enforcement point
// =============================================================================

func TestAccountCredentialLeaser_Validate(t *testing.T) {
	leaser, _, _ := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}

	valid, err := leaser.Validate(ctx, issued.LeaseID, testScope())
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if valid.LeaseID != issued.LeaseID {
		t.Fatalf("Validate() = %q, want %q", valid.LeaseID, issued.LeaseID)
	}
}

// TestAccountCredentialLeaser_Validate_RefusesRevoked is the assertion the whole
// mechanism exists for: once the session's ending withdraws the grant, the host
// that holds a copy cannot obtain another one.
func TestAccountCredentialLeaser_Validate_RefusesRevoked(t *testing.T) {
	leaser, _, _ := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	if err := leaser.Revoke(ctx, issued.LeaseID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	_, err = leaser.Validate(ctx, issued.LeaseID, testScope())
	if !lease.IsLeaseRevoked(err) {
		t.Fatalf("Validate() after Revoke error = %v, want a revoked refusal", err)
	}
}

func TestAccountCredentialLeaser_Validate_BadInput(t *testing.T) {
	leaser, mgr, _ := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}

	t.Run("unknown lease", func(t *testing.T) {
		if _, err := leaser.Validate(ctx, "lease_nope", testScope()); !lease.IsLeaseNotFound(err) {
			t.Fatalf("Validate() of an unknown grant error = %v, want not-found", err)
		}
	})

	t.Run("expired lease", func(t *testing.T) {
		lapsed, lapsedErr := NewAccountCredentialLeaser(mgr, lease.MinLeaseTTL, nil).
			GrantDelivery(ctx, AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-lapsed"})
		if lapsedErr != nil {
			t.Fatalf("GrantDelivery() error = %v", lapsedErr)
		}
		// Drive the clock forward by rewriting the record rather than sleeping:
		// enforcement reads ExpiresAt, so an already-past one is the same fact a
		// wall-clock wait would produce.
		expireLease(t, mgr, lapsed.LeaseID)

		_, err := NewAccountCredentialLeaser(mgr, 0, nil).
			Validate(ctx, lapsed.LeaseID, AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-lapsed"})
		if !lease.IsLeaseExpired(err) {
			t.Fatalf("Validate() of a lapsed grant error = %v, want expired", err)
		}
	})

	scopeCases := []struct {
		name  string
		scope AccountLeaseScope
		field string
	}{
		{name: "other tenant", scope: withScope(func(s *AccountLeaseScope) { s.TenantID = "tenant-b" }), field: "tenant_id"},
		{name: "other account", scope: withScope(func(s *AccountLeaseScope) { s.AccountID = "acct-2" }), field: "account_id"},
		{name: "other project", scope: withScope(func(s *AccountLeaseScope) { s.ProjectID = "proj-2" }), field: "project_id"},
		{name: "other job", scope: withScope(func(s *AccountLeaseScope) { s.JobID = "job-2" }), field: "job_id"},
		{name: "no project", scope: withScope(func(s *AccountLeaseScope) { s.ProjectID = "" }), field: "project_id"},
	}
	for _, tc := range scopeCases {
		t.Run("scope mismatch: "+tc.name, func(t *testing.T) {
			_, err := leaser.Validate(ctx, issued.LeaseID, tc.scope)
			var mismatch *AccountLeaseScopeError
			if !asErr(err, &mismatch) {
				t.Fatalf("Validate() error = %v, want AccountLeaseScopeError", err)
			}
			if mismatch.Field != tc.field {
				t.Fatalf("AccountLeaseScopeError.Field = %q, want %q", mismatch.Field, tc.field)
			}
		})
	}

	t.Run("agent identity is not a scope check", func(t *testing.T) {
		// The transport binds AgentID from the authenticated principal, so
		// comparing it here would compare a field against itself.
		other := withScope(func(s *AccountLeaseScope) { s.AgentID = "agent-2" })
		if _, err := leaser.Validate(ctx, issued.LeaseID, other); err != nil {
			t.Fatalf("Validate() with a different agent error = %v, want success", err)
		}
	})

	t.Run("no leaser configured", func(t *testing.T) {
		var unwired *accountCredentialLeaser
		_, err := unwired.Validate(ctx, "lease_x", testScope())
		var notConfigured *AccountLeaseNotConfiguredError
		if !asErr(err, &notConfigured) {
			t.Fatalf("Validate() on an unwired leaser error = %v, want AccountLeaseNotConfiguredError", err)
		}
	})
}

// =============================================================================
// Revoke
// =============================================================================

func TestAccountCredentialLeaser_Revoke_IsIdempotent(t *testing.T) {
	leaser, _, _ := newTestLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}
	if err := leaser.Revoke(ctx, issued.LeaseID); err != nil {
		t.Fatalf("first Revoke() error = %v", err)
	}
	// A retrying session conclusion must converge.
	if err := leaser.Revoke(ctx, issued.LeaseID); err != nil {
		t.Fatalf("second Revoke() error = %v, want a no-op", err)
	}
}

func TestAccountCredentialLeaser_Revoke_BadInput(t *testing.T) {
	leaser, _, _ := newTestLeaser(t)
	ctx := context.Background()

	t.Run("empty lease is nothing to withdraw", func(t *testing.T) {
		// A session that used no pooled account has no grant; treating that as
		// a failure would make it unconcludable.
		if err := leaser.Revoke(ctx, ""); err != nil {
			t.Fatalf("Revoke(\"\") error = %v, want nil", err)
		}
	})

	t.Run("unknown lease is nothing to withdraw", func(t *testing.T) {
		if err := leaser.Revoke(ctx, "lease_already_purged"); err != nil {
			t.Fatalf("Revoke() of an absent grant error = %v, want nil", err)
		}
	})

	t.Run("no leaser configured", func(t *testing.T) {
		var unwired *accountCredentialLeaser
		err := unwired.Revoke(ctx, "lease_x")
		var notConfigured *AccountLeaseNotConfiguredError
		if !asErr(err, &notConfigured) {
			t.Fatalf("Revoke() on an unwired leaser error = %v, want AccountLeaseNotConfiguredError", err)
		}
	})
}

// =============================================================================
// No secret material anywhere in the grant
// =============================================================================

// TestAccountCredentialLeaser_NoSecretInLeaseOrAudit is the leak guard. It
// issues a grant covering an account whose credential is a subscription OAuth
// envelope, then asserts that no field of that envelope appears in the lease
// record or in the audit trail.
func TestAccountCredentialLeaser_NoSecretInLeaseOrAudit(t *testing.T) {
	leaser, mgr, auditor := newTestLeaser(t)
	ctx := context.Background()

	// The values a subscription credential actually carries. None of them is an
	// API key: the credential is OAuth, materialized as a JSON file.
	secrets := []string{
		"oauth-access-token-value",
		"oauth-refresh-token-value",
		"oauth-client-id-value",
		`{"access_token":"oauth-access-token-value"}`,
	}

	issued, err := leaser.GrantDelivery(ctx, testScope())
	if err != nil {
		t.Fatalf("GrantDelivery() error = %v", err)
	}

	held, err := mgr.GetLease(ctx, issued.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}

	rendered := held.SecretPath
	for k, v := range held.Metadata {
		rendered += " " + k + "=" + v
	}
	for _, event := range auditor.all() {
		rendered += " " + event.TenantID + " " + event.Resource + " " + event.ResourceID + " " + event.Action
	}

	for _, secret := range secrets {
		if strings.Contains(rendered, secret) {
			t.Fatalf("credential material %q reached a lease record or audit event: %q", secret, rendered)
		}
	}

	// And the metadata is all identifiers, positively.
	if held.Metadata[accountLeaseMetaKind] != accountLeaseKind {
		t.Fatalf("lease kind = %q, want %q", held.Metadata[accountLeaseMetaKind], accountLeaseKind)
	}
	for _, want := range []struct{ key, value string }{
		{accountLeaseMetaTenantID, "tenant-a"},
		{accountLeaseMetaAccountID, "acct-1"},
		{accountLeaseMetaProjectID, "proj-1"},
		{accountLeaseMetaJobID, "job-1"},
		{accountLeaseMetaAgentID, "agent-1"},
		{accountLeaseMetaProvider, string(AccountProviderClaude)},
	} {
		if held.Metadata[want.key] != want.value {
			t.Fatalf("lease metadata[%s] = %q, want %q", want.key, held.Metadata[want.key], want.value)
		}
	}
}

// =============================================================================
// Error rendering
// =============================================================================

func TestAccountLeaseErrors_Messages(t *testing.T) {
	notConfigured := &AccountLeaseNotConfiguredError{Operation: "issue a delivery grant"}
	if !strings.Contains(notConfigured.Error(), "issue a delivery grant") {
		t.Fatalf("AccountLeaseNotConfiguredError = %q", notConfigured.Error())
	}

	mismatch := &AccountLeaseScopeError{LeaseID: "lease_a", Field: "job_id", Want: "job-1", Got: ""}
	msg := mismatch.Error()
	for _, want := range []string{"lease_a", "job_id", "job-1", "(none)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("AccountLeaseScopeError = %q, want it to name %q", msg, want)
		}
	}

	cause := &AccountStoreError{Message: "lease store down"}
	issue := &AccountLeaseIssueError{TenantID: "tenant-a", AccountID: "acct-1", Cause: cause}
	if !strings.Contains(issue.Error(), "lease store down") {
		t.Fatalf("AccountLeaseIssueError = %q", issue.Error())
	}
	if issue.Unwrap() != cause {
		t.Fatal("AccountLeaseIssueError.Unwrap() did not expose the cause")
	}
}

// =============================================================================
// Helpers
// =============================================================================

// asErr is errors.As with the argument order a table reads better in.
func asErr(err error, target any) bool {
	return stderrors.As(err, target)
}

// withScope returns testScope() with mutate applied.
func withScope(mutate func(*AccountLeaseScope)) AccountLeaseScope {
	scope := testScope()
	mutate(&scope)
	return scope
}

// expireLease rewrites a lease record so its TTL has already elapsed. It is how
// every expiry assertion in this package advances the clock: the enforcement
// path reads ExpiresAt, so a record already past it is the same fact a wall
// clock wait would eventually produce — without the wait.
func expireLease(t *testing.T, mgr lease.LeaseManager, leaseID string) {
	t.Helper()
	if _, err := mgr.RenewLease(context.Background(), leaseID, -time.Hour); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
}

// =============================================================================
// The backend's one revocation duty
// =============================================================================

// TestACPBackend_Spawn_WithdrawsTheGrantWhenDispatchFails closes the one gap the
// orchestrator cannot: a spawn whose account resolution succeeded and whose
// dispatch then failed returns no Session, so nothing downstream ever learns the
// grant. Without this the pool would carry a live delivery lease for a session
// that never existed.
func TestACPBackend_Spawn_WithdrawsTheGrantWhenDispatchFails(t *testing.T) {
	backend, store, secretStore, _, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()

	leaser := newTestLeaserForBackend(t, backend, store, secretStore)
	ctx := context.Background()
	seedSubscriptionAccount(t, store, secretStore, "tenant-a", "acct-1")

	// Make the dispatch fail AFTER resolution: the transport factory is the
	// last thing a spawn touches.
	backend.transportFactory = func(string, []string, string, []string) (*StdioTransport, error) {
		return nil, &AccountStoreError{Message: "transport refused"}
	}

	if _, err := backend.Spawn(ContextWithTenant(ctx, "tenant-a"), &SpawnRequest{
		WorkDir:   t.TempDir(),
		AccountID: "acct-1",
	}); err == nil {
		t.Fatal("Spawn() succeeded with a refusing transport")
	}

	// Exactly one grant was issued and it is no longer live.
	grants, err := leaser.issued(ctx)
	if err != nil {
		t.Fatalf("listing grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("issued grants = %d, want 1", len(grants))
	}
	if _, err := leaser.Validate(ctx, grants[0], AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-1"}); !lease.IsLeaseRevoked(err) {
		t.Fatalf("Validate after a failed dispatch error = %v, want a revoked refusal", err)
	}
}

func TestACPBackend_SetAccountCredentialLeaser_IsOptional(t *testing.T) {
	backend, store, secretStore, _, cleanup := newTestACPBackendForProvider(t, string(AccountProviderClaude))
	defer cleanup()
	ctx := context.Background()
	seedSubscriptionAccount(t, store, secretStore, "tenant-a", "acct-1")

	// No leaser wired: releaseAccountLease has nothing to do and a failing
	// dispatch must still report its own error rather than panicking.
	backend.transportFactory = func(string, []string, string, []string) (*StdioTransport, error) {
		return nil, &AccountStoreError{Message: "transport refused"}
	}
	if _, err := backend.Spawn(ContextWithTenant(ctx, "tenant-a"), &SpawnRequest{
		WorkDir:   t.TempDir(),
		AccountID: "acct-1",
	}); err == nil {
		t.Fatal("Spawn() succeeded with a refusing transport")
	}

	// And an empty grant is a no-op even once one is wired.
	leaser, _, _ := newTestLeaser(t)
	backend.SetAccountCredentialLeaser(leaser)
	backend.releaseAccountLease(ctx, "")
}

// backendLeaser is an AccountCredentialLeaser that also reports which grants it
// has issued, so a test can find a grant the production code never handed back.
type backendLeaser struct {
	AccountCredentialLeaser
	mgr lease.LeaseManager
}

func (l *backendLeaser) issued(ctx context.Context) ([]string, error) {
	leases, err := l.mgr.ListLeases(ctx, accountLeasePathPrefix)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(leases))
	for _, held := range leases {
		ids = append(ids, held.LeaseID)
	}
	return ids, nil
}

// newTestLeaserForBackend rebuilds the backend's resolver with a delivery leaser
// wired, the way the server does, and installs the leaser on the backend too.
func newTestLeaserForBackend(
	t *testing.T,
	backend *ACPBackend,
	store AccountStore,
	secretStore *AccountSecretStore,
) *backendLeaser {
	t.Helper()
	mgr := newTestLeaseManager(t)
	leaser := &backendLeaser{AccountCredentialLeaser: NewAccountCredentialLeaser(mgr, 0, nil), mgr: mgr}

	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(NewCostTracker(nil, nil))
	backend.SetAccountResolver(NewAccountResolver(store, tokens, strategies,
		WithAccountDeliveryGrantor(leaser)))
	backend.SetAccountCredentialLeaser(leaser)
	return leaser
}
