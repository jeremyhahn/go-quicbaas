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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// stubLedgerPricer returns a canned sweep outcome.
type stubLedgerPricer struct {
	result *LedgerPricingResult
	err    error
}

func (s stubLedgerPricer) PriceUnpricedEntries(context.Context) (*LedgerPricingResult, error) {
	return s.result, s.err
}

func TestLedgerPricingHandler_ReportsTheSweep(t *testing.T) {
	h := NewLedgerPricingHandler(stubLedgerPricer{result: &LedgerPricingResult{Unpriced: 221, Priced: 219, Failed: 1, TenantsFailed: 1}})

	result, err := h.Handler()(context.Background(), &scheduler.Job{ID: LedgerPricingJobID})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "priced 219 of 221 unpriced entries, 1 failed, 1 tenants unreadable", result.Message)
	assert.Equal(t, HandlerLedgerPricing, h.Name())
}

func TestLedgerPricingHandler_SweepFailureFailsTheRun(t *testing.T) {
	cause := errors.New("ledger not wired")
	h := NewLedgerPricingHandler(stubLedgerPricer{err: cause})

	result, err := h.Handle(context.Background(), &scheduler.Job{ID: LedgerPricingJobID})

	assert.Nil(t, result)
	var failed *scheduler.HandlerFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, HandlerLedgerPricing, failed.Handler)
	assert.Equal(t, LedgerPricingJobID, failed.JobID)
	assert.ErrorIs(t, err, cause)
}

func TestLedgerPricingJob_Definition(t *testing.T) {
	tests := []struct {
		name         string
		interval     time.Duration
		wantSchedule string
	}{
		{"zero selects the default", 0, "@every 1h0m0s"},
		{"explicit interval", 10 * time.Minute, "@every 10m0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job, err := LedgerPricingJob(tc.interval)
			require.NoError(t, err)
			assert.Equal(t, LedgerPricingJobID, job.ID)
			assert.NotEqual(t, KMSRotationJobID, job.ID, "two system jobs cannot share a reserved ID")
			assert.GreaterOrEqual(t, job.ID, scheduler.SystemJobIDFloor)
			assert.Equal(t, scheduler.OwnerTypeSystem, job.OwnerType)
			assert.Equal(t, scheduler.JobTypeRecurring, job.Type)
			assert.Equal(t, tc.wantSchedule, job.Schedule)
			assert.Equal(t, HandlerLedgerPricing, job.Handler)
		})
	}
}

func TestLedgerPricingJob_RejectsAnUnschedulableInterval(t *testing.T) {
	for _, interval := range []time.Duration{-time.Second, 500 * time.Millisecond, 1500 * time.Millisecond} {
		_, err := LedgerPricingJob(interval)
		var invalid scheduler.InvalidJobError
		assert.ErrorAs(t, err, &invalid, "interval %s", interval)
	}
}
