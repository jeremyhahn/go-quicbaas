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
	"encoding/json"
	stderrors "errors"
	"math"
	"testing"
	"time"
)

func TestCostSettingsStoreRoundTrips(t *testing.T) {
	store := NewInMemoryCostSettingsStore()
	ctx := ContextWithTenant(context.Background(), "t1")

	empty, err := store.Get(ctx, "t1")
	if err != nil || !empty.Empty() || empty.TenantID != "t1" {
		t.Fatalf("Get before any Put = %+v, %v; want empty settings for t1", empty, err)
	}

	settings := &CostSettings{TenantID: "t1", ElectricityPricePerKWh: ptrFloat(0.2), EnginePowerWatts: map[string]float64{"openvino": 12}}
	if err := store.Put(ctx, settings); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if settings.UpdatedAt.IsZero() {
		t.Error("Put did not stamp UpdatedAt")
	}
	got, err := store.Get(ctx, "t1")
	if err != nil || *got.ElectricityPricePerKWh != 0.2 || got.EnginePowerWatts["openvino"] != 12 {
		t.Fatalf("Get = %+v, %v; want the stored settings", got, err)
	}

	// Settings that override nothing restore the configuration.
	if err := store.Put(ctx, &CostSettings{TenantID: "t1"}); err != nil {
		t.Fatalf("clearing Put: %v", err)
	}
	if cleared, err := store.Get(ctx, "t1"); err != nil || !cleared.Empty() {
		t.Fatalf("Get after clearing = %+v, %v; want empty", cleared, err)
	}
}

func TestCostSettingsStoreRefusesInvalidSettings(t *testing.T) {
	store := NewInMemoryCostSettingsStore()
	ctx := ContextWithTenant(context.Background(), "t1")
	tests := []struct {
		name     string
		settings *CostSettings
	}{
		{"nil", nil},
		{"a negative rate", &CostSettings{TenantID: "t1", ElectricityPricePerKWh: ptrFloat(-1)}},
		{"a NaN rate", &CostSettings{TenantID: "t1", ElectricityPricePerKWh: ptrFloat(math.NaN())}},
		{"a negative draw", &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"ollama": -1}}},
		{"an infinite draw", &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"ollama": math.Inf(1)}}},
		{"an engine that is not a name", &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"a/b": 1}}},
		{"no tenant", &CostSettings{ElectricityPricePerKWh: ptrFloat(0.1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Put(ctx, tc.settings); !stderrors.Is(err, ErrCostSettingsInvalid) {
				t.Fatalf("Put = %v, want ErrCostSettingsInvalid", err)
			}
		})
	}
}

func TestCostSettingsStoreRefusesAnotherTenantsScope(t *testing.T) {
	store := NewInMemoryCostSettingsStore()
	ctx := ContextWithTenant(context.Background(), "t2")
	if _, err := store.Get(ctx, "t1"); err == nil {
		t.Fatal("a t2-scoped read of t1's settings must be refused")
	}
	if err := store.Put(ctx, &CostSettings{TenantID: "t1", ElectricityPricePerKWh: ptrFloat(0.1)}); err == nil {
		t.Fatal("a t2-scoped write of t1's settings must be refused")
	}
}

func TestReadAndWriteCostSettingsReportTheRateInForce(t *testing.T) {
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	store := NewInMemoryCostSettingsStore()
	ctx := ContextWithTenant(context.Background(), "acme")

	report, err := ReadCostSettings(ctx, store, book, "acme")
	if err != nil {
		t.Fatalf("ReadCostSettings: %v", err)
	}
	if report.Electricity.PricePerKWh != 0.30 || report.Electricity.Source != ElectricityFromTenantConfig {
		t.Fatalf("rate = %+v, want acme's configured $0.30", report.Electricity)
	}
	if report.EngineDraws["ollama"] != 250 {
		t.Fatalf("draws = %+v, want the deployment's ollama 250 W", report.EngineDraws)
	}

	report, err = WriteCostSettings(ctx, store, book, &CostSettings{
		TenantID: "acme", ElectricityPricePerKWh: ptrFloat(0.08), EnginePowerWatts: map[string]float64{"ollama": 180},
	})
	if err != nil {
		t.Fatalf("WriteCostSettings: %v", err)
	}
	if report.Electricity.PricePerKWh != 0.08 || report.Electricity.Source != ElectricityFromTenantSettings || report.EngineDraws["ollama"] != 180 {
		t.Fatalf("report = %+v, want the tenant's runtime settings in force", report)
	}

	if _, err := ReadCostSettings(ctx, nil, book, "acme"); !stderrors.Is(err, ErrCostSettingsStore) {
		t.Fatalf("read with no store = %v, want ErrCostSettingsStore", err)
	}
	if _, err := WriteCostSettings(ctx, nil, book, &CostSettings{TenantID: "acme"}); !stderrors.Is(err, ErrCostSettingsStore) {
		t.Fatalf("write with no store = %v, want ErrCostSettingsStore", err)
	}
}

