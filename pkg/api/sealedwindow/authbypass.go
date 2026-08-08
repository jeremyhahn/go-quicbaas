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
	"strings"
)

// Probe reports whether the storage barrier is currently sealed. It mirrors
// go-qrdb's sealed-window probe contract (qrdbsdk.SealedWindowProbe and the
// per-transport probe interfaces satisfy it structurally), so the single
// probe instance lifecycle installs on every transport gate also drives the
// authentication bypass below.
type Probe interface {
	IsSealed() bool
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
// All per-transport lookup sets are derived once, at construction, from the
// SAME canonical table (Table / RESTExtra) that feeds the sealed-window deny
// gates, so the bypass surface can never drift from the sealed surface.
// The zero of the type is unusable; a nil *AuthBypass is safe and never
// bypasses, so gates may consult an uninstalled predicate unconditionally.
type AuthBypass struct {
	probe Probe
	sink  AuditSink

	// grpcMethods holds the fully-qualified gRPC method paths
	// ("/qrdb.v1.QRDBService/BarrierUnseal"), matched in full against
	// FullMethod exactly as the qrdb sealed-window interceptor does.
	grpcMethods map[string]struct{}

	// restExact / restPrefixes mirror qrdb's REST sealed-window path
	// matching: entries ending in "/" are prefix matches, everything else
	// matches exactly.
	restExact    map[string]struct{}
	restPrefixes []string

	// nativeOps is the native quicbaas-quic (opcode, sub-op) allowlist.
	nativeOps map[QUICOp]struct{}

	// qrdbQUICMethods is the qrdb-quic ALPN bridge method allowlist.
	qrdbQUICMethods map[string]struct{}

	// mcpTools is the MCP tool-name allowlist.
	mcpTools map[string]struct{}
}

// NewAuthBypass derives the shared sealed-window authentication-bypass
// predicate from the canonical derivation table. probe is the single
// sealed-window probe lifecycle installs on every transport; a nil probe
// yields a predicate that never bypasses. sink receives one audit event per
// bypass and may be nil (no auditing — tests only; production wiring always
// supplies a sink).
func NewAuthBypass(probe Probe, sink AuditSink) *AuthBypass {
	b := &AuthBypass{
		probe:           probe,
		sink:            sink,
		grpcMethods:     make(map[string]struct{}),
		restExact:       make(map[string]struct{}),
		nativeOps:       QUICAllowlist(),
		qrdbQUICMethods: QRDBQUICAllowlist(),
		mcpTools:        MCPAllowlist(),
	}
	for _, method := range GRPCFullMethodAllowlist() {
		b.grpcMethods[method] = struct{}{}
	}
	for _, path := range RESTAllowlist() {
		if strings.HasSuffix(path, "/") {
			b.restPrefixes = append(b.restPrefixes, path)
			continue
		}
		b.restExact[path] = struct{}{}
	}
	return b
}

// sealed reports whether the bypass is armed: an installed probe currently
// reporting sealed. Nil-safe on both the receiver and the probe.
func (b *AuthBypass) sealed() bool {
	return b != nil && b.probe != nil && b.probe.IsSealed()
}

// audit emits the bypass audit event. Best-effort; a nil sink is a no-op.
func (b *AuthBypass) audit(transport, operation string) {
	if b.sink != nil {
		b.sink.SealedWindowAuthBypass(transport, operation)
	}
}

// BypassesGRPC reports whether the gRPC (or Unix-over-gRPC, which shares the
// interceptor chain) request identified by fullMethod bypasses
// authentication because the barrier is sealed and the method is on the
// sealed-window allowlist.
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
	if _, ok := b.grpcMethods[fullMethod]; !ok {
		return false
	}
	b.audit("grpc", fullMethod)
	return true
}

// BypassesRESTPath reports whether the REST request path bypasses
// authentication because the barrier is sealed and the path is on the
// sealed-window allowlist. Path matching mirrors qrdb's REST sealed-window
// middleware exactly: allowlist entries ending in "/" are prefix matches,
// everything else matches exactly. Emits the audit event on every bypass.
func (b *AuthBypass) BypassesRESTPath(path string) bool {
	if !b.sealed() {
		return false
	}
	if _, ok := b.restExact[path]; ok {
		b.audit("rest", path)
		return true
	}
	for _, prefix := range b.restPrefixes {
		if strings.HasPrefix(path, prefix) {
			b.audit("rest", path)
			return true
		}
	}
	return false
}

// BypassesNativeQUIC reports whether the native quicbaas-quic request
// (opcode + raw payload) bypasses authentication because the barrier is
// sealed and the operation is on the sealed-window allowlist. The sub-op
// byte is payload[0] for the sub-dispatched opcodes (OpBarrier /
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
	if _, ok := b.nativeOps[key]; !ok {
		return false
	}
	b.audit("quic", fmt.Sprintf("opcode 0x%02X sub-op 0x%02X", key.Opcode, key.SubOp))
	return true
}

// BypassesQRDBQUIC reports whether the qrdb-quic ALPN bridge method bypasses
// authentication because the barrier is sealed and the method is on the
// sealed-window allowlist. Emits the audit event on every bypass.
func (b *AuthBypass) BypassesQRDBQUIC(method string) bool {
	if !b.sealed() {
		return false
	}
	if _, ok := b.qrdbQUICMethods[method]; !ok {
		return false
	}
	b.audit("qrdb-quic", method)
	return true
}

// BypassesMCPTool reports whether the MCP tools/call for the named tool
// bypasses authentication because the barrier is sealed and the tool is on
// the sealed-window allowlist. Emits the audit event on every bypass.
func (b *AuthBypass) BypassesMCPTool(name string) bool {
	if !b.sealed() {
		return false
	}
	if _, ok := b.mcpTools[name]; !ok {
		return false
	}
	b.audit("mcp", name)
	return true
}
