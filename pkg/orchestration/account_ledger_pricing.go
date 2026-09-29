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
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

// =============================================================================
// Late pricing — the one amendment the ledger admits
// =============================================================================
//
// An entry is written once and never edited, with ONE exception: an entry
// recorded without a price may be priced later, exactly once. A turn is
// unpriced when its model was not in the price table when it ran (the
// claude-opus-5 turns recorded before the table named it), or when the
// tenant's cost settings could not be read. Such a turn cost something nobody
// had written down yet; leaving it a dash forever would under-report every
// total it is part of.
//
// THE AMENDMENT ONLY PRICES. The priced record must equal the stored one in
// every field but the price, energy and billing figures, and the stored one
// must carry no price. A priced entry is never re-priced — a price changed
// next week still does not re-price this week's turns — and nothing else about
// a turn can be rewritten through this door.
//
// IT IS DECIDED BY THE STORE, plane by plane. Each plane is compare-and-swapped
// from the exact stored bytes to the priced bytes, the quota planes before the
// attribution plane for the reason Record orders them: a partial amendment
// leaves the price visible to the limit and missing from the invoice, and the
// next sweep, which reads the attribution plane, finishes it. Two sweepers on
// two hosts converge: the loser of a swap finds the priced bytes already there.

// ConsumptionPricer prices ledger entries that were recorded without a price.
type ConsumptionPricer interface {
	// PriceUnpriced offers every unpriced entry under tenantID to price, and
	// stores each one price reports it priced. An empty tenantID visits every
	// tenant the store routes the call to. A failure to amend one entry is
	// counted and does not stop the sweep; a failure to read the ledger fails
	// it.
	PriceUnpriced(ctx context.Context, tenantID string, price func(*ConsumptionEntry) bool) (*PricingSweepResult, error)
}

// PricingSweepResult counts what one pricing sweep did.
type PricingSweepResult struct {
	// Unpriced is how many unpriced entries were examined, Priced how many of
	// them were priced and stored, and Failed how many were priced but could
	// not be stored. Unpriced - Priced - Failed stayed unpriced because
	// nothing prices them yet.
	Unpriced int `json:"unpriced"`
	Priced   int `json:"priced"`
	Failed   int `json:"failed"`

	// TenantsFailed counts the tenants whose ledger could not be read.
	TenantsFailed int `json:"tenants_failed"`

	// Failures are the typed errors behind Failed, each naming its entry, so
	// a sweep that could not store a price says why rather than only how
	// often.
	Failures []error `json:"-"`
}

// add folds another sweep's counts into r.
func (r *PricingSweepResult) add(other *PricingSweepResult) {
	r.Unpriced += other.Unpriced
	r.Priced += other.Priced
	r.Failed += other.Failed
	r.TenantsFailed += other.TenantsFailed
	r.Failures = append(r.Failures, other.Failures...)
}

// Compile-time interface check.
var _ ConsumptionPricer = (*kvConsumptionLedger)(nil)

// storedEntry is one attribution-plane record: its decoded form and the exact
// bytes it is stored as, which every plane holds identically.
type storedEntry struct {
	entry ConsumptionEntry
	raw   []byte
}

// PriceUnpriced implements ConsumptionPricer.
func (l *kvConsumptionLedger) PriceUnpriced(ctx context.Context, tenantID string, price func(*ConsumptionEntry) bool) (*PricingSweepResult, error) {
	if l.kv == nil {
		return nil, &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if price == nil {
		return nil, &ConsumptionInvalidError{Field: "price", Message: "a pricing function is required"}
	}
	swapper, ok := l.kv.(guardedSwapper)
	if !ok {
		return nil, &ConsumptionUnguardedError{
			Message: "pricing an entry late is a compare-and-swap on every plane; this store cannot decide one",
		}
	}
	prefix := consumptionChainKeyspace
	if tenantID != "" {
		if err := requireSegment("tenant_id", tenantID); err != nil {
			return nil, err
		}
		prefix += tenantID + keySeparator
	}

	// Read first, amend after: a write inside the scan would mutate the
	// keyspace the iterator is walking.
	var unpriced []storedEntry
	err := l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var entry ConsumptionEntry
		if decodeErr := json.Unmarshal(value, &entry); decodeErr != nil {
			return &ConsumptionReadError{Key: key, Message: "stored consumption record could not be decoded", Cause: decodeErr}
		}
		if !entry.Priced() {
			unpriced = append(unpriced, storedEntry{entry: entry, raw: append([]byte(nil), value...)})
		}
		return nil
	})
	if err != nil {
		var readErr *ConsumptionReadError
		if errors.As(err, &readErr) {
			return nil, err
		}
		return nil, &ConsumptionReadError{Key: prefix, Message: "subtree scan failed", Cause: err}
	}

	result := &PricingSweepResult{Unpriced: len(unpriced)}
	for _, stored := range unpriced {
		priced := stored.entry
		if !price(&priced) || !priced.Priced() {
			continue
		}
		if err := l.amendPrice(ctx, swapper, stored, priced); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, err)
			continue
		}
		result.Priced++
	}
	return result, nil
}

