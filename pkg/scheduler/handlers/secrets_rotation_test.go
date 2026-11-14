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

// stubSecretsRotator returns a canned scan outcome.
type stubSecretsRotator struct {
	result *SecretsRotationResult
	err    error
}

func (s stubSecretsRotator) RotateDueSecrets(context.Context) (*SecretsRotationResult, error) {
	return s.result, s.err
}

func TestSecretsRotationHandler_ReportsTheScan(t *testing.T) {
	h := NewSecretsRotationHandler(stubSecretsRotator{result: &SecretsRotationResult{Due: 3, Rotated: 2, Failed: 1}})

	result, err := h.Handler()(context.Background(), &scheduler.Job{ID: SecretsRotationJobID})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "rotated 2 of 3 due secrets, 1 failed", result.Message)
	assert.Equal(t, HandlerSecretsRotation, h.Name())
}

func TestSecretsRotationHandler_ScanFailureFailsTheRun(t *testing.T) {
	cause := errors.New("list secrets: storage unavailable")
	h := NewSecretsRotationHandler(stubSecretsRotator{err: cause})

	result, err := h.Handle(context.Background(), &scheduler.Job{ID: SecretsRotationJobID})

	assert.Nil(t, result)
	var failed *scheduler.HandlerFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, HandlerSecretsRotation, failed.Handler)
	assert.Equal(t, SecretsRotationJobID, failed.JobID)
	assert.ErrorIs(t, err, cause)
}

func TestSecretsRotationJob_Definition(t *testing.T) {
	tests := []struct {
		name         string
		interval     time.Duration
		wantSchedule string
		wantTimeout  time.Duration
	}{
		{"zero selects the default", 0, "@every 5m0s", DefaultSecretsRotationScanInterval},
		{"explicit interval", 90 * time.Second, "@every 1m30s", 90 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job, err := SecretsRotationJob(tc.interval)
			require.NoError(t, err)

			assert.Equal(t, SecretsRotationJobID, job.ID)
			assert.NotEqual(t, KMSRotationJobID, job.ID)
			assert.NotEqual(t, LedgerPricingJobID, job.ID)
			assert.GreaterOrEqual(t, job.ID, scheduler.SystemJobIDFloor)
			assert.Equal(t, scheduler.OwnerTypeSystem, job.OwnerType)
			assert.Equal(t, scheduler.JobTypeRecurring, job.Type)
			assert.Empty(t, job.NodeAffinity, "the scan is cluster-wide")
			assert.Equal(t, HandlerSecretsRotation, job.Handler)
			assert.Equal(t, tc.wantSchedule, job.Schedule)
			assert.Equal(t, tc.wantTimeout, job.Timeout)
		})
	}
}

func TestSecretsRotationJob_RejectsIntervalsCronCannotExpress(t *testing.T) {
	for _, interval := range []time.Duration{-time.Minute, 500 * time.Millisecond, 1500 * time.Millisecond} {
		t.Run(interval.String(), func(t *testing.T) {
			job, err := SecretsRotationJob(interval)

			assert.Nil(t, job)
			var invalid scheduler.InvalidJobError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, "schedule", invalid.Field)
		})
	}
}
