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
// Exhaustive authz guard tests for the OpCeremony sub-op permission table,
// modelled on handler_barrier_authz_guard_test.go.
//
// The bug these tests lock down: OpCeremony authorized at the SURFACE level
// under ceremony:status. qrdb grants ceremony:status to Admin, Operator AND the
// read-only Auditor role, while CeremonyInitialize / CeremonySignCSR /
// CeremonyEnrollExternalOfficer are Security-Officer exclusive. An Auditor could
// therefore drive the whole initialization ceremony over native QUIC: mint the
// CA, the server TLS identity and the SO credential, sign arbitrary CSRs from
// the ceremony CA, seal a Shamir share to an attacker-supplied key, and claim
// officer shares. Read-only role -> trust-root compromise.
// =============================================================================

// newAllowCeremonyValidator returns a fakeValidatorAuthorizer pre-loaded with an
// allow AuthorizedContext so gate-level tests can observe the permission the
// gate demands.
func newAllowCeremonyValidator() *fakeValidatorAuthorizer {
	return &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "tenant-a",
			Roles:    []string{"admin"},
		},
	}
}

// TestCeremonyAuthz_BidirectionalGuard verifies every key in
// ceremonySubOpHandlers has an entry in ceremonySubOpPermissions and vice versa.
// A future sub-op cannot ship routable-but-unauthorizable, or
// authorized-but-unroutable.
func TestCeremonyAuthz_BidirectionalGuard(t *testing.T) {
	for subOp := range ceremonySubOpHandlers {
		_, ok := ceremonySubOpPermissions[subOp]
		assert.Truef(t, ok,
			"sub-op 0x%02X is in ceremonySubOpHandlers but missing from ceremonySubOpPermissions",
			subOp)
	}
	for subOp := range ceremonySubOpPermissions {
		_, ok := ceremonySubOpHandlers[subOp]
		assert.Truef(t, ok,
			"sub-op 0x%02X is in ceremonySubOpPermissions but missing from ceremonySubOpHandlers",
			subOp)
	}
}

// ceremonySubOpPermissionSpec is the authoritative table for the nine
// SubOpCeremony* constants. Values are copied from the gRPC canon
// (pkg/api/grpc/authz_mapping.go quicbaas.ceremony.v1.Ceremony and
// qrdb.v1.QRDBService Ceremony* entries).
var ceremonySubOpPermissionSpec = []struct {
	name     string
	subOp    byte
	resource string
	action   string
}{
	{"Initialize", SubOpCeremonyInitialize, "ceremony", "initialize"},
	{"Status", SubOpCeremonyStatus, "ceremony", "status"},
	{"ClaimCertBegin", SubOpCeremonyClaimCertBegin, "ceremony", "claim_cert"},
	{"ClaimCertComplete", SubOpCeremonyClaimCertComplete, "ceremony", "claim_cert"},
	{"BeginClaimShare", SubOpCeremonyBeginClaimShare, "ceremony", "claim_share_begin"},
	{"CompleteClaimShare", SubOpCeremonyCompleteClaimShare, "ceremony", "claim_share_complete"},
	{"SignCSR", SubOpCeremonySignCSR, "ceremony", "sign_csr"},
	{"ListOfficers", SubOpCeremonyListOfficers, "ceremony", "status"},
	{"EnrollExternalOfficer", SubOpCeremonyEnrollExternalOfficer, "ceremony", "enroll_custodian"},
}

// TestCeremonyAuthz_SubOpPermissions pins the exact canonical permission
// getQRDBPermission must return for each sub-op, and asserts the spec table
// covers the live dispatch map in both directions.
func TestCeremonyAuthz_SubOpPermissions(t *testing.T) {
	spec := make(map[byte]struct{}, len(ceremonySubOpPermissionSpec))
	for _, tc := range ceremonySubOpPermissionSpec {
		spec[tc.subOp] = struct{}{}
	}
	for subOp := range ceremonySubOpHandlers {
		_, ok := spec[subOp]
		assert.Truef(t, ok,
			"sub-op 0x%02X is routable but has no entry in ceremonySubOpPermissionSpec; "+
				"add it to the spec and to ceremonySubOpPermissions", subOp)
	}

	for _, tc := range ceremonySubOpPermissionSpec {
		t.Run(tc.name, func(t *testing.T) {
			// Extra bytes after the sub-op prove the resolver only inspects payload[0].
			payload := []byte{tc.subOp, 0xDE, 0xAD}
			res, act, ok := getQRDBPermission(OpCeremony, payload)
			require.Truef(t, ok, "sub-op 0x%02X must be mapped", tc.subOp)
			assert.Equalf(t, tc.resource, res, "sub-op 0x%02X resource", tc.subOp)
			assert.Equalf(t, tc.action, act, "sub-op 0x%02X action", tc.subOp)
		})
	}
}

