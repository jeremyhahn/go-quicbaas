//go:build mcp

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

package mcp

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

// SealedWindowError is returned by the tools/call dispatcher when the storage
// barrier is sealed and the requested tool is not on the sealed-window
// allowlist. It maps to a JSON-RPC ServiceUnavailable-class error so MCP
// clients can distinguish the sealed state from a permission denial.
type SealedWindowError struct {
	Tool string
}

// Error implements the error interface. The message is deliberately coarse
// (mirroring qrdb's sealed envelope) so a caller cannot probe internal state.
func (e *SealedWindowError) Error() string {
	return "barrier sealed: tool " + e.Tool + " is not available until the barrier is unsealed"
}

// SetSealedWindowGate installs the sealed-window gate on the MCP tool
// dispatcher. While probe reports sealed, every tools/call whose tool name is
// not present in allowlist is rejected before authorization and execution.
// Protocol-level methods (initialize, tools/list, ping, resources) and the
// HTTP /health endpoint remain reachable so clients can discover the sealed
// surface. When the probe reports unsealed the gate is a no-op pass-through,
// so it stays installed for the daemon's whole lifetime — a post-boot barrier
// seal immediately re-narrows the tool surface.
//
// The allowlist is derived from the single canonical table in
// pkg/api/sealedwindow (sealedwindow.MCPAllowlist) so the MCP surface cannot
// drift from the REST / gRPC / QUIC gates. A nil probe leaves the gate
// uninstalled.
func (s *Server) SetSealedWindowGate(probe SealedWindowProbe, allowlist map[string]struct{}) {
	// Derive the shared sealed-window authentication-bypass predicate from
	// the same probe. tools/call consults it so that WHILE SEALED the
	// allowlisted tools that pass the gate are admitted WITHOUT a bearer
	// token (audited, reason "sealed-window allowlist") — a sealed IAM
	// plane cannot mint or validate tokens, so the allowlist is the entire
	// control surface. Unsealed, the predicate is inert and tool
	// authorization is unchanged.
	var bypass *sealedwindow.AuthBypass
	if probe != nil {
		bypass = sealedwindow.NewAuthBypass(probe, sealedwindow.NewLoggerAuditSink(s.logger))
	}

	s.sealedMu.Lock()
	s.sealedProbe = probe
	s.sealedToolAllow = allowlist
	s.sealedBypass = bypass
	s.sealedMu.Unlock()
}

// sealedWindowDeniesTool reports whether the sealed-window gate denies the
// given tool name while the barrier is sealed.
func (s *Server) sealedWindowDeniesTool(name string) bool {
	s.sealedMu.RLock()
	probe := s.sealedProbe
	allow := s.sealedToolAllow
	s.sealedMu.RUnlock()

	if probe == nil || !probe.IsSealed() {
		return false
	}
	_, ok := allow[name]
	return !ok
}

// sealedWindowBypassesTool reports whether the tools/call for the named tool
// bypasses authentication because the barrier is sealed and the tool is on
// the sealed-window allowlist (the shared predicate audits every admit with
// reason "sealed-window allowlist"). Unsealed, always false.
func (s *Server) sealedWindowBypassesTool(name string) bool {
	s.sealedMu.RLock()
	bypass := s.sealedBypass
	s.sealedMu.RUnlock()

	return bypass.BypassesMCPTool(name)
}
