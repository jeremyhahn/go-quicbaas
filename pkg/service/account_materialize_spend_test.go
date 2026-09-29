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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The remote half of the cap
// =============================================================================
//
// MaterializeAccountCredential hands a live paid credential to a host the
// central does not run, and the worker caches it and spends against it
// without asking again. That makes an unmetered release here the most
// expensive one in the system, and it is why the cap is checked by the same
// authority the in-process spawn is checked by rather than by a second rule
// written for this path.

// spendGatedFixture wires a spend authority into the release path and returns
// the ledger it decides from.
func spendGatedFixture(t *testing.T) (*materializeFixture, orchestration.ConsumptionLedger) {
	t.Helper()
	f := newMaterializeFixture(t)
	kv := orchestration.NewMemoryTaskKV()
	ledger := orchestration.NewConsumptionLedger(kv)
	authority, err := orchestration.NewSpendAuthority(ledger, kv, "node-a")
	if err != nil {
		t.Fatalf("NewSpendAuthority() error = %v", err)
	}
	wirer, ok := f.svc.(interface {
		SetSpendAuthority(orchestration.SpendAuthority)
	})
	if !ok {
		t.Fatal("account service does not expose the spend wiring the server uses")
	}
	wirer.SetSpendAuthority(authority)
	return f, ledger
}

// meterAccount attaches a token cap to an already-created account.
func meterAccount(t *testing.T, f *materializeFixture, tenantID, accountID string, maxTokens int64) {
	t.Helper()
	ctx := context.Background()
	account, err := f.store.Get(ctx, tenantID, accountID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	account.Spend = orchestration.AccountSpendPolicy{
		Quota: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: maxTokens},
		}},
	}
	if err := f.store.Update(ctx, account); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

// spendAgainst records consumption for the account inside the live window.
func spendAgainst(t *testing.T, ledger orchestration.ConsumptionLedger, tenantID, accountID string, tokens int64) {
	t.Helper()
	if err := ledger.Record(context.Background(), orchestration.ConsumptionEntry{
		EntryID:     "e1",
		TenantID:    tenantID,
		AccountID:   accountID,
		Engine:      "claude",
		Model:       "claude-opus-5",
		Outcome:     orchestration.ConsumptionOutcomeSuccess,
		InputTokens: tokens,
		OccurredAt:  time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestMaterialize_ReleasesAnAccountUnderItsCap(t *testing.T) {
	f, ledger := spendGatedFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")
	meterAccount(t, f, "tenant-a", accountID, 1000)
	spendAgainst(t, ledger, "tenant-a", accountID, 100)

	resp, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() under the cap error = %v", err)
	}
	if resp.Credential == nil || resp.Credential.AccessToken == "" {
		t.Error("release returned no envelope; the gate must not alter what an admitted release gets")
	}
}

func TestMaterialize_RefusesAnAccountOverItsCap(t *testing.T) {
	f, ledger := spendGatedFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")
	meterAccount(t, f, "tenant-a", accountID, 1000)
	spendAgainst(t, ledger, "tenant-a", accountID, 1500)

	resp, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	})
	if !errors.Is(err, orchestration.ErrAccountQuotaExhausted) {
		t.Fatalf("MaterializeAccountCredential() over the cap error = %v, want a quota refusal", err)
	}
	if resp != nil {
		t.Fatal("a refused release returned a response; the response body IS the secret")
	}

	// One audit event per attempt, refusals included: an agent repeatedly
	// asking for an account that is over its cap is exactly the pattern an
	// operator wants to see.
	events := f.auditor.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1 for one refused release", len(events))
	}
	if events[0].Success {
		t.Error("the refused release was audited as a success")
	}
}

func TestMaterialize_RefusesAnAccountWhoseBreakerTripped(t *testing.T) {
	f, ledger := spendGatedFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")

	ctx := context.Background()
	account, err := f.store.Get(ctx, "tenant-a", accountID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	account.Spend = orchestration.AccountSpendPolicy{
		Breaker: &orchestration.BreakerPolicy{
			Window: 24 * time.Hour, FailureThreshold: 2, Cooldown: 10 * time.Minute,
		},
	}
	if err := f.store.Update(ctx, account); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	now := time.Now().UTC()
	for i, at := range []time.Time{now.Add(-2 * time.Minute), now.Add(-time.Minute)} {
		if recErr := ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID:    "f" + string(rune('1'+i)),
			TenantID:   "tenant-a",
			AccountID:  accountID,
			Engine:     "claude",
			Model:      "claude-opus-5",
			Outcome:    orchestration.ConsumptionOutcomeFailure,
			OccurredAt: at,
		}); recErr != nil {
			t.Fatalf("Record() error = %v", recErr)
		}
	}

	_, err = f.svc.MaterializeAccountCredential(ctx, &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	})
	if !errors.Is(err, orchestration.ErrAccountBreakerOpen) {
		t.Fatalf("MaterializeAccountCredential() on a tripped breaker error = %v, want ErrAccountBreakerOpen", err)
	}
}

func TestMaterialize_WithoutAnAuthorityIsUnmetered(t *testing.T) {
	// The deployment with no ledger. It must keep releasing: a gate that
	// failed closed when it was absent would take every pooled credential
	// offline on upgrade.
	f := newMaterializeFixture(t)
	accountID := f.createAccount(t, "tenant-a", "")
	meterAccount(t, f, "tenant-a", accountID, 1)

	if _, err := f.svc.MaterializeAccountCredential(context.Background(), &MaterializeAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: accountID, AgentID: "agent-1",
	}); err != nil {
		t.Fatalf("MaterializeAccountCredential() with no authority wired error = %v", err)
	}
}
