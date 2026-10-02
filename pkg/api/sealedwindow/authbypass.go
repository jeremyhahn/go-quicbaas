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

package sealedwindow

import (
	"fmt"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
)

// Probe reports whether the storage barrier is currently sealed. It mirrors
// go-qrdb's sealed-window probe contract (qrdbsdk.SealedWindowProbe and the
// per-transport probe interfaces satisfy it structurally), so the single
// probe instance lifecycle installs on every transport gate also drives the
// authentication bypass below.
type Probe interface {
	IsSealed() bool
}

// OperationalProbe reports whether the deployment has left the initialization
// ceremony's pre-operational phase. It is the second axis of the strict
// ceremony model: verbs marked Names.PreOperationalOnly keep their
// authentication exemption only while this probe reports NOT operational (see
// the Scope constants and Table).
//
// FAIL CLOSED IS THE CONTRACT. An implementation MUST return true whenever it
// cannot POSITIVELY establish that the ceremony is still unfinished — an
// absent dependency, a status read that errors, and an unrecognized ceremony
// state all report operational. AuthBypass applies the same rule to the probe
// itself: a nil OperationalProbe is treated as operational. An unavailable
// probe therefore NARROWS the unauthenticated surface; it can never widen it.
//
// The single-method shape mirrors Probe so the wiring that installs the
// sealed-window allowlists installs both probes together, in one place, per
// transport.
type OperationalProbe interface {
	IsOperational() bool
}

// AuditSink receives one event per sealed-window authentication bypass. The
// bypass is a privileged admit (an unauthenticated request reaches a
// handler), so every admit leaves an audit trail carrying the transport and
// the operation that was admitted, with the fixed reason
// "sealed-window allowlist" — mirroring the audited ceremony bypass on
// go-xkms's QUIC transport. Emission is best-effort: a sink must never
// block or fail the request.
type AuditSink interface {
	SealedWindowAuthBypass(transport, operation string)
}

// restPathSet is one scope's REST allowlist, matched exactly as go-qrdb's
// REST sealed-window middleware matches it: an entry is "/path" (any method)
// or "METHOD /path", and a path ending in "/" is a prefix (go-qrdb's
// sealedwindow.RESTEntryMatches, which composers must use since go-qrdb v0.25
// made entries method-qualified).
type restPathSet struct {
	entries []string
}

// newRESTPathSet holds a derived REST allowlist.
func newRESTPathSet(entries []string) restPathSet {
	return restPathSet{entries: entries}
}

// matches reports whether a request with method and path is admitted.
func (s restPathSet) matches(method, path string) bool {
	for _, entry := range s.entries {
		if qrdbsealedwindow.RESTEntryMatches(entry, method, path) {
			return true
		}
	}
	return false
}

// AuthBypass is the single sealed-state-aware authentication-bypass
// predicate shared by every quicbaas transport authorization gate (REST,
// gRPC, Unix-over-gRPC, native QUIC, qrdb-quic bridge, MCP).
//
// Contract (owner-approved): WHILE the barrier is SEALED, the sealed-window
// allowlist IS the entire control surface — possession of unseal material is
// the authorization, because a sealed IAM plane cannot mint or validate
// tokens. A request that is (a) on the canonical sealed-window allowlist and
// (b) arriving while the probe reports sealed therefore bypasses
// authentication and authorization entirely (audited); everything else while
// sealed is denied by the sealed-window gate itself, and once the barrier is
// unsealed every predicate returns false so full enforcement applies with
// zero behavior change.
//
// The allowlist is split across the two exemption scopes of the strict
// ceremony model, and each request is matched against the scopes that
// currently apply:
//
//   - ScopeAlways — the barrier / unseal / recovery surface. Exempt in EVERY
//     ceremony state. A daemon restarted after a completed ceremony is
//     operational AND sealed, and these verbs are the only way back to a
//     usable data plane, so they must never depend on the ceremony state.
//   - ScopePreOperational — the self-authorizing ceremony claim handshakes
//     and the read-only ceremony status probe. Exempt only while the
//     OperationalProbe reports NOT operational. Serving the claim endpoints
//     unauthenticated on an already-initialized daemon is a live
//     vulnerability, so that exemption expires with the ceremony.
//
// The two scopes together are exactly ScopeAll (see InScope), so a
// pre-operational daemon has the same exempt surface it always had while an
// operational one loses only the ceremony claim/status exemption.
//
// All per-transport lookup sets are derived once, at construction, from the
// SAME canonical table (Table / RESTExtra) that feeds the sealed-window deny
// gates, so the bypass surface can never drift from the sealed surface.
// The zero of the type is unusable; a nil *AuthBypass is safe and never
// bypasses, so gates may consult an uninstalled predicate unconditionally.
type AuthBypass struct {
	probe       Probe
	operational OperationalProbe
	sink        AuditSink

	// grpcAlways / grpcPreOp hold the fully-qualified gRPC method paths
	// ("/qrdb.v1.QRDBService/BarrierUnseal"), matched in full against
	// FullMethod exactly as the qrdb sealed-window interceptor does.
	grpcAlways map[string]struct{}
	grpcPreOp  map[string]struct{}

	// restAlways / restPreOp are the REST path allowlists per scope.
	restAlways restPathSet
	restPreOp  restPathSet

	// nativeAlways / nativePreOp are the native quicbaas-quic (opcode,
	// sub-op) allowlists per scope.
	nativeAlways map[QUICOp]struct{}
	nativePreOp  map[QUICOp]struct{}

	// qrdbQUICAlways / qrdbQUICPreOp are the qrdb-quic ALPN bridge method
	// allowlists per scope.
	qrdbQUICAlways map[string]struct{}
	qrdbQUICPreOp  map[string]struct{}

	// mcpAlways / mcpPreOp are the MCP tool-name allowlists per scope.
	mcpAlways map[string]struct{}
	mcpPreOp  map[string]struct{}
}

