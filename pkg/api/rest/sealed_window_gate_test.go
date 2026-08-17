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
)

// restSealedProbe is a mutable sealed-window probe for router gate tests.
type restSealedProbe struct{ sealed bool }

func (p *restSealedProbe) IsSealed() bool { return p.sealed }

// restCeremonyStateProbe reports the ceremony state that decides whether the
// sealed-window bypass admits an unauthenticated request (operational =
// ceremony completed for this node).
type restCeremonyStateProbe struct{ operational bool }

func (p *restCeremonyStateProbe) IsOperational() bool { return p.operational }

// newSealedGateRESTServer builds a REST server over the shared mock facade
// with the sealed-window gate installed from the canonical derivation.
func newSealedGateRESTServer(t *testing.T, probe *restSealedProbe) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	s.SetSealedWindowGate(probe, &restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())
	return s
}

// TestRESTSealedWindowGate_SealedAdmitDenyTable drives the router with the
// gate engaged and asserts, table-driven from the single canonical
// derivation, that off-allowlist paths receive qrdb's 503 sealed envelope
// while allowlisted paths reach their handlers (any non-503-sealed status).
func TestRESTSealedWindowGate_SealedAdmitDenyTable(t *testing.T) {
	s := newSealedGateRESTServer(t, &restSealedProbe{sealed: true})

	cases := []struct {
		name   string
		method string
		path   string
		denied bool
	}{
		// Allowlisted sealed-window surface (barrier / ceremony / init /
		// bootstrap). The handlers themselves may fail (no barrier service
		// wired in this fixture) but the request must NOT be rejected by the
		// sealed gate.
		{"barrier status (qrdb wire)", http.MethodGet, "/v1/barrier/status", false},
		{"barrier status (api mirror)", http.MethodGet, "/api/v1/barrier/status", false},
		{"barrier unseal (qrdb wire)", http.MethodPost, "/v1/barrier/unseal", false},
		{"barrier initialize (api mirror)", http.MethodPost, "/api/v1/barrier/initialize", false},
		{"shamir unseal-share", http.MethodPost, "/v1/barrier/shamir/unseal-share", false},
		{"ceremony status (qrdb wire)", http.MethodGet, "/v1/ceremony/status", false},
		{"init status (api mirror)", http.MethodGet, "/api/v1/init/status", false},
		{"bootstrap prefix", http.MethodPost, "/api/v1/bootstrap/auto", false},

		// Off-allowlist surfaces: sealed envelope.
		{"kv get", http.MethodGet, "/api/v1/kv/foo/bar", true},
		{"entities", http.MethodGet, "/api/v1/entities/foo", true},
		{"kms keys", http.MethodGet, "/api/v1/kms/keys", true},
		{"secrets", http.MethodGet, "/api/v1/secrets/", true},
		{"credentials wire (deliberate exclusion)", http.MethodGet, "/v1/credentials/", true},
		{"barrier seal (deliberate exclusion)", http.MethodPost, "/v1/barrier/seal", true},
		{"barrier enable-encryption (deliberate exclusion)", http.MethodPost, "/api/v1/barrier/enable-encryption", true},
		{"legacy seal surface", http.MethodGet, "/api/v1/seal/status", true},
		{"iam users", http.MethodGet, "/api/v1/iam/users", true},
		{"user factors wire", http.MethodGet, "/v1/users/factors/list", true},

		// The strict ceremony model (sealedwindow.StrictCeremonyExclusions)
		// withholds the three ceremony verbs that MINT or DISTRIBUTE trust on
		// EVERY transport. They keep their full entries in the canonical
		// Table — so the quicbaas table still diffs mechanically against
		// qrdb's, which allowlists all three — but every derivation skips
		// them, so the sealed gate must reject them. Reaching any of these
		// pre-authentication on a fresh daemon hands over the trust root; they
		// are driven in-process by `quicbaasd init` instead.
		{"ceremony initialize (strict exclusion)", http.MethodPost, "/v1/ceremony/initialize", true},
		{"ceremony initialize api mirror (strict exclusion)", http.MethodPost, "/api/v1/init/initialize", true},
		{"ceremony sign-csr (strict exclusion)", http.MethodPost, "/v1/ceremony/sign-csr", true},
		{"init enroll-external-officer (strict exclusion)", http.MethodPost, "/v1/init/enroll-external-officer", true},
		{"init enroll-external-officer api mirror (strict exclusion)", http.MethodPost, "/api/v1/init/enroll-external-officer", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, req)

			sealedEnvelope := rec.Code == http.StatusServiceUnavailable &&
				rec.Body.String() == `{"error":"barrier sealed","code":"SEALED"}`
			if tc.denied {
				require.True(t, sealedEnvelope,
					"expected sealed 503 envelope, got %d %q", rec.Code, rec.Body.String())
				assert.Equal(t, "5", rec.Header().Get("Retry-After"))
			} else {
				assert.False(t, sealedEnvelope,
					"allowlisted path must pass the sealed gate, got sealed envelope")
			}
		})
	}
}

// TestRESTSealedWindowGate_UnsealedPassThrough asserts the SAME installed
// gate becomes a pass-through the moment the probe reports unsealed — the
// gate stays installed for the daemon's lifetime and re-engages on a
// post-boot seal.
func TestRESTSealedWindowGate_UnsealedPassThrough(t *testing.T) {
	probe := &restSealedProbe{sealed: true}
	s := newSealedGateRESTServer(t, probe)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "sealed: off-allowlist denied")

	// Unseal: same router, same middleware instance, now a pass-through.
	probe.sealed = false
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	assert.NotEqual(t, `{"error":"barrier sealed","code":"SEALED"}`, rec.Body.String(),
		"unsealed: request must reach the handler")

	// Re-seal: the gate re-engages without any rewiring.
	probe.sealed = true
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "re-sealed: off-allowlist denied again")
}
