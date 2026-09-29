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

package config

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidatePricingAccepts(t *testing.T) {
	tests := []struct {
		name string
		cfg  PricingConfig
	}{
		{"an empty table", PricingConfig{}},
		{"a full table", PricingConfig{
			Version: "2026-09-23",
			Models: []ModelPriceConfig{
				{Model: "claude-opus-5", Aliases: []string{"opus"}, Provider: "anthropic",
					InputPerMTok: 5, OutputPerMTok: 25, CacheReadPerMTok: 0.5, CacheWritePerMTok: 6.25},
				{Model: "qwen3-8b", Provider: "local"},
				// One name priced once per engine is two entries, not a duplicate.
				{Model: "qwen3-8b", Engine: "openvino", Provider: "local", InputPerMTok: 0.02},
			},
			Tenants: map[string]TenantPricingConfig{
				"acme": {Models: []ModelPriceConfig{{Model: "claude-opus-5", Provider: "anthropic", InputPerMTok: 4}}},
			},
		}},
		{"electricity at every scope", PricingConfig{
			Version:                "v1",
			Models:                 []ModelPriceConfig{{Model: "qwen3-8b", Engine: "openvino", Provider: "local", PowerWatts: 12}},
			ElectricityPricePerKWh: ptrFloat(0.12),
			EnginePowerWatts:       map[string]float64{"ollama": 250},
			Tenants: map[string]TenantPricingConfig{
				"acme": {ElectricityPricePerKWh: ptrFloat(0), EnginePowerWatts: map[string]float64{"ollama": 0}},
			},
		}},
		{"engines bound to every device class", PricingConfig{
			EngineDevices: map[string]string{"ollama": "gpu", "openvino": "npu", "llama-cpp": "cpu"},
		}},
		{"a telemetry retention", PricingConfig{TelemetryRetention: 72 * time.Hour}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePricing(tc.cfg); err != nil {
				t.Fatalf("ValidatePricing = %v, want nil", err)
			}
		})
	}
}

func TestValidatePricingRejects(t *testing.T) {
	tests := []struct {
		name string
		cfg  PricingConfig
		want string
	}{
		{"priced models with no version", PricingConfig{
			Models: []ModelPriceConfig{{Model: "m", Provider: "p"}},
		}, "orchestration.pricing.version"},
		{"a tenant override with no version", PricingConfig{
			Tenants: map[string]TenantPricingConfig{"acme": {Models: []ModelPriceConfig{{Model: "m", Provider: "p"}}}},
		}, "orchestration.pricing.version"},
		{"no model name", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Provider: "p"}}}, "models[0].model"},
		{"no provider", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m"}}}, "models[0].provider"},
		{"a negative rate", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Provider: "p", CacheReadPerMTok: -0.1}}}, "cache_read_per_mtok"},
		{"a NaN rate", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Provider: "p", InputPerMTok: math.NaN()}}}, "input_per_mtok"},
		{"an infinite rate", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Provider: "p", OutputPerMTok: math.Inf(1)}}}, "output_per_mtok"},
		{"an alias colliding with a model", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Provider: "p"}, {Model: "n", Aliases: []string{"m"}, Provider: "p"}}}, "already priced"},
		{"an empty alias", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Aliases: []string{" "}, Provider: "p"}}}, "aliases"},
		{"an empty tenant id", PricingConfig{Version: "v1",
			Tenants: map[string]TenantPricingConfig{"": {}}}, "tenant id"},
		{"a negative electricity price", PricingConfig{ElectricityPricePerKWh: ptrFloat(-0.1)},
			"orchestration.pricing.electricity_price_per_kwh"},
		{"a NaN tenant electricity price", PricingConfig{
			Tenants: map[string]TenantPricingConfig{"acme": {ElectricityPricePerKWh: ptrFloat(math.NaN())}}},
			"tenants.acme.electricity_price_per_kwh"},
		{"a negative engine draw", PricingConfig{EnginePowerWatts: map[string]float64{"ollama": -5}},
			"engine_power_watts.ollama"},
		{"an unnamed engine", PricingConfig{EnginePowerWatts: map[string]float64{" ": 5}}, "engine name"},
		{"an engine bound to an unknown device", PricingConfig{EngineDevices: map[string]string{"ollama": "tpu"}},
			"engine_devices.ollama"},
		{"a device binding with no engine", PricingConfig{EngineDevices: map[string]string{"": "gpu"}}, "engine name"},
		{"a negative telemetry retention", PricingConfig{TelemetryRetention: -time.Hour},
			"orchestration.pricing.telemetry_retention"},
		{"an infinite model draw", PricingConfig{Version: "v1",
			Models: []ModelPriceConfig{{Model: "m", Provider: "local", PowerWatts: math.Inf(1)}}}, "power_watts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePricing(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidatePricing = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestEffectiveTelemetryRetention(t *testing.T) {
	tests := []struct {
		name string
		cfg  PricingConfig
		want time.Duration
	}{
		{"unset is the default", PricingConfig{}, DefaultTelemetryRetention},
		{"a configured retention is kept", PricingConfig{TelemetryRetention: 48 * time.Hour}, 48 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.EffectiveTelemetryRetention(); got != tc.want {
				t.Fatalf("EffectiveTelemetryRetention = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidatePricingIsReachedThroughValidate(t *testing.T) {
	v := NewValidator()
	v.validateOrchestration(&OrchestrationConfig{Pricing: PricingConfig{
		Models: []ModelPriceConfig{{Model: "m", Provider: "p"}},
	}})
	if err := v.Errors(); err == nil || !strings.Contains(err.Error(), "orchestration.pricing.version") {
		t.Fatalf("validateOrchestration = %v, want the pricing section validated", err)
	}
}

func ptrFloat(f float64) *float64 { return &f }

func TestModelPriceHasTokenRates(t *testing.T) {
	if (ModelPriceConfig{PowerWatts: 12}).HasTokenRates() {
		t.Fatal("an entry with only a draw has no token rates")
	}
	if !(ModelPriceConfig{CacheWritePerMTok: 1}).HasTokenRates() {
		t.Fatal("an entry with a cache-write rate has token rates")
	}
}
