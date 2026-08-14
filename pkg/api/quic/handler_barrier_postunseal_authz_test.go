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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Post-unseal barrier contract for the NATIVE quicbaas-quic opcode path.
//
// Companion to TestQUICSealedWindowAuthBypass_Composition: that table pins the
// sealed-window bypass mechanics; this one pins the POST-UNSEAL enforcement
// contract for the barrier verbs observed in the gate-3 audit (status / seal /
// rekey), using the production dispatch order (server sealed gate, then
// handler validator enforcement — exactly handleStream):
//
//	sealed   + allowlisted, no token -> admitted (bypass, audited)
//	unsealed + no token              -> StatusUnauthorized
//	unsealed + valid scope           -> admitted, gRPC-canon permission consulted
//	unsealed + wrong scope           -> StatusPermissionDenied
//
// The required permissions MUST match pkg/api/grpc/authz_mapping.go
// byte-for-byte (tenant_barrier:status / tenant_barrier:seal / seal:rekey).
//
// NOTE: the qrdb-quic ALPN bridge (handleQRDBStream) is NOT under this
// token contract — the qrdb wire frame carries no bearer token. Under
// RequireAuthz the bridge instead fail-closes on the VERIFIED mTLS peer
// certificate (see handler_qrdb_bridge_auth_test.go and the posture
// documentation on buildQRDBHandlers).
// =============================================================================

func TestQUICBarrierPostUnseal_Enforcement(t *testing.T) {
	cases := []struct {
		name       string
		sealed     bool
		subOp      byte
		token      string
		scopeErr   error
		wantSealed bool
		wantAdmit  bool
		wantStatus byte
		wantPerm   string
		wantAudit  int
	}{
		// --- sealed + allowlisted -> admitted unauthenticated ---
		{
			name:   "sealed status admitted unauthenticated",
			sealed: true, subOp: SubOpBarrierStatus,
			wantAdmit: true, wantAudit: 1,
		},

		// --- unsealed + no token -> Unauthorized ---
		{
			name:   "unsealed status no token unauthorized",
			sealed: false, subOp: SubOpBarrierStatus,
			wantStatus: StatusUnauthorized,
		},
		{
			name:   "unsealed seal no token unauthorized",
			sealed: false, subOp: SubOpBarrierSeal,
			wantStatus: StatusUnauthorized,
		},
		{
			name:   "unsealed rekey no token unauthorized",
			sealed: false, subOp: SubOpBarrierRekey,
			wantStatus: StatusUnauthorized,
		},
		{
			name:   "unsealed root-token no token unauthorized",
			sealed: false, subOp: SubOpBarrierRootToken,
			wantStatus: StatusUnauthorized,
		},

		// --- unsealed + valid scope -> admitted with the canonical permission ---
		{
			name:   "unsealed status valid scope admitted",
			sealed: false, subOp: SubOpBarrierStatus, token: "good",
			wantAdmit: true, wantPerm: "tenant_barrier:status",
		},
		{
			name:   "unsealed seal valid scope admitted",
			sealed: false, subOp: SubOpBarrierSeal, token: "good",
			wantAdmit: true, wantPerm: "tenant_barrier:seal",
		},
		{
			name:   "unsealed rekey valid scope maps seal:rekey",
			sealed: false, subOp: SubOpBarrierRekey, token: "good",
			wantAdmit: true, wantPerm: "seal:rekey",
		},

		// --- unsealed + wrong scope -> PermissionDenied ---
		{
			name:   "unsealed status wrong scope denied",
			sealed: false, subOp: SubOpBarrierStatus, token: "good",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "tenant_barrier:status"},
			wantStatus: StatusPermissionDenied, wantPerm: "tenant_barrier:status",
		},
		{
			name:   "unsealed seal wrong scope denied",
			sealed: false, subOp: SubOpBarrierSeal, token: "good",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "tenant_barrier:seal"},
			wantStatus: StatusPermissionDenied, wantPerm: "tenant_barrier:seal",
		},
		{
			name:   "unsealed rekey wrong scope denied",
			sealed: false, subOp: SubOpBarrierRekey, token: "good",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "seal:rekey"},
			wantStatus: StatusPermissionDenied, wantPerm: "seal:rekey",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &quicTogglableProbe{sealedState: tc.sealed}
			sink := &quicRecordingSink{}
			authorizer := &countingQUICAuthorizer{
				fakeValidatorAuthorizer: fakeValidatorAuthorizer{
					authzCtx: &platformtoken.AuthorizedContext{Subject: "officer-1", TenantID: "system"},
					err:      tc.scopeErr,
				},
			}
			fixture := newQUICSealedFixture(probe, &quicCeremonyStateProbe{}, authorizer, sink)

			out := fixture.dispatch(&Request{
				Opcode:  OpBarrier,
				Payload: []byte{tc.subOp},
				Token:   tc.token,
			})

			assert.Equal(t, tc.wantSealed, out.sealedDenied, "sealed gate denial")
			if tc.wantAdmit {
				assert.Nil(t, out.authzResp, "request must be admitted to the handler")
			} else if !tc.wantSealed {
				require.NotNil(t, out.authzResp, "request must be denied")
				assert.Equal(t, tc.wantStatus, out.authzResp.Status, "deny status")
			}
			if tc.wantPerm != "" {
				assert.Equal(t, 1, authorizer.calls, "validator consulted exactly once")
				assert.Equal(t, tc.wantPerm, authorizer.gotRequiredPerm, "qrdb-canonical permission")
			}
			if tc.sealed {
				assert.Zero(t, authorizer.calls, "sealed-window bypass must not consult the validator")
			}
			assert.Len(t, sink.events, tc.wantAudit, "audit emissions")
		})
	}
}
