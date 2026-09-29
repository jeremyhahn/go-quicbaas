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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// =============================================================================
// Credential release (go-quicbaas#734).
//
// The verb under test is the one place in the account surface where a secret
// leaves the server, so the tests are written as a REFUSAL MATRIX first and a
// happy path second: every state that must not release a credential is
// exercised, and the assertions check the envelope by SHAPE (which field is
// populated) rather than reproducing a token literal anywhere a log or a
// failure message could carry it.
// =============================================================================

// recordingReleaseAuditor captures the release events the service emits.
type recordingReleaseAuditor struct {
	mu     sync.Mutex
	events []orchestration.StoreAuditEvent
}

func (a *recordingReleaseAuditor) RecordStoreEvent(_ context.Context, event orchestration.StoreAuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *recordingReleaseAuditor) snapshot() []orchestration.StoreAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]orchestration.StoreAuditEvent(nil), a.events...)
}

// materializeFixture is a wired AccountService plus the pieces a test needs to
// reach around it (the store, to move an account into a refusing state; the
// auditor, to assert the trail).
type materializeFixture struct {
	svc     AccountService
	store   orchestration.AccountStore
	auditor *recordingReleaseAuditor
}

// newMaterializeFixture builds an AccountService with the token manager and
// auditor production wires in, over in-memory stores.
func newMaterializeFixture(t *testing.T) *materializeFixture {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := orchestration.NewInMemoryAccountStore()
	secretStore := orchestration.NewAccountSecretStore(mgr)
	svc := NewAccountService(store, secretStore)

	auditor := &recordingReleaseAuditor{}
	wirer, ok := svc.(interface {
		SetTokenManager(*orchestration.AccountTokenManager)
		SetAuditor(orchestration.StoreAuditor)
	})
	if !ok {
		t.Fatalf("account service does not expose the release wiring the server uses")
	}
	wirer.SetTokenManager(orchestration.NewAccountTokenManager(secretStore, store, 0))
	wirer.SetAuditor(auditor)

	return &materializeFixture{svc: svc, store: store, auditor: auditor}
}

// createAccount provisions one static-token account and returns its ID.
func (f *materializeFixture) createAccount(t *testing.T, tenantID, projectID string) string {
	t.Helper()
	resp, err := f.svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      tenantID,
		ProjectID:     projectID,
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "pool-seat",
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential: &orchestration.AccountCredential{
			AccessToken:       "release-me",
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return resp.Account.ID
}

// TestMaterialize_ReleasesTheEnvelopeToItsOwnTenant is the happy path: the
// account's own tenant gets the envelope, with the metadata a cache needs.
func TestMaterialize_ReleasesTheEnvelopeToItsOwnTenant(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	resp, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID:  "tenant-a",
		AccountID: accountID,
		AgentID:   "agent-agent-04",
	})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.Credential == nil {
		t.Fatal("release returned no credential envelope")
	}
	// Shape, not value: the token must be present and projected into the
	// variable the provider CLI reads.
	if resp.Credential.AccessToken == "" {
		t.Error("released envelope carries no access token")
	}
	if resp.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("released envelope projects into %q, want CLAUDE_CODE_OAUTH_TOKEN",
			resp.Credential.AccessTokenEnvKey)
	}
	if resp.AccountID != accountID {
		t.Errorf("released account %q, want %q", resp.AccountID, accountID)
	}
	if resp.UpdatedAt.IsZero() {
		t.Error("release reported no UpdatedAt; a cache has no invalidation signal")
	}
	if resp.Provider != orchestration.AccountProviderClaude {
		t.Errorf("released provider %q, want claude", resp.Provider)
	}
}

// TestMaterialize_StampsUsageOnRelease proves the release IS the use: the
// central's bookkeeping advances even though the spawn happens elsewhere.
func TestMaterialize_StampsUsageOnRelease(t *testing.T) {
	f := newMaterializeFixture(t)
	ctx := context.Background()
	accountID := f.createAccount(t, "tenant-a", "")

	before, err := f.store.Get(ctx, "tenant-a", accountID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !before.LastUsedAt.IsZero() {
		t.Fatalf("test setup: a freshly created account already reports use")
	}

	if _, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	}); err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}

	after, err := f.store.Get(ctx, "tenant-a", accountID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if after.LastUsedAt.IsZero() {
		t.Error("release did not stamp LastUsedAt; a remote pool would report every account as never used")
	}
	if after.LastValidatedAt.IsZero() {
		t.Error("release did not stamp LastValidatedAt")
	}
}

// TestMaterialize_EmitsOneAuditEventNamingAgentAndAccount pins the trail.
func TestMaterialize_EmitsOneAuditEventNamingAgentAndAccount(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	if _, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-agent-04",
	}); err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}

	events := f.auditor.snapshot()
	// The create above emits its own store event; the release is the last one.
	if len(events) == 0 {
		t.Fatal("release emitted no audit event")
	}
	release := events[len(events)-1]
	switch {
	case release.Action != orchestration.AuditActionUse:
		t.Errorf("release audited as %q, want %q", release.Action, orchestration.AuditActionUse)
	case release.TenantID != "tenant-a":
		t.Errorf("release audited under tenant %q, want tenant-a", release.TenantID)
	case !release.Success:
		t.Error("successful release audited as a failure")
	case !strings.Contains(release.ResourceID, accountID):
		t.Errorf("release audit %q does not name the account", release.ResourceID)
	case !strings.Contains(release.ResourceID, "agent-agent-04"):
		t.Errorf("release audit %q does not name the agent", release.ResourceID)
	}
}

