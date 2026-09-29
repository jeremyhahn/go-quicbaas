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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// testPricing is a table with a generic entry, an engine-specific entry for a
// local model, and a tenant override.
func testPricing() config.PricingConfig {
	return config.PricingConfig{
		Version: "2026-09-23",
		Models: []config.ModelPriceConfig{
			{
				Model: "claude-opus-5", Aliases: []string{"opus"}, Provider: "anthropic",
				InputPerMTok: 5, OutputPerMTok: 25, CacheReadPerMTok: 0.5, CacheWritePerMTok: 6.25,
			},
			{Model: "qwen3-8b", Provider: "local", InputPerMTok: 1, OutputPerMTok: 1},
			{Model: "qwen3-8b", Engine: "openvino", Provider: "local", InputPerMTok: 0.02, OutputPerMTok: 0.02},
		},
		Tenants: map[string]config.TenantPricingConfig{
			"acme": {Models: []config.ModelPriceConfig{
				{Model: "claude-opus-5", Provider: "anthropic", InputPerMTok: 4, OutputPerMTok: 20},
			}},
		},
	}
}

func newTestPriceBook(t *testing.T) *PriceBook {
	t.Helper()
	book, err := NewPriceBook(testPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	return book
}

func TestPriceBookStamp(t *testing.T) {
	book := newTestPriceBook(t)
	tests := []struct {
		name         string
		entry        ConsumptionEntry
		wantPriced   bool
		wantMicros   int64
		wantRef      string
		wantProvider string
	}{
		{
			name: "every dimension at its own rate",
			entry: ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5",
				InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 2_000_000, CacheWriteTokens: 10_000},
			wantPriced: true, wantMicros: 8_562_500,
			wantRef: "2026-09-23:default:*/claude-opus-5", wantProvider: "anthropic",
		},
		{
			name:       "an alias prices as its model",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "opus", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 5_000_000,
			wantRef: "2026-09-23:default:*/claude-opus-5", wantProvider: "anthropic",
		},
		{
			name:       "an engine-specific entry wins over the generic one",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "openvino", Model: "qwen3-8b", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 20_000,
			wantRef: "2026-09-23:default:openvino/qwen3-8b", wantProvider: "local",
		},
		{
			name:       "another engine takes the generic entry",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "ollama", Model: "qwen3-8b", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 1_000_000,
			wantRef: "2026-09-23:default:*/qwen3-8b", wantProvider: "local",
		},
		{
			name:       "a tenant override wins",
			entry:      ConsumptionEntry{TenantID: "acme", Engine: "claude", Model: "claude-opus-5", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 4_000_000,
			wantRef: "2026-09-23:tenant/acme:*/claude-opus-5", wantProvider: "anthropic",
		},
		{
			// go-quicbaas#1058 review: "opus" is the deployment's alias for
			// claude-opus-5, so acme's re-price of the model re-prices it.
			name:       "a tenant override answers to the model's declared alias",
			entry:      ConsumptionEntry{TenantID: "acme", Engine: "claude", Model: "opus", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 4_000_000,
			wantRef: "2026-09-23:tenant/acme:*/claude-opus-5", wantProvider: "anthropic",
		},
		{
			name:       "a tenant inherits what it does not override",
			entry:      ConsumptionEntry{TenantID: "acme", Engine: "openvino", Model: "qwen3-8b", InputTokens: 1_000_000},
			wantPriced: true, wantMicros: 20_000,
			wantRef: "2026-09-23:default:openvino/qwen3-8b", wantProvider: "local",
		},
		{
			name:  "rounds half up to the micro",
			entry: ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5", CacheReadTokens: 3},
			// 3 × $0.50/M = $0.0000015 = 1.5 micros.
			wantPriced: true, wantMicros: 2,
			wantRef: "2026-09-23:default:*/claude-opus-5", wantProvider: "anthropic",
		},
		{
			name:  "an unknown model is unpriced, never guessed",
			entry: ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5-20260101", InputTokens: 10},
		},
		{
			name:  "a turn that consumed nothing is not priced",
			entry: ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := tc.entry
			if got := book.Stamp(&entry, nil); got != tc.wantPriced {
				t.Fatalf("Stamp = %t, want %t", got, tc.wantPriced)
			}
			if entry.EquivalentMicros != tc.wantMicros || entry.PriceRef != tc.wantRef || entry.Provider != tc.wantProvider {
				t.Fatalf("entry = micros %d ref %q provider %q, want %d %q %q",
					entry.EquivalentMicros, entry.PriceRef, entry.Provider, tc.wantMicros, tc.wantRef, tc.wantProvider)
			}
		})
	}
}

// TestPriceBookTenantAliasOfItsOwnWins asserts an inherited alias never
// shadows a name the tenant's own table answers to: a tenant that prices
// "opus" as a model of its own keeps that price.
func TestPriceBookTenantAliasOfItsOwnWins(t *testing.T) {
	cfg := testPricing()
	cfg.Tenants["acme"] = config.TenantPricingConfig{Models: []config.ModelPriceConfig{
		{Model: "claude-opus-5", Provider: "anthropic", InputPerMTok: 4, OutputPerMTok: 20},
		{Model: "opus", Provider: "anthropic", InputPerMTok: 3, OutputPerMTok: 15},
	}}
	book, err := NewPriceBook(cfg)
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	entry := ConsumptionEntry{TenantID: "acme", Engine: "claude", Model: "opus", InputTokens: 1_000_000}
	if !book.Stamp(&entry, nil) || entry.EquivalentMicros != 3_000_000 || entry.PriceRef != "2026-09-23:tenant/acme:*/opus" {
		t.Fatalf("entry = micros %d ref %q, want the tenant's own opus entry at $3/M", entry.EquivalentMicros, entry.PriceRef)
	}

	other := ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "opus", InputTokens: 1_000_000}
	if !book.Stamp(&other, nil) || other.PriceRef != "2026-09-23:default:*/claude-opus-5" {
		t.Fatalf("another tenant's opus = ref %q, want the deployment's", other.PriceRef)
	}
}

