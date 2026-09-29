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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// opusBook prices claude-opus-5, which the entries below were recorded
// without.
func opusBook(t *testing.T) *PriceBook {
	t.Helper()
	book, err := NewPriceBook(config.PricingConfig{
		Version: "2026-09-23",
		Models: []config.ModelPriceConfig{{
			Model: "claude-opus-5", Provider: "anthropic", InputPerMTok: 5, OutputPerMTok: 25,
		}},
	})
	require.NoError(t, err)
	return book
}

// seedUnpriced records the three kinds of entry a sweep meets: one the table
// can now price, one it never can (unmeasured), and one already priced.
func seedUnpriced(t *testing.T, ledger ConsumptionLedger) {
	t.Helper()
	ctx := context.Background()
	entries := []ConsumptionEntry{
		spendEntry("opus-1", "p1", "task-a", "job-1", "s1", 0, "acct-1", 1_000_000, ConsumptionOutcomeSuccess, ledgerBase),
		spendEntry("opus-2", "", "", "job-2", "s2", 0, "", 1_000_000, ConsumptionOutcomeSuccess, ledgerBase.Add(time.Minute)),
	}
	entries[0].Billing = BillingAPI
	unmeasured := spendEntry("codex-1", "p1", "task-a", "job-1", "s1", 1, "acct-1", 0, ConsumptionOutcomeSuccess, ledgerBase)
	unmeasured.Unmeasured = true
	priced := spendEntry("done-1", "p1", "task-a", "job-1", "s1", 2, "acct-1", 10, ConsumptionOutcomeSuccess, ledgerBase)
	priced.PriceRef = "old:ref"
	priced.EquivalentMicros = 7
	entries = append(entries, unmeasured, priced)
	for _, e := range entries {
		require.NoError(t, ledger.Record(ctx, e))
	}
}

// stamper prices an entry the way the service's sweep does.
func stamper(book *PriceBook) func(*ConsumptionEntry) bool {
	return func(entry *ConsumptionEntry) bool {
		if !book.Stamp(entry, nil) {
			return false
		}
		stampBilling(entry, AccountBilling{Mode: entry.Billing})
		return true
	}
}

func TestPriceUnpriced_PricesEveryPlaneOnceAndOnlyOnce(t *testing.T) {
	t.Parallel()
	ledger := NewInMemoryConsumptionLedger()
	seedUnpriced(t, ledger)
	pricer := ledger.(ConsumptionPricer)
	ctx := context.Background()

	result, err := pricer.PriceUnpriced(ctx, "t1", stamper(opusBook(t)))
	require.NoError(t, err)
	assert.Equal(t, 3, result.Unpriced, "two opus turns and one unmeasured turn")
	assert.Equal(t, 2, result.Priced)
	assert.Zero(t, result.Failed)

	// $5 + $25 per million, one million of each: $30 per turn.
	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int64(60_000_000+7), rollup.EquivalentMicros)
	assert.Equal(t, 3, rollup.PricedEntries)
	assert.Equal(t, 1, rollup.UnpricedEntries, "an unmeasured turn has no counts to price")

	// The quota planes were priced too: the account's window and the tenant's.
	window, err := ledger.WindowUsage(ctx, "t1", "acct-1", ledgerBase.Add(-time.Hour), ledgerBase.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(30_000_000+7), window.EquivalentMicros)
	assert.Equal(t, int64(30_000_000), window.BilledMicros, "a metered key bills at the late list price")
	tenantWindow, err := ledger.TenantWindowUsage(ctx, "t1", ledgerBase.Add(-time.Hour), ledgerBase.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, rollup.EquivalentMicros, tenantWindow.EquivalentMicros)

	// A second sweep finds nothing left to price and changes nothing.
	again, err := pricer.PriceUnpriced(ctx, "t1", stamper(opusBook(t)))
	require.NoError(t, err)
	assert.Equal(t, 1, again.Unpriced)
	assert.Zero(t, again.Priced)
}

func TestPriceUnpriced_EveryTenantWhenNoneIsNamed(t *testing.T) {
	t.Parallel()
	ledger := NewInMemoryConsumptionLedger()
	seedUnpriced(t, ledger)
	result, err := ledger.(ConsumptionPricer).PriceUnpriced(context.Background(), "", stamper(opusBook(t)))
	require.NoError(t, err)
	assert.Equal(t, 2, result.Priced)
}

func TestPriceUnpriced_RefusesAnAmendmentThatChangesTheTurn(t *testing.T) {
	t.Parallel()
	ledger := NewInMemoryConsumptionLedger()
	seedUnpriced(t, ledger)
	book := opusBook(t)
	result, err := ledger.(ConsumptionPricer).PriceUnpriced(context.Background(), "t1", func(entry *ConsumptionEntry) bool {
		entry.InputTokens++ // a price stamp may not rewrite what was consumed
		return book.Stamp(entry, nil)
	})
	require.NoError(t, err)
	assert.Zero(t, result.Priced)
	assert.Equal(t, 2, result.Failed)
	require.Len(t, result.Failures, 2)
	assert.ErrorIs(t, result.Failures[0], ErrConsumptionInvalid)
}

