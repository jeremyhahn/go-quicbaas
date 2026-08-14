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
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// serverOperationalProbe is the single sealed-window ceremony-state probe that
// drives the ScopePreOperational half of the authentication bypass on all five
// quicbaas transports. One derivation means the transports can never disagree
// about whether the init ceremony is still unfinished.
//
// It answers exactly one question: has this deployment left the initialization
// ceremony's pre-operational phase? Verbs marked PreOperationalOnly in the
// canonical table (the self-authorizing claim handshakes and the read-only
// ceremony status probe) keep their authentication exemption only while the
// answer is no.
//
// FAIL CLOSED. IsOperational reports true — operational, no exemption —
// unless the state can be POSITIVELY established as still-unfinished. An
// absent ceremony service, a status read that errors, and an unrecognized
// state all report operational, so an unavailable probe can only ever NARROW
// the unauthenticated surface.
//
// Two signals decide it, because neither is sufficient alone:
//
//   - The live in-process ceremony state. qrdb's ceremony service holds it in
//     memory and starts every process at "awaiting_init", so on its own it
//     would re-open the pre-operational window on EVERY restart of an
//     already-initialized daemon — precisely the exposure this probe closes.
//   - The barrier's persisted initialization timestamp, which qrdb recovers
//     from the sealed barrier metadata on a warm boot. It is the durable
//     record that a trust root already exists for this deployment.
//
// Combined:
//
//	enrolling      -> pre-operational. Only Initialize sets this state, and
//	                  it can only be reached in-process (`quicbaasd init`) or
//	                  by an authenticated, authorized caller on an unsealed
//	                  daemon, because the strict model withholds
//	                  ceremony-initialize from the sealed window on every
//	                  transport. The officer share enclaves that make the
//	                  claim handshakes meaningful live in the SAME process, so
//	                  this state is exactly the window in which they are
//	                  claimable.
//	awaiting_init  -> pre-operational ONLY while the barrier has never been
//	                  initialized (a genuine first boot with no trust root to
//	                  steal). An initialized barrier means the ceremony
//	                  already ran and this process merely restarted: report
//	                  operational.
//	operational    -> operational.
//	anything else  -> operational (fail closed).
//
// The dependencies are snapshotted at construction, on the boot goroutine
// that installs the transport gates, so the request path reads immutable
// fields — no lock and no read of concurrently-mutated Server state.
type serverOperationalProbe struct {
	// ceremony reports the live init-ceremony state. Nil means the state
	// cannot be determined; the probe reports operational.
	ceremony ceremonyStateReader

	// barrier reports the persisted initialization timestamp. Nil means
	// initialization cannot be ruled out; the probe treats the barrier as
	// initialized.
	barrier barrierStatusReader
}

// ceremonyStateReader is the one-method view of the ceremony service the probe
// needs. Satisfied by qrdbsdk.ServiceCeremonyServicer.
type ceremonyStateReader interface {
	CeremonyStatus(ctx context.Context) (*qrdbsdk.CeremonyStatusInfo, error)
}

// barrierStatusReader is the one-method view of the barrier service the probe
// needs. Satisfied by BaasBarrierService (qrdbsdk.BarrierServicer).
type barrierStatusReader interface {
	BarrierStatus(ctx context.Context) (*qrdbsdk.BarrierStatusInfo, error)
}

// IsOperational implements sealedwindow.OperationalProbe.
func (p *serverOperationalProbe) IsOperational() bool {
	if p == nil || p.ceremony == nil {
		return true
	}

	info, err := p.ceremony.CeremonyStatus(context.Background())
	if err != nil || info == nil {
		return true
	}

	switch qrdbsdk.CeremonyState(info.State) {
	case qrdbsdk.StateEnrolling:
		return false
	case qrdbsdk.StateAwaitingInit:
		return p.barrierInitialized()
	case qrdbsdk.StateOperational:
		return true
	}
	// Unrecognized state: fail closed.
	return true
}

// barrierInitialized reports whether the barrier already carries a persisted
// initialization timestamp. A missing barrier servicer or a failed status read
// reports true (initialized) so the caller falls through to operational
// semantics rather than granting an exemption it cannot justify.
func (p *serverOperationalProbe) barrierInitialized() bool {
	if p.barrier == nil {
		return true
	}
	status, err := p.barrier.BarrierStatus(context.Background())
	if err != nil || status == nil {
		return true
	}
	return !status.InitializedAt.IsZero()
}

// Compile-time check that the probe satisfies the sealed-window contract the
// authentication bypass consumes.
var _ sealedwindow.OperationalProbe = (*serverOperationalProbe)(nil)

// OperationalProbe returns the server's canonical sealed-window ceremony-state
// probe. It is installed alongside SealedWindowProbe on every transport gate
// so the pre-operational exemption expires identically on REST, gRPC, Unix,
// QUIC and MCP.
//
// It snapshots the ceremony and barrier servicers wired into s.ceremonyServices
// (populated before any transport starts, by both the sealed-window boot phase
// and the full boot). When they are absent the returned probe reports
// operational, withholding the pre-operational exemption.
func (s *Server) OperationalProbe() sealedwindow.OperationalProbe {
	probe := &serverOperationalProbe{}
	if s == nil || s.ceremonyServices == nil {
		return probe
	}
	if ceremony := s.ceremonyServices.Ceremony; ceremony != nil {
		probe.ceremony = ceremony
	}
	if barrier := s.ceremonyServices.Barrier; barrier != nil {
		probe.barrier = barrier
	}
	return probe
}
