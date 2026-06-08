//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Barrier handler request-validation branches.
//
// The Shamir-initialize, rekey, and recovery-generate handlers reject invalid
// threshold/total combinations before delegating to the barrier facade. These
// guard branches (threshold<=0, total<=0, threshold>total) were uncovered.
// =============================================================================

func encBarrier(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := EncodePayload(v)
	require.NoError(t, err)
	return b
}

// TestBarrierValidation_RejectsBadThresholdTotal exercises every threshold/total
// validation branch across the three Shamir-style barrier handlers. A valid
// barrier facade is wired so the failure is attributable to validation, not a
// missing service.
func TestBarrierValidation_RejectsBadThresholdTotal(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{})
	ctx := context.Background()

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		// Shamir initialize
		{"shamir-init/threshold-zero", SubOpBarrierShamirInitialize, encBarrier(t, &barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 0, TotalShares: 3})},
		{"shamir-init/total-zero", SubOpBarrierShamirInitialize, encBarrier(t, &barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 2, TotalShares: 0})},
		{"shamir-init/threshold-exceeds-total", SubOpBarrierShamirInitialize, encBarrier(t, &barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 4, TotalShares: 3})},
		// Rekey
		{"rekey/threshold-zero", SubOpBarrierRekey, encBarrier(t, &barrierRekeyWireRequest{Threshold: 0, Total: 3})},
		{"rekey/total-zero", SubOpBarrierRekey, encBarrier(t, &barrierRekeyWireRequest{Threshold: 2, Total: 0})},
		{"rekey/threshold-exceeds-total", SubOpBarrierRekey, encBarrier(t, &barrierRekeyWireRequest{Threshold: 4, Total: 3})},
		// Recovery generate
		{"recovery-generate/threshold-zero", SubOpBarrierRecoveryGenerate, encBarrier(t, &barrierRecoveryGenerateWireRequest{Threshold: 0, Total: 3})},
		{"recovery-generate/total-zero", SubOpBarrierRecoveryGenerate, encBarrier(t, &barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 0})},
		{"recovery-generate/threshold-exceeds-total", SubOpBarrierRecoveryGenerate, encBarrier(t, &barrierRecoveryGenerateWireRequest{Threshold: 4, Total: 3})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, barrierRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status, "%s must be rejected by validation", tc.name)
		})
	}
}

// TestBarrierValidation_RecoveryRecoverRequiresKeys verifies the recovery-recover
// handler rejects an empty key set.
func TestBarrierValidation_RecoveryRecoverRequiresKeys(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{})
	ctx := context.Background()

	resp := handler.HandleRequest(ctx, barrierRequest(SubOpBarrierRecoveryRecover,
		encBarrier(t, &barrierRecoveryRecoverWireRequest{Keys: nil})))
	assert.NotEqual(t, StatusOK, resp.Status, "recovery-recover must require at least one key")
}