// TestPriceBookInheritedAliasesKeepTheirEngine is the go-quicbaas#1058 review
// regression: the same alias names different models on different engines
// (A/X and B/Y are "fast", A/Y is "slow"). A tenant re-price of fast on A
// inherits A's alias X only; it must not claim Y, which on A is slow.
func TestPriceBookInheritedAliasesKeepTheirEngine(t *testing.T) {
	cfg := config.PricingConfig{
		Version: "v1",
		Models: []config.ModelPriceConfig{
			{Model: "fast", Engine: "A", Aliases: []string{"X"}, Provider: "p", InputPerMTok: 1},
			{Model: "slow", Engine: "A", Aliases: []string{"Y"}, Provider: "p", InputPerMTok: 2},
			{Model: "fast", Engine: "B", Aliases: []string{"Y"}, Provider: "p", InputPerMTok: 1},
		},
		Tenants: map[string]config.TenantPricingConfig{
			"acme": {Models: []config.ModelPriceConfig{
				{Model: "fast", Engine: "A", Provider: "p", InputPerMTok: 9},
			}},
		},
	}
	book, err := NewPriceBook(cfg)
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	tests := []struct {
		engine, model string
		wantMicros    int64
		wantRef       string
	}{
		{"A", "X", 9_000_000, "v1:tenant/acme:A/fast"},
		{"A", "fast", 9_000_000, "v1:tenant/acme:A/fast"},
		{"A", "Y", 2_000_000, "v1:default:A/slow"},
		{"B", "Y", 1_000_000, "v1:default:B/fast"},
	}
	for _, tc := range tests {
		entry := ConsumptionEntry{TenantID: "acme", Engine: tc.engine, Model: tc.model, InputTokens: 1_000_000}
		if !book.Stamp(&entry, nil) || entry.EquivalentMicros != tc.wantMicros || entry.PriceRef != tc.wantRef {
			t.Errorf("%s/%s = micros %d ref %q, want %d %q",
				tc.engine, tc.model, entry.EquivalentMicros, entry.PriceRef, tc.wantMicros, tc.wantRef)
		}
	}
}

