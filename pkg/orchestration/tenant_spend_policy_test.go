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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func monthlyDollars(micros int64) SpendLimits {
	return SpendLimits{Windows: []QuotaWindow{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: micros}}}
}

func TestTenantSpendPolicyStoreRoundTrip(t *testing.T) {
	store := NewInMemoryTenantSpendPolicyStore()
	ctx := context.Background()

	unset, err := store.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get(unset): %v", err)
	}
	if unset.TenantID != "t1" || unset.Limits.Metered() || !unset.UpdatedAt.IsZero() {
		t.Fatalf("unset policy = %+v, want an unmetered policy for t1", unset)
	}

	policy := &TenantSpendPolicy{TenantID: "t1", Limits: monthlyDollars(2_000_000_000)}
	if err := store.Put(ctx, policy); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if policy.UpdatedAt.IsZero() {
		t.Fatal("Put did not stamp UpdatedAt")
	}
	got, err := store.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Limits.Windows) != 1 || got.Limits.Windows[0] != policy.Limits.Windows[0] {
		t.Fatalf("stored = %+v, want %+v", got.Limits, policy.Limits)
	}

	// Limits that meter nothing remove the record.
	if err := store.Put(ctx, &TenantSpendPolicy{TenantID: "t1"}); err != nil {
		t.Fatalf("Put(clear): %v", err)
	}
	cleared, err := store.Get(ctx, "t1")
	if err != nil || cleared.Limits.Metered() || !cleared.UpdatedAt.IsZero() {
		t.Fatalf("cleared = %+v, %v; want unmetered", cleared, err)
	}
	// Clearing a tenant that has nothing is not an error.
	if err := store.Put(ctx, &TenantSpendPolicy{TenantID: "t2"}); err != nil {
		t.Fatalf("Put(clear absent): %v", err)
	}
}

func TestTenantSpendPolicyStoreRefuses(t *testing.T) {
	store := NewInMemoryTenantSpendPolicyStore()
	tests := []struct {
		name string
		call func() error
		is   error
	}{
		{"a nil policy", func() error { return store.Put(context.Background(), nil) }, ErrTenantSpendPolicyInvalid},
		{"no tenant", func() error { _, err := store.Get(context.Background(), ""); return err }, ErrTenantSpendPolicyInvalid},
		{"a malformed window", func() error {
			return store.Put(context.Background(), &TenantSpendPolicy{TenantID: "t1",
				Limits: SpendLimits{Windows: []QuotaWindow{{Name: "w", Size: time.Hour}}}})
		}, ErrQuotaInvalid},
		{"another tenant's context", func() error {
			_, err := store.Get(ContextWithTenant(context.Background(), "t2"), "t1")
			return err
		}, ErrAccountTenantScope},
		{"no KV", func() error { _, err := NewTenantSpendPolicyStore(nil).Get(context.Background(), "t1"); return err }, ErrTenantSpendPolicyStore},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, want %v", err, tc.is)
			}
		})
	}
}

// faultyGetKV fails every read.
type faultyGetKV struct {
	interfaces.KVStore
	err error
}

func (k *faultyGetKV) Get(context.Context, string) ([]byte, error) { return nil, k.err }

func TestTenantSpendPolicyStoreFailsWithItsKV(t *testing.T) {
	boom := errors.New("kv down")
	store := NewTenantSpendPolicyStore(&faultyGetKV{KVStore: NewMemoryTaskKV(), err: boom})
	_, err := store.Get(context.Background(), "t1")
	if !errors.Is(err, boom) || !errors.Is(err, ErrTenantSpendPolicyStore) {
		t.Fatalf("err = %v, want the KV failure as a store error — never an unmetered tenant", err)
	}
}

// TestReadTenantSpendPolicyMeasuresEveryWindow asserts the report carries the
// tenant's usage against each window from the tenant quota plane, local turns
// included.
func TestReadTenantSpendPolicyMeasuresEveryWindow(t *testing.T) {
	store := NewInMemoryTenantSpendPolicyStore()
	ledger := NewInMemoryConsumptionLedger()
	ctx := context.Background()
	now := time.Now().UTC()
	entry := validEntry()
	entry.AccountID = ""
	entry.EquivalentMicros = 1_500_000
	entry.OccurredAt = now.Add(-time.Hour)
	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := WriteTenantSpendPolicy(ctx, store, "t1", monthlyDollars(1_000_000)); err != nil {
		t.Fatalf("WriteTenantSpendPolicy: %v", err)
	}

	report, err := ReadTenantSpendPolicy(ctx, store, ledger, "t1", now)
	if err != nil {
		t.Fatalf("ReadTenantSpendPolicy: %v", err)
	}
	if len(report.Windows) != 1 || report.Windows[0].UsedEquivalentMicros != 1_500_000 || !report.Windows[0].Exhausted {
		t.Fatalf("windows = %+v, want the monthly window spent", report.Windows)
	}

	noLedger, err := ReadTenantSpendPolicy(ctx, store, nil, "t1", now)
	if err != nil || len(noLedger.Windows) != 0 || !noLedger.Limits.Metered() {
		t.Fatalf("no ledger = %+v, %v; want the limits without usage", noLedger, err)
	}
}

func TestTenantSpendPolicyHelpersRefuseAnUnwiredStore(t *testing.T) {
	if _, err := ReadTenantSpendPolicy(context.Background(), nil, nil, "t1", time.Time{}); !errors.Is(err, ErrTenantSpendPolicyStore) {
		t.Fatalf("Read = %v, want ErrTenantSpendPolicyStore — limits unknown is not no limits", err)
	}
	if _, err := WriteTenantSpendPolicy(context.Background(), nil, "t1", SpendLimits{}); !errors.Is(err, ErrTenantSpendPolicyStore) {
		t.Fatalf("Write = %v, want ErrTenantSpendPolicyStore", err)
	}
}

func TestAgentServiceExposesTheTenantSpendPolicyStore(t *testing.T) {
	svc, _, _ := newConsumptionRecorder(t)
	if svc.TenantSpendPolicies() != nil {
		t.Fatal("an unwired service reported a store")
	}
	store := NewInMemoryTenantSpendPolicyStore()
	svc.SetTenantSpendPolicyStore(store)
	var accessor TenantSpendPolicyAccessor = svc
	if accessor.TenantSpendPolicies() != store {
		t.Fatal("the wired store was not exposed")
	}
}
