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
	"bytes"
	"context"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/pkg/api/quic/wire"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// =============================================================================
// qrdb-quic ALPN bridge peer-certificate enforcement (owner-approved,
// fail-closed, no wire change).
//
// The qrdb wire frame carries NO bearer token, so under RequireAuthz=true
// (SetValidatorEnforcement installed) post-unseal bridge method dispatch
// requires a VERIFIED mTLS client certificate on the connection. Contract:
//
//	sealed   + allowlisted, no cert -> admitted (sealed-window bypass, audited)
//	unsealed + no cert              -> wire.StatusUnauthenticated (auth required)
//	unsealed + verified peer cert   -> dispatched
//	RequireAuthz=false              -> legacy connection-level behaviour unchanged
//	admin.Health                    -> public in every posture (liveness probe)
//
// The sealed-window DENY gate runs before the certificate check, so while
// sealed a non-allowlisted method stays StatusUnavailable (never
// auth-required) — gate ordering is pinned below.
// =============================================================================

// newBridgeAuthTestServer builds a bare Server with the qrdb-quic bridge
// dispatch state wired: logger, context, sealed-window gate (canonical
// allowlists), the barrier facade, and — when requireAuthz is set — the
// validator posture signal lifecycle installs under RequireAuthz=true.
func newBridgeAuthTestServer(t *testing.T, sealed, requireAuthz bool) (*Server, *fakeSealedProbe) {
	t.Helper()
	s := &Server{}
	s.logger = createTestLogger()
	s.ctx = context.Background()
	probe := &fakeSealedProbe{sealed: sealed}
	s.SetSealedWindowGate(probe, sealedwindow.QUICAllowlist(), sealedwindow.QRDBQUICAllowlist())
	s.SetBarrierService(&e2eBarrierService{})
	if requireAuthz {
		s.SetValidatorEnforcement(&fakeValidatorAuthorizer{})
	}
	return s, probe
}

// qrdbWirePipe is an in-memory io.ReadWriter carrying one qrdb wire-format
// request (in) and capturing the response (out) so serveQRDBRequest is
// exercised end-to-end without a live QUIC stream.
type qrdbWirePipe struct {
	in  bytes.Buffer
	out bytes.Buffer
}

func (p *qrdbWirePipe) Read(b []byte) (int, error)  { return p.in.Read(b) }
func (p *qrdbWirePipe) Write(b []byte) (int, error) { return p.out.Write(b) }

// serveBridgeMethod drives one full serveQRDBRequest cycle for method and
// returns the wire response status + payload.
func serveBridgeMethod(t *testing.T, s *Server, method string, peerCert *x509.Certificate) (uint16, []byte) {
	t.Helper()
	pipe := &qrdbWirePipe{}
	require.NoError(t, wire.WriteRequest(&pipe.in, method, "", 7, nil))
	s.serveQRDBRequest(pipe, peerCert)
	requestID, status, payload, err := wire.ReadResponse(&pipe.out)
	require.NoError(t, err, "bridge must always write a wire response")
	assert.Equal(t, uint64(7), requestID, "request id echoed")
	return status, payload
}

// TestQRDBBridgeAuthRequired_Decision pins the fail-closed decision table of
// qrdbBridgeAuthRequired across every posture combination.
func TestQRDBBridgeAuthRequired_Decision(t *testing.T) {
	cert := &x509.Certificate{}

	cases := []struct {
		name         string
		sealed       bool
		requireAuthz bool
		method       string
		peerCert     *x509.Certificate
		want         bool
	}{
		// RequireAuthz=false: legacy behaviour — never auth-required.
		{"authz off unsealed no cert", false, false, "barrier.Status", nil, false},
		{"authz off sealed no cert", true, false, "credential.Submit", nil, false},

		// RequireAuthz=true, unsealed: cert required for method dispatch.
		{"authz on unsealed no cert barrier", false, true, "barrier.Status", nil, true},
		{"authz on unsealed no cert tenant", false, true, "tenant.BarrierStatus", nil, true},
		{"authz on unsealed no cert credential", false, true, "credential.Submit", nil, true},
		{"authz on unsealed no cert user", false, true, "user.GetByUsername", nil, true},

		// RequireAuthz=true, verified peer cert: admitted.
		{"authz on unsealed with cert", false, true, "barrier.Status", cert, false},
		{"authz on sealed with cert", true, true, "barrier.Unseal", cert, false},

		// admin.Health stays public in every posture (transport liveness).
		{"authz on unsealed no cert health", false, true, "admin.Health", nil, false},
		{"authz on sealed no cert health", true, true, "admin.Health", nil, false},

		// Sealed-window allowlist unaffected: allowlisted unseal-flow methods
		// are admitted without a certificate WHILE SEALED (audited bypass).
		{"authz on sealed no cert unseal", true, true, "barrier.Unseal", nil, false},
		{"authz on sealed no cert shamir share", true, true, "barrier.ShamirUnsealShare", nil, false},
		{"authz on sealed no cert ceremony", true, true, "ceremony.Initialize", nil, false},

		// Sealed + NON-allowlisted stays fail-closed (the sealed deny gate
		// rejects these first in dispatch; the predicate is fail-closed too).
		{"authz on sealed no cert credential fail-closed", true, true, "credential.Submit", nil, true},
		{"authz on sealed no cert seal fail-closed", true, true, "barrier.Seal", nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newBridgeAuthTestServer(t, tc.sealed, tc.requireAuthz)
			assert.Equal(t, tc.want, s.qrdbBridgeAuthRequired(tc.method, tc.peerCert))
		})
	}
}