// amendPrice swaps every plane of stored to priced.
func (l *kvConsumptionLedger) amendPrice(ctx context.Context, swapper guardedSwapper, stored storedEntry, priced ConsumptionEntry) error {
	if !onlyPriceDiffers(stored.entry, priced) {
		return &ConsumptionInvalidError{
			Field:   "amendment",
			Message: "a late price may change the price, energy and billing figures of an unpriced entry and nothing else",
		}
	}
	data, err := json.Marshal(priced)
	if err != nil {
		return &ConsumptionWriteError{EntryID: priced.EntryID, Plane: consumptionPlaneChain, Cause: err}
	}
	type plane struct{ name, key string }
	var planes []plane
	if priced.Pooled() {
		planes = append(planes, plane{consumptionPlaneWindow, l.windowKey(priced)})
	}
	if priced.ProjectID != consumptionUnattributed {
		planes = append(planes, plane{consumptionPlaneProject, l.projectWindowKey(priced)})
	}
	planes = append(planes,
		plane{consumptionPlaneTenant, l.tenantWindowKey(priced)},
		plane{consumptionPlaneChain, l.chainKey(priced)})
	for _, p := range planes {
		if err := l.swap(ctx, swapper, p.name, p.key, stored.raw, data, priced.EntryID); err != nil {
			return err
		}
	}
	return nil
}

// swap moves one plane's record from expected to next. Losing the swap is the
// ordinary case when another sweeper got there first, and is success when the
// stored bytes are already next.
func (l *kvConsumptionLedger) swap(ctx context.Context, swapper guardedSwapper, plane, key string, expected, next []byte, entryID string) error {
	swapped, err := swapper.CompareAndSwap(ctx, key, expected, next)
	if err != nil {
		return &ConsumptionWriteError{EntryID: entryID, Plane: plane, Key: key, Cause: err}
	}
	if swapped {
		return nil
	}
	current, err := l.kv.Get(ctx, key)
	if err != nil {
		return &ConsumptionReadError{Key: key, Message: "entry " + entryID + " lost a pricing swap on the " + plane + " plane and could not be read back", Cause: err}
	}
	if !bytes.Equal(current, next) {
		return &ConsumptionConflictError{EntryID: entryID, Plane: plane, Key: key}
	}
	return nil
}

// onlyPriceDiffers reports whether priced is stored with a price and nothing
// else changed.
func onlyPriceDiffers(stored, priced ConsumptionEntry) bool {
	return !stored.Priced() && withoutPrice(stored) == withoutPrice(priced)
}

// withoutPrice clears every field a price stamp writes.
func withoutPrice(entry ConsumptionEntry) ConsumptionEntry {
	entry.EquivalentMicros = 0
	entry.PriceRef = ""
	entry.PriceBasis = ""
	entry.Provider = ""
	entry.EnergyMilliWh = 0
	entry.EnergyProvenance = ProvenanceUnknown
	entry.Billing = ""
	entry.BilledMicros = 0
	return entry
}

// =============================================================================
// The service's sweep
// =============================================================================

// ErrConsumptionPricingUnavailable indicates a deployment whose ledger cannot
// be priced late: none is wired, or it does not support the amendment.
var ErrConsumptionPricingUnavailable = errors.New("ledger: late pricing is not available")

// ConsumptionPricingUnavailableError reports why a pricing sweep cannot run.
type ConsumptionPricingUnavailableError struct {
	Message string
}

// Error implements the error interface.
func (e *ConsumptionPricingUnavailableError) Error() string {
	return ErrConsumptionPricingUnavailable.Error() + ": " + e.Message
}

// Unwrap returns ErrConsumptionPricingUnavailable.
func (e *ConsumptionPricingUnavailableError) Unwrap() error { return ErrConsumptionPricingUnavailable }

// PriceUnpricedConsumption prices every ledger entry recorded without a price
// that the price table in force now can price, in every tenant. It is the
// body of the ledger pricing system job.
//
// Each entry is priced as the turn recorder would price it today: the tenant's
// runtime cost settings and the current table, and the entry's own stamped
// billing mode. A tenant whose ledger cannot be read is logged and counted,
// and the sweep moves on; an error is returned only when the sweep cannot run
// at all.
func (s *agentServiceImpl) PriceUnpricedConsumption(ctx context.Context) (*PricingSweepResult, error) {
	if s.ledger == nil {
		return nil, &ConsumptionPricingUnavailableError{Message: "no consumption ledger is wired"}
	}
	pricer, ok := s.ledger.(ConsumptionPricer)
	if !ok {
		return nil, &ConsumptionPricingUnavailableError{Message: "the consumption ledger cannot price entries late"}
	}

	// On an unrouted store one scan of every tenant sees everything; a
	// tenant-routed store must be named tenant by tenant, as every other
	// maintenance pass does (eachSessionRecord).
	tenants := []string{""}
	if s.tenants != nil {
		listed, err := s.tenants.ListTenantIDs(ctx)
		if err != nil {
			return nil, err
		}
		tenants = dedupeTenants(append([]string{SystemTenantID}, listed...))
	}

	total := &PricingSweepResult{}
	for _, tenantID := range tenants {
		tenantCtx := ctx
		if tenantID != "" {
			tenantCtx = ContextWithTenant(ctx, tenantID)
		}
		result, err := pricer.PriceUnpriced(tenantCtx, tenantID, func(entry *ConsumptionEntry) bool {
			if !s.prices.Stamp(entry, s.tenantCostSettings(tenantCtx, entry.TenantID)) {
				return false
			}
			stampBilling(entry, AccountBilling{Mode: entry.Billing})
			return true
		})
		if err != nil {
			s.logger.Warnf("orchestration: ledger pricing sweep: tenant %s could not be swept: %s", tenantID, err.Error())
			total.TenantsFailed++
			continue
		}
		for _, failure := range result.Failures {
			s.logger.Warnf("orchestration: ledger pricing sweep: tenant %s: %s", tenantID, failure.Error())
		}
		total.add(result)
	}
	if total.Priced > 0 || total.Failed > 0 {
		s.logger.Infof("orchestration: ledger pricing sweep priced %d of %d unpriced entries (%d failed)",
			total.Priced, total.Unpriced, total.Failed)
	}
	return total, nil
}

// dedupeTenants drops repeated tenant IDs, keeping the first of each.
func dedupeTenants(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
