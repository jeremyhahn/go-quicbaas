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

package server

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// serverSealedProbe is the single sealed-window probe that drives the
// sealed-window gates on all five quicbaas transports (REST, gRPC, Unix,
// QUIC, MCP). One probe instance means the transports can never disagree
// about the sealed state.
//
// Seal-enabled boots (embedded qrdb node owns the barrier): the probe is
// the node's IsSealed — true from construction until Unseal completes the
// boot, and true again if the barrier is sealed post-boot.
//
// Seal-disabled boots (cfg.Seal.Enabled=false): the node carries no barrier,
// so the probe follows the persistent data-plane barrier built by
// buildDataPlaneBarrierService (the same strategy-aware barrier that backs
// the `barrier` CLI verbs). An INITIALIZED barrier that is sealed gates the
// surface; an uninitialized barrier (fresh data dir, nothing protected yet)
// reports unsealed so the bootstrap-window behaviour is unchanged.
type serverSealedProbe struct {
	server *Server
}

// IsSealed implements qrdbsdk.SealedWindowProbe (and, structurally, the
// per-transport probe contracts in pkg/api/{rest,quic,mcp}).
func (p *serverSealedProbe) IsSealed() bool {
	s := p.server

	// Node-owned barrier (seal-enabled boots). IsSealed is false when the
	// node has no barrier (seal disabled), so this never gates the
	// seal-disabled path.
	if node := s.qrdbNode; node != nil && node.IsSealed() {
		return true
	}

	// Data-plane barrier (seal-disabled boots with a persistent DataDir).
	// Only an initialized barrier can meaningfully be sealed: a fresh,
	// never-initialized barrier reports state "sealed" internally but
	// protects nothing, and gating it would lock every first boot out of
	// the full surface.
	if barrier := s.dataPlaneBarrier; barrier != nil {
		if st := barrier.Status(); st != nil && st.Sealed && !st.InitializedAt.IsZero() {
			return true
		}
	}

	return false
}

// Compile-time check that the probe satisfies the SDK contract used by the
// REST middleware and the gRPC interceptors.
var _ qrdbsdk.SealedWindowProbe = (*serverSealedProbe)(nil)

// SealedWindowProbe returns the server's canonical sealed-window probe. The
// same instance is installed on every transport gate; it is cheap (two
// atomic loads) and safe to call at any lifecycle phase.
func (s *Server) SealedWindowProbe() qrdbsdk.SealedWindowProbe {
	return &serverSealedProbe{server: s}
}
