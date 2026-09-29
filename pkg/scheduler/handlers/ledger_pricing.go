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

package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// HandlerLedgerPricing is the handler name for the ledger pricing sweep.
const HandlerLedgerPricing = "ledger.pricing"

// LedgerPricingJobID is the reserved ID of the ledger pricing system job. Every
// node declares the job under this ID, so a cluster holds exactly one record
// and the firing claim lets exactly one node run each sweep.
const LedgerPricingJobID = scheduler.SystemJobIDFloor + 2

// DefaultLedgerPricingInterval is how often the pricing sweep runs when no
// interval is configured. A turn that could not be priced when it ran waits at
// most this long for a price the table has since gained.
const DefaultLedgerPricingInterval = time.Hour

// LedgerPricingResult summarizes one pricing sweep.
type LedgerPricingResult struct {
	// Unpriced is the number of unpriced ledger entries examined.
	Unpriced int
	// Priced is the number of them priced and stored.
	Priced int
	// Failed is the number priced but not stored. They stay unpriced and are
	// retried by the next sweep.
	Failed int
	// TenantsFailed is the number of tenants whose ledger could not be read.
	TenantsFailed int
}

// LedgerPricer prices every consumption-ledger entry that was recorded without
// a price and can be priced now. It is satisfied by the orchestration
// service, through the server's wiring.
type LedgerPricer interface {
	// PriceUnpricedEntries runs one sweep. It returns an error only when the
	// sweep itself could not run; per-entry and per-tenant failures are
	// counted in the result.
	PriceUnpricedEntries(ctx context.Context) (*LedgerPricingResult, error)
}

// LedgerPricingHandler runs the ledger pricing sweep as a scheduler job.
type LedgerPricingHandler struct {
	pricer LedgerPricer
}

// NewLedgerPricingHandler creates a pricing handler over pricer.
func NewLedgerPricingHandler(pricer LedgerPricer) *LedgerPricingHandler {
	return &LedgerPricingHandler{pricer: pricer}
}

// Name returns the handler name for registration.
func (h *LedgerPricingHandler) Name() string {
	return HandlerLedgerPricing
}

// Handle runs one pricing sweep.
func (h *LedgerPricingHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	result, err := h.pricer.PriceUnpricedEntries(ctx)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerLedgerPricing,
			JobID:   job.ID,
			Cause:   err,
		}
	}
	return &scheduler.JobResult{
		Success: true,
		Message: fmt.Sprintf("priced %d of %d unpriced entries, %d failed, %d tenants unreadable",
			result.Priced, result.Unpriced, result.Failed, result.TenantsFailed),
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *LedgerPricingHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// LedgerPricingJob returns the system job definition for the pricing sweep,
// running every interval. A zero interval selects
// DefaultLedgerPricingInterval. The scheduler's cron schedules resolve to
// whole seconds, so interval must be a positive whole number of seconds.
//
// A sweep's timeout equals its interval: a sweep still running when the next
// is due has stalled. The sweep is idempotent — an entry is priced at most
// once — so a retried or overlapping firing prices nothing twice.
func LedgerPricingJob(interval time.Duration) (*scheduler.Job, error) {
	if interval == 0 {
		interval = DefaultLedgerPricingInterval
	}
	if interval < time.Second || interval%time.Second != 0 {
		return nil, scheduler.InvalidJobError{
			Field:   "schedule",
			Message: fmt.Sprintf("ledger pricing interval %s must be a positive whole number of seconds", interval),
		}
	}
	return &scheduler.Job{
		ID:          LedgerPricingJobID,
		Name:        "ledger-pricing",
		Description: "Prices consumption-ledger entries recorded without a price, once the price table names their model.",
		OwnerType:   scheduler.OwnerTypeSystem,
		Type:        scheduler.JobTypeRecurring,
		Schedule:    "@every " + interval.String(),
		Handler:     HandlerLedgerPricing,
		Timeout:     interval,
	}, nil
}
