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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
)

// stubConsumptionPricer returns a canned sweep.
type stubConsumptionPricer struct {
	result *orchestration.PricingSweepResult
	err    error
}

func (s stubConsumptionPricer) PriceUnpricedConsumption(context.Context) (*orchestration.PricingSweepResult, error) {
	return s.result, s.err
}

func TestLedgerPricingAdapter_CarriesTheCounts(t *testing.T) {
	adapter := &ledgerPricingAdapter{pricer: stubConsumptionPricer{result: &orchestration.PricingSweepResult{
		Unpriced: 5, Priced: 3, Failed: 1, TenantsFailed: 2,
	}}}
	got, err := adapter.PriceUnpricedEntries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, &schedulerhandlers.LedgerPricingResult{Unpriced: 5, Priced: 3, Failed: 1, TenantsFailed: 2}, got)

	cause := errors.New("no ledger")
	_, err = (&ledgerPricingAdapter{pricer: stubConsumptionPricer{err: cause}}).PriceUnpricedEntries(context.Background())
	assert.ErrorIs(t, err, cause)
}

func TestRegisterLedgerPricing_DeclaresTheSystemJobOnce(t *testing.T) {
	engine := scheduler.New(scheduler.DefaultConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	pricer := stubConsumptionPricer{result: &orchestration.PricingSweepResult{}}

	require.NoError(t, registerLedgerPricing(engine, pricer))
	assert.Equal(t, 1, engine.HandlerCount())
	assert.Error(t, registerLedgerPricing(engine, pricer), "the pricing job is declared once per engine")
}