// TestCeremonyAuthz_WriteSubOpsAreNotStatus is the direct regression assertion
// for the escalation: every ceremony sub-op that mutates the trust root must
// resolve to something OTHER than the Auditor-held ceremony:status. Restoring
// the surface-level OpCeremony mapping fails this test.
func TestCeremonyAuthz_WriteSubOpsAreNotStatus(t *testing.T) {
	writeSubOps := []struct {
		name  string
		subOp byte
	}{
		{"Initialize", SubOpCeremonyInitialize},
		{"SignCSR", SubOpCeremonySignCSR},
		{"EnrollExternalOfficer", SubOpCeremonyEnrollExternalOfficer},
		{"ClaimCertBegin", SubOpCeremonyClaimCertBegin},
		{"ClaimCertComplete", SubOpCeremonyClaimCertComplete},
		{"BeginClaimShare", SubOpCeremonyBeginClaimShare},
		{"CompleteClaimShare", SubOpCeremonyCompleteClaimShare},
	}
	for _, tc := range writeSubOps {
		t.Run(tc.name, func(t *testing.T) {
			res, act, ok := getQRDBPermission(OpCeremony, []byte{tc.subOp})
			require.True(t, ok)
			assert.NotEqualf(t, "ceremony:status", res+":"+act,
				"ceremony write verb %s must not authorize under the read-only "+
					"ceremony:status, which qrdb grants to the Auditor role", tc.name)
		})
	}
}

// TestCeremonyAuthz_NilPayloadDenied verifies an OpCeremony request with a nil
// payload is unmapped, so the gate default-denies without consulting the
// validator.
func TestCeremonyAuthz_NilPayloadDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpCeremony, nil)
	assert.False(t, ok, "OpCeremony with nil payload must be unmapped (default-deny)")
}

// TestCeremonyAuthz_EmptyPayloadDenied verifies an explicit empty slice also
// triggers default-deny.
func TestCeremonyAuthz_EmptyPayloadDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpCeremony, []byte{})
	assert.False(t, ok, "OpCeremony with empty payload must be unmapped (default-deny)")
}

// TestCeremonyAuthz_UnknownSubOpDenied verifies an unassigned sub-op byte fails
// closed rather than inheriting a surface-level permission. 0xFF is not
// assigned.
func TestCeremonyAuthz_UnknownSubOpDenied(t *testing.T) {
	_, _, ok := getQRDBPermission(OpCeremony, []byte{0xFF})
	assert.False(t, ok, "unknown ceremony sub-op 0xFF must be unmapped (default-deny)")
}

// TestCeremonyAuthz_GateLevel_InitializePassesCorrectPerm verifies the full gate
// path: an OpCeremony request carrying SubOpCeremonyInitialize demands
// "ceremony:initialize" from the ValidatorAuthorizer.
func TestCeremonyAuthz_GateLevel_InitializePassesCorrectPerm(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	body := encMust(t, &ceremonyInitializeWireRequest{SOPin: "1234", UserPin: "5678", Threshold: 2})
	req := &Request{
		Opcode:  OpCeremony,
		Token:   "bearer-token",
		Payload: append([]byte{SubOpCeremonyInitialize}, body...),
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp, "expected allow, got denial %v", resp)
	assert.Equal(t, "ceremony:initialize", v.gotRequiredPerm,
		"OpCeremony/SubOpCeremonyInitialize must authorize as ceremony:initialize")
	assert.Equal(t, "bearer-token", v.gotToken)
	assert.Empty(t, v.gotDPoP, "QUIC must pass empty DPoP")
}

