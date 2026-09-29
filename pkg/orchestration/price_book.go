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
	"math"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// The price book — what a turn's tokens cost at list price
// =============================================================================
//
// A consumption entry records what the provider metered; this is what turns
// that into money. It is built once from the operator's price table
// (config.PricingConfig) and consulted once per turn, at the moment the turn is
// recorded, so the figure an entry carries is the price that applied WHEN THE
// TURN RAN: a price changed next week never re-prices this week's turns
// (go-quicbaas#791, #1058).
//
// A MODEL THE TABLE DOES NOT NAME IS NOT PRICED. The entry keeps its tokens and
// carries no price reference, and every reader renders it as unmeasured. There
// is no fallback rate, no family match and no prefix match: each of those is a
// price nobody wrote down.

// priceCurrency is the currency every price and every stamped figure is in.
// There is exactly one, so no roll-up ever sums two currencies.
const priceCurrency = "USD"

// PriceCurrency returns the currency every stamped figure is denominated in.
func PriceCurrency() string { return priceCurrency }

// microsPerDollar converts the table's dollar rates to integer micro-dollars.
const microsPerDollar = 1_000_000

// tokensPerRateUnit is the token count a rate is quoted per.
const tokensPerRateUnit = 1_000_000

// defaultPriceScope names the deployment-wide table in a price reference.
const defaultPriceScope = "default"

// modelPrice is one resolved table entry, with its rates in integer
// micro-dollars per million tokens.
type modelPrice struct {
	ref      string
	provider string

	inputMicros      int64
	outputMicros     int64
	cacheReadMicros  int64
	cacheWriteMicros int64

	// hasTokenRates is whether the entry named any per-token rate; when it
	// did, the turn is priced by the rates whatever powerWatts says.
	hasTokenRates bool

	// powerWatts is the device draw the entry declares for local inference,
	// zero when it declares none.
	powerWatts float64
}

// priceKey identifies a table entry. An empty engine is the entry that
// applies to every engine.
type priceKey struct {
	engine string
	model  string
}

// PriceBook resolves a turn's list price from the operator's price table,
// honouring per-tenant overrides. The zero value and a nil *PriceBook hold no
// table and cost electricity at DefaultElectricityPricePerKWh, which is the
// behaviour of a deployment that configured no pricing: every turn is
// unpriced unless a tenant's runtime cost settings give its engine a draw.
type PriceBook struct {
	version  string
	defaults map[priceKey]modelPrice
	tenants  map[string]map[priceKey]modelPrice

	// electricity is the deployment's rate and draws, and tenantElectricity
	// each tenant's configured overrides.
	electricity       electricityConfig
	tenantElectricity map[string]electricityConfig

	// engineDevices binds a local engine to the device class its turns draw
	// on, so their energy can be measured (turn_energy.go).
	engineDevices map[string]DeviceKind
}

// electricityConfig is one configuration scope's electricity rate and engine
// draws. A nil rate and an absent engine inherit the next scope out.
type electricityConfig struct {
	pricePerKWh *float64
	engineWatts map[string]float64
}

// NewPriceBook builds a price book from the operator's price table, refusing
// exactly what configuration loading refuses.
func NewPriceBook(cfg config.PricingConfig) (*PriceBook, error) {
	if err := config.ValidatePricing(cfg); err != nil {
		return nil, &PriceTableInvalidError{Cause: err}
	}
	book := &PriceBook{
		version:           cfg.Version,
		defaults:          indexModelPrices(cfg.Version, defaultPriceScope, cfg.Models, nil),
		tenants:           make(map[string]map[priceKey]modelPrice, len(cfg.Tenants)),
		electricity:       electricityConfig{pricePerKWh: cfg.ElectricityPricePerKWh, engineWatts: cfg.EnginePowerWatts},
		tenantElectricity: make(map[string]electricityConfig, len(cfg.Tenants)),
		engineDevices:     make(map[string]DeviceKind, len(cfg.EngineDevices)),
	}
	for engine, device := range cfg.EngineDevices {
		// Validated above; a binding config accepted is a kind this package
		// knows.
		if kind, ok := ParseDeviceKind(device); ok {
			book.engineDevices[engine] = kind
		}
	}
	declared := declaredAliases(cfg.Models)
	for tenantID, tenant := range cfg.Tenants {
		book.tenants[tenantID] = indexModelPrices(cfg.Version, "tenant/"+tenantID, tenant.Models, declared)
		book.tenantElectricity[tenantID] = electricityConfig{
			pricePerKWh: tenant.ElectricityPricePerKWh,
			engineWatts: tenant.EnginePowerWatts,
		}
	}
	return book, nil
}

