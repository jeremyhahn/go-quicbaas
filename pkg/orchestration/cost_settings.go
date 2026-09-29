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
	"errors"
	"math"
	"strconv"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// A tenant's runtime cost settings — go-quicbaas#1058
// =============================================================================
//
// The price table is configuration, read at startup. What local inference
// costs depends on two facts an operator learns and changes while the node
// runs — what their electricity costs, and what each device draws — so those
// two are also settable at runtime, per tenant, in one record in the tenant's
// own orchestration KV:
//
//	orch/costsettings/{tenant}
//
// A setting here beats the tenant's configured override, which beats the
// deployment's (PriceBook.ElectricityRate, PriceBook.powerDraw). A tenant
// with no record inherits the configuration unchanged.

// costSettingsKeyspace prefixes the one record per tenant.
const costSettingsKeyspace = "orch/costsettings/"

// CostSettings is a tenant's runtime cost settings.
type CostSettings struct {
	TenantID string `json:"tenant_id"`

	// ElectricityPricePerKWh is what a kilowatt-hour costs the tenant, in US
	// dollars. Nil inherits the configured rate.
	ElectricityPricePerKWh *float64 `json:"electricity_price_per_kwh,omitempty"`

	// EnginePowerWatts is each local engine's device draw while serving, in
	// watts, keyed by engine name. An engine named with zero watts is not
	// costed from electricity for this tenant, whatever the configuration
	// says; an engine not named inherits the configuration.
	EnginePowerWatts map[string]float64 `json:"engine_power_watts,omitempty"`

	// UpdatedAt is when the settings were last written; zero for a tenant
	// that never set any.
	UpdatedAt time.Time `json:"updated_at,omitzero"`

	// unavailable marks settings that could not be read. It is never stored
	// or serialized: it tells the price book the tenant's overrides are
	// UNKNOWN, which is not the same as having none.
	unavailable bool
}

// UnavailableCostSettings is the settings value for a tenant whose settings
// could not be read. A turn priced with it keeps its per-token price, which
// does not depend on them, but any electricity cost is recorded as unknown:
// the tenant may have set a different rate or draw.
func UnavailableCostSettings(tenantID string) *CostSettings {
	return &CostSettings{TenantID: tenantID, unavailable: true}
}

// Unavailable reports whether the settings could not be read.
func (c *CostSettings) Unavailable() bool {
	return c != nil && c.unavailable
}

// Empty reports whether the settings override nothing.
func (c *CostSettings) Empty() bool {
	return c == nil || (c.ElectricityPricePerKWh == nil && len(c.EnginePowerWatts) == 0)
}

// Validate refuses a rate or a draw that is negative or not finite, and an
// engine that is not a plain name.
func (c *CostSettings) Validate() error {
	if c == nil {
		return &CostSettingsInvalidError{Field: "settings", Message: "must not be nil"}
	}
	if c.ElectricityPricePerKWh != nil && !finiteNonNegative(*c.ElectricityPricePerKWh) {
		return &CostSettingsInvalidError{Field: "electricity_price_per_kwh",
			Message: "must be a finite, non-negative dollar amount; got " + strconv.FormatFloat(*c.ElectricityPricePerKWh, 'g', -1, 64)}
	}
	for engine, watts := range c.EnginePowerWatts {
		if err := validateKeySegment("engine", engine); err != nil {
			return &CostSettingsInvalidError{Field: "engine_power_watts", Message: err.Error()}
		}
		if !finiteNonNegative(watts) {
			return &CostSettingsInvalidError{Field: "engine_power_watts." + engine,
				Message: "must be a finite, non-negative wattage; got " + strconv.FormatFloat(watts, 'g', -1, 64)}
		}
	}
	return nil
}

// finiteNonNegative reports whether f is a usable rate or wattage.
func finiteNonNegative(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0
}

// CostSettingsStore persists each tenant's runtime cost settings.
type CostSettingsStore interface {
	// Get returns the tenant's settings. A tenant that never set any gets
	// empty settings and no error: it inherits the configuration.
	Get(ctx context.Context, tenantID string) (*CostSettings, error)

	// Put validates and replaces the tenant's settings. Settings that
	// override nothing remove the record.
	Put(ctx context.Context, settings *CostSettings) error
}

// kvCostSettingsStore is the KV-backed CostSettingsStore.
type kvCostSettingsStore struct {
	kv interfaces.KVStore
}

// Compile-time interface satisfaction check.
var _ CostSettingsStore = (*kvCostSettingsStore)(nil)

// NewCostSettingsStore returns a CostSettingsStore backed by kv — the
// tenant-routed orchestration KV, so each record lives inside its tenant's
// barrier.
func NewCostSettingsStore(kv interfaces.KVStore) CostSettingsStore {
	return &kvCostSettingsStore{kv: kv}
}

// NewInMemoryCostSettingsStore returns a memory-backed binding of the same
// store, for tests and embedded use.
func NewInMemoryCostSettingsStore() CostSettingsStore {
	return NewCostSettingsStore(NewMemoryTaskKV())
}

// Get implements CostSettingsStore.
func (s *kvCostSettingsStore) Get(ctx context.Context, tenantID string) (*CostSettings, error) {
	if err := s.check(ctx, tenantID); err != nil {
		return nil, err
	}
	raw, err := s.kv.Get(ctx, costSettingsKeyspace+tenantID)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return &CostSettings{TenantID: tenantID}, nil
		}
		return nil, &CostSettingsStoreError{TenantID: tenantID, Op: "read", Cause: err}
	}
	// Some stores answer an absent key with no bytes rather than NotFound;
	// both mean the tenant never set any.
	if len(raw) == 0 {
		return &CostSettings{TenantID: tenantID}, nil
	}
	var settings CostSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, &CostSettingsStoreError{TenantID: tenantID, Op: "decode", Cause: err}
	}
	return &settings, nil
}