// TestPriceBookOverrideDoesNotClaimAnotherEnginesAlias is the collision the
// review named: "default" is acp's alias for large and openvino's for small.
// A tenant re-price of openvino/large declares no alias on openvino, so
// openvino/default stays small at the deployment's price.
func TestPriceBookOverrideDoesNotClaimAnotherEnginesAlias(t *testing.T) {
	cfg := config.PricingConfig{
		Version: "v1",
		Models: []config.ModelPriceConfig{
			{Model: "large", Engine: "acp", Aliases: []string{"default"}, Provider: "p", InputPerMTok: 10},
			{Model: "small", Engine: "openvino", Aliases: []string{"default"}, Provider: "p", InputPerMTok: 1},
		},
		Tenants: map[string]config.TenantPricingConfig{
			"acme": {Models: []config.ModelPriceConfig{
				{Model: "large", Engine: "openvino", Provider: "p", InputPerMTok: 7},
			}},
		},
	}
	book, err := NewPriceBook(cfg)
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	entry := ConsumptionEntry{TenantID: "acme", Engine: "openvino", Model: "default", InputTokens: 1_000_000}
	if !book.Stamp(&entry, nil) || entry.EquivalentMicros != 1_000_000 || entry.PriceRef != "v1:default:openvino/small" {
		t.Fatalf("openvino/default = micros %d ref %q, want small at $1/M from the deployment", entry.EquivalentMicros, entry.PriceRef)
	}
	acp := ConsumptionEntry{TenantID: "acme", Engine: "acp", Model: "default", InputTokens: 1_000_000}
	if !book.Stamp(&acp, nil) || acp.PriceRef != "v1:default:acp/large" {
		t.Fatalf("acp/default = ref %q, want the deployment's acp/large", acp.PriceRef)
	}
}

func TestPriceBookNilPricesNothing(t *testing.T) {
	var book *PriceBook
	entry := ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5", InputTokens: 10}
	if book.Stamp(&entry, nil) {
		t.Fatal("a nil price book priced a turn")
	}
	if book.Stamp(nil, nil) {
		t.Fatal("a nil entry was priced")
	}
}

func TestNewPriceBookRefusesAnInvalidTable(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.PricingConfig
	}{
		{"priced models with no version", config.PricingConfig{
			Models: []config.ModelPriceConfig{{Model: "m", Provider: "p", InputPerMTok: 1}},
		}},
		{"a negative rate", config.PricingConfig{
			Version: "v1", Models: []config.ModelPriceConfig{{Model: "m", Provider: "p", OutputPerMTok: -1}},
		}},
		{"a model priced twice on one engine", config.PricingConfig{
			Version: "v1", Models: []config.ModelPriceConfig{
				{Model: "m", Provider: "p"}, {Model: "x", Aliases: []string{"m"}, Provider: "p"},
			},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPriceBook(tc.cfg)
			if !errors.Is(err, ErrPriceTableInvalid) {
				t.Fatalf("err = %v, want ErrPriceTableInvalid", err)
			}
		})
	}
}

func TestNewPriceBookAcceptsAnEmptyTable(t *testing.T) {
	book, err := NewPriceBook(config.PricingConfig{})
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	entry := ConsumptionEntry{TenantID: "t1", Engine: "claude", Model: "claude-opus-5", InputTokens: 10}
	if book.Stamp(&entry, nil) {
		t.Fatal("an empty table priced a turn")
	}
}