// declaredAliases maps each deployment table entry — engine and model — to
// the aliases it declares. The engine is part of the key: an alias is only
// ever a name on the engine its entry is scoped to, and the same alias may
// name a different model on another engine (go-quicbaas#1058 review).
func declaredAliases(models []config.ModelPriceConfig) map[priceKey][]string {
	aliases := make(map[priceKey][]string, len(models))
	for _, m := range models {
		key := priceKey{engine: m.Engine, model: m.Model}
		aliases[key] = append(aliases[key], m.Aliases...)
	}
	return aliases
}

// indexModelPrices keys a list of validated table entries by every name they
// answer to.
//
// A TENANT OVERRIDE ANSWERS TO THE DEPLOYMENT'S ALIASES TOO. An alias names
// the same model, so a tenant that re-prices claude-opus-5 has re-priced
// "opus": otherwise the CLI's short name would reach the deployment's price
// and bypass the tenant's (go-quicbaas#1058 review). inherited carries those
// aliases, keyed by the entry's engine and model, so an override inherits only
// the aliases its own engine scope declares for the model; a name the tenant's own table already answers to keeps the
// tenant's own entry, so an explicit tenant alias or model always wins.
func indexModelPrices(version, scope string, models []config.ModelPriceConfig, inherited map[priceKey][]string) map[priceKey]modelPrice {
	index := make(map[priceKey]modelPrice, len(models))
	prices := make([]modelPrice, len(models))
	for i, m := range models {
		engine := m.Engine
		if engine == "" {
			engine = "*"
		}
		price := modelPrice{
			ref:              version + ":" + scope + ":" + engine + "/" + m.Model,
			provider:         m.Provider,
			inputMicros:      dollarsToMicros(m.InputPerMTok),
			outputMicros:     dollarsToMicros(m.OutputPerMTok),
			cacheReadMicros:  dollarsToMicros(m.CacheReadPerMTok),
			cacheWriteMicros: dollarsToMicros(m.CacheWritePerMTok),
			hasTokenRates:    m.HasTokenRates(),
			powerWatts:       m.PowerWatts,
		}
		for _, name := range append([]string{m.Model}, m.Aliases...) {
			index[priceKey{engine: m.Engine, model: name}] = price
		}
		prices[i] = price
	}
	for i, m := range models {
		for _, alias := range inherited[priceKey{engine: m.Engine, model: m.Model}] {
			key := priceKey{engine: m.Engine, model: alias}
			if _, taken := index[key]; !taken {
				index[key] = prices[i]
			}
		}
	}
	return index
}

// dollarsToMicros converts a validated dollar rate to integer micro-dollars,
// rounding to the nearest micro.
func dollarsToMicros(dollars float64) int64 {
	return int64(math.Round(dollars * microsPerDollar))
}

// lookup resolves the price of model on engine for tenantID. The tenant's own
// table is consulted before the deployment's, and within each an
// engine-specific entry before the one that applies to every engine.
func (b *PriceBook) lookup(tenantID, engine, model string) (modelPrice, bool) {
	if b == nil || model == "" {
		return modelPrice{}, false
	}
	if price, ok := lookupIn(b.tenants[tenantID], engine, model); ok {
		return price, true
	}
	return lookupIn(b.defaults, engine, model)
}

// Lists reports whether the price table — the tenant's or the deployment's,
// aliases included — names model on engine. It is what bounds a metric's
// model label (go-quicbaas#1058, ruling F7): a name an operator wrote down, or
// "other".
func (b *PriceBook) Lists(tenantID, engine, model string) bool {
	_, ok := b.lookup(tenantID, engine, model)
	return ok
}

// lookupIn resolves model on engine in one table: the engine-specific entry,
// then the one for every engine.
func lookupIn(table map[priceKey]modelPrice, engine, model string) (modelPrice, bool) {
	if table == nil || model == "" {
		return modelPrice{}, false
	}
	if price, ok := table[priceKey{engine: engine, model: model}]; ok {
		return price, true
	}
	price, ok := table[priceKey{model: model}]
	return price, ok
}

