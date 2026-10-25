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

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// restListenerProbe is a go-qrdb sealed-window listener probe with a settable
// barrier state and enrollment always closed.
type restListenerProbe struct{ sealed bool }

func (p *restListenerProbe) IsSealed() bool                      { return p.sealed }
func (p *restListenerProbe) EnrollmentOpen(context.Context) bool { return false }
func (p *restListenerProbe) Admit(bool) func()                   { return func() {} }

const restSealedEnvelope = `{"error":"barrier sealed","code":"SEALED"}`

func serveSealedListener(s *Server, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// TestRESTSealedWindowListenerGate_ServesListenerOnlyBarrierStatus asserts
// the boot-time listener gate serves qrdb's wire barrier status, which
// go-qrdb v0.50 makes ListenerOnly (the regular gate refuses it), and still
// refuses every off-allowlist path while sealed.
func TestRESTSealedWindowListenerGate_ServesListenerOnlyBarrierStatus(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetSealedWindowListenerGate(&restListenerProbe{sealed: true},
		&restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())

	rec := serveSealedListener(s, http.MethodGet, "/v1/barrier/status")
	assert.NotEqual(t, restSealedEnvelope, rec.Body.String(),
		"the listener gate must pass the ListenerOnly barrier status")

	rec = serveSealedListener(s, http.MethodGet, "/api/v1/kms/keys")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, restSealedEnvelope, rec.Body.String())
}

// TestRESTSealedWindowListenerGate_RefusesEverythingOnceUnsealed asserts the
// listener gate is no pass-through: once the barrier unseals, the anonymous
// listener refuses even its allowlist until the full boot replaces it.
func TestRESTSealedWindowListenerGate_RefusesEverythingOnceUnsealed(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetSealedWindowListenerGate(&restListenerProbe{sealed: false},
		&restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())

	for _, path := range []string{"/v1/barrier/status", "/api/v1/kms/keys"} {
		rec := serveSealedListener(s, http.MethodGet, path)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
	}
}

// TestRESTSealedWindowListenerGate_NilProbeLeavesGateUninstalled asserts a
// nil probe installs no gate, as SetSealedWindowGate does.
func TestRESTSealedWindowListenerGate_NilProbeLeavesGateUninstalled(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetSealedWindowListenerGate(nil, &restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())

	assert.Nil(t, s.sealedListener)
	assert.Nil(t, s.sealedProbe)
	assert.Nil(t, s.sealedBypass)
}
