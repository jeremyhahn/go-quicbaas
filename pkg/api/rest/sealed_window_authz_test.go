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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// alwaysInvalidTokenValidator rejects every presented token, modelling the
// sealed state where no OIDC plane exists to validate against.
type alwaysInvalidTokenValidator struct{}

func (v *alwaysInvalidTokenValidator) ValidateToken(context.Context, string) (*auth.TokenValidationResult, error) {
	return nil, &InvalidTokenResultError{}
}

// restTogglableProbe flips sealed state so one composed middleware chain can
// be driven through the full re-seal cycle, exactly as the daemon-lifetime
// router is.
type restTogglableProbe struct{ sealedState bool }

func (p *restTogglableProbe) IsSealed() bool { return p.sealedState }

// restCeremonyStateProbe reports the ceremony state that decides whether the
// pre-operational sealed-window exemptions still apply. operational=false is a
// daemon whose init ceremony is unfinished; operational=true is the
// already-initialized daemon (restart / post-boot re-seal).
type restCeremonyStateProbe struct{ operational bool }

func (p *restCeremonyStateProbe) IsOperational() bool { return p.operational }

// restRecordingSink captures the sealed-window bypass audit events.
type restRecordingSink struct{ events [][2]string }

func (s *restRecordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

// newSealedValidatorChain composes the PRODUCTION middleware order for tests:
// qrdb's REST sealed-window middleware (via the SDK re-export, exactly what
// setupRouter installs) runs FIRST, the platform-token validator gate with
// the shared sealed-window bypass runs SECOND. Both consume the same probe
// and the same canonical-table derivation, mirroring setupRouter.
func newSealedValidatorChain(
	probe *restTogglableProbe,
	operational sealedwindow.OperationalProbe,
	authorizer ValidatorAuthorizer,
	sink *restRecordingSink,
	final http.Handler,
) http.Handler {
	mw := newValidatorMiddleware(authorizer)
	mw.sealedBypass = sealedwindow.NewAuthBypass(probe, operational, sink)
	sealed := qrdbsdk.SealedWindowMiddleware(probe, sealedwindow.RESTAllowlist())
	return sealed(mw.AuthorizeViaValidator(final))
}

// TestRESTSealedWindowAuthBypass_ValidatorGate is the per-transport contract
// table for the REST RequireAuthz=true posture (AuthorizeViaValidator below
// the sealed-window middleware):
//
//	sealed+allowlisted            -> handler, no auth, audited
//	sealed+non-allowlisted        -> 503 sealed envelope
//	unsealed+allowlisted no token -> 401 (bypass is sealed-only)
//	unsealed+valid scope          -> 200 (normal enforcement, unchanged)
func TestRESTSealedWindowAuthBypass_ValidatorGate(t *testing.T) {
	cases := []struct {
		name string
		// sealed is the barrier state; operational is the ceremony state that
		// governs the pre-operational half of the exemption.
		sealed      bool
		operational bool
		method      string
		path        string
		bearer      string
		wantStatus  int
		wantAudit   int
		wantAuthz   bool
	}{
		{
			name:       "sealed allowlisted unauthenticated admitted",
			sealed:     true,
			method:     http.MethodPost,
			path:       "/api/v1/barrier/unseal",
			wantStatus: http.StatusOK,
			wantAudit:  1,
		},
		{
			name:       "sealed allowlisted wire mount admitted",
			sealed:     true,
			method:     http.MethodGet,
			path:       "/v1/barrier/status",
			wantStatus: http.StatusOK,
			wantAudit:  1,
		},
		{
			name:       "sealed pre-operational init status admitted",
			sealed:     true,
			method:     http.MethodGet,
			path:       "/api/v1/init/status",
			wantStatus: http.StatusOK,
			wantAudit:  1,
		},
		// Scope axis, end to end: the ceremony status / claim routes are
		// ScopePreOperational. On an already-initialized (operational) sealed
		// daemon the sealed middleware still SERVES them — they stay on the
		// deny-gate allowlist — but the exemption has expired, so the
		// validator gate demands a token and the unauthenticated request is
		// rejected instead of admitted.
		{
			name:        "sealed operational init status requires auth",
			sealed:      true,
			operational: true,
			method:      http.MethodGet,
			path:        "/api/v1/init/status",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "sealed operational claim-cert begin requires auth",
			sealed:      true,
			operational: true,
			method:      http.MethodPost,
			path:        "/api/v1/init/claim-cert/begin",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "sealed operational unseal still admitted",
			sealed:      true,
			operational: true,
			method:      http.MethodPost,
			path:        "/api/v1/barrier/unseal",
			wantStatus:  http.StatusOK,
			wantAudit:   1,
		},
		{
			name:       "sealed non-allowlisted sealed error",
			sealed:     true,
			method:     http.MethodGet,
			path:       "/api/v1/kms/keys",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "sealed credentials excluded sealed error",
			sealed:     true,
			method:     http.MethodPost,
			path:       "/v1/credentials",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "unsealed allowlisted verb without token 401",
			sealed:     false,
			method:     http.MethodPost,
			path:       "/api/v1/barrier/unseal",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unsealed valid scope admitted",
			sealed:     false,
			method:     http.MethodPost,
			path:       "/api/v1/barrier/unseal",
			bearer:     "tok",
			wantStatus: http.StatusOK,
			wantAuthz:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &restTogglableProbe{sealedState: tc.sealed}
			sink := &restRecordingSink{}
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{
					Subject:  "alice",
					TenantID: "system",
					Scopes:   []string{"tenant_barrier:unseal"},
				},
			}
			final := &testHandler{}
			chain := newSealedValidatorChain(
				probe, &restCeremonyStateProbe{operational: tc.operational},
				authorizer, sink, final)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			rr := httptest.NewRecorder()
			chain.ServeHTTP(rr, req)

			assert.Equal(t, tc.wantStatus, rr.Code)
			assert.Equal(t, tc.wantStatus == http.StatusOK, final.called, "handler reached")
			assert.Len(t, sink.events, tc.wantAudit, "audit emissions")
			if tc.wantAudit > 0 {
				assert.Equal(t, "rest", sink.events[0][0])
				assert.Equal(t, tc.path, sink.events[0][1])
			}
			assert.Equal(t, tc.wantAuthz, authorizer.called, "validator consultations")
		})
	}
}