// Price bases: how a ledger entry's money was worked out.
const (
	// PriceBasisTokenRate is a turn priced from per-token list rates.
	PriceBasisTokenRate = "token_rate"

	// PriceBasisElectricity is a local turn costed from the device's draw,
	// the turn's wall-clock duration and the electricity rate.
	PriceBasisElectricity = "electricity"
)

// Stamp prices entry in place: its list-price-equivalent figure, the basis
// and price entry that figure came from, the vendor that list-prices the
// model, and — for a turn costed from electricity — the energy it drew.
// settings is the tenant's runtime cost settings, or nil for none.
//
// THE ORDER IS THE PRECISION ORDER. An entry with per-token rates is priced by
// them: an explicit rate is the operator's exact figure. Otherwise a turn with
// a device draw is costed from electricity. Otherwise an entry the table
// names is priced by its (zero) rates, and a model the table does not name is
// unpriced.
//
// A turn costed from electricity with no measured duration is left UNPRICED —
// no reference, no figure — because its cost is unknown, not zero. Stamp
// reports whether it priced the entry.
func (b *PriceBook) Stamp(entry *ConsumptionEntry, settings *CostSettings) bool {
	return b.StampWithEnergy(entry, settings, nil)
}

// StampWithEnergy is Stamp with the energy the fleet's telemetry measured for
// the turn, nil when nothing measured it.
//
// A MEASUREMENT OUTRANKS A DECLARATION. A turn not priced by per-token rates
// whose energy was measured is costed from that energy, whether or not a draw
// is configured for its engine, and its entry says so
// (EnergyProvenance measured or derived). Only a turn nothing measured is
// costed from the configured draw, and its entry is marked estimated.
func (b *PriceBook) StampWithEnergy(entry *ConsumptionEntry, settings *CostSettings, energy *MeasuredEnergy) bool {
	if entry == nil {
		return false
	}
	price, found := b.lookup(entry.TenantID, entry.Engine, entry.Model)
	if found && price.hasTokenRates {
		return stampTokens(entry, price)
	}
	if settings.Unavailable() {
		// The tenant's overrides are unknown, so an electricity cost worked
		// from the configuration alone could be the wrong figure recorded as
		// a known one. A turn the configuration costs from electricity, or
		// one the table names, is recorded with its cost unknown.
		if _, ok := b.powerDraw(entry.TenantID, entry.Engine, entry.Model, nil); ok || found {
			entry.PriceBasis = PriceBasisElectricity
			entry.Provider = price.provider
			if !found {
				entry.Provider = localProvider
			}
		}
		return false
	}
	provider := price.provider
	if !found {
		provider = localProvider
	}
	if energy != nil && energy.Provenance.FromTelemetry() {
		return b.stampMeasuredElectricity(entry, energy, provider, settings)
	}
	if draw, ok := b.powerDraw(entry.TenantID, entry.Engine, entry.Model, settings); ok {
		return b.stampElectricity(entry, draw, provider, settings)
	}
	if found {
		return stampTokens(entry, price)
	}
	return false
}

// localProvider is the provider an electricity-costed turn on a model the
// table does not name is reported under.
const localProvider = "local"

// stampTokens prices entry from per-token rates. A turn that consumed no
// tokens is not priced by them, and neither is one whose counts are
// UNMEASURED: repaired accounting — a negative count clamped to zero — is not
// a figure a price can be applied to, and pricing it would report complete
// coverage over counts nobody measured (go-quicbaas#1058 review). Such a turn
// stays unpriced, which the roll-up counts as missing coverage.
func stampTokens(entry *ConsumptionEntry, price modelPrice) bool {
	if entry.TotalTokens() == 0 || entry.Unmeasured {
		return false
	}
	scaled := entry.InputTokens*price.inputMicros +
		entry.OutputTokens*price.outputMicros +
		entry.CacheReadTokens*price.cacheReadMicros +
		entry.CacheWriteTokens*price.cacheWriteMicros
	// Rounded half up to the nearest micro-dollar. The products are summed
	// before dividing, so four sub-micro remainders are not each rounded
	// away.
	entry.EquivalentMicros = (scaled + tokensPerRateUnit/2) / tokensPerRateUnit
	entry.PriceRef = price.ref
	entry.Provider = price.provider
	entry.PriceBasis = PriceBasisTokenRate
	return true
}