// TestRecordTurnUsageCostsALocalTurnFromElectricity drives the recorder end to
// end: a local turn on an engine the tenant gave a draw is ledgered with its
// energy and electricity cost, and the roll-up reports both.
func TestRecordTurnUsageCostsALocalTurnFromElectricity(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	svc.SetCostSettingsStore(NewInMemoryCostSettingsStore())
	ctx := ContextWithTenant(context.Background(), "t1")
	if err := svc.CostSettingsStore().Put(ctx, &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"openvino": 12}}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", projectID: "proj-1", engine: "openvino", model: "qwen3-8b", duration: 3_600_000_000_000,
	}, &TokenUsage{InputTokens: 10, OutputTokens: 5})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.ElectricityEntries != 1 || rollup.EnergyMilliWh != 12_000 || rollup.EquivalentMicros != 1_440 || rollup.PricedEntries != 1 {
		t.Fatalf("rollup = %+v, want one electricity entry: 12 Wh at $0.12/kWh", rollup.ConsumptionTotals)
	}
	if rollup.BilledEntries != 1 || rollup.BilledMicros != 1_440 {
		t.Errorf("billed = %d entries / %d micros, want the local turn billed at its electricity cost", rollup.BilledEntries, rollup.BilledMicros)
	}
}

// failingCostSettingsStore cannot read or write.
type failingCostSettingsStore struct{}

// failingCostSettingsStoreError is failingCostSettingsStore's injected failure.
type failingCostSettingsStoreError struct{}

func (failingCostSettingsStoreError) Error() string { return "cost settings store down" }

func (failingCostSettingsStore) Get(context.Context, string) (*CostSettings, error) {
	return nil, failingCostSettingsStoreError{}
}

func (failingCostSettingsStore) Put(context.Context, *CostSettings) error {
	return failingCostSettingsStoreError{}
}

// TestRecordTurnUsageWithUnreadableSettingsRecordsElectricityAsUnknown is the
// go-quicbaas#1058 review regression: a settings read that failed used to be
// read as "no overrides", and the turn was stamped at the configuration's rate
// and draw as a KNOWN cost, though the tenant may have set others. The turn is
// still recorded; its electricity cost is unknown. A per-token price, which
// does not depend on the settings, is unaffected.
func TestRecordTurnUsageWithUnreadableSettingsRecordsElectricityAsUnknown(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	svc.SetPriceBook(book)
	svc.SetCostSettingsStore(failingCostSettingsStore{})
	ctx := ContextWithTenant(context.Background(), "t1")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "local", projectID: "proj-1", engine: "openvino", model: "qwen3-8b", duration: time.Hour,
	}, &TokenUsage{InputTokens: 10})
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "rated", projectID: "proj-1", engine: "ollama", model: "llama-70b", duration: time.Minute,
	}, &TokenUsage{InputTokens: 1_000_000})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 2 {
		t.Fatalf("entries = %d, want both turns recorded", rollup.Entries)
	}
	if rollup.PricedEntries != 1 || rollup.UnpricedEntries != 1 || rollup.EquivalentMicros != 500_000 || rollup.ElectricityEntries != 0 {
		t.Fatalf("rollup = %+v, want the token-rated turn priced and the electricity turn unknown", rollup.ConsumptionTotals)
	}
}

// TestRecordTurnUsageDoesNotPriceRepairedCounts is the go-quicbaas#1058 review
// regression: accounting that had to be repaired (a negative count clamped to
// zero) was priced as if measured, reporting complete coverage.
func TestRecordTurnUsageDoesNotPriceRepairedCounts(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	svc.SetPriceBook(book)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	var usage TokenUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":100,"output_tokens":-1}`), &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if !usage.Malformed() {
		t.Fatal("fixture: a negative count must decode as repaired accounting")
	}
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "ollama", model: "llama-70b",
	}, &usage)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.PricedEntries != 0 || rollup.UnpricedEntries != 1 {
		t.Fatalf("rollup = %+v, want the repaired turn recorded but unpriced", rollup.ConsumptionTotals)
	}
}
