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
// Router-level authorization contract for the remaining root-mounted qrdb-wire
// mounts (RequireAuthz=true posture): /v1/credentials, /v1/ceremony, /v1/init,
// /users, /v1/users/factors — plus the /api/v1/init mirror.
//
// Same routing-hole class as the /v1/barrier fix pinned by
// server_barrier_wire_authz_test.go: these mounts live OUTSIDE the /api/v1
// chi group, so without useAPIAuthzMiddleware inside each Route closure they
// never pass through the validator gate. These tests exercise the REAL router
// built by setupRouter (a chain-composition test cannot catch a routing hole).
//
// Contract per the owner-approved sealed-window design:
//
//	sealed   + allowlisted verb, no token  -> admitted (sealed-window bypass)
//	sealed   + NON-allowlisted mount       -> 503 (sealed-window deny gate)
//	unsealed + no token                    -> 401
//	unsealed + valid scope                 -> admitted
//	unsealed + wrong scope                 -> 403
//
// There are NO deliberately-public paths on these mounts while unsealed: the
// pre-auth/first-boot semantics of the ceremony + init surfaces exist ONLY as
// sealed-window bypasses (pkg/api/sealedwindow Table + RESTExtra). The
// credential, user-lookup, and user-factor surfaces are deliberately OFF the
// sealed allowlist and are never public in any barrier state.
// =============================================================================

// newWireMountValidatorServer builds a real REST server with the validator
// (RequireAuthz=true) posture, wired ceremony/barrier facades, and the
// sealed-window gate driven by probe — mirroring the production lifecycle
// wiring order (same recipe as newBarrierValidatorServer).
func newWireMountValidatorServer(t *testing.T, probe *restTogglableProbe, authorizer ValidatorAuthorizer) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	s.SetBarrierService(&fakeBarrierService{})
	s.SetCeremonyService(&fakeCeremonyService{})
	s.SetValidatorAuthzEnforcement(authorizer)
	s.SetSealedWindowGate(probe, &restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())
	return s
}

// wireMountScopes are the scopes granted to the recording authorizer's
// AuthorizedContext in the admit cases; the fake approves any permission
// unless retErr is set, so the list is informational.
var wireMountScopes = []string{
	"credentials:list", "credentials:rotate", "ceremony:status", "users:read",
}