// stampElectricity costs entry from draw, its duration and the electricity
// rate in force for its tenant:
//
//	kWh = watts × seconds / 3.6e6;   dollars = kWh × price_per_kWh
//
// Energy is recorded in milliwatt-hours (watts × milliseconds / 3600) and
// money in micro-dollars (mWh × price, since a kWh is 1e6 mWh and a dollar
// 1e6 micros), each rounded once from the unrounded product.
func (b *PriceBook) stampElectricity(entry *ConsumptionEntry, draw powerDraw, provider string, settings *CostSettings) bool {
	if entry.DurationMillis <= 0 {
		// The basis is known and the figure is not: see
		// ConsumptionEntry.PriceBasis.
		entry.PriceBasis = PriceBasisElectricity
		entry.Provider = provider
		return false
	}
	rate := b.ElectricityRate(entry.TenantID, settings)
	milliWh := draw.watts * float64(entry.DurationMillis) / 3600
	entry.EnergyMilliWh = int64(math.Round(milliWh))
	entry.EquivalentMicros = int64(math.Round(milliWh * rate.PricePerKWh))
	entry.PriceBasis = PriceBasisElectricity
	entry.Provider = provider
	entry.PriceRef = b.electricityRef(entry, draw, rate)
	entry.EnergyProvenance = ProvenanceEstimated
	return true
}

// stampMeasuredElectricity costs entry from the energy its device was measured
// to draw over the turn, at the electricity rate in force for its tenant. The
// energy is rounded once to the milliwatt-hour and the money once to the
// micro-dollar, from the unrounded figure, as stampElectricity does.
func (b *PriceBook) stampMeasuredElectricity(entry *ConsumptionEntry, energy *MeasuredEnergy, provider string, settings *CostSettings) bool {
	rate := b.ElectricityRate(entry.TenantID, settings)
	entry.EnergyMilliWh = int64(math.Round(energy.MilliWh))
	entry.EquivalentMicros = int64(math.Round(energy.MilliWh * rate.PricePerKWh))
	entry.PriceBasis = PriceBasisElectricity
	entry.Provider = provider
	entry.EnergyProvenance = energy.Provenance
	ref := PriceBasisElectricity + ":" + energy.Provenance.String() + ":" + string(energy.Kind) + "@" + entry.Host + ":" +
		entry.Engine + "/" + entry.Model + ":" +
		string(rate.Source) + "@" + strconv.FormatFloat(rate.PricePerKWh, 'f', -1, 64) + "/kWh"
	if b != nil && b.version != "" {
		ref = b.version + ":" + ref
	}
	entry.PriceRef = ref
	return true
}

// EngineDevice returns the device class engine's turns draw on, or false when
// the deployment binds it to none (orchestration.pricing.engine_devices).
func (b *PriceBook) EngineDevice(engine string) (DeviceKind, bool) {
	if b == nil {
		return "", false
	}
	kind, ok := b.engineDevices[engine]
	return kind, ok
}

// EngineDraw returns the configured draw a turn of model on engine is
// estimated at, in watts, or false when none is configured. It is the scale an
// NPU's measured busy fraction is multiplied by.
func (b *PriceBook) EngineDraw(tenantID, engine, model string, settings *CostSettings) (float64, bool) {
	draw, ok := b.powerDraw(tenantID, engine, model, settings)
	return draw.watts, ok
}

// electricityRef names everything an electricity figure was worked from, so
// it can be traced as a token price can: the table revision when there is
// one, where the draw and the rate came from, and their values.
func (b *PriceBook) electricityRef(entry *ConsumptionEntry, draw powerDraw, rate ElectricityRate) string {
	ref := PriceBasisElectricity + ":" + draw.source + ":" + entry.Engine + "/" + entry.Model +
		"@" + strconv.FormatFloat(draw.watts, 'f', -1, 64) + "W:" +
		string(rate.Source) + "@" + strconv.FormatFloat(rate.PricePerKWh, 'f', -1, 64) + "/kWh"
	if b != nil && b.version != "" {
		ref = b.version + ":" + ref
	}
	return ref
}

// powerDraw is a resolved device draw and the scope that declared it.
type powerDraw struct {
	watts  float64
	source string
}