// TestMaterialize_AuditsARefusal proves a refused release is on the trail too:
// an agent repeatedly asking for a revoked account is what an operator wants
// to see.
func TestMaterialize_AuditsARefusal(t *testing.T) {
	f := newMaterializeFixture(t)
	ctx := context.Background()
	accountID := f.createAccount(t, "tenant-a", "")

	if _, err := f.svc.RevokeAccount(ctx, &RevokeAccountRequest{
		TenantID: "tenant-a", AccountID: accountID, Reason: "seat cancelled",
	}); err != nil {
		t.Fatalf("RevokeAccount() error = %v", err)
	}

	if _, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	}); err == nil {
		t.Fatal("released the credential of a revoked account")
	}

	events := f.auditor.snapshot()
	release := events[len(events)-1]
	if release.Success {
		t.Error("refused release audited as a success")
	}
	if !strings.Contains(release.ResourceID, accountID) {
		t.Errorf("refusal audit %q does not name the account", release.ResourceID)
	}
}

// TestMaterialize_RefusesAnotherTenantsAccount is the isolation case: the
// account exists, but not in the caller's tenant, and the answer must not
// distinguish the two (it is a not-found, not a "wrong tenant" oracle).
func TestMaterialize_RefusesAnotherTenantsAccount(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	_, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-b", AccountID: accountID, AgentID: "agent-b",
	})
	var notFound *AccountNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("cross-tenant release error = %v (%T), want *AccountNotFoundError", err, err)
	}
}

// TestMaterialize_RefusesNonSelectableStates walks the refusal matrix: an
// account the pool has judged unusable is never released, whichever way it
// became unusable.
func TestMaterialize_RefusesNonSelectableStates(t *testing.T) {
	cases := []struct {
		name   string
		status orchestration.AccountStatus
		hold   time.Time
	}{
		{name: "revoked", status: orchestration.AccountStatusRevoked},
		{name: "errored", status: orchestration.AccountStatusError},
		{name: "limit hold in force", status: orchestration.AccountStatusLimited, hold: time.Now().UTC().Add(time.Hour)},
		{name: "indefinite limit hold", status: orchestration.AccountStatusLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaterializeFixture(t)
			ctx := context.Background()
			accountID := f.createAccount(t, "tenant-a", "")

			if err := f.store.Touch(ctx, "tenant-a", accountID, orchestration.TouchAccountOptions{
				SetStatusValid:      true,
				SetStatus:           tc.status,
				SetAvailableAt:      tc.hold,
				SetAvailableAtValid: true,
			}); err != nil {
				t.Fatalf("Touch() error = %v", err)
			}

			_, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
				TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
			})
			var notReleasable *AccountNotReleasableError
			if !errors.As(err, &notReleasable) {
				t.Fatalf("release error = %v (%T), want *AccountNotReleasableError", err, err)
			}
			if notReleasable.Status != tc.status {
				t.Errorf("refusal reports status %q, want %q", notReleasable.Status, tc.status)
			}
		})
	}
}

// TestMaterialize_ReleasesALapsedLimitHold is the other side of the matrix: a
// limited account whose hold has expired is back in the pool and IS released.
// Refusing it would strand a fleet behind a hold nobody has to act on.
func TestMaterialize_ReleasesALapsedLimitHold(t *testing.T) {
	f := newMaterializeFixture(t)
	ctx := context.Background()
	accountID := f.createAccount(t, "tenant-a", "")

	if err := f.store.Touch(ctx, "tenant-a", accountID, orchestration.TouchAccountOptions{
		SetStatusValid:      true,
		SetStatus:           orchestration.AccountStatusLimited,
		SetAvailableAt:      time.Now().UTC().Add(-time.Minute),
		SetAvailableAtValid: true,
	}); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}

	resp, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("release of a lapsed hold error = %v", err)
	}
	if resp.Credential == nil || resp.Credential.AccessToken == "" {
		t.Fatal("release of a lapsed hold returned no usable envelope")
	}
}

