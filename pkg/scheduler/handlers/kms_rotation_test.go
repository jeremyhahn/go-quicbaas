// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// stubKMSKeyRotator returns a canned scan outcome.
type stubKMSKeyRotator struct {
	result *KMSRotationResult
	err    error
}

func (s stubKMSKeyRotator) RotateDueKeys(context.Context) (*KMSRotationResult, error) {
	return s.result, s.err
}

func TestKMSRotationHandler_ReportsTheScan(t *testing.T) {
	h := NewKMSRotationHandler(stubKMSKeyRotator{result: &KMSRotationResult{Due: 3, Rotated: 2, Failed: 1}})

	result, err := h.Handler()(context.Background(), &scheduler.Job{ID: KMSRotationJobID})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "rotated 2 of 3 due keys, 1 failed", result.Message)
	assert.Equal(t, HandlerKMSRotation, h.Name())
}

func TestKMSRotationHandler_ScanFailureFailsTheRun(t *testing.T) {
	cause := errors.New("list keys: storage unavailable")
	h := NewKMSRotationHandler(stubKMSKeyRotator{err: cause})

	result, err := h.Handle(context.Background(), &scheduler.Job{ID: KMSRotationJobID})

	assert.Nil(t, result)
	var failed *scheduler.HandlerFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, HandlerKMSRotation, failed.Handler)
	assert.Equal(t, KMSRotationJobID, failed.JobID)
	assert.ErrorIs(t, err, cause)
}

func TestKMSRotationJob_Definition(t *testing.T) {
	tests := []struct {
		name         string
		interval     time.Duration
		wantSchedule string
		wantTimeout  time.Duration
	}{
		{"zero selects the default", 0, "@every 5m0s", DefaultKMSRotationScanInterval},
		{"explicit interval", 90 * time.Second, "@every 1m30s", 90 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job, err := KMSRotationJob(tc.interval)
			require.NoError(t, err)

			assert.Equal(t, KMSRotationJobID, job.ID)
			assert.GreaterOrEqual(t, job.ID, scheduler.SystemJobIDFloor)
			assert.Equal(t, scheduler.OwnerTypeSystem, job.OwnerType)
			assert.Equal(t, scheduler.JobTypeRecurring, job.Type)
			assert.Empty(t, job.NodeAffinity, "the scan is cluster-wide")
			assert.Equal(t, HandlerKMSRotation, job.Handler)
			assert.Equal(t, tc.wantSchedule, job.Schedule)
			assert.Equal(t, tc.wantTimeout, job.Timeout)
			assert.Zero(t, job.MaxRetries,
				"the scan takes the scheduler's per-firing default budget; a failing scan moves on to the next one")
		})
	}
}

func TestKMSRotationJob_RejectsIntervalsCronCannotExpress(t *testing.T) {
	for _, interval := range []time.Duration{-time.Minute, 500 * time.Millisecond, 1500 * time.Millisecond} {
		t.Run(interval.String(), func(t *testing.T) {
			job, err := KMSRotationJob(interval)

			assert.Nil(t, job)
			var invalid scheduler.InvalidJobError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, "schedule", invalid.Field)
		})
	}
}
