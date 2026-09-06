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

package xkms_test

import (
	"bytes"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof the peer aliases are the go-xkms types, not lookalikes:
// a value of the upstream type must be assignable to the alias and back.
var (
	_ xkms.PeerAttestationVerifier     = xkmssdk.PeerAttestationVerifier(nil)
	_ xkmssdk.PeerEvidence             = xkms.PeerEvidence{}
	_ xkmssdk.PeerPolicy               = xkms.PeerPolicy{}
	_ xkmssdk.PeerVerdict              = xkms.PeerVerdict{}
	_ xkmssdk.PeerChecks               = xkms.PeerChecks{}
	_ xkmssdk.PeerClockInfo            = xkms.PeerClockInfo{}
	_ xkmssdk.PeerFailureClass         = xkms.PeerFailureNone
	_ *xkmssdk.PeerPolicyError         = (*xkms.PeerPolicyError)(nil)
	_ *xkmssdk.PeerContextError        = (*xkms.PeerContextError)(nil)
	_ *xkmssdk.PeerChannelBindingError = (*xkms.PeerChannelBindingError)(nil)
)

// TestPeerAliases_CarryTheUpstreamValues pins the constants and functions to
// their go-xkms values. The failure classes and the channel-binding label are
// wire-visible: a verifier and an attester disagreeing on either would fail
// every attestation while both still "spoke" the primitive.
func TestPeerAliases_CarryTheUpstreamValues(t *testing.T) {
	if xkms.PeerFailureNone != xkmssdk.PeerFailureNone ||
		xkms.PeerFailureAbsence != xkmssdk.PeerFailureAbsence ||
		xkms.PeerFailureCryptographic != xkmssdk.PeerFailureCryptographic {
		t.Fatal("failure classes differ from upstream")
	}
	if xkms.PeerFailureAbsence == xkms.PeerFailureCryptographic {
		t.Fatal("absence and cryptographic failure must be distinct classes")
	}
	if xkms.PeerChannelBindingExporterLabel != xkmssdk.PeerChannelBindingExporterLabel ||
		xkms.PeerChannelBindingLength != xkmssdk.PeerChannelBindingLength {
		t.Fatal("channel-binding constants differ from upstream")
	}
	if xkms.PeerChannelBindingLength != 32 {
		t.Fatalf("PeerChannelBindingLength = %d, want 32 (SHA-256 exporter output)", xkms.PeerChannelBindingLength)
	}

	nonce := []byte("nonce-0123456789abcdef")
	binding := bytes.Repeat([]byte{0xab}, xkms.PeerChannelBindingLength)
	if got, want := xkms.PeerQualifyingData(nonce, binding), xkmssdk.PeerQualifyingData(nonce, binding); !bytes.Equal(got, want) {
		t.Fatalf("PeerQualifyingData diverged from upstream: %x vs %x", got, want)
	}
	if xkms.NewPeerAttestationVerifier(xkms.WithPeerVerifierClock(nil)) == nil {
		t.Fatal("NewPeerAttestationVerifier returned nil")
	}
}

// TestPeerChannelBinding_RefusesNilState proves the aliased function is the
// real one: a nil TLS state is a typed refusal, never a derived binding.
func TestPeerChannelBinding_RefusesNilState(t *testing.T) {
	if _, err := xkms.PeerChannelBinding(nil); err == nil {
		t.Fatal("PeerChannelBinding(nil) must fail")
	}
}