// Put implements CostSettingsStore.
func (s *kvCostSettingsStore) Put(ctx context.Context, settings *CostSettings) error {
	if settings == nil {
		return &CostSettingsInvalidError{Field: "settings", Message: "must not be nil"}
	}
	if err := s.check(ctx, settings.TenantID); err != nil {
		return err
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	key := costSettingsKeyspace + settings.TenantID
	if settings.Empty() {
		if err := s.kv.Delete(ctx, key); err != nil && !qerrors.IsNotFound(err) {
			return &CostSettingsStoreError{TenantID: settings.TenantID, Op: "delete", Cause: err}
		}
		settings.UpdatedAt = time.Time{}
		return nil
	}
	stored := *settings
	stored.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(stored)
	if err != nil {
		return &CostSettingsStoreError{TenantID: settings.TenantID, Op: "encode", Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &CostSettingsStoreError{TenantID: settings.TenantID, Op: "write", Cause: err}
	}
	settings.UpdatedAt = stored.UpdatedAt
	return nil
}

// check refuses a store with no KV, a missing tenant, and a context scoped to
// another tenant.
func (s *kvCostSettingsStore) check(ctx context.Context, tenantID string) error {
	if s.kv == nil {
		return &CostSettingsStoreError{TenantID: tenantID, Op: "open", Cause: ErrCostSettingsStore}
	}
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return &CostSettingsInvalidError{Field: "tenant_id", Message: err.Error()}
	}
	return scopeCheck(ctx, tenantID)
}

var (
	// ErrCostSettingsInvalid is the sentinel for refused cost settings.
	ErrCostSettingsInvalid = errors.New("orchestration: invalid cost settings")

	// ErrCostSettingsStore is the sentinel for a store that could not answer.
	ErrCostSettingsStore = errors.New("orchestration: cost settings store failed")
)

// CostSettingsInvalidError is cost settings refused at the write.
type CostSettingsInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *CostSettingsInvalidError) Error() string {
	return ErrCostSettingsInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrCostSettingsInvalid.
func (e *CostSettingsInvalidError) Unwrap() error { return ErrCostSettingsInvalid }

// CostSettingsStoreError is a store that could not answer.
type CostSettingsStoreError struct {
	TenantID string
	Op       string
	Cause    error
}

// Error implements the error interface.
func (e *CostSettingsStoreError) Error() string {
	return ErrCostSettingsStore.Error() + ": " + e.Op + " tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap exposes the sentinel and the cause.
func (e *CostSettingsStoreError) Unwrap() []error {
	return []error{ErrCostSettingsStore, e.Cause}
}

// =============================================================================
// The read and write every transport serves
// =============================================================================

// CostSettingsAccessor is implemented by an agent service that holds the cost
// settings store and the price book they are resolved against. Every
// transport reaches both through this one declaration.
type CostSettingsAccessor interface {
	// CostSettingsStore returns the store, or nil when none is wired.
	CostSettingsStore() CostSettingsStore

	// PriceBook returns the configured price book; nil prices from the
	// defaults alone.
	PriceBook() *PriceBook
}

// CostSettingsReport is a tenant's runtime settings and what its turns are
// costed at once they are layered over the configuration.
type CostSettingsReport struct {
	// Settings is what the tenant set at runtime.
	Settings CostSettings `json:"settings"`

	// Electricity is the rate in force and where it came from.
	Electricity ElectricityRate `json:"electricity"`

	// EngineDraws is every per-engine draw in force, configured and set.
	// Per-model draws in the price table are not included.
	EngineDraws map[string]float64 `json:"engine_power_watts,omitempty"`
}

// ReadCostSettings returns a tenant's settings and the rates in force. A
// store that is not wired fails the read: "nothing set" and "unknown" are
// different answers.
func ReadCostSettings(ctx context.Context, store CostSettingsStore, book *PriceBook, tenantID string) (*CostSettingsReport, error) {
	if store == nil {
		return nil, &CostSettingsStoreError{TenantID: tenantID, Op: "open", Cause: ErrCostSettingsStore}
	}
	settings, err := store.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &CostSettingsReport{
		Settings:    *settings,
		Electricity: book.ElectricityRate(tenantID, settings),
		EngineDraws: book.EngineDraws(tenantID, settings),
	}, nil
}

// WriteCostSettings replaces a tenant's settings and returns the report the
// replacement leaves in force. Settings that override nothing restore the
// configuration.
func WriteCostSettings(ctx context.Context, store CostSettingsStore, book *PriceBook, settings *CostSettings) (*CostSettingsReport, error) {
	if store == nil {
		tenantID := ""
		if settings != nil {
			tenantID = settings.TenantID
		}
		return nil, &CostSettingsStoreError{TenantID: tenantID, Op: "open", Cause: ErrCostSettingsStore}
	}
	if err := store.Put(ctx, settings); err != nil {
		return nil, err
	}
	return ReadCostSettings(ctx, store, book, settings.TenantID)
}

// Compile-time check: the agent service serves the cost-settings API.
var _ CostSettingsAccessor = (*agentServiceImpl)(nil)
