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

package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The daemon balances on the control plane's live-session count
// =============================================================================

func newBalancingClient(t *testing.T, central *fakeCentral) *AccountAPIClient {
	t.Helper()
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL,
		TenantID:   "tenant-a",
		HTTPClient: central.server.Client(),
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}
	return client
}

func TestAccountAPIClient_LiveSessionsReadsTheCentralCount(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("claude1", "claude", "", false)
	central.addAccount("claude2", "claude", "", false)
	central.live["claude1"] = 2

	live, err := newBalancingClient(t, central).LiveSessions(context.Background(), "tenant-a", []string{"claude1", "claude2"})
	if err != nil {
		t.Fatalf("LiveSessions() error = %v", err)
	}
	if live["claude1"] != 2 || live["claude2"] != 0 {
		t.Fatalf("LiveSessions() = %v, want claude1:2 claude2:0", live)
	}
}

func TestAccountAPIClient_LiveSessionsRefusesAnotherTenant(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	_, err := newBalancingClient(t, central).LiveSessions(context.Background(), "tenant-b", []string{"claude1"})
	var mismatch *AccountAPITenantMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want AccountAPITenantMismatchError", err)
	}
}

func TestAccountAPIClient_LiveSessionsFailsClosed(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("claude1", "claude", "", false)
	central.usageFails = true

	_, err := newBalancingClient(t, central).LiveSessions(context.Background(), "tenant-a", []string{"claude1"})
	var countErr *orchestration.AccountSessionCountError
	if !errors.As(err, &countErr) {
		t.Fatalf("error = %v, want AccountSessionCountError — an unread count is not zero", err)
	}
}

// TestAccountPool_DefaultSelectionSpreadsAcrossThePool is the daemon half of
// go-quicbaas#1046: an unpinned worker spawn on a host must not land on the
// default account while another sits idle.
func TestAccountPool_DefaultSelectionSpreadsAcrossThePool(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("claude1", "claude", "", false)
	central.addAccount("claude3", "claude", "", true)
	central.live["claude3"] = 1
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "claude1" {
		t.Fatalf("auto selection chose %q, want claude1 — the default is already serving a session", resolved.AccountID)
	}
	if resolved.Strategy != orchestration.LeastUsageAccountStrategyName {
		t.Fatalf("Strategy = %q, want the balanced default", resolved.Strategy)
	}
}

func TestAccountPool_ACappedPoolIsSaturated(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("claude1", "claude", "", true)
	account.record.MaxConcurrentSessions = 1
	central.live["claude1"] = 1
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true,
	})
	if !errors.Is(err, orchestration.ErrAccountsSaturated) {
		t.Fatalf("Resolve() error = %v, want the pool reported saturated", err)
	}
}

// =============================================================================
// The daemon fails over on the control plane's spend verdict (ruling F6)
// =============================================================================

// TestAccountPool_FailsOverPastASpentAccount asserts the daemon's resolver
// skips an account whose allowance the control plane reports spent, even
// though it is the least busy, and a pool spent whole is the typed deferral.
func TestAccountPool_FailsOverPastASpentAccount(t *testing.T) {
	freesAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("claude1", "claude", "", true)
	central.addAccount("claude2", "claude", "", false)
	central.live["claude2"] = 1
	central.spend = map[string]*orchestration.SpendHeadroom{
		"claude1": {Refusal: orchestration.SpendRefusalQuota, Window: "weekly", Used: 12, Limit: 10, FreesAt: &freesAt},
	}
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	req := orchestration.AccountResolveRequest{TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true}

	resolved, err := pool.Resolver.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "claude2" {
		t.Fatalf("resolved %q, want claude2 — claude1 is idle but its allowance is spent", resolved.AccountID)
	}

	central.mu.Lock()
	central.spend["claude2"] = &orchestration.SpendHeadroom{Refusal: orchestration.SpendRefusalBreaker, Failures: 3, FreesAt: &freesAt}
	central.mu.Unlock()
	_, err = pool.Resolver.Resolve(context.Background(), req)
	var spent *orchestration.AccountsSpentError
	if !errors.As(err, &spent) || !spent.AvailableAt.Equal(freesAt) {
		t.Fatalf("Resolve() error = %v, want the pool spent until %v", err, freesAt)
	}
}

// TestAccountAPIClient_CheckReadsTheCentralVerdict asserts Check admits with
// no verdict or an admitting one, rebuilds a refusal, and fails closed on an
// unreadable usage call or another tenant's account.
func TestAccountAPIClient_CheckReadsTheCentralVerdict(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("claude1", "claude", "", false)
	client := newBalancingClient(t, central)
	account := &orchestration.Account{ID: "claude1", TenantID: "tenant-a"}
	req := orchestration.SpendAuthorizeRequest{Account: account}
	now := time.Now().UTC()

	if err := client.Check(context.Background(), req, now); err != nil {
		t.Fatalf("Check() with no verdict = %v, want an admission: the control plane meters nothing", err)
	}
	central.spend = map[string]*orchestration.SpendHeadroom{"claude1": {Admits: true}}
	if err := client.Authorize(context.Background(), req, now); err != nil {
		t.Fatalf("Authorize() on an admitting verdict = %v", err)
	}
	central.spend["claude1"] = &orchestration.SpendHeadroom{Refusal: orchestration.SpendRefusalQuota, Window: "daily"}
	if err := client.Check(context.Background(), req, now); !errors.Is(err, orchestration.ErrAccountQuotaExhausted) {
		t.Fatalf("Check() on a spent verdict = %v, want the quota refusal", err)
	}

	central.usageFails = true
	if err := client.Check(context.Background(), req, now); err == nil {
		t.Fatal("Check() on an unreadable usage call = nil, want the failure")
	}
	var mismatch *AccountAPITenantMismatchError
	if err := client.Check(context.Background(), orchestration.SpendAuthorizeRequest{
		Account: &orchestration.Account{ID: "x", TenantID: "tenant-b"},
	}, now); !errors.As(err, &mismatch) {
		t.Fatalf("Check() on another tenant's account = %v, want a tenant mismatch", err)
	}
	if err := client.Check(context.Background(), orchestration.SpendAuthorizeRequest{}, now); err == nil {
		t.Fatal("Check() with no account = nil, want a configuration error")
	}
}
