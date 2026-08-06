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
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// SealedWindowProbe reports whether the storage barrier is currently sealed.
// It mirrors go-qrdb's sealed-window probe contract (qrdbsdk.SealedWindowProbe
// satisfies it structurally) so one probe instance drives the gates on every
// quicbaas transport.
type SealedWindowProbe interface {
	IsSealed() bool
}

// sealedWindowMessage is the coarse deny payload written while the barrier is
// sealed, mirroring qrdb's native QUIC sealed-window envelope.
const sealedWindowMessage = "barrier sealed"

// SetSealedWindowGate installs the sealed-window gate on both QUIC dispatch
// paths — the native quicbaas-quic opcode dispatcher and the qrdb-quic ALPN
// method bridge. While probe reports sealed, every operation not present in
// the supplied allowlists is rejected with StatusUnavailable so operators can
// still drive the unseal flow (barrier/ceremony verbs + health) and nothing
// else. When the probe reports unsealed the gate is a no-op pass-through.
//
// Both allowlists are derived from the single canonical table in
// pkg/api/sealedwindow (see sealedwindow.QUICAllowlist and
// sealedwindow.QRDBQUICAllowlist) so the two dispatch paths cannot drift.
// Safe to call before Start; a nil probe leaves the gate uninstalled.
func (s *Server) SetSealedWindowGate(
	probe SealedWindowProbe,
	opAllow map[sealedwindow.QUICOp]struct{},
	qrdbAllow map[string]struct{},
) {
	// Derive the shared sealed-window authentication-bypass predicate from
	// the same probe. The per-request handler consults it so that WHILE
	// SEALED the allowlisted native opcodes that pass the sealed gate are
	// admitted WITHOUT a bearer token (audited, reason "sealed-window
	// allowlist") — a sealed IAM plane cannot mint or validate tokens, so
	// the allowlist is the entire control surface. Unsealed, the predicate
	// is inert and validator enforcement is unchanged. The qrdb-quic bridge
	// consults the same predicate for its peer-certificate requirement
	// (qrdbBridgeAuthRequired): while sealed, allowlisted bridge methods
	// are admitted without a verified client certificate.
	var bypass *sealedwindow.AuthBypass
	if probe != nil {
		bypass = sealedwindow.NewAuthBypass(probe, sealedwindow.NewLoggerAuditSink(s.logger))
	}

	s.svcMu.Lock()
	s.sealedProbe = probe
	s.sealedOpAllow = opAllow
	s.sealedQRDBAllow = qrdbAllow
	s.sealedBypass = bypass
	s.svcMu.Unlock()
}

// sealedWindowDeniesOp reports whether the sealed-window gate denies the
// given native quicbaas-quic request (opcode + raw payload). The sub-op byte
// is payload[0] for the sub-dispatched opcodes (OpBarrier / OpCeremony);
// every other opcode is matched on the opcode alone (SubOp zero — real
// sub-op codes start at 0x01, so zero is unambiguous).
func (s *Server) sealedWindowDeniesOp(opcode byte, payload []byte) bool {
	s.svcMu.RLock()
	probe := s.sealedProbe
	allow := s.sealedOpAllow
	s.svcMu.RUnlock()

	if probe == nil || !probe.IsSealed() {
		return false
	}

	key := sealedwindow.QUICOp{Opcode: opcode}
	if (opcode == OpBarrier || opcode == OpCeremony) && len(payload) > 0 {
		key.SubOp = payload[0]
	}
	_, ok := allow[key]
	return !ok
}

// sealedWindowDeniesQRDBMethod reports whether the sealed-window gate denies
// the given qrdb-quic ALPN bridge method while the barrier is sealed.
func (s *Server) sealedWindowDeniesQRDBMethod(method string) bool {
	s.svcMu.RLock()
	probe := s.sealedProbe
	allow := s.sealedQRDBAllow
	s.svcMu.RUnlock()

	if probe == nil || !probe.IsSealed() {
		return false
	}
	_, ok := allow[method]
	return !ok
}