// TestRouter_QRDBWireMounts_PostUnsealEnforcement pins the post-unseal
// contract for every remaining root-mounted qrdb-wire mount on the real
// router under validator enforcement. Regression guard for the routing-hole
// class where a root mount never passed through any authz middleware.
func TestRouter_QRDBWireMounts_PostUnsealEnforcement(t *testing.T) {
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
		// --- sealed + allowlisted ceremony/init verbs bypass authentication ---
		{
			name:   "sealed ceremony status admitted unauthenticated",
			sealed: true, method: http.MethodGet, path: "/v1/ceremony/status",
		},
		{
			name:   "sealed ceremony initialize admitted unauthenticated",
			sealed: true, method: http.MethodPost, path: "/v1/ceremony/initialize",
		},
		{
			name:   "sealed ceremony officer-share begin admitted unauthenticated",
			sealed: true, method: http.MethodPost, path: "/v1/ceremony/officer-share/begin",
		},
		{
			name:   "sealed init enroll-external-officer admitted unauthenticated",
			sealed: true, method: http.MethodPost, path: "/v1/init/enroll-external-officer",
		},
		{
			name:   "sealed api init mirror status admitted unauthenticated",
			sealed: true, method: http.MethodGet, path: "/api/v1/init/status",
		},

		// --- sealed + NON-allowlisted mounts stay sealed-denied (503), never
		// admitted and never bypassed: credentials / users / user factors are
		// deliberately OFF the sealed-window allowlist ---
		{
			name:   "sealed credentials list sealed-denied",
			sealed: true, method: http.MethodGet, path: "/v1/credentials",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:   "sealed credential rotate sealed-denied",
			sealed: true, method: http.MethodPost, path: "/v1/credentials/db/rotate",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:   "sealed user lookup sealed-denied",
			sealed: true, method: http.MethodGet, path: "/users/by-username/alice",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:   "sealed user factor list sealed-denied",
			sealed: true, method: http.MethodGet, path: "/v1/users/factors/list",
			wantStatus: http.StatusServiceUnavailable,
		},

		// --- unsealed + no token -> 401 on every mount (nothing is public) ---
		{
			name:   "unsealed credentials list no token 401",
			sealed: false, method: http.MethodGet, path: "/v1/credentials",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed credential rotate no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/credentials/db/rotate",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed ceremony status no token 401",
			sealed: false, method: http.MethodGet, path: "/v1/ceremony/status",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed ceremony sign-csr no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/ceremony/sign-csr",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed init enroll-external-officer no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/init/enroll-external-officer",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed api init mirror status no token 401",
			sealed: false, method: http.MethodGet, path: "/api/v1/init/status",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed user lookup no token 401",
			sealed: false, method: http.MethodGet, path: "/users/by-username/alice",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed user factor add no token 401",
			sealed: false, method: http.MethodPost, path: "/v1/users/factors",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "unsealed user factor list no token 401",
			sealed: false, method: http.MethodGet, path: "/v1/users/factors/list",
			wantStatus: http.StatusUnauthorized,
		},

		// --- unsealed + valid scope -> admitted with the gRPC-canon
		// qrdb-canonical permission (multi-protocol parity) ---
		{
			name:   "unsealed credentials list valid scope admitted",
			sealed: false, method: http.MethodGet, path: "/v1/credentials",
			bearer: "tok", wantPerm: "credentials:list",
		},
		{
			name:   "unsealed credential strategy maps credentials:strategy",
			sealed: false, method: http.MethodGet, path: "/v1/credentials/strategy",
			bearer: "tok", wantPerm: "credentials:strategy",
		},
		{
			name:   "unsealed credential submit maps credentials:submit",
			sealed: false, method: http.MethodPost, path: "/v1/credentials/submit",
			bearer: "tok", wantPerm: "credentials:submit",
		},
		{
			name:   "unsealed credential delete maps credentials:delete",
			sealed: false, method: http.MethodDelete, path: "/v1/credentials/db",
			bearer: "tok", wantPerm: "credentials:delete",
		},
		{
			name:   "unsealed credential rotate maps credentials:rotate",
			sealed: false, method: http.MethodPost, path: "/v1/credentials/db/rotate",
			bearer: "tok", wantPerm: "credentials:rotate",
		},
		{
			name:   "unsealed credential gc maps credentials:gc_expired",
			sealed: false, method: http.MethodPost, path: "/v1/credentials/db/gc",
			bearer: "tok", wantPerm: "credentials:gc_expired",
		},
		{
			name:   "unsealed credential versions maps credentials:list_versions",
			sealed: false, method: http.MethodGet, path: "/v1/credentials/db/versions",
			bearer: "tok", wantPerm: "credentials:list_versions",
		},
		{
			name:   "unsealed credential version get maps credentials:retrieve_version",
			sealed: false, method: http.MethodGet, path: "/v1/credentials/db/versions/3",
			bearer: "tok", wantPerm: "credentials:retrieve_version",
		},
		{
			name:   "unsealed ceremony status maps ceremony:status",
			sealed: false, method: http.MethodGet, path: "/v1/ceremony/status",
			bearer: "tok", wantPerm: "ceremony:status",
		},
		{
			name:   "unsealed ceremony officers maps ceremony:status",
			sealed: false, method: http.MethodGet, path: "/v1/ceremony/officers",
			bearer: "tok", wantPerm: "ceremony:status",
		},
		{
			name:   "unsealed ceremony initialize maps ceremony:initialize",
			sealed: false, method: http.MethodPost, path: "/v1/ceremony/initialize",
			bearer: "tok", wantPerm: "ceremony:initialize",
		},
		{
			name:   "unsealed ceremony claim-cert begin maps ceremony:claim_cert",
			sealed: false, method: http.MethodPost, path: "/v1/ceremony/claim-cert/begin",
			bearer: "tok", wantPerm: "ceremony:claim_cert",
		},
		{
			name:   "unsealed ceremony officer-share complete maps ceremony:claim_share_complete",
			sealed: false, method: http.MethodPost, path: "/v1/ceremony/officer-share/complete",
			bearer: "tok", wantPerm: "ceremony:claim_share_complete",
		},
		{
			name:   "unsealed ceremony sign-csr maps ceremony:sign_csr",
			sealed: false, method: http.MethodPost, path: "/v1/ceremony/sign-csr",
			bearer: "tok", wantPerm: "ceremony:sign_csr",
		},
		{
			name:   "unsealed init enroll maps ceremony:enroll_custodian",
			sealed: false, method: http.MethodPost, path: "/v1/init/enroll-external-officer",
			bearer: "tok", wantPerm: "ceremony:enroll_custodian",
		},
		{
			name:   "unsealed api init mirror status maps ceremony:status",
			sealed: false, method: http.MethodGet, path: "/api/v1/init/status",
			bearer: "tok", wantPerm: "ceremony:status",
		},
		{
			name:   "unsealed api init mirror enroll maps ceremony:enroll_custodian",
			sealed: false, method: http.MethodPost, path: "/api/v1/init/enroll-external-officer",
			bearer: "tok", wantPerm: "ceremony:enroll_custodian",
		},
		{
			name:   "unsealed user lookup maps users:read",
			sealed: false, method: http.MethodGet, path: "/users/by-username/alice",
			bearer: "tok", wantPerm: "users:read",
		},
		{
			name:   "unsealed user factor add maps user_authfactors:add",
			sealed: false, method: http.MethodPost, path: "/v1/users/factors",
			bearer: "tok", wantPerm: "user_authfactors:add",
		},
		{
			name:   "unsealed user factor get maps user_authfactors:get",
			sealed: false, method: http.MethodGet, path: "/v1/users/factors/fac-1",
			bearer: "tok", wantPerm: "user_authfactors:get",
		},
		{
			name:   "unsealed user factor remove maps user_authfactors:remove",
			sealed: false, method: http.MethodDelete, path: "/v1/users/factors/fac-1",
			bearer: "tok", wantPerm: "user_authfactors:remove",
		},

		// --- unsealed + wrong scope -> 403 ---
		{
			name:   "unsealed credential rotate wrong scope 403",
			sealed: false, method: http.MethodPost, path: "/v1/credentials/db/rotate",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "credentials:rotate"},
			wantStatus: http.StatusForbidden, wantPerm: "credentials:rotate",
		},
		{
			name:   "unsealed ceremony status wrong scope 403",
			sealed: false, method: http.MethodGet, path: "/v1/ceremony/status",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "ceremony:status"},
			wantStatus: http.StatusForbidden, wantPerm: "ceremony:status",
		},
		{
			name:   "unsealed user lookup wrong scope 403",
			sealed: false, method: http.MethodGet, path: "/users/by-username/alice",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "users:read"},
			wantStatus: http.StatusForbidden, wantPerm: "users:read",
		},
		{
			name:   "unsealed user factor add wrong scope 403",
			sealed: false, method: http.MethodPost, path: "/v1/users/factors",
			bearer:     "tok",
			scopeErr:   &platformtoken.ScopeDeniedError{Required: "user_authfactors:add"},
			wantStatus: http.StatusForbidden, wantPerm: "user_authfactors:add",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &restTogglableProbe{sealedState: tc.sealed}
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{
					Subject:  "officer-1",
					TenantID: "system",
					Scopes:   wireMountScopes,
				},
				retErr: tc.scopeErr,
			}
			s := newWireMountValidatorServer(t, probe, authorizer)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			rr := httptest.NewRecorder()
			s.router.ServeHTTP(rr, req)

			if tc.wantStatus != 0 {
				assert.Equal(t, tc.wantStatus, rr.Code, "deny status")
			} else {
				// Admitted past the authz layer: the response must not be an
				// authn/authz denial or a sealed-window denial. The handler
				// itself may still fail (e.g. unwired credential facade) —
				// that is a service-layer concern, not an authz one.
				assert.NotEqual(t, http.StatusUnauthorized, rr.Code, "must not be 401")
				assert.NotEqual(t, http.StatusForbidden, rr.Code, "must not be 403")
			}
			if tc.wantPerm != "" {
				require.True(t, authorizer.called, "validator must be consulted")
				assert.Equal(t, tc.wantPerm, authorizer.gotPerm, "qrdb-canonical permission")
			}
			if tc.sealed {
				assert.False(t, authorizer.called,
					"sealed state must never consult the validator (bypass or sealed-deny)")
			}
		})
	}
}