// electricityPricing is a table with a local model declaring its draw, an
// engine-level draw, a local model with an explicit per-token rate, and a
// tenant that overrides the electricity rate.
func electricityPricing() config.PricingConfig {
	tenantRate := 0.30
	return config.PricingConfig{
		Version: "v1",
		Models: []config.ModelPriceConfig{
			{Model: "qwen3-8b", Engine: "openvino", Provider: "local", PowerWatts: 12},
			{Model: "llama-70b", Engine: "ollama", Provider: "local", PowerWatts: 300, InputPerMTok: 0.5},
		},
		EnginePowerWatts: map[string]float64{"ollama": 250},
		Tenants: map[string]config.TenantPricingConfig{
			"acme": {ElectricityPricePerKWh: &tenantRate},
		},
	}
}

// TestPriceBookCostsLocalTurnsFromElectricity is the owner's cost model:
// watts × seconds / 3.6e6 kWh × $/kWh, at DefaultElectricityPricePerKWh
// unless a tenant overrides it.
func TestPriceBookCostsLocalTurnsFromElectricity(t *testing.T) {
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	tests := []struct {
		name        string
		entry       ConsumptionEntry
		settings    *CostSettings
		wantMicros  int64
		wantMilliWh int64
		wantRef     string
	}{
		{
			// 12 W for an hour is 12 Wh = 12,000 mWh = 0.012 kWh; at the
			// default $0.12/kWh that is $0.00144 = 1,440 micros.
			name:       "the default rate applies",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "openvino", Model: "qwen3-8b", DurationMillis: 3_600_000},
			wantMicros: 1_440, wantMilliWh: 12_000,
			wantRef: "v1:electricity:default:openvino/qwen3-8b@12W:default@0.12/kWh",
		},
		{
			name:       "a tenant's configured rate applies",
			entry:      ConsumptionEntry{TenantID: "acme", Engine: "openvino", Model: "qwen3-8b", DurationMillis: 3_600_000},
			wantMicros: 3_600, wantMilliWh: 12_000,
			wantRef: "v1:electricity:default:openvino/qwen3-8b@12W:tenant_config@0.3/kWh",
		},
		{
			name:       "a tenant's runtime rate beats its configured one",
			entry:      ConsumptionEntry{TenantID: "acme", Engine: "openvino", Model: "qwen3-8b", DurationMillis: 3_600_000},
			settings:   &CostSettings{TenantID: "acme", ElectricityPricePerKWh: ptrFloat(0.5)},
			wantMicros: 6_000, wantMilliWh: 12_000,
			wantRef: "v1:electricity:default:openvino/qwen3-8b@12W:tenant_settings@0.5/kWh",
		},
		{
			// 250 W for 90 s = 6.25 Wh = 6,250 mWh; × $0.12 = 750 micros.
			name:       "an engine draw prices a model the table does not name",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "ollama", Model: "qwen3-coder", DurationMillis: 90_000},
			wantMicros: 750, wantMilliWh: 6_250,
			wantRef: "v1:electricity:default:ollama/qwen3-coder@250W:default@0.12/kWh",
		},
		{
			name:       "a tenant's runtime engine draw beats the configured one",
			entry:      ConsumptionEntry{TenantID: "t1", Engine: "ollama", Model: "qwen3-coder", DurationMillis: 90_000},
			settings:   &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"ollama": 100}},
			wantMicros: 300, wantMilliWh: 2_500,
			wantRef: "v1:electricity:tenant-settings:ollama/qwen3-coder@100W:default@0.12/kWh",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := tc.entry
			if !book.Stamp(&entry, tc.settings) {
				t.Fatal("Stamp = false, want the turn costed from electricity")
			}
			if entry.PriceBasis != PriceBasisElectricity || entry.EquivalentMicros != tc.wantMicros ||
				entry.EnergyMilliWh != tc.wantMilliWh || entry.PriceRef != tc.wantRef {
				t.Fatalf("entry = basis %q micros %d mWh %d ref %q, want electricity %d %d %q",
					entry.PriceBasis, entry.EquivalentMicros, entry.EnergyMilliWh, entry.PriceRef,
					tc.wantMicros, tc.wantMilliWh, tc.wantRef)
			}
			if entry.Provider != "local" {
				t.Errorf("provider = %q, want local", entry.Provider)
			}
		})
	}
}