// TestRESTSealedWindowAuthBypass_ResealCycle drives ONE composed chain
// through unsealed -> re-sealed -> unsealed and asserts the bypass window
// opens and closes with the probe.
func TestRESTSealedWindowAuthBypass_ResealCycle(t *testing.T) {
	probe := &restTogglableProbe{sealedState: false}
	sink := &restRecordingSink{}
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{Subject: "alice", Scopes: []string{"tenant_barrier:unseal"}},
	}
	final := &testHandler{}
	chain := newSealedValidatorChain(
		probe, &restCeremonyStateProbe{}, authorizer, sink, final)

	do := func(method, path, bearer string) int {
		final.called = false
		req := httptest.NewRequest(method, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		chain.ServeHTTP(rr, req)
		return rr.Code
	}

	// Unsealed: token required.
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodPost, "/api/v1/barrier/unseal", ""))
	assert.Equal(t, http.StatusOK, do(http.MethodPost, "/api/v1/barrier/unseal", "tok"))

	// Runtime RE-SEAL: allowlisted verbs admit unauthenticated, others 503.
	probe.sealedState = true
	assert.Equal(t, http.StatusOK, do(http.MethodPost, "/api/v1/barrier/unseal", ""))
	require.Len(t, sink.events, 1)
	assert.Equal(t, http.StatusServiceUnavailable, do(http.MethodGet, "/api/v1/kms/keys", "tok"))

	// Unseal completes: enforcement returns immediately.
	probe.sealedState = false
	assert.Equal(t, http.StatusUnauthorized, do(http.MethodPost, "/api/v1/barrier/unseal", ""))
	assert.Len(t, sink.events, 1, "no further audit after the window closes")
}

// TestRESTSealedWindowAuthBypass_DefaultDenyGate covers the top-router
// default-deny posture: sealed+allowlisted admits unauthenticated (audited),
// unsealed demands a token again.
func TestRESTSealedWindowAuthBypass_DefaultDenyGate(t *testing.T) {
	probe := &restTogglableProbe{sealedState: true}
	sink := &restRecordingSink{}
	mw := newValidatorMiddleware(&recordingValidatorAuthorizer{})
	mw.sealedBypass = sealedwindow.NewAuthBypass(probe, &restCeremonyStateProbe{}, sink)
	final := &testHandler{}
	sealed := qrdbsdk.SealedWindowMiddleware(probe, sealedwindow.RESTAllowlist())
	chain := sealed(mw.EnforceDefaultDeny("")(final))

	// Sealed + allowlisted -> admitted unauthenticated + audited.
	req := httptest.NewRequest(http.MethodPost, "/v1/barrier/unseal", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, final.called)
	require.Len(t, sink.events, 1)

	// Sealed + non-allowlisted -> sealed envelope.
	final.called = false
	req = httptest.NewRequest(http.MethodPost, "/v1/credentials", nil)
	rr = httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.False(t, final.called)

	// Unsealed + allowlisted without token -> 401 (bypass is sealed-only).
	probe.sealedState = false
	final.called = false
	req = httptest.NewRequest(http.MethodPost, "/v1/barrier/unseal", nil)
	rr = httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, final.called)
	assert.Len(t, sink.events, 1)
}

// TestRESTSealedWindowAuthBypass_TokenBindingGate pins that the
// proof-of-possession gate also honors the bypass: while sealed, an
// allowlisted path with a stale/garbage bearer is admitted (the sealed IAM
// plane cannot validate it), while unsealed the same request 401s.
func TestRESTSealedWindowAuthBypass_TokenBindingGate(t *testing.T) {
	probe := &restTogglableProbe{sealedState: true}
	sink := &restRecordingSink{}
	mw := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		TokenValidator: &alwaysInvalidTokenValidator{},
	})
	mw.sealedBypass = sealedwindow.NewAuthBypass(probe, &restCeremonyStateProbe{}, sink)
	final := &testHandler{}
	chain := mw.EnforceTokenBinding(final)

	// Sealed + allowlisted + garbage bearer -> admitted (audited).
	req := httptest.NewRequest(http.MethodPost, "/v1/barrier/unseal", nil)
	req.Header.Set("Authorization", "Bearer stale-token")
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, final.called)
	require.Len(t, sink.events, 1)

	// Unsealed + same garbage bearer -> 401 (binding enforcement restored).
	probe.sealedState = false
	final.called = false
	rr = httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, final.called)
}
