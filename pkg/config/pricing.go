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
	"fmt"
	"math"
	"strings"
	"time"
)

// PricingConfig is the model price table: what a million tokens of each kind
// cost on each model, in US dollars. It is what turns a turn's token counts
// into the list-price-equivalent figure every consumption entry carries
// (orchestration.PriceBook).
//
// A MODEL THE TABLE DOES NOT NAME IS UNPRICED, never guessed. Its turns are
// still recorded with their tokens and render as "—" wherever money is shown.
//
// LOCAL INFERENCE IS COSTED FROM ELECTRICITY. A local entry carries
// power_watts — the device's draw while serving, an NPU's or a GPU's — rather
// than a per-token rate nobody measured, and a turn on it costs
//
//	power_watts × turn seconds / 3.6e6 kWh × electricity_price_per_kwh
//
// engine_power_watts gives an engine's draw for every model it serves that
// the table does not list with its own. electricity_price_per_kwh defaults to
// DefaultElectricityPricePerKWh and is overridable per tenant; both are also
// editable at runtime through the cost-settings API. An entry with a per-token
// rate is priced by the rate even when it carries power_watts: an explicit
// rate is the operator's precise figure.
//
//	orchestration:
//	  pricing:
//	    version: "2026-09-23"
//	    models:
//	      - model: claude-opus-4-5-20251101
//	        aliases: [claude-opus-4-5]
//	        provider: anthropic
//	        input_per_mtok: 5.00
//	        output_per_mtok: 25.00
//	        cache_read_per_mtok: 0.50
//	        cache_write_per_mtok: 6.25
//	      - model: qwen3-8b
//	        engine: openvino
//	        provider: local
//	        power_watts: 12
//	    electricity_price_per_kwh: 0.12
//	    engine_power_watts:
//	      ollama: 250
//	    tenants:
//	      acme:
//	        models:
//	          - model: claude-opus-4-5-20251101
//	            provider: anthropic
//	            input_per_mtok: 4.00
//	            output_per_mtok: 20.00
type PricingConfig struct {
	// Version names this revision of the table. Every priced entry records
	// it in its price reference, so a figure can always be traced to the
	// sheet it was stamped from. Required whenever any model is priced.
	Version string `mapstructure:"version"`

	// Models is the deployment-wide table.
	Models []ModelPriceConfig `mapstructure:"models"`

	// ElectricityPricePerKWh is what a kilowatt-hour costs, in US dollars,
	// for every turn priced from electricity. Nil is
	// DefaultElectricityPricePerKWh; zero is a deliberate "free" (on-site
	// generation, say), not an absence.
	ElectricityPricePerKWh *float64 `mapstructure:"electricity_price_per_kwh"`

	// EnginePowerWatts is each local engine's device draw while serving, in
	// watts, keyed by engine name. It prices every turn on that engine whose
	// model has no entry of its own.
	EnginePowerWatts map[string]float64 `mapstructure:"engine_power_watts"`

	// EngineDevices binds each local engine to the class of device its turns
	// draw on — "gpu", "npu" or "cpu" — keyed by engine name. A turn on a
	// bound engine is costed from the energy the fleet's device telemetry
	// measured on the host that ran it; an unbound engine, or a window no
	// telemetry covers, is costed from EnginePowerWatts and marked estimated.
	// There is no default binding: which device an engine uses is a fact
	// about the deployment, and a guessed one would file an NPU's turns
	// against the GPU's draw.
	EngineDevices map[string]string `mapstructure:"engine_devices"`

	// TelemetryRetention is how long the fleet's raw device telemetry
	// (fleet.device.* series) is kept in the time-series engine. Zero is
	// DefaultTelemetryRetention; the policy is applied to every tenant's
	// series at startup and on a tenant's first report.
	TelemetryRetention time.Duration `mapstructure:"telemetry_retention"`

	// Tenants overrides the table per tenant, keyed by tenant ID. An override
	// entry replaces the deployment entry with the same model and engine;
	// every other deployment entry still applies to the tenant.
	Tenants map[string]TenantPricingConfig `mapstructure:"tenants"`
}

// DefaultElectricityPricePerKWh is the electricity rate, in US dollars per
// kilowatt-hour, a deployment that names none is costed at.
const DefaultElectricityPricePerKWh = 0.12