// NewAuthBypass derives the shared sealed-window authentication-bypass
// predicate from the canonical derivation table.
//
// probe is the single sealed-window probe lifecycle installs on every
// transport; a nil probe yields a predicate that never bypasses.
//
// operational is the ceremony-state probe that decides whether the
// ScopePreOperational exemptions still apply. It FAILS CLOSED: a nil probe
// (or one reporting operational) means the pre-operational verbs get no
// exemption at all, so an unwired or unavailable probe narrows the
// unauthenticated surface to the state-independent ScopeAlways set instead of
// widening it.
//
// sink receives one audit event per bypass and may be nil (no auditing —
// tests only; production wiring always supplies a sink).
func NewAuthBypass(probe Probe, operational OperationalProbe, sink AuditSink) *AuthBypass {
	b := &AuthBypass{
		probe:          probe,
		operational:    operational,
		sink:           sink,
		grpcAlways:     make(map[string]struct{}),
		grpcPreOp:      make(map[string]struct{}),
		restAlways:     newRESTPathSet(RESTAllowlistScoped(ScopeAlways)),
		restPreOp:      newRESTPathSet(RESTAllowlistScoped(ScopePreOperational)),
		nativeAlways:   QUICAllowlistScoped(ScopeAlways),
		nativePreOp:    QUICAllowlistScoped(ScopePreOperational),
		qrdbQUICAlways: QRDBQUICAllowlistScoped(ScopeAlways),
		qrdbQUICPreOp:  QRDBQUICAllowlistScoped(ScopePreOperational),
		mcpAlways:      MCPAllowlistScoped(ScopeAlways),
		mcpPreOp:       MCPAllowlistScoped(ScopePreOperational),
	}
	for _, method := range GRPCFullMethodAllowlistScoped(ScopeAlways) {
		b.grpcAlways[method] = struct{}{}
	}
	for _, method := range GRPCFullMethodAllowlistScoped(ScopePreOperational) {
		b.grpcPreOp[method] = struct{}{}
	}
	return b
}

// sealed reports whether the bypass is armed: an installed probe currently
// reporting sealed. Nil-safe on both the receiver and the probe.
func (b *AuthBypass) sealed() bool {
	return b != nil && b.probe != nil && b.probe.IsSealed()
}

// preOperational reports whether the ScopePreOperational exemptions still
// apply.
//
// FAIL CLOSED: an absent operational probe is treated as OPERATIONAL, so the
// pre-operational exemption is withheld. An unavailable probe must never
// widen the unauthenticated surface — the ceremony claim handshakes stay
// authenticated whenever the ceremony state cannot be positively established
// as unfinished (the probe implementation applies the same rule to its own
// dependencies; see OperationalProbe).
func (b *AuthBypass) preOperational() bool {
	if b.operational == nil {
		return false
	}
	return !b.operational.IsOperational()
}

