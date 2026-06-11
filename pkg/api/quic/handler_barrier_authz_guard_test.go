//go:build quic

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

package quic

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Exhaustive authz guard tests for the OpBarrier sub-op permission table.
//
// Task 3 requirements:
//   a. Bidirectional guard: every key in barrierSubOpHandlers has an entry in
//      barrierSubOpPermissions AND vice versa.
//   b. Table-driven: each of the 13 sub-ops resolves via
//      getQRDBPermission(OpBarrier, []byte{subOp, ...}) to the EXACT canonical
//      permission (this test is the permission spec).
//   c. getQRDBPermission(OpBarrier, nil) and unknown sub-op → ok=false.
//   d. Gate-level: an OpBarrier request with sub-op SubOpBarrierRekey passes
//      requiredPerm "seal:rekey" to the validator; empty-payload OpBarrier is
//      denied with StatusPermissionDenied without calling the validator.
// =============================================================================

// newAllowBarrierValidator returns a fakeValidatorAuthorizer pre-loaded with an
// allow AuthorizedContext so gate-level tests can simulate validator admission.
func newAllowBarrierValidator() *fakeValidatorAuthorizer {
	return &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "tenant-a",
			Roles:    []string{"admin"},
		},
	}
}

// TestBarrierAuthz_BidirectionalGuard verifies that every key in
// barrierSubOpHandlers has an entry in barrierSubOpPermissions and vice versa.
// A future sub-op cannot ship routable-but-unauthorizable, or
// authorized-but-unroutable.
func TestBarrierAuthz_BidirectionalGuard(t *testing.T) {
	for subOp := range barrierSubOpHandlers {
		_, ok := barrierSubOpPermissions[subOp]
		assert.Truef(t, ok,
			"sub-op 0x%02X is in barrierSubOpHandlers but missing from barrierSubOpPermissions",
			subOp)
	}
	for subOp := range barrierSubOpPermissions {
		_, ok := barrierSubOpHandlers[subOp]
		assert.Truef(t, ok,
			"sub-op 0x%02X is in barrierSubOpPermissions but missing from barrierSubOpHandlers",
			subOp)
	}
}

// TestBarrierAuthz_SubOpPermissions is the authoritative spec for each of the
// 13 SubOpBarrier* constants: it pins the exact canonical permission that
// getQRDBPermission must return when OpBarrier is dispatched with that sub-op.
// Values MUST match go-qrdb/pkg/permission/constants.go and the gRPC authz
// mapping (pkg/api/grpc/authz_mapping.go lines 259-275).
func TestBarrierAuthz_SubOpPermissions(t *testing.T) {
	cases := []struct {
		name     string
		subOp    byte
		resource string
		action   string
	}{
		{"Status", SubOpBarrierStatus, "tenant_barrier", "status"},
		{"Initialize", SubOpBarrierInitialize, "tenant_barrier", "init"},
		{"Unseal", SubOpBarrierUnseal, "tenant_barrier", "unseal"},
		{"Seal", SubOpBarrierSeal, "tenant_barrier", "seal"},
		{"ShamirInitialize", SubOpBarrierShamirInitialize, "seal", "initialize_shamir"},
		{"ShamirUnsealShare", SubOpBarrierShamirUnsealShare, "barrier", "unseal-share"},
		{"ShamirUnsealShares", SubOpBarrierShamirUnsealShares, "barrier", "unseal-share"},
		{"Rekey", SubOpBarrierRekey, "seal", "rekey"},
		{"RecoveryGenerate", SubOpBarrierRecoveryGenerate, "seal", "generate_recovery"},
		{"RecoveryRecover", SubOpBarrierRecoveryRecover, "seal", "recover_with_keys"},
		{"RecoveryDelete", SubOpBarrierRecoveryDelete, "seal", "generate_recovery"},
		{"RecoveryStatus", SubOpBarrierRecoveryStatus, "seal", "status"},
		{"RootToken", SubOpBarrierRootToken, "seal", "bootstrap"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Extra bytes after the sub-op prove the resolver only inspects payload[0].
			payload := []byte{tc.subOp, 0xDE, 0xAD}
			res, act, ok := getQRDBPermission(OpBarrier, payload)
			require.Truef(t, ok, "sub-op 0x%02X must be mapped", tc.subOp)
			assert.Equalf(t, tc.resource, res, "sub-op 0x%02X resource", tc.subOp)
			assert.Equalf(t, tc.action, act, "sub-op 0x%02X action", tc.subOp)
		})
	}
}

// TestBarrierAuthz_NilPayloadDenied verifies that an OpBarrier request with nil
// payload returns ok=false — the authz gate default-denies without consulting
// the validator.
func TestBarrierAuthz_NilPayloadDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpBarrier, nil)
	assert.False(t, ok, "OpBarrier with nil payload must be unmapped (default-deny)")
}

// TestBarrierAuthz_EmptyPayloadDenied verifies an explicit empty slice also
// triggers default-deny.
func TestBarrierAuthz_EmptyPayloadDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpBarrier, []byte{})
	assert.False(t, ok, "OpBarrier with empty payload must be unmapped (default-deny)")
}

// TestBarrierAuthz_UnknownSubOpDenied verifies that a sub-op byte that is not
// in barrierSubOpPermissions returns ok=false. 0xFF is not assigned.
func TestBarrierAuthz_UnknownSubOpDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpBarrier, []byte{0xFF})
	assert.False(t, ok, "unknown sub-op 0xFF must be unmapped (default-deny)")
}

