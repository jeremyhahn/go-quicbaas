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
	"testing"
)

func TestStaticAccountStrategy_PrefersPinned(t *testing.T) {
	strategy := StaticAccountStrategy{}
	candidates := []*Account{
		{ID: "acct-1", Default: true},
		{ID: "acct-2"},
	}
	got, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{PinnedAccountID: "acct-2"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID != "acct-2" {
		t.Fatalf("Select() = %s, want acct-2 (pinned overrides default)", got.ID)
	}
}

func TestStaticAccountStrategy_FallsBackToDefault(t *testing.T) {
	strategy := StaticAccountStrategy{}
	candidates := []*Account{
		{ID: "acct-1"},
		{ID: "acct-2", Default: true},
	}
	got, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID != "acct-2" {
		t.Fatalf("Select() = %s, want acct-2 (the Default account)", got.ID)
	}
}

func TestStaticAccountStrategy_AmbiguousWithoutDefault_Errors(t *testing.T) {
	strategy := StaticAccountStrategy{}
	candidates := []*Account{{ID: "acct-1"}, {ID: "acct-2"}}
	_, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{})
	var notAvailable *NoAccountAvailableError
	if !errors.As(err, &notAvailable) {
		t.Fatalf("Select() error = %v (%T), want *NoAccountAvailableError (ambiguous pick refused)", err, err)
	}
}

func TestStaticAccountStrategy_PinnedNotInCandidates_Errors(t *testing.T) {
	strategy := StaticAccountStrategy{}
	candidates := []*Account{{ID: "acct-1"}}
	_, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{PinnedAccountID: "does-not-exist"})
	if err == nil {
		t.Fatalf("Select() error = nil, want NoAccountAvailableError")
	}
}

type fakeUsageLookup struct {
	tokens map[string]int64
	err    error
}

func (f *fakeUsageLookup) AccountTokens(_ context.Context, _, accountID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.tokens[accountID], nil
}

func TestLeastUsageAccountStrategy_PicksLowestTokens(t *testing.T) {
	strategy := LeastUsageAccountStrategy{Usage: &fakeUsageLookup{tokens: map[string]int64{
		"acct-1": 5000,
		"acct-2": 100,
		"acct-3": 9999,
	}}}
	candidates := []*Account{{ID: "acct-1"}, {ID: "acct-2"}, {ID: "acct-3"}}
	got, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID != "acct-2" {
		t.Fatalf("Select() = %s, want acct-2 (lowest usage)", got.ID)
	}
}

func TestLeastUsageAccountStrategy_TieBreaksByID(t *testing.T) {
	strategy := LeastUsageAccountStrategy{Usage: &fakeUsageLookup{tokens: map[string]int64{
		"acct-b": 100,
		"acct-a": 100,
	}}}
	candidates := []*Account{{ID: "acct-b"}, {ID: "acct-a"}}
	got, err := strategy.Select(context.Background(), candidates, AccountSelectionRequest{})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID != "acct-a" {
		t.Fatalf("Select() = %s, want acct-a (stable tie-break)", got.ID)
	}
}

func TestLeastUsageAccountStrategy_NoUsageLookup_Errors(t *testing.T) {
	strategy := LeastUsageAccountStrategy{}
	_, err := strategy.Select(context.Background(), []*Account{{ID: "acct-1"}}, AccountSelectionRequest{})
	if err == nil {
		t.Fatalf("Select() error = nil, want error when Usage is nil")
	}
}

func TestAccountStrategyRegistry_DefaultAndLookup(t *testing.T) {
	registry := NewAccountStrategyRegistry(nil)
	strategy, ok := registry.Get("")
	if !ok || strategy.Name() != StaticAccountStrategyName {
		t.Fatalf("Get(\"\") = %v, %v; want static strategy", strategy, ok)
	}
	if _, ok := registry.Get(LeastUsageAccountStrategyName); ok {
		t.Fatalf("Get(least_usage) succeeded with a nil usage lookup, want unregistered")
	}

	registryWithUsage := NewAccountStrategyRegistry(&fakeUsageLookup{})
	if _, ok := registryWithUsage.Get(LeastUsageAccountStrategyName); !ok {
		t.Fatalf("Get(least_usage) failed with a usage lookup configured")
	}
}

func TestAccountStrategyRegistry_UnknownName(t *testing.T) {
	registry := NewAccountStrategyRegistry(nil)
	if _, ok := registry.Get("does-not-exist"); ok {
		t.Fatalf("Get(does-not-exist) succeeded, want false")
	}
}