func TestPriceUnpriced_ConvergesWithAnotherSweeper(t *testing.T) {
	t.Parallel()
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv).(*kvConsumptionLedger)
	ctx := context.Background()
	require.NoError(t, ledger.Record(ctx, spendEntry("e1", "p1", "", "", "", 0, "a1", 100, ConsumptionOutcomeSuccess, ledgerBase)))

	var stored storedEntry
	require.NoError(t, kv.Scan(ctx, consumptionChainKeyspace, func(_ string, value []byte) error {
		stored.raw = value
		return nil
	}))
	stored.entry = spendEntry("e1", "p1", "", "", "", 0, "a1", 100, ConsumptionOutcomeSuccess, ledgerBase).normalize()
	priced := stored.entry
	require.True(t, stamper(opusBook(t))(&priced))

	swapper := kv.(guardedSwapper)
	require.NoError(t, ledger.amendPrice(ctx, swapper, stored, priced))
	assert.NoError(t, ledger.amendPrice(ctx, swapper, stored, priced),
		"the loser of the race finds the priced bytes already stored")

	// A different price for the same entry is a conflict, never an overwrite.
	other := stored.entry
	require.True(t, stamper(opusBook(t))(&other))
	other.EquivalentMicros++
	var conflict *ConsumptionConflictError
	assert.ErrorAs(t, ledger.amendPrice(ctx, swapper, stored, other), &conflict)
}

func TestPriceUnpriced_RefusalPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	unguarded := NewConsumptionLedger(kvstore.NewMemory()).(ConsumptionPricer)
	_, err := unguarded.PriceUnpriced(ctx, "t1", func(*ConsumptionEntry) bool { return false })
	assert.ErrorIs(t, err, ErrConsumptionUnguarded)

	ledger := NewInMemoryConsumptionLedger().(ConsumptionPricer)
	_, err = ledger.PriceUnpriced(ctx, "t1", nil)
	assert.ErrorIs(t, err, ErrConsumptionInvalid)
	_, err = ledger.PriceUnpriced(ctx, "bad/tenant", func(*ConsumptionEntry) bool { return false })
	assert.ErrorIs(t, err, ErrConsumptionInvalid)

	assert.False(t, onlyPriceDiffers(ConsumptionEntry{PriceRef: "x"}, ConsumptionEntry{PriceRef: "y"}),
		"a priced entry is never re-priced")
}

func TestPriceUnpricedConsumption_SweepsEveryTenant(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	impl.SetPriceBook(opusBook(t))
	ledger := NewInMemoryConsumptionLedger()
	impl.SetConsumptionLedger(ledger)
	seedUnpriced(t, ledger)

	// Without a lister, one scan of the unrouted store sees every tenant.
	result, err := impl.PriceUnpricedConsumption(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, result.Priced)

	// With one, every listed tenant and the system tenant are named, once.
	second := newTestService(t)
	second.SetPriceBook(opusBook(t))
	fresh := NewInMemoryConsumptionLedger()
	second.SetConsumptionLedger(fresh)
	seedUnpriced(t, fresh)
	second.SetTenantLister(&fakeTenantLister{ids: []string{"t1", SystemTenantID, "t1"}})
	result, err = second.PriceUnpricedConsumption(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, result.Priced)
	assert.Zero(t, result.TenantsFailed)
}

func TestPriceUnpricedConsumption_RefusalPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bare := newTestService(t)
	_, err := bare.PriceUnpricedConsumption(ctx)
	assert.ErrorIs(t, err, ErrConsumptionPricingUnavailable)

	notPricer := newTestService(t)
	notPricer.SetConsumptionLedger(&countingLedger{ConsumptionLedger: NewInMemoryConsumptionLedger()})
	_, err = notPricer.PriceUnpricedConsumption(ctx)
	assert.ErrorIs(t, err, ErrConsumptionPricingUnavailable)

	listFails := newTestService(t)
	listFails.SetConsumptionLedger(NewInMemoryConsumptionLedger())
	listErr := errors.New("tenant manager sealed")
	listFails.SetTenantLister(&fakeTenantLister{err: listErr})
	_, err = listFails.PriceUnpricedConsumption(ctx)
	assert.ErrorIs(t, err, listErr)

	badTenant := newTestService(t)
	badTenant.SetConsumptionLedger(NewInMemoryConsumptionLedger())
	badTenant.SetTenantLister(&fakeTenantLister{ids: []string{"bad/tenant"}})
	result, err := badTenant.PriceUnpricedConsumption(ctx)
	require.NoError(t, err, "one tenant that cannot be swept does not stop the sweep")
	assert.Equal(t, 1, result.TenantsFailed)
}
