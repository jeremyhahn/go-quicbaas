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

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fixedSessionLookup answers a fixed live-session count, or fails.
type fixedSessionLookup struct {
	live map[string]int
	err  error
}

func (f *fixedSessionLookup) LiveSessions(_ context.Context, _ string, ids []string) (map[string]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		out[id] = f.live[id]
	}
	return out, nil
}

func createCappedAccount(t *testing.T, svc AccountService, maxSessions int) *orchestration.Account {
	t.Helper()
	created, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Credential:            &orchestration.AccountCredential{AccessToken: "x"},
		MaxConcurrentSessions: maxSessions,
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return created.Account
}

func TestAccountService_SessionCapIsSetAtCreateAndChangedOnlyWhenAsked(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	account := createCappedAccount(t, svc, 2)
	if account.MaxConcurrentSessions != 2 {
		t.Fatalf("created cap = %d, want 2", account.MaxConcurrentSessions)
	}

	// An update that does not name the cap leaves it alone.
	renamed, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: account.ID, DisplayName: "seat"})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if renamed.Account.MaxConcurrentSessions != 2 {
		t.Fatalf("cap after an unrelated update = %d, want 2", renamed.Account.MaxConcurrentSessions)
	}

	// Zero is a value an operator sets on purpose: unmetered.
	zero := 0
	cleared, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: account.ID, MaxConcurrentSessions: &zero})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if cleared.Account.MaxConcurrentSessions != 0 {
		t.Fatalf("cap after clearing = %d, want 0", cleared.Account.MaxConcurrentSessions)
	}
}

func TestAccountService_ANegativeSessionCapIsRefused(t *testing.T) {
	svc := newTestAccountService(t)
	if _, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Credential:            &orchestration.AccountCredential{AccessToken: "x"},
		MaxConcurrentSessions: -1,
	}); err == nil {
		t.Fatal("CreateAccount() accepted a negative session cap")
	}

	account := createCappedAccount(t, svc, 1)
	negative := -2
	if _, err := svc.UpdateAccount(context.Background(), &UpdateAccountRequest{
		TenantID: "tenant-a", AccountID: account.ID, MaxConcurrentSessions: &negative,
	}); err == nil {
		t.Fatal("UpdateAccount() accepted a negative session cap")
	}
}

// Reserve answers an unmetered reservation; the reservation verbs have their
// own tests over the real table.
func (f *fixedSessionLookup) Reserve(context.Context, string, orchestration.SessionReservationRequest) (string, error) {
	return "", f.err
}

// Release accepts every release.
func (f *fixedSessionLookup) Release(context.Context, string, string, string) error { return f.err }

func newUsageService(t *testing.T, sessions orchestration.AccountSessionReserver) (*accountServiceImpl, *orchestration.Account) {
	t.Helper()
	svc := newTestAccountService(t).(*accountServiceImpl)
	svc.SetUsageTracker(orchestration.NewCostTracker(kvstore.NewMemory(), noopLogger{}))
	if sessions != nil {
		svc.SetSessionReserver(sessions)
	}
	return svc, createCappedAccount(t, svc, 0)
}

func TestAccountService_GetAccountUsageReportsLiveSessions(t *testing.T) {
	lookup := &fixedSessionLookup{live: map[string]int{}}
	svc, account := newUsageService(t, lookup)
	lookup.live[account.ID] = 3

	resp, err := svc.GetAccountUsage(context.Background(), &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: account.ID})
	if err != nil {
		t.Fatalf("GetAccountUsage() error = %v", err)
	}
	if resp.LiveSessions != 3 {
		t.Fatalf("LiveSessions = %d, want 3", resp.LiveSessions)
	}
}

func TestAccountService_GetAccountUsageWithNoSessionLookupReportsZero(t *testing.T) {
	svc, account := newUsageService(t, nil)
	resp, err := svc.GetAccountUsage(context.Background(), &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: account.ID})
	if err != nil {
		t.Fatalf("GetAccountUsage() error = %v", err)
	}
	if resp.LiveSessions != 0 {
		t.Fatalf("LiveSessions = %d, want 0 with no session store", resp.LiveSessions)
	}
}

func TestAccountService_GetAccountUsageFailsOnAnUnreadableCount(t *testing.T) {
	boom := errors.New("session store down")
	svc, account := newUsageService(t, &fixedSessionLookup{err: boom})
	_, err := svc.GetAccountUsage(context.Background(), &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: account.ID})
	if !errors.Is(err, boom) {
		t.Fatalf("GetAccountUsage() error = %v, want the count failure — a daemon balancing on a guessed zero piles onto this account", err)
	}
}

// verdictSpend answers every spend check with err.
type verdictSpend struct{ err error }

func (v verdictSpend) Authorize(context.Context, orchestration.SpendAuthorizeRequest, time.Time) error {
	return v.err
}

func (v verdictSpend) Check(context.Context, orchestration.SpendAuthorizeRequest, time.Time) error {
	return v.err
}

// TestAccountService_GetAccountUsageCarriesTheSpendVerdict asserts the usage
// verb reports the spend gate's verdict — the figure a worker daemon fails
// over on (go-quicbaas#1058, ruling F6) — and none when no gate is wired.
func TestAccountService_GetAccountUsageCarriesTheSpendVerdict(t *testing.T) {
	svc, account := newUsageService(t, nil)
	req := &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: account.ID}

	resp, err := svc.GetAccountUsage(context.Background(), req)
	if err != nil {
		t.Fatalf("GetAccountUsage() error = %v", err)
	}
	if resp.Spend != nil {
		t.Fatalf("Spend = %+v with no gate wired, want nil: nothing is metered", resp.Spend)
	}

	freesAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	svc.SetSpendAuthority(verdictSpend{err: &orchestration.AccountQuotaExhaustedError{
		AccountID: account.ID, Window: "weekly", Axis: orchestration.QuotaAxisTokens, Used: 12, Limit: 10, RetryAt: freesAt,
	}})
	resp, err = svc.GetAccountUsage(context.Background(), req)
	if err != nil {
		t.Fatalf("GetAccountUsage() error = %v", err)
	}
	if resp.Spend == nil || resp.Spend.Admits || resp.Spend.Refusal != orchestration.SpendRefusalQuota ||
		resp.Spend.Window != "weekly" || resp.Spend.FreesAt == nil || !resp.Spend.FreesAt.Equal(freesAt) {
		t.Fatalf("Spend = %+v, want the spent weekly window", resp.Spend)
	}
}

// TestAccountService_GetAccountUsageFailsWhenTheGateCannotAnswer asserts an
// unreadable ledger is an error, never a verdict of headroom.
func TestAccountService_GetAccountUsageFailsWhenTheGateCannotAnswer(t *testing.T) {
	boom := errors.New("ledger unreadable")
	svc, account := newUsageService(t, nil)
	svc.SetSpendAuthority(verdictSpend{err: boom})
	if _, err := svc.GetAccountUsage(context.Background(), &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: account.ID}); !errors.Is(err, boom) {
		t.Fatalf("GetAccountUsage() error = %v, want the gate's failure", err)
	}
}