// TestQRDBBridgeAuthRequired_UninstalledGateFailsClosed pins that with
// RequireAuthz=true and NO sealed-window gate installed (nil bypass) the
// predicate still fail-closes for cert-less callers: a nil *AuthBypass never
// bypasses.
func TestQRDBBridgeAuthRequired_UninstalledGateFailsClosed(t *testing.T) {
	s := &Server{}
	s.SetValidatorEnforcement(&fakeValidatorAuthorizer{})
	assert.True(t, s.qrdbBridgeAuthRequired("barrier.Status", nil))
	assert.False(t, s.qrdbBridgeAuthRequired("barrier.Status", &x509.Certificate{}))
}

// TestQRDBBridge_Dispatch_PeerCertEnforcement drives the REAL wire dispatch
// (serveQRDBRequest: read frame -> sealed gate -> cert gate -> handler) and
// pins the owner-approved contract end-to-end.
func TestQRDBBridge_Dispatch_PeerCertEnforcement(t *testing.T) {
	cert := &x509.Certificate{}

	t.Run("sealed allowlisted no cert admitted", func(t *testing.T) {
		s, _ := newBridgeAuthTestServer(t, true, true)
		status, _ := serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusOK, status, "sealed-window bypass must admit the unseal flow")
	})

	t.Run("sealed non-allowlisted no cert stays sealed-denied", func(t *testing.T) {
		// Gate ordering: the sealed-window DENY gate runs BEFORE the
		// certificate requirement, so a non-allowlisted method while sealed
		// is StatusUnavailable, not StatusUnauthenticated.
		s, _ := newBridgeAuthTestServer(t, true, true)
		status, payload := serveBridgeMethod(t, s, "credential.Submit", nil)
		assert.Equal(t, wire.StatusUnavailable, status)
		assert.Equal(t, sealedWindowMessage, string(payload))
	})

	t.Run("unsealed no cert auth required", func(t *testing.T) {
		s, _ := newBridgeAuthTestServer(t, false, true)
		status, payload := serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusUnauthenticated, status)
		assert.Contains(t, string(payload), "authentication required")
		assert.Contains(t, string(payload), "barrier.Status")
	})

	t.Run("unsealed with verified peer cert dispatched", func(t *testing.T) {
		s, _ := newBridgeAuthTestServer(t, false, true)
		status, _ := serveBridgeMethod(t, s, "barrier.Status", cert)
		assert.Equal(t, wire.StatusOK, status, "verified peer cert must dispatch to the handler")
	})

	t.Run("unsealed no cert health stays public", func(t *testing.T) {
		s, _ := newBridgeAuthTestServer(t, false, true)
		status, _ := serveBridgeMethod(t, s, "admin.Health", nil)
		assert.Equal(t, wire.StatusOK, status, "connect-time liveness probe must stay public")
	})

	t.Run("requireauthz off legacy behaviour unchanged", func(t *testing.T) {
		s, _ := newBridgeAuthTestServer(t, false, false)
		status, _ := serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusOK, status, "RequireAuthz=false must not add a cert requirement")
	})

	t.Run("reseal cycle reopens and closes the cert-free window", func(t *testing.T) {
		s, probe := newBridgeAuthTestServer(t, false, true)

		// Unsealed: cert-less barrier.Status is auth-required.
		status, _ := serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusUnauthenticated, status)

		// Runtime re-seal: the allowlisted verb is admitted without a cert.
		probe.sealed = true
		status, _ = serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusOK, status)

		// Unseal completes: enforcement returns immediately.
		probe.sealed = false
		status, _ = serveBridgeMethod(t, s, "barrier.Status", nil)
		assert.Equal(t, wire.StatusUnauthenticated, status)
	})
}