// DefaultTelemetryRetention is how long raw fleet device telemetry is kept
// when the deployment names no retention: long enough to re-cost two weeks of
// turns from what their devices drew.
const DefaultTelemetryRetention = 14 * 24 * time.Hour

// EffectiveTelemetryRetention is the retention fleet device telemetry is kept
// for: TelemetryRetention, or DefaultTelemetryRetention when it is unset.
func (p PricingConfig) EffectiveTelemetryRetention() time.Duration {
	if p.TelemetryRetention == 0 {
		return DefaultTelemetryRetention
	}
	return p.TelemetryRetention
}

// TenantPricingConfig is one tenant's price overrides.
type TenantPricingConfig struct {
	Models []ModelPriceConfig `mapstructure:"models"`

	// ElectricityPricePerKWh and EnginePowerWatts override the deployment's
	// for this tenant; nil and absent engines inherit it.
	ElectricityPricePerKWh *float64           `mapstructure:"electricity_price_per_kwh"`
	EnginePowerWatts       map[string]float64 `mapstructure:"engine_power_watts"`
}

// ModelPriceConfig is one model's list price, in US dollars per million
// tokens of each kind.
type ModelPriceConfig struct {
	// Model is the model name exactly as the backend reports it. Required.
	Model string `mapstructure:"model"`

	// Aliases are further names the same price applies to — a model the CLI
	// reports under a dated and an undated name, for example. Matched
	// exactly; there is no prefix or pattern matching, because a price that
	// applies to a name nobody wrote down is a guess.
	Aliases []string `mapstructure:"aliases"`

	// Engine restricts the entry to one backend ("openvino", "ollama"). Empty
	// applies to every engine. When both exist, the engine-specific entry
	// wins — the same open-weights model can cost differently on two pieces
	// of hardware.
	Engine string `mapstructure:"engine"`

	// Provider is the vendor that list-prices the model ("anthropic",
	// "openai", "local"). Required: it is the provider dimension every
	// report and metric breaks spend down by.
	Provider string `mapstructure:"provider"`

	// The four rates, in US dollars per million tokens. Each must be finite
	// and non-negative. A cache rate left at zero prices those tokens at zero,
	// which is correct for a backend that has no prompt cache.
	InputPerMTok      float64 `mapstructure:"input_per_mtok"`
	OutputPerMTok     float64 `mapstructure:"output_per_mtok"`
	CacheReadPerMTok  float64 `mapstructure:"cache_read_per_mtok"`
	CacheWritePerMTok float64 `mapstructure:"cache_write_per_mtok"`

	// PowerWatts is the device's draw while serving this model, for local
	// inference: the turn is then costed from electricity. Ignored when any
	// per-token rate is set, which is the more precise figure.
	PowerWatts float64 `mapstructure:"power_watts"`
}

// HasTokenRates reports whether the entry names any per-token rate.
func (p ModelPriceConfig) HasTokenRates() bool {
	return p.InputPerMTok > 0 || p.OutputPerMTok > 0 || p.CacheReadPerMTok > 0 || p.CacheWritePerMTok > 0
}

// ValidatePricing reports every problem in a price table, or nil.
//
// It is exported so the component that BUILDS a price book from the table
// refuses exactly what configuration loading refuses, from one rule set.
func ValidatePricing(cfg PricingConfig) error {
	v := NewValidator()
	v.validatePricing(cfg)
	return v.Errors()
}

// validatePricing validates the orchestration.pricing section. It is called
// from validateOrchestration.
func (v *Validator) validatePricing(cfg PricingConfig) {
	priced := len(cfg.Models) > 0
	for _, tenant := range cfg.Tenants {
		priced = priced || len(tenant.Models) > 0
	}
	if priced && strings.TrimSpace(cfg.Version) == "" {
		v.addError("orchestration.pricing.version",
			"required when any model is priced: every priced record names the table revision it was stamped from")
	}
	v.validateModelPrices("orchestration.pricing.models", cfg.Models)
	v.validateElectricity("orchestration.pricing", cfg.ElectricityPricePerKWh, cfg.EnginePowerWatts)
	v.validateEngineDevices("orchestration.pricing.engine_devices", cfg.EngineDevices)
	if cfg.TelemetryRetention < 0 {
		v.addError("orchestration.pricing.telemetry_retention", fmt.Sprintf("must not be negative; got %s", cfg.TelemetryRetention))
	}
	for tenantID, tenant := range cfg.Tenants {
		field := "orchestration.pricing.tenants." + tenantID
		if strings.TrimSpace(tenantID) == "" {
			v.addError(field, "tenant id must not be empty")
		}
		v.validateModelPrices(field+".models", tenant.Models)
		v.validateElectricity(field, tenant.ElectricityPricePerKWh, tenant.EnginePowerWatts)
	}
}