// TestPriceBookPerTokenRateBeatsElectricity asserts an explicit per-token
// rate on a local model is the precise override: the draw is ignored.
func TestPriceBookPerTokenRateBeatsElectricity(t *testing.T) {
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	entry := ConsumptionEntry{TenantID: "t1", Engine: "ollama", Model: "llama-70b", InputTokens: 1_000_000, DurationMillis: 60_000}
	if !book.Stamp(&entry, &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"ollama": 100}}) {
		t.Fatal("Stamp = false")
	}
	if entry.PriceBasis != PriceBasisTokenRate || entry.EquivalentMicros != 500_000 || entry.EnergyMilliWh != 0 {
		t.Fatalf("entry = basis %q micros %d mWh %d, want the $0.50/M token rate and no energy",
			entry.PriceBasis, entry.EquivalentMicros, entry.EnergyMilliWh)
	}
}

// TestPriceBookUnknownDurationIsUnknownCost asserts a turn that would be
// costed from electricity but has no measured duration is unpriced — its
// cost unknown, never $0 — while keeping the basis it would have had.
func TestPriceBookUnknownDurationIsUnknownCost(t *testing.T) {
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	for _, duration := range []int64{0, -5} {
		entry := ConsumptionEntry{TenantID: "t1", Engine: "openvino", Model: "qwen3-8b", InputTokens: 10, DurationMillis: duration}
		if book.Stamp(&entry, nil) {
			t.Fatalf("duration %d: Stamp = true, want unpriced", duration)
		}
		if entry.Priced() || entry.EquivalentMicros != 0 || entry.PriceBasis != PriceBasisElectricity {
			t.Fatalf("duration %d: entry = priced %t micros %d basis %q, want unpriced with the electricity basis",
				duration, entry.Priced(), entry.EquivalentMicros, entry.PriceBasis)
		}
		var totals ConsumptionTotals
		totals.add(entry)
		if totals.PricedEntries != 0 || totals.UnpricedEntries != 1 || totals.ElectricityEntries != 0 {
			t.Fatalf("duration %d: totals = %+v, want one unpriced entry", duration, totals)
		}
	}
}

// TestPriceBookZeroEngineDrawSwitchesElectricityOff asserts a per-engine draw
// of zero is a decision: a tenant can switch off the deployment's draw.
func TestPriceBookZeroEngineDrawSwitchesElectricityOff(t *testing.T) {
	book, err := NewPriceBook(electricityPricing())
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	entry := ConsumptionEntry{TenantID: "t1", Engine: "ollama", Model: "qwen3-coder", DurationMillis: 90_000}
	if book.Stamp(&entry, &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"ollama": 0}}) {
		t.Fatalf("Stamp = true (%q), want unpriced: the tenant switched the draw off", entry.PriceRef)
	}
}

// TestNilPriceBookCostsFromRuntimeSettingsAtTheDefaultRate asserts a
// deployment with no pricing configured still costs a local turn once a
// tenant gives its engine a draw.
func TestNilPriceBookCostsFromRuntimeSettingsAtTheDefaultRate(t *testing.T) {
	var book *PriceBook
	entry := ConsumptionEntry{TenantID: "t1", Engine: "openvino", Model: "m", DurationMillis: 3_600_000}
	if !book.Stamp(&entry, &CostSettings{TenantID: "t1", EnginePowerWatts: map[string]float64{"openvino": 10}}) {
		t.Fatal("Stamp = false")
	}
	if entry.EquivalentMicros != 1_200 || entry.PriceRef != "electricity:tenant-settings:openvino/m@10W:default@0.12/kWh" {
		t.Fatalf("entry = micros %d ref %q", entry.EquivalentMicros, entry.PriceRef)
	}
	if rate := book.ElectricityRate("t1", nil); rate.PricePerKWh != config.DefaultElectricityPricePerKWh || rate.Source != ElectricityFromDefault {
		t.Fatalf("rate = %+v, want the default", rate)
	}
}

func ptrFloat(f float64) *float64 { return &f }
