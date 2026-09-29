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
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// THE SERVER-SIDE ENFORCEMENT POINT.
//
// MaterializeAccountCredential is the only surface in the platform whose
// response body is a live provider login, so it is the only place a control over
// that login has to hold. These tests establish exactly what the control is, and
// deliberately do not overclaim it:
//
//   - A REFRESH of a copy a host already holds presents the grant it was
//     released under, and is REFUSED once that grant has been revoked or has
//     lapsed. That is the case that matters: the daemon's cache presents its
//     grant on every refresh (pkg/fleet/account_secrets.go), so a revoked grant
//     means the copy in that process ages out with nothing able to replace it.
//   - A FIRST release presents no grant and is governed by the account's own
//     status, project fence and spend cap — the checks that already existed.
//     Revoking one delivery does not, and must not, disable the account.
// =============================================================================

// withDeliveryLeases wires a real delivery leaser onto a materialize fixture and
// returns it, so the test can issue and revoke grants the way the session reaper
// does.
func withDeliveryLeases(t *testing.T, f *materializeFixture) (orchestration.AccountCredentialLeaser, lease.LeaseManager) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}
	mgr, err := lease.NewManager(&lease.ManagerConfig{Store: kvstore.NewMemory(), Logger: logger.NewAdapter(log)})
	if err != nil {
		t.Fatalf("lease.NewManager() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	leaser := orchestration.NewAccountCredentialLeaser(mgr, 0, nil)
	wirer, ok := f.svc.(interface {
		SetAccountCredentialLeaser(orchestration.AccountCredentialLeaser)
	})
	if !ok {
		t.Fatal("account service does not expose the delivery-lease wiring the server uses")
	}
	wirer.SetAccountCredentialLeaser(leaser)
	return leaser, mgr
}

func TestMaterialize_IssuesADeliveryGrant(t *testing.T) {
	f := newMaterializeFixture(t)
	_, mgr := withDeliveryLeases(t, f)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	resp, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID:  "tenant-a",
		AccountID: accountID,
		ProjectID: "",
		JobID:     "job-1",
		AgentID:   "agent-1",
	})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.LeaseID == "" {
		t.Fatal("a release handed over a credential with no delivery grant")
	}
	if resp.LeaseExpiresAt.Before(time.Now()) {
		t.Fatalf("LeaseExpiresAt = %v, already past", resp.LeaseExpiresAt)
	}
	if resp.Credential == nil || resp.Credential.AccessToken == "" {
		t.Fatal("the release withheld the credential")
	}

	held, err := mgr.GetLease(ctx, resp.LeaseID)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	wantPath, _ := orchestration.AccountLeasePath(orchestration.AccountLeaseScope{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1",
	})
	if held.SecretPath != wantPath {
		t.Fatalf("grant path = %q, want %q", held.SecretPath, wantPath)
	}
}

// TestMaterialize_RefusesARefreshUnderARevokedGrant is the load-bearing proof:
// after the session's ending revokes the grant, the host holding that copy
// cannot obtain another one — refused SERVER-SIDE, from the lease store.
func TestMaterialize_RefusesARefreshUnderARevokedGrant(t *testing.T) {
	f := newMaterializeFixture(t)
	leaser, _ := withDeliveryLeases(t, f)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	first, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("first release error = %v", err)
	}

	// A refresh under the live grant still works: the control is revocation,
	// not a one-shot.
	if _, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
		LeaseID: first.LeaseID,
	}); err != nil {
		t.Fatalf("refresh under a live grant error = %v", err)
	}

	// The session ends; the reaper withdraws the grant.
	if err := leaser.Revoke(ctx, first.LeaseID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	refused, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
		LeaseID: first.LeaseID,
	})
	if err == nil {
		t.Fatal("a refresh under a revoked grant was released")
	}
	if refused != nil {
		t.Fatal("the refusal came with a credential attached")
	}
	if !lease.IsLeaseRevoked(err) {
		t.Fatalf("refusal = %v, want a revoked-grant refusal", err)
	}
}

// TestMaterialize_RefusesARefreshUnderALapsedGrant proves expiry is ENFORCED
// rather than recorded: nothing swept the grant, its ExpiresAt simply passed.
func TestMaterialize_RefusesARefreshUnderALapsedGrant(t *testing.T) {
	f := newMaterializeFixture(t)
	_, mgr := withDeliveryLeases(t, f)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	first, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("first release error = %v", err)
	}

	// Move the grant's expiry into the past without waiting on a clock and
	// without running the reaper: enforcement must not depend on either.
	if _, err := mgr.RenewLease(ctx, first.LeaseID, -time.Hour); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}

	if _, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
		LeaseID: first.LeaseID,
	}); !lease.IsLeaseExpired(err) {
		t.Fatalf("refusal = %v, want an expired-grant refusal", err)
	}
}