// TestCeremonyAuthz_GateLevel_SignCSRPassesCorrectPerm pins the second
// SO-exclusive verb at the gate.
func TestCeremonyAuthz_GateLevel_SignCSRPassesCorrectPerm(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	body := encMust(t, &ceremonySignCSRWireRequest{Username: "alice", CSRPEM: "csr", Role: "officer"})
	req := &Request{
		Opcode:  OpCeremony,
		Token:   "tok",
		Payload: append([]byte{SubOpCeremonySignCSR}, body...),
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp)
	assert.Equal(t, "ceremony:sign_csr", v.gotRequiredPerm)
}

// TestCeremonyAuthz_GateLevel_StatusPermission verifies the genuine read still
// authorizes as the read permission — the fix must not over-privilege status.
func TestCeremonyAuthz_GateLevel_StatusPermission(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpCeremony,
		Token:   "tok",
		Payload: []byte{SubOpCeremonyStatus},
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp)
	assert.Equal(t, "ceremony:status", v.gotRequiredPerm)
}

// TestCeremonyAuthz_GateLevel_EmptyPayloadDeniedBeforeValidator verifies an
// empty-payload OpCeremony request is denied with StatusPermissionDenied and the
// validator is never consulted.
func TestCeremonyAuthz_GateLevel_EmptyPayloadDeniedBeforeValidator(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	req := &Request{
		Opcode:  OpCeremony,
		Token:   "bearer-token",
		Payload: []byte{}, // no sub-op byte
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp, "expected denial response")
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Empty(t, v.gotToken,
		"validator must not be called when OpCeremony payload is empty")
}

// TestCeremonyAuthz_GateLevel_NilPayloadDeniedBeforeValidator verifies nil
// payload also denies before calling the validator.
func TestCeremonyAuthz_GateLevel_NilPayloadDeniedBeforeValidator(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	req := &Request{Opcode: OpCeremony, Token: "tok", Payload: nil}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp)
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Empty(t, v.gotToken, "validator must not be called when payload is nil")
}

// TestCeremonyAuthz_GateLevel_UnknownSubOpDeniedBeforeValidator verifies an
// unassigned sub-op denies before calling the validator.
func TestCeremonyAuthz_GateLevel_UnknownSubOpDeniedBeforeValidator(t *testing.T) {
	v := newAllowCeremonyValidator()
	h := newCeremonyHandler(t, &mockCeremonyServicer{})
	h.validatorAuthorizer = v

	req := &Request{Opcode: OpCeremony, Token: "tok", Payload: []byte{0xFF}}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.NotNil(t, resp)
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Empty(t, v.gotToken, "validator must not be called for an unknown sub-op")
}

// TestCeremonyAuthz_GateLevel_AllSubOpsReachValidator ensures every registered
// sub-op reaches the validator with its own permission, so no sub-op is silently
// short-circuited or collapsed onto a sibling's permission.
func TestCeremonyAuthz_GateLevel_AllSubOpsReachValidator(t *testing.T) {
	for subOp, perm := range ceremonySubOpPermissions {
		subOp, perm := subOp, perm
		t.Run(CeremonySubOpcodeToString(subOp), func(t *testing.T) {
			v := newAllowCeremonyValidator()
			h := newCeremonyHandler(t, &mockCeremonyServicer{})
			h.validatorAuthorizer = v

			req := &Request{Opcode: OpCeremony, Token: "tok", Payload: []byte{subOp}}

			_, resp := h.authorizeAndInject(context.Background(), req)
			require.Nil(t, resp, "sub-op 0x%02X with valid token must reach validator", subOp)
			assert.NotEmpty(t, v.gotToken, "sub-op 0x%02X: validator must have been called", subOp)
			assert.Equal(t, perm.resource+":"+perm.action, v.gotRequiredPerm,
				"sub-op 0x%02X requiredPerm mismatch", subOp)
		})
	}
}

// TestCeremonyAuthz_SurfaceLevelMappingIsGone asserts the OpCeremony opcode has
// no entry in quicOpcodePermissions. A surface-level entry would be unreachable
// today (the sub-op branch returns first) but would silently become the
// fallback if that branch were ever removed, restoring the escalation.
func TestCeremonyAuthz_SurfaceLevelMappingIsGone(t *testing.T) {
	_, found := quicOpcodePermissions[OpCeremony]
	assert.False(t, found,
		"OpCeremony must not carry a surface-level permission; it resolves per sub-op "+
			"through ceremonySubOpPermissions")
}