// audit emits the bypass audit event. Best-effort; a nil sink is a no-op.
func (b *AuthBypass) audit(transport, operation string) {
	if b.sink != nil {
		b.sink.SealedWindowAuthBypass(transport, operation)
	}
}

// admits is the shared scope decision for the set-based transports: an entry
// in the always set is exempt in every ceremony state, an entry in the
// pre-operational set only while the ceremony is unfinished.
func admits[K comparable](b *AuthBypass, always, preOp map[K]struct{}, key K) bool {
	if _, ok := always[key]; ok {
		return true
	}
	if _, ok := preOp[key]; !ok {
		return false
	}
	return b.preOperational()
}

// BypassesGRPC reports whether the gRPC (or Unix-over-gRPC, which shares the
// interceptor chain) request identified by fullMethod bypasses
// authentication because the barrier is sealed and the method is exempt in
// the daemon's current ceremony state.
//
// Matching is on the FULL service-qualified path, byte-for-byte identical to
// the sealed-window interceptor's own matching, so the bypass surface can
// never be wider than the sealed surface: a same-named method on another
// co-registered service (e.g. "/quicbaas.tenant.v1.TenantBarrier/Status") is
// not admitted. Emits the audit event on every bypass.
func (b *AuthBypass) BypassesGRPC(fullMethod string) bool {
	if !b.sealed() {
		return false
	}
	if !admits(b, b.grpcAlways, b.grpcPreOp, fullMethod) {
		return false
	}
	b.audit("grpc", fullMethod)
	return true
}

// BypassesRESTPath reports whether the REST request (method and path)
// bypasses authentication because the barrier is sealed and the request is
// exempt in the daemon's current ceremony state. Matching is go-qrdb's
// (sealedwindow.RESTEntryMatches): an entry admits every method unless it
// names one, and an entry ending in "/" is a prefix. Emits the audit event on
// every bypass.
func (b *AuthBypass) BypassesRESTPath(method, path string) bool {
	if !b.sealed() {
		return false
	}
	exempt := b.restAlways.matches(method, path) ||
		(b.restPreOp.matches(method, path) && b.preOperational())
	if !exempt {
		return false
	}
	b.audit("rest", path)
	return true
}

// BypassesNativeQUIC reports whether the native quicbaas-quic request
// (opcode + raw payload) bypasses authentication because the barrier is
// sealed and the operation is exempt in the daemon's current ceremony state.
// The sub-op byte is payload[0] for the sub-dispatched opcodes (OpBarrier /
// OpCeremony); every other opcode matches on the opcode alone (SubOp zero —
// real sub-op codes start at 0x01, so zero is unambiguous). Emits the audit
// event on every bypass.
func (b *AuthBypass) BypassesNativeQUIC(opcode byte, payload []byte) bool {
	if !b.sealed() {
		return false
	}
	key := QUICOp{Opcode: opcode}
	if (opcode == opBarrier || opcode == opCeremony) && len(payload) > 0 {
		key.SubOp = payload[0]
	}
	if !admits(b, b.nativeAlways, b.nativePreOp, key) {
		return false
	}
	b.audit("quic", fmt.Sprintf("opcode 0x%02X sub-op 0x%02X", key.Opcode, key.SubOp))
	return true
}

// BypassesQRDBQUIC reports whether the qrdb-quic ALPN bridge method bypasses
// authentication because the barrier is sealed and the method is exempt in
// the daemon's current ceremony state. Emits the audit event on every bypass.
func (b *AuthBypass) BypassesQRDBQUIC(method string) bool {
	if !b.sealed() {
		return false
	}
	if !admits(b, b.qrdbQUICAlways, b.qrdbQUICPreOp, method) {
		return false
	}
	b.audit("qrdb-quic", method)
	return true
}

// BypassesMCPTool reports whether the MCP tools/call for the named tool
// bypasses authentication because the barrier is sealed and the tool is
// exempt in the daemon's current ceremony state. Emits the audit event on
// every bypass.
func (b *AuthBypass) BypassesMCPTool(name string) bool {
	if !b.sealed() {
		return false
	}
	if !admits(b, b.mcpAlways, b.mcpPreOp, name) {
		return false
	}
	b.audit("mcp", name)
	return true
}
