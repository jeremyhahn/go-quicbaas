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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
)

// unpricedConsumptionPricer is the orchestration service's pricing sweep.
type unpricedConsumptionPricer interface {
	PriceUnpricedConsumption(ctx context.Context) (*orchestration.PricingSweepResult, error)
}

// ledgerPricingAdapter bridges the orchestration service's sweep to the
// scheduler handler, which knows nothing of the orchestration package.
type ledgerPricingAdapter struct {
	pricer unpricedConsumptionPricer
}

// Compile-time interface check.
var _ schedulerhandlers.LedgerPricer = (*ledgerPricingAdapter)(nil)

// PriceUnpricedEntries implements schedulerhandlers.LedgerPricer.
func (a *ledgerPricingAdapter) PriceUnpricedEntries(ctx context.Context) (*schedulerhandlers.LedgerPricingResult, error) {
	result, err := a.pricer.PriceUnpricedConsumption(ctx)
	if err != nil {
		return nil, err
	}
	return &schedulerhandlers.LedgerPricingResult{
		Unpriced:      result.Unpriced,
		Priced:        result.Priced,
		Failed:        result.Failed,
		TenantsFailed: result.TenantsFailed,
	}, nil
}

// registerLedgerPricing registers the pricing handler on engine and declares
// the pricing system job.
//
// It runs on the scheduler rather than on a timer of its own so a cluster
// prices each entry on one node per sweep: the job is cluster-wide and the
// firing claim lets exactly one node execute each firing. The sweep is
// idempotent besides, so a firing that overlaps another prices nothing twice.
func registerLedgerPricing(engine *scheduler.SchedulerEngine, pricer unpricedConsumptionPricer) error {
	job, err := schedulerhandlers.LedgerPricingJob(0)
	if err != nil {
		return err
	}
	handler := schedulerhandlers.NewLedgerPricingHandler(&ledgerPricingAdapter{pricer: pricer})
	if err := engine.RegisterHandler(handler.Name(), handler.Handler()); err != nil {
		return err
	}
	return engine.RegisterSystemJob(job)
}