// TestMaterialize_HonoursTheProjectFence proves a project-scoped account is
// released only into its own project.
func TestMaterialize_HonoursTheProjectFence(t *testing.T) {
	f := newMaterializeFixture(t)
	ctx := context.Background()
	accountID := f.createAccount(t, "tenant-a", "project-x")

	t.Run("matching project releases", func(t *testing.T) {
		resp, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
			TenantID: "tenant-a", AccountID: accountID, ProjectID: "project-x", AgentID: "agent-1",
		})
		if err != nil {
			t.Fatalf("in-project release error = %v", err)
		}
		if resp.Credential == nil {
			t.Fatal("in-project release returned no envelope")
		}
	})

	t.Run("other project refused", func(t *testing.T) {
		_, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
			TenantID: "tenant-a", AccountID: accountID, ProjectID: "project-y", AgentID: "agent-1",
		})
		var scope *AccountProjectScopeError
		if !errors.As(err, &scope) {
			t.Fatalf("cross-project release error = %v (%T), want *AccountProjectScopeError", err, err)
		}
	})

	t.Run("no project refused", func(t *testing.T) {
		_, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
			TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
		})
		var scope *AccountProjectScopeError
		if !errors.As(err, &scope) {
			t.Fatalf("unscoped release of a fenced account error = %v (%T), want *AccountProjectScopeError", err, err)
		}
	})
}

// TestMaterialize_TenantWideAccountReleasesIntoAnyProject proves the fence only
// applies where an operator set one.
func TestMaterialize_TenantWideAccountReleasesIntoAnyProject(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	resp, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, ProjectID: "any-project", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("release of a tenant-wide account into a project error = %v", err)
	}
	if resp.Credential == nil {
		t.Fatal("release returned no envelope")
	}
}

// TestMaterialize_RejectsIncompleteRequests covers the bad-input path for each
// required identifier.
func TestMaterialize_RejectsIncompleteRequests(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	cases := []struct {
		name  string
		req   *MaterializeAccountCredentialRequest
		field string
	}{
		{name: "nil request", req: nil, field: "tenant_id"},
		{name: "no tenant", req: &MaterializeAccountCredentialRequest{AccountID: accountID}, field: "tenant_id"},
		{name: "no account", req: &MaterializeAccountCredentialRequest{TenantID: "tenant-a"}, field: "account_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.MaterializeAccountCredential(context.Background(), tc.req)
			var invalid *AccountInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v (%T), want *AccountInvalidError", err, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("error names field %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// TestMaterialize_FailsClosedWithoutATokenManager proves an unwired deployment
// refuses rather than reading the stored envelope directly — which would hand
// out a credential the token manager would have renewed first.
func TestMaterialize_FailsClosedWithoutATokenManager(t *testing.T) {
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := orchestration.NewInMemoryAccountStore()
	svc := NewAccountService(store, orchestration.NewAccountSecretStore(mgr))

	created, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential:    &orchestration.AccountCredential{AccessToken: "tok", AccessTokenEnvKey: "K"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	_, err = svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: created.Account.ID,
	})
	var noTokens *AccountTokenManagerNotConfiguredError
	if !errors.As(err, &noTokens) {
		t.Fatalf("error = %v (%T), want *AccountTokenManagerNotConfiguredError", err, err)
	}
}

// TestAccountReleaseSubject_MarksAnUnattributedRelease proves an audit entry
// never silently drops the agent half of the pair.
func TestAccountReleaseSubject_MarksAnUnattributedRelease(t *testing.T) {
	attributed := accountReleaseSubject(&MaterializeAccountCredentialRequest{AccountID: "acct-1", AgentID: "agent-1"})
	if !strings.Contains(attributed, "agent-1") {
		t.Errorf("attributed subject %q does not name the agent", attributed)
	}
	unattributed := accountReleaseSubject(&MaterializeAccountCredentialRequest{AccountID: "acct-1"})
	if !strings.Contains(unattributed, "unattributed") {
		t.Errorf("unattributed subject %q does not mark the gap", unattributed)
	}
}

// TestAccountNotReleasableError_ReportsTheHoldWindow proves the refusal tells a
// caller when to come back rather than only that it was refused.
func TestAccountNotReleasableError_ReportsTheHoldWindow(t *testing.T) {
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	held := (&AccountNotReleasableError{
		TenantID: "t", AccountID: "a", Status: orchestration.AccountStatusLimited, AvailableAt: at,
	}).Error()
	if !strings.Contains(held, "2026-09-09T12:00:00Z") {
		t.Errorf("held refusal %q does not report the return time", held)
	}
	revoked := (&AccountNotReleasableError{
		TenantID: "t", AccountID: "a", Status: orchestration.AccountStatusRevoked,
	}).Error()
	if strings.Contains(revoked, "until") {
		t.Errorf("revoked refusal %q reports a return time it does not have", revoked)
	}
}

// TestAccountProjectScopeError_NamesBothProjects proves the refusal is
// actionable: it says which fence and which request.
func TestAccountProjectScopeError_NamesBothProjects(t *testing.T) {
	msg := (&AccountProjectScopeError{
		AccountID: "acct-1", AccountProjectID: "project-x", RequestProjectID: "project-y",
	}).Error()
	if !strings.Contains(msg, "project-x") || !strings.Contains(msg, "project-y") {
		t.Errorf("scope refusal %q does not name both projects", msg)
	}
	unscoped := (&AccountProjectScopeError{AccountID: "acct-1", AccountProjectID: "project-x"}).Error()
	if !strings.Contains(unscoped, "(none)") {
		t.Errorf("unscoped refusal %q does not mark the absent request project", unscoped)
	}
}
