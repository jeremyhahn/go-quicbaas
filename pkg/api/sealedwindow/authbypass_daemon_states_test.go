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
	"testing"

	"github.com/stretchr/testify/assert"
)

// The three real daemon states, named the way an operator would describe them.
// The per-transport tables elsewhere in this package assert the (sealed x
// ceremony) matrix entry by entry; this test asserts the LIFECYCLE claim built
// on top of it, which is the thing a reader actually needs to trust:
//
//  1. A virgin node can bootstrap its barrier with no credentials.
//  2. A restarted, already-initialized node that is SEALED can unseal, and its
//     unauthenticated Initialize call is harmless because the barrier's
//     compare-and-set rejects re-initialization (qrdb StorageBarrier.Initialize
//     returns ErrAlreadyInitialized) - the exemption cannot overwrite a trust
//     root.
//  3. An UNSEALED node - which is what a cloud/hardware-strategy daemon boots
//     into, because the node auto-initializes and auto-unseals at startup -
//     exempts NOTHING. An unauthenticated barrier call against such a daemon is
//     correctly rejected; that denial is enforcement working, not a deadlock.
const (
	grpcBarrierInitialize = "/qrdb.v1.QRDBService/BarrierInitialize"
	grpcBarrierUnseal     = "/qrdb.v1.QRDBService/BarrierUnseal"
	grpcCeremonyStatus    = "/qrdb.v1.QRDBService/CeremonyStatus"
)

// TestAuthBypass_VirginNodeCanBootstrap pins state 1: a never-initialized
// seal-enabled node reports sealed (a freshly constructed qrdb StorageBarrier
// starts in the sealed state) and pre-operational, so the whole unseal-flow
// surface plus the ceremony handshakes are reachable without a token. Without
// this the node could never be bootstrapped: a sealed IAM plane cannot mint or
// validate the token its own bootstrap would require.
func TestAuthBypass_VirginNodeCanBootstrap(t *testing.T) {
	bypass, _, _, _ := newBypassFixture(true, statePreOperational)

	assert.True(t, bypass.BypassesGRPC(grpcBarrierInitialize),
		"a virgin node must be able to initialize its barrier unauthenticated")
	assert.True(t, bypass.BypassesGRPC(grpcBarrierUnseal),
		"a virgin node must be able to unseal unauthenticated")
	assert.True(t, bypass.BypassesGRPC(grpcCeremonyStatus),
		"the ceremony status probe is exempt while the ceremony is unfinished")
}

// TestAuthBypass_InitializedSealedNodeKeepsOnlyTheUnsealFlow pins state 2: a
// daemon restarted after a completed ceremony is sealed AND operational. The
// unseal flow survives (it is the only way back to a usable data plane), but
// the ceremony claim/status exemption has expired - serving the
// self-authorizing claim handshakes unauthenticated on an already-initialized
// daemon is a live vulnerability.
func TestAuthBypass_InitializedSealedNodeKeepsOnlyTheUnsealFlow(t *testing.T) {
	bypass, _, _, _ := newBypassFixture(true, stateOperational)

	assert.True(t, bypass.BypassesGRPC(grpcBarrierUnseal),
		"a restarted sealed node must still be able to unseal")
	assert.False(t, bypass.BypassesGRPC(grpcCeremonyStatus),
		"the pre-operational ceremony exemption must expire with the ceremony")

	// Initialize stays exempt by design: the daemon cannot distinguish a
	// virgin barrier from an initialized one without reading the very barrier
	// the caller is trying to reach, so the durable guard is the barrier
	// itself - qrdb's StorageBarrier.Initialize refuses an already-initialized
	// store with a compare-and-set, so this exemption cannot destroy a trust
	// root. If that guard is ever relaxed, move VerbBarrierInitialize to
	// PreOperationalOnly in Table rather than relying on the service layer.
	assert.True(t, bypass.BypassesGRPC(grpcBarrierInitialize),
		"Initialize is ScopeAlways; re-init is refused by the barrier's CAS")
}

// TestAuthBypass_UnsealedNodeExemptsNothing pins state 3, the state the
// cloud-seal integration daemons actually boot into. Every barrier verb
// requires full authentication; there is no sealed window to hide in.
func TestAuthBypass_UnsealedNodeExemptsNothing(t *testing.T) {
	for _, ceremony := range []state{statePreOperational, stateOperational} {
		t.Run(ceremony.String(), func(t *testing.T) {
			bypass, _, _, sink := newBypassFixture(false, ceremony)

			assert.False(t, bypass.BypassesGRPC(grpcBarrierInitialize))
			assert.False(t, bypass.BypassesGRPC(grpcBarrierUnseal))
			assert.False(t, bypass.BypassesGRPC(grpcCeremonyStatus))
			assert.False(t, bypass.BypassesRESTPath("/api/v1/barrier/status"))
			assert.Empty(t, sink.events, "an unsealed daemon must never audit a bypass")
		})
	}
}

// TestAuthBypass_UnsealingClosesTheWindowImmediately pins the transition: the
// predicate reads the live probe, so completing an unseal revokes every
// exemption on the SAME instance, with no restart and no re-derivation.
func TestAuthBypass_UnsealingClosesTheWindowImmediately(t *testing.T) {
	bypass, probe, _, _ := newBypassFixture(true, statePreOperational)
	assert.True(t, bypass.BypassesGRPC(grpcBarrierInitialize))

	probe.sealedState = false

	assert.False(t, bypass.BypassesGRPC(grpcBarrierInitialize),
		"unsealing must revoke the exemption on the live predicate")
	assert.False(t, bypass.BypassesGRPC(grpcBarrierUnseal))
}