// TestBarrierAuthz_GateLevel_RekeyPassesCorrectPerm verifies the full gate
// path: an OpBarrier request with sub-op SubOpBarrierRekey passes
// requiredPerm "seal:rekey" to the ValidatorAuthorizer and is admitted when
// the validator allows it.
func TestBarrierAuthz_GateLevel_RekeyPassesCorrectPerm(t *testing.T) {
	v := newAllowBarrierValidator()
	h := newBarrierHandler(t, &mockBarrierServicer{})
	h.validatorAuthorizer = v

	rekeyBody, err := EncodePayload(&barrierRekeyWireRequest{Threshold: 2, Total: 3})
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpBarrier,
		Token:   "bearer-token",
		Payload: append([]byte{SubOpBarrierRekey}, rekeyBody...),
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp, "expected allow, got denial %v", resp)
	assert.Equal(t, "seal:rekey", v.gotRequiredPerm,
		"OpBarrier/SubOpBarrierRekey must authorize as seal:rekey")
	assert.Equal(t, "bearer-token", v.gotToken)
	assert.Empty(t, v.gotDPoP, "QUIC must pass empty DPoP")
}

// TestBarrierAuthz_GateLevel_EmptyPayloadDeniedBeforeValidator verifies that
// an OpBarrier request with an empty payload is denied with
// StatusPermissionDenied and the ValidatorAuthorizer is never called.
func TestBarrierAuthz_GateLevel_EmptyPayloadDeniedBeforeValidator(t *testing.T) {
	v := newAllowBarrierValidator()
	h := newBarrierHandler(t, &mockBarrierServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpBarrier,
		Token:   "bearer-token",
		Payload: []byte{}, // empty — no sub-op byte
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp, "expected denial response")
	assert.Equal(t, StatusPermissionDenied, resp.Status,
		"empty-payload OpBarrier must return StatusPermissionDenied")
	assert.Empty(t, v.gotToken,
		"validator must not be called when OpBarrier payload is empty")
}

// TestBarrierAuthz_GateLevel_NilPayloadDeniedBeforeValidator verifies that nil
// payload also denies before calling the validator.
func TestBarrierAuthz_GateLevel_NilPayloadDeniedBeforeValidator(t *testing.T) {
	v := newAllowBarrierValidator()
	h := newBarrierHandler(t, &mockBarrierServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpBarrier,
		Token:   "bearer-token",
		Payload: nil,
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp)
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Empty(t, v.gotToken, "validator must not be called when payload is nil")
}

// TestBarrierAuthz_GateLevel_UnknownSubOpDeniedBeforeValidator verifies that
// an unrecognised sub-op denies before calling the validator.
func TestBarrierAuthz_GateLevel_UnknownSubOpDeniedBeforeValidator(t *testing.T) {
	v := newAllowBarrierValidator()
	h := newBarrierHandler(t, &mockBarrierServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpBarrier,
		Token:   "bearer-token",
		Payload: []byte{0xFF}, // unassigned sub-op
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp)
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Empty(t, v.gotToken, "validator must not be called for an unknown sub-op")
}

// TestBarrierAuthz_StatusAndRekeyHaveDifferentPermissions verifies that the
// read-only SubOpBarrierStatus and the mutating SubOpBarrierRekey resolve to
// different permissions — closing the escalation gap this change fixes.
func TestBarrierAuthz_StatusAndRekeyHaveDifferentPermissions(t *testing.T) {
	statusRes, statusAct, ok := getQRDBPermission(OpBarrier, []byte{SubOpBarrierStatus})
	require.True(t, ok)

	rekeyRes, rekeyAct, ok := getQRDBPermission(OpBarrier, []byte{SubOpBarrierRekey})
	require.True(t, ok)

	assert.NotEqual(t,
		statusRes+":"+statusAct,
		rekeyRes+":"+rekeyAct,
		"SubOpBarrierStatus and SubOpBarrierRekey must resolve to different permissions")
}

// TestBarrierAuthz_GateLevel_StatusPermission verifies that SubOpBarrierStatus
// passes the canonical "tenant_barrier:status" permission to the validator.
func TestBarrierAuthz_GateLevel_StatusPermission(t *testing.T) {
	v := newAllowBarrierValidator()
	h := newBarrierHandler(t, &mockBarrierServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpBarrier,
		Token:   "tok",
		Payload: []byte{SubOpBarrierStatus},
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp)
	assert.Equal(t, "tenant_barrier:status", v.gotRequiredPerm)
}

// TestBarrierAuthz_GateLevel_AllSubOpsReachValidator ensures that for every
// registered sub-op, when a valid token is present, the validator is actually
// consulted (the request is not short-circuited before reaching it).
func TestBarrierAuthz_GateLevel_AllSubOpsReachValidator(t *testing.T) {
	for subOp := range barrierSubOpPermissions {
		subOp := subOp // capture
		t.Run(BarrierSubOpcodeToString(subOp), func(t *testing.T) {
			v := newAllowBarrierValidator()
			h := newBarrierHandler(t, &mockBarrierServicer{})
			h.validatorAuthorizer = v

			req := &Request{
				Opcode:  OpBarrier,
				Token:   "tok",
				Payload: []byte{subOp},
			}

			_, resp := h.authorizeAndInject(context.Background(), req)
			require.Nil(t, resp, "sub-op 0x%02X with valid token must reach validator", subOp)
			assert.NotEmpty(t, v.gotToken,
				"sub-op 0x%02X: validator must have been called", subOp)

			perm, ok := barrierSubOpPermissions[subOp]
			require.True(t, ok)
			assert.Equal(t, perm.resource+":"+perm.action, v.gotRequiredPerm,
				"sub-op 0x%02X requiredPerm mismatch", subOp)
		})
	}
}
