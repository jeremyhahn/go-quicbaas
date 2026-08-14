//go:build rest

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

package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Router-level barrier authorization contract (RequireAuthz=true posture).
//
// These tests exercise the REAL router built by setupRouter — NOT a manually
// composed middleware chain — because the enforcement gap they pin was a
// ROUTING hole: the root-mounted qrdb-wire barrier mount (/v1/barrier) never
// passed through the validator gate, which setupRouter installed only on the
// /api/v1 chi group. A chain-composition test cannot catch that class of bug.
//
// Contract per the owner-approved sealed-window design:
//
//	sealed   + allowlisted verb, no token -> admitted (sealed-window bypass)
//	unsealed + no token                   -> 401
//	unsealed + valid scope                -> admitted
//	unsealed + wrong scope                -> 403
//
// Both barrier mounts (/api/v1/barrier canonical, /v1/barrier qrdb-wire) must
// enforce identically, with the same seal:* / tenant_barrier:* vocabulary as
// the gRPC mapping (pkg/api/grpc/authz_mapping.go).
// =============================================================================

// newBarrierValidatorServer builds a real REST server with the validator
// (RequireAuthz=true) posture, a wired barrier facade, and the sealed-window
// gate driven by probe — mirroring the production lifecycle wiring order.
func newBarrierValidatorServer(t *testing.T, probe *restTogglableProbe, authorizer ValidatorAuthorizer) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	s.SetBarrierService(&fakeBarrierService{})
	s.SetValidatorAuthzEnforcement(authorizer)
	s.SetSealedWindowGate(probe, &restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())
	return s
}

// TestRouter_BarrierWireMount_PostUnsealEnforcement pins the post-unseal
// contract for BOTH barrier mounts on the real router under validator
// enforcement. Regression guard for the gate-3 finding where /v1/barrier/*
// served unauthenticated calls post-unseal.
func TestRouter_BarrierWireMount_PostUnsealEnforcement(t *testing.T) {
	cases := []struct {
		name       string
		sealed     bool
		method     string
		path       string
		bearer     string
		scopeErr   error
		wantStatus int // exact status for deny; 0 means "admitted past authz"
		wantPerm   string
	}{
		// --- sealed + allowlisted verbs bypass authentication (both mounts) ---
		{
			name:   "sealed wire status admitted unauthenticated",
			sealed: true, method: http.MethodGet, path: "/v1/barrier/status",
		},
		{
			name:   "sealed api status admitted unauthenticated",
			sealed: true, method: http.MethodGet, path: "/api/v1/barrier/status",
		},
		{
			name:   "sealed wire unseal admitted unauthenticated",
			sealed: true, method: http.MethodPost, path: "/v1/barrier/unseal",
		},

		// --- unsealed + no token -> 401 (both mounts, every verb class) ---
		{
			name:   "unsealed wire status no token 401",
			sealed: false, method: http.MethodGet, path: "/v1/barrier/status",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed wire seal no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/seal",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed wire rekey no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/rekey",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed wire root-token no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/root-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed wire recovery generate no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/recovery/generate",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed api status no token 401",
			sealed: false, method: http.MethodGet, path: "/api/v1/barrier/status",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed api rekey no token 401",
			sealed: false, method: http.MethodPost, path: "/api/v1/barrier/rekey",
			wantStatus: http.StatusUnauthorized,
		},

		// --- unsealed + valid scope -> admitted with the gRPC-canon vocabulary ---
		{
			name:   "unsealed wire status valid scope admitted",
			sealed: false, method: http.MethodGet, path: "/v1/barrier/status",
			bearer: "tok", wantPerm: "tenant_barrier:status",
		},
		{
			name:   "unsealed api status valid scope admitted",
			sealed: false, method: http.MethodGet, path: "/api/v1/barrier/status",
			bearer: "tok", wantPerm: "tenant_barrier:status",
		},
		{
			name:   "unsealed wire seal valid scope admitted",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/seal",
			bearer: "tok", wantPerm: "tenant_barrier:seal",
		},
		{
			name:   "unsealed wire rekey valid scope maps seal:rekey",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/rekey",
			bearer: "tok", wantPerm: "seal:rekey",
		},

		// --- unsealed + wrong scope -> 403 ---
		{
			name:   "unsealed wire rekey wrong scope 403",
			sealed: false, method: http.MethodPost, path: "/v1/barrier/rekey",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "seal:rekey"},
			wantStatus: http.StatusForbidden, wantPerm: "seal:rekey",
		},
		{
			name:   "unsealed wire status wrong scope 403",
			sealed: false, method: http.MethodGet, path: "/v1/barrier/status",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "tenant_barrier:status"},
			wantStatus: http.StatusForbidden, wantPerm: "tenant_barrier:status",
		},
		{
			name:   "unsealed api seal wrong scope 403",
			sealed: false, method: http.MethodPost, path: "/api/v1/barrier/seal",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "tenant_barrier:seal"},
			wantStatus: http.StatusForbidden, wantPerm: "tenant_barrier:seal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &restTogglableProbe{sealedState: tc.sealed}
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{
					Subject:  "officer-1",
					TenantID: "system",
					Scopes:   []string{"tenant_barrier:status", "tenant_barrier:seal", "seal:rekey"},
				},
				retErr: tc.scopeErr,
			}
			s := newBarrierValidatorServer(t, probe, authorizer)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			rr := httptest.NewRecorder()
			s.router.ServeHTTP(rr, req)

			if tc.wantStatus != 0 {
				assert.Equal(t, tc.wantStatus, rr.Code, "deny status")
			} else {
				// Admitted past the authz layer: the handler ran, so the
				// response must not be an authn/authz denial.
				assert.NotEqual(t, http.StatusUnauthorized, rr.Code, "must not be 401")
				assert.NotEqual(t, http.StatusForbidden, rr.Code, "must not be 403")
				assert.NotEqual(t, http.StatusServiceUnavailable, rr.Code, "must not be sealed-denied")
			}
			if tc.wantPerm != "" {
				require.True(t, authorizer.called, "validator must be consulted")
				assert.Equal(t, tc.wantPerm, authorizer.gotPerm, "qrdb-canonical permission")
			}
			if tc.sealed {
				assert.False(t, authorizer.called,
					"sealed-window bypass must not consult the validator")
			}
		})
	}
}

// TestRouter_BarrierWireMount_ResealCycle drives the REAL router through
// unsealed -> re-sealed -> unsealed and asserts the wire mount's bypass window
// opens and closes with the probe (the bypass is sealed-only).
func TestRouter_BarrierWireMount_ResealCycle(t *testing.T) {
	probe := &restTogglableProbe{sealedState: false}
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{Subject: "officer-1", TenantID: "system"},
	}
	s := newBarrierValidatorServer(t, probe, authorizer)

	status := func(bearer string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/barrier/status", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		s.router.ServeHTTP(rr, req)
		return rr.Code
	}

	// Unsealed: no token denied, valid token admitted.
	assert.Equal(t, http.StatusUnauthorized, status(""))
	assert.Equal(t, http.StatusOK, status("tok"))

	// Runtime re-seal: allowlisted status admitted without a token.
	probe.sealedState = true
	assert.Equal(t, http.StatusOK, status(""))

	// Unseal completes: enforcement returns immediately.
	probe.sealedState = false
	assert.Equal(t, http.StatusUnauthorized, status(""))
}