// validateElectricity validates an electricity rate and a set of engine
// draws: each finite and non-negative, every engine named.
func (v *Validator) validateElectricity(section string, pricePerKWh *float64, engineWatts map[string]float64) {
	if pricePerKWh != nil && !finiteNonNegative(*pricePerKWh) {
		v.addError(section+".electricity_price_per_kwh", fmt.Sprintf("must be a finite, non-negative dollar amount; got %v", *pricePerKWh))
	}
	for engine, watts := range engineWatts {
		field := section + ".engine_power_watts." + engine
		if strings.TrimSpace(engine) == "" {
			v.addError(field, "engine name must not be empty")
		}
		if !finiteNonNegative(watts) {
			v.addError(field, fmt.Sprintf("must be a finite, non-negative wattage; got %v", watts))
		}
	}
}

// engineDeviceKinds is the set of device classes an engine may be bound to. It
// mirrors orchestration.DeviceKind, which this package cannot import.
var engineDeviceKinds = map[string]struct{}{
	"gpu": {},
	"npu": {},
	"cpu": {},
}

// validateEngineDevices validates engine-to-device bindings: every engine
// named, every device a known class.
func (v *Validator) validateEngineDevices(section string, devices map[string]string) {
	for engine, device := range devices {
		field := section + "." + engine
		if strings.TrimSpace(engine) == "" {
			v.addError(field, "engine name must not be empty")
		}
		if _, ok := engineDeviceKinds[device]; !ok {
			v.addError(field, fmt.Sprintf("must be one of gpu, npu, cpu; got %q", device))
		}
	}
}

// finiteNonNegative reports whether f is a usable rate or wattage.
func finiteNonNegative(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0
}

// validateModelPrices validates one list of model prices: every entry well
// formed, and no two entries claiming one (engine, name) pair.
func (v *Validator) validateModelPrices(section string, models []ModelPriceConfig) {
	claimed := make(map[string]int)
	for i, price := range models {
		field := fmt.Sprintf("%s[%d]", section, i)
		if strings.TrimSpace(price.Model) == "" {
			v.addError(field+".model", "must not be empty")
			continue
		}
		if strings.TrimSpace(price.Provider) == "" {
			v.addError(field+".provider", "must not be empty: spend is broken down by the vendor that list-prices the model")
		}
		rates := []struct {
			name  string
			value float64
		}{
			{"input_per_mtok", price.InputPerMTok},
			{"output_per_mtok", price.OutputPerMTok},
			{"cache_read_per_mtok", price.CacheReadPerMTok},
			{"cache_write_per_mtok", price.CacheWritePerMTok},
		}
		for _, rate := range rates {
			if !finiteNonNegative(rate.value) {
				v.addError(field+"."+rate.name, fmt.Sprintf("must be a finite, non-negative dollar amount; got %v", rate.value))
			}
		}
		if !finiteNonNegative(price.PowerWatts) {
			v.addError(field+".power_watts", fmt.Sprintf("must be a finite, non-negative wattage; got %v", price.PowerWatts))
		}
		for _, name := range append([]string{price.Model}, price.Aliases...) {
			if strings.TrimSpace(name) == "" {
				v.addError(field+".aliases", "must not contain an empty name")
				continue
			}
			key := price.Engine + "\x00" + name
			if first, duplicate := claimed[key]; duplicate {
				v.addError(field, fmt.Sprintf("model %q on engine %q is already priced at index %d", name, price.Engine, first))
				continue
			}
			claimed[key] = i
		}
	}
}