// TestRouter_QRDBWireMounts_ResealCycle drives the REAL router through
// unsealed -> re-sealed -> unsealed on a sealed-allowlisted ceremony verb and
// a non-allowlisted credential verb, asserting the bypass window opens and
// closes with the probe for the former and NEVER opens for the latter.
func TestRouter_QRDBWireMounts_ResealCycle(t *testing.T) {
	probe := &restTogglableProbe{sealedState: false}
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{Subject: "officer-1", TenantID: "system"},
	}
	s := newWireMountValidatorServer(t, probe, authorizer)

	do := func(method, path, bearer string) int {
		req := httptest.NewRequest(method, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		s.router.ServeHTTP(rr, req)
		return rr.Code
	}

	// Unsealed: ceremony status requires a token; a valid one is admitted.
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/v1/ceremony/status", ""))
	assert.Equal(t, http.StatusOK, do(http.MethodGet, "/v1/ceremony/status", "tok"))

	// Unsealed: credentials list requires a token too.
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/v1/credentials", ""))

	// Runtime re-seal: allowlisted ceremony status admitted without a token,
	// the credential surface is sealed-denied (503), never bypassed.
	probe.sealedState = true
	assert.Equal(t, http.StatusOK, do(http.MethodGet, "/v1/ceremony/status", ""))
	assert.Equal(t, http.StatusServiceUnavailable, do(http.MethodGet, "/v1/credentials", ""))

	// Unseal completes: enforcement returns immediately on both mounts.
	probe.sealedState = false
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/v1/ceremony/status", ""))
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodGet, "/v1/credentials", ""))
}