func TestMaterialize_RefusesAGrantFromAnotherScope(t *testing.T) {
	f := newMaterializeFixture(t)
	_, _ = withDeliveryLeases(t, f)
	accountA := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	first, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountA, JobID: "job-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("first release error = %v", err)
	}

	// A grant for job-1 is not a grant for job-2. Widening it would make one
	// authorised delivery unlock every other one.
	_, err = f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountA, JobID: "job-2", AgentID: "agent-1",
		LeaseID: first.LeaseID,
	})
	var scopeErr *orchestration.AccountLeaseScopeError
	if !stderrors.As(err, &scopeErr) {
		t.Fatalf("refusal = %v, want AccountLeaseScopeError", err)
	}
	if scopeErr.Field != "job_id" {
		t.Fatalf("AccountLeaseScopeError.Field = %q, want job_id", scopeErr.Field)
	}
}

// TestMaterialize_RevokingOneDeliveryDoesNotDisableTheAccount states the LIMIT
// of the control, so nobody reads more into it than it provides. A withdrawn
// grant stops the host that held that copy; the account itself is still usable,
// and disabling it is the account-status control's job.
func TestMaterialize_RevokingOneDeliveryDoesNotDisableTheAccount(t *testing.T) {
	f := newMaterializeFixture(t)
	leaser, _ := withDeliveryLeases(t, f)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	first, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("first release error = %v", err)
	}
	if err := leaser.Revoke(ctx, first.LeaseID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	second, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-2", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("a NEW delivery for a different job was refused: %v", err)
	}
	if second.LeaseID == first.LeaseID {
		t.Fatal("a new delivery reused a revoked grant")
	}
}

// TestMaterialize_UnleasedDeploymentStillReleases keeps the pre-existing
// behaviour reachable, and pins the deliberate asymmetry: a server with no lease
// manager cannot check a presented grant, so it must not pretend to — refusing
// would let a caller obtain a release simply by omitting the field.
func TestMaterialize_UnleasedDeploymentStillReleases(t *testing.T) {
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	resp, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
		LeaseID: "lease_from_somewhere_else",
	})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.LeaseID != "" {
		t.Fatalf("LeaseID = %q with no leaser wired, want empty", resp.LeaseID)
	}
	if resp.Credential == nil {
		t.Fatal("an unleased deployment must still release the envelope")
	}
}

// TestMaterialize_AuditNamesTheGrantAndNeverTheCredential is the leak guard for
// the audit trail.
func TestMaterialize_AuditNamesTheGrantAndNeverTheCredential(t *testing.T) {
	f := newMaterializeFixture(t)
	_, _ = withDeliveryLeases(t, f)
	accountID := f.createAccount(t, "tenant-a", "")
	ctx := context.Background()

	first, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("first release error = %v", err)
	}
	if _, err := f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, JobID: "job-1", AgentID: "agent-1",
		LeaseID: first.LeaseID,
	}); err != nil {
		t.Fatalf("refresh error = %v", err)
	}

	var rendered strings.Builder
	var namedTheGrant bool
	for _, event := range f.auditor.snapshot() {
		rendered.WriteString(event.TenantID + " " + event.Resource + " " + event.ResourceID + " " + event.Action + "\n")
		if strings.Contains(event.ResourceID, first.LeaseID) {
			namedTheGrant = true
		}
	}
	if !namedTheGrant {
		t.Fatalf("no audit event named the grant a release was made under:\n%s", rendered.String())
	}
	// "release-me" is the access token newMaterializeFixture provisions.
	if strings.Contains(rendered.String(), "release-me") {
		t.Fatalf("the audit trail carried credential material:\n%s", rendered.String())
	}
	if !strings.Contains(rendered.String(), "for job job-1") {
		t.Fatalf("the audit trail does not name the job the release served:\n%s", rendered.String())
	}
}

// =============================================================================
// The facade accessor (go-quicbaas#902)
// =============================================================================

// TestQuicBaaS_DynamicSecretsIsNilUntilWired is the defect this issue names,
// captured as a test so it cannot come back: the facade declared the field,
// returned it, and nothing ever assigned it.
func TestQuicBaaS_DynamicSecretsIsNilUntilWired(t *testing.T) {
	svc := createTestService(t)
	defer func() { _ = svc.Close() }()

	if svc.DynamicSecrets() != nil {
		t.Fatal("DynamicSecrets() is non-nil on an unwired facade; this test no longer pins anything")
	}

	handler, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store:             newDSStore(),
		LeaseManager:      newMockDSLeaseManager(),
		GeneratorRegistry: credentials.NewGeneratorRegistry(),
		Logger:            newMockDSLogger(),
	})
	if err != nil {
		t.Fatalf("NewDynamicSecretsHandler() error = %v", err)
	}

	wirer, ok := svc.(interface {
		SetDynamicSecretsService(DynamicSecretsService)
	})
	if !ok {
		t.Fatal("the facade does not expose SetDynamicSecretsService")
	}
	wirer.SetDynamicSecretsService(handler)

	if svc.DynamicSecrets() == nil {
		t.Fatal("DynamicSecrets() is still nil after wiring")
	}

	// A nil must not un-wire a working capability: every transport resolves the
	// accessor and would dereference the result.
	wirer.SetDynamicSecretsService(nil)
	if svc.DynamicSecrets() == nil {
		t.Fatal("SetDynamicSecretsService(nil) cleared a configured capability")
	}
}
