//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestExecuteIAMGDPRDeletionRequest_ZeroGracePeriodTakesDefault pins that an
// explicit grace_period_days of 0, like an omitted one, reaches
// RequestDeletion as iam.DefaultGracePeriodDays, as over gRPC: a deletion
// request never schedules the erasure to run at once.
func TestExecuteIAMGDPRDeletionRequest_ZeroGracePeriodTakesDefault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{name: "number zero", value: float64(0)},
		{name: "string zero", value: "0"},
		{name: "omitted", value: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := &ToolRegistry{service: newGDPRMockWithManager(t), logger: newTestLogger(t)}
			args := map[string]interface{}{
				"tenant_id":    "t1",
				"user_id":      "30001",
				"requested_by": "50100",
				"reason":       "user requested deletion",
			}
			if tc.value != nil {
				args["grace_period_days"] = tc.value
			}

			result, err := reg.executeIAMGDPRDeletionRequest(context.Background(), args)
			require.NoError(t, err)
			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			requestedAt, err := time.Parse(time.RFC3339Nano, resultMap["requested_at"].(string))
			require.NoError(t, err)
			endsRaw, ok := resultMap["grace_period_ends_at"].(string)
			require.True(t, ok, "the deletion request carries no grace period end")
			endsAt, err := time.Parse(time.RFC3339Nano, endsRaw)
			require.NoError(t, err)
			assert.Equal(t, time.Duration(iam.DefaultGracePeriodDays)*24*time.Hour, endsAt.Sub(requestedAt))
		})
	}
}

// TestExecuteIAMGDPRDeletionRequest_NegativeGracePeriodIsRefused pins that a
// negative grace_period_days is refused as invalid input before any request
// is recorded.
func TestExecuteIAMGDPRDeletionRequest_NegativeGracePeriodIsRefused(t *testing.T) {
	for _, value := range []interface{}{float64(-1), -1, int64(-1), "-1"} {
		reg := &ToolRegistry{service: newGDPRMockWithManager(t), logger: newTestLogger(t)}
		_, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
			"tenant_id":         "t1",
			"user_id":           "30001",
			"requested_by":      "50100",
			"reason":            "user requested deletion",
			"grace_period_days": value,
		})
		require.Error(t, err, "grace_period_days %#v", value)
		var qbErr *qberrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code, "grace_period_days %#v", value)
	}
}