// powerDraw resolves the device draw a turn of model on engine is costed at,
// tenant scopes before the deployment's and, within a scope, the most
// specific declaration first:
//
//  1. the tenant's runtime cost settings, per engine;
//  2. the tenant's configured price entry for the model, then its
//     configured per-engine draw;
//  3. the deployment's price entry for the model, then its per-engine draw.
//
// The first scope that names the engine decides. A per-engine draw of zero is
// a decision too — "this engine is not costed from electricity here" — so a
// tenant can switch off a draw the deployment declares. A model entry's zero
// draw is simply undeclared.
func (b *PriceBook) powerDraw(tenantID, engine, model string, settings *CostSettings) (powerDraw, bool) {
	decide := func(watts float64, source string) (powerDraw, bool) {
		return powerDraw{watts: watts, source: source}, watts > 0
	}
	if settings != nil {
		if watts, ok := settings.EnginePowerWatts[engine]; ok {
			return decide(watts, "tenant-settings")
		}
	}
	if b == nil {
		return powerDraw{}, false
	}
	if price, ok := lookupIn(b.tenants[tenantID], engine, model); ok && price.powerWatts > 0 {
		return decide(price.powerWatts, "tenant/"+tenantID)
	}
	if watts, ok := b.tenantElectricity[tenantID].engineWatts[engine]; ok {
		return decide(watts, "tenant/"+tenantID)
	}
	if price, ok := lookupIn(b.defaults, engine, model); ok && price.powerWatts > 0 {
		return decide(price.powerWatts, defaultPriceScope)
	}
	if watts, ok := b.electricity.engineWatts[engine]; ok {
		return decide(watts, defaultPriceScope)
	}
	return powerDraw{}, false
}

// ElectricityRateSource names where an electricity rate came from.
type ElectricityRateSource string

// The electricity rate's sources, most specific first.
const (
	ElectricityFromTenantSettings   ElectricityRateSource = "tenant_settings"
	ElectricityFromTenantConfig     ElectricityRateSource = "tenant_config"
	ElectricityFromDeploymentConfig ElectricityRateSource = "deployment_config"
	ElectricityFromDefault          ElectricityRateSource = "default"
)

// ElectricityRate is the rate a tenant's turns are costed at and where it
// came from.
type ElectricityRate struct {
	PricePerKWh float64               `json:"price_per_kwh"`
	Source      ElectricityRateSource `json:"source"`
}

// ElectricityRate resolves the electricity rate for tenantID: its runtime
// cost settings, then its configured override, then the deployment's, then
// config.DefaultElectricityPricePerKWh.
func (b *PriceBook) ElectricityRate(tenantID string, settings *CostSettings) ElectricityRate {
	if settings != nil && settings.ElectricityPricePerKWh != nil {
		return ElectricityRate{PricePerKWh: *settings.ElectricityPricePerKWh, Source: ElectricityFromTenantSettings}
	}
	if b != nil {
		if price := b.tenantElectricity[tenantID].pricePerKWh; price != nil {
			return ElectricityRate{PricePerKWh: *price, Source: ElectricityFromTenantConfig}
		}
		if price := b.electricity.pricePerKWh; price != nil {
			return ElectricityRate{PricePerKWh: *price, Source: ElectricityFromDeploymentConfig}
		}
	}
	return ElectricityRate{PricePerKWh: config.DefaultElectricityPricePerKWh, Source: ElectricityFromDefault}
}

// EngineDraws is every per-engine draw in force for tenantID, the
// deployment's overlaid by the tenant's configured draws and then by its
// runtime settings. Per-model draws in the price table are not included.
func (b *PriceBook) EngineDraws(tenantID string, settings *CostSettings) map[string]float64 {
	draws := make(map[string]float64)
	if b != nil {
		for engine, watts := range b.electricity.engineWatts {
			draws[engine] = watts
		}
		for engine, watts := range b.tenantElectricity[tenantID].engineWatts {
			draws[engine] = watts
		}
	}
	if settings != nil {
		for engine, watts := range settings.EnginePowerWatts {
			draws[engine] = watts
		}
	}
	return draws
}

// ErrPriceTableInvalid is the sentinel for a price table that cannot be built.
var ErrPriceTableInvalid = errors.New("orchestration: invalid price table")

// PriceTableInvalidError is a price table that failed validation.
type PriceTableInvalidError struct {
	Cause error
}

// Error implements the error interface.
func (e *PriceTableInvalidError) Error() string {
	return "orchestration: invalid price table: " + e.Cause.Error()
}

// Unwrap exposes both the sentinel and the validation failures.
func (e *PriceTableInvalidError) Unwrap() []error {
	return []error{ErrPriceTableInvalid, e.Cause}
}
